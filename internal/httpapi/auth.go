package httpapi

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ruoxizhnya/tellurion-kit/auth"
	kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"
)

// 鉴权闸的失败分类。实现只负责把 kit 的判定映射到这两个哨兵。
var (
	ErrUnauthenticated = errors.New("no valid credentials")
	ErrForbidden       = errors.New("insufficient role")
)

// Authorizer 是受保护路由的鉴权闸（policy.go 的八条写端点）。
//
// 放行时返回调用方身份（限流键）；`kitAuthorizer` 在无 secret 且未显式声明
// 开放访问时**不放行**，返回 errAuthUnavailable ⇒ 503。身份只用于限流分桶，
// 不参与鉴权判定——鉴权结论仍由 Authorize 的 error 表达。
//
// 三种姿态：有 secret ⇒ kit 验签；无 secret + 未声明 ⇒ 503；无 secret + 显式
// 声明 `AUTH_INSECURE_EXPOSURE=loopback-published` ⇒ `NewOpenAccessAuthorizer`，
// 受保护路由走业务（只由 cmd/assetservice 在启动闸放行后装配）。
type Authorizer interface {
	Authorize(r *http.Request) (principal string, err error)
}

// WithAuthorizer 装配鉴权闸。未装配时受保护路由一律 503（fail-closed），
// 不用「没配就等于放行」把闸门变成装饰。
func WithAuthorizer(a Authorizer) Option {
	return func(s *API) { s.authorizer = a }
}

// NewAuthorizer 用 kit 的 net/http 适配构造鉴权闸：token 解析 / kind=access /
// 角色 any-of 都在 kit，本仓只提供策略表（policy.go）。
func NewAuthorizer(svc *auth.TokenService) Authorizer {
	return kitAuthorizer{svc: svc}
}

// NewOpenAccessAuthorizer 返回第 3 态（无 secret + 显式声明
// `AUTH_INSECURE_EXPOSURE=loopback-published`）的开放闸：受保护路由不带 token
// 也直接进入业务层。**只由 cmd/assetservice 在启动闸已放行、且逐字声明
// loopback-published 时装配**；第 1/2 态必须继续用 NewAuthorizer（无 secret ⇒ 503），
// 否则等于把「漏配 secret」重新变成放行后门。
func NewOpenAccessAuthorizer() Authorizer {
	return openAccessAuthorizer{}
}

type openAccessAuthorizer struct{}

func (openAccessAuthorizer) Authorize(*http.Request) (string, error) {
	return "", nil
}

type kitAuthorizer struct {
	svc *auth.TokenService
}

func (a kitAuthorizer) Authorize(r *http.Request) (string, error) {
	if a.svc == nil || !a.svc.Enabled() {
		// 无 secret = 鉴权未配置。回环绑定的豁免只免「启动拒绝」，不免写端点
		// 鉴权：这一档一律 503，**不是**就地放行。要让无 secret 的写面可用，
		// 必须走显式声明（`AUTH_INSECURE_EXPOSURE=loopback-published`）装配
		// `NewOpenAccessAuthorizer`——两种意图分开表达，不靠绑定地址猜。
		return "", fmt.Errorf("%w: JWT secret is not configured", errAuthUnavailable)
	}
	key := kithttp.RouteKey(r)
	rule, declared := servedPolicy.Rule(key)
	if !declared {
		// 路由表 ≡ 策略表由守卫测试钉住；运行时仍 fail-closed。
		return "", fmt.Errorf("%w: no authorization policy for %q", ErrForbidden, key)
	}
	claims, err := kithttp.Authenticate(a.svc, r)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	if err := kithttp.Authorize(claims, rule); err != nil {
		if errors.Is(err, kithttp.ErrForbidden) {
			return "", fmt.Errorf("%w: %v", ErrForbidden, err)
		}
		return "", fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	return principalOf(claims), nil
}

// principalOf 取限流键：username 优先、缺失退 subject（服务 token 只有 sub）。
func principalOf(claims *auth.Claims) string {
	if claims == nil {
		return ""
	}
	if claims.Username != "" {
		return claims.Username
	}
	return claims.Subject
}

// requireAuth 是受保护路由的闸：未装配 Authorizer → 503；无凭证 / 坏 token →
// 401；角色不足 / 表外路由 → 403。只有 Authorize 返回 nil 才进入 handler。
//
// 顺序是**鉴权先于限流**（G1）：401 / 403 永远不会被 429 顶替。反过来的话，
// 未登录调用方会被读成「被限流」，core 侧 AUD-57 的同型坑（探针被限流读成
// 未登录，整站弹登录页）。
func (a *API) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.authorizer == nil {
			writeError(w, fmt.Errorf("%w: endpoints require an authorizer", errAuthUnavailable))
			return
		}
		principal, err := a.authorizer.Authorize(r)
		switch {
		case err == nil:
			if !a.limit(w, r, principal) {
				return
			}
			next(w, r)
		case errors.Is(err, ErrUnauthenticated):
			writeError(w, ErrUnauthenticated)
		case errors.Is(err, ErrForbidden):
			writeError(w, ErrForbidden)
		case errors.Is(err, errAuthUnavailable):
			// 未配置 secret：503，不记成 authorizer 异常（每个写请求都会命中）。
			writeError(w, err)
		default:
			log.Printf("httpapi: authorizer error: %v", err)
			writeError(w, fmt.Errorf("%w: %v", errAuthUnavailable, err))
		}
	}
}

// limit 消耗调用方的一枚令牌；超限写 429 + Retry-After 并返回 false。
// 探针不经过这里（Routes 直接注册）。
func (a *API) limit(w http.ResponseWriter, r *http.Request, principal string) bool {
	if a.limiter == nil {
		return true
	}
	if a.limiter.allow(rateKey(principal), time.Now()) {
		return true
	}
	w.Header().Set("Retry-After", "1")
	writeError(w, errRateLimited)
	return false
}

// openRoute 给未挂鉴权闸的公开端点加限流（身份桶统一落在 open-access）。
func (a *API) openRoute(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.limit(w, r, "") {
			return
		}
		next(w, r)
	}
}
