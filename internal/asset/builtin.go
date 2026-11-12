package asset

import (
	"context"
	"fmt"
	"time"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// 内置策略登记（G 裁决，2026-10-21：保留 core 的 init 注册 + 在资产服务同步登记，
// 不做全量迁移）。
//
// 登记的是**定义 + 出厂版本 + 版本级激活**（carrier=builtin、artifact 为空）：
// 字节码/符号在 core 二进制里，本服务只回答「这个名字存在、它属于哪个 scope /
// 入口符号、当前生效的是哪个版本」，消除第二真相源。
//
// 元数据依据（不是猜的）：
//   - 名单：core `pkg/strategy/plugins/registration_test.go` 的 `builtinNames`（14 个），
//     `expression_template` 是 carrier=dsl 的另一条腿，不在此列；
//   - scope：core 的 `strategy.SignalGenerator` 一次拿整张 `bars map[string][]Bar`
//     与 portfolio（`pkg/strategy/interfaces.go`），所以内置策略是 market scope；
//   - entry：设计 `strategy-source-of-truth-and-cache.md` §4「entry 指向二进制内符号」，
//     登记时用策略名作为符号名（equals `Name()`，由 core 注册表保证）。
const builtinScope = contracts.ScopeMarket

// FactorySeedActor 是内置策略出厂种子写审计时使用的 actor。
//
// 它把「出厂种子」与「人工 promote」在 strategy.strategy_promotions 里区分开：
//
//	SELECT DISTINCT strategy_id FROM strategy.strategy_promotions
//	 WHERE actor = 'factory-seed';
//
// 人工 promote 的 actor 来自 core 鉴权后转发的 `X-Actor`，不会等于本常量；
// 出厂之后的人工改动会追加人工身份的行，审计历史因此能看出「谁在出厂版本之外
// 重新激活过」。
const FactorySeedActor = "factory-seed"

// FactorySeedVersion 标识出厂种子形态的内容版本。种子形状或 source 文案变化时
// 递增，便于排障时判断「库里的内置版本是哪一版种子写的」。
const FactorySeedVersion = 1

// BuiltinNames 是本批登记的 14 个内置策略名（顺序固定，便于审计对表）。
func BuiltinNames() []string {
	return []string{
		"convertible_bond",
		"event_driven",
		"mean_reversion",
		"momentum",
		"multi_factor",
		"quality",
		"risk_parity",
		"sentiment",
		"td_sequential",
		"bollinger_mr",
		"volume_price_trend",
		"volatility_breakout",
		"value",
		"value_screening",
	}
}

// BuiltinCatalog 返回 14 条内置策略定义（id 即策略名）。
func BuiltinCatalog() []Definition {
	names := BuiltinNames()
	out := make([]Definition, 0, len(names))
	for _, name := range names {
		out = append(out, Definition{
			ID:         name,
			Carrier:    CarrierBuiltin,
			ABIVersion: "",
			Scope:      builtinScope,
			Entry:      name,
		})
	}
	return out
}

// BuiltinSource 是出厂版本行的 source 文本。
//
// builtin 的 source 不是可执行代码，而是「实现指针」的登记说明（ADR-0006）。
// 内容稳定 ⇒ version_hash 稳定：重启不会因为写入新文案而改掉生效版本。
func BuiltinSource(name string) string {
	return name + "@builtin (in-binary implementation)"
}

// builtinVersion 构造出厂版本行；hash 走 contracts 的唯一编码器（含内容寻址）。
func builtinVersion(def Definition, now time.Time) (Version, error) {
	source := BuiltinSource(def.ID)
	hash, err := VersionHash(contracts.StrategyVersionInput{
		StrategyID: def.ID,
		Carrier:    string(def.Carrier),
		ABIVersion: def.ABIVersion,
		Scope:      def.Scope,
		Entry:      def.Entry,
		Source:     source,
	})
	if err != nil {
		return Version{}, err
	}
	return Version{
		StrategyID:  def.ID,
		VersionHash: hash,
		Source:      source,
		Carrier:     def.Carrier,
		ABIVersion:  def.ABIVersion,
		Scope:       def.Scope,
		Entry:       def.Entry,
		CreatedAt:   now,
	}, nil
}

// validateBuiltinSeed 让出厂版本与普通版本过同一道结构闸（ADR-0006）：
// builtin 不要求 artifact，但定义形状、entry 符号、capability 闭合、source 文本
// 必须成立——种子不能绕过人工 promote 要过的检查。
func validateBuiltinSeed(def Definition, version Version) error {
	return validateBuiltinActivation(def, version)
}

// RegisterBuiltins 在启动时同步登记内置策略（幂等、可重放）。
//
// F6 根因（2026-11-13）：旧实现只建 definition，从不登记版本、也不写
// active_version_hash；而 ADR-0006 的 resolve 判据是「active 版本存在」，
// 于是内置策略永远 404 no_active_version。修复按 risk 仓 internal/rule/seed.go
// 的出厂种子模式：每条策略在一个事务（PG）/ 一把锁（Memory）里完成
// 「建定义 → 登记出厂版本 → 置 active」，审计 actor 固定 FactorySeedActor。
//
// 幂等与「不覆盖人工改动」：已存在且已有 active 版本或任版本行 ⇒ 整条跳过，
// 一个字节都不写；只有「定义在、版本与 active 都没有」的旧库形态会被补齐。
// 同名定义若是别的载体 / scope / entry，仍 fail-loud（命名空间冲突），不覆盖。
func (s *Service) RegisterBuiltins(ctx context.Context) (int, error) {
	seeded := 0
	for _, def := range BuiltinCatalog() {
		now := s.now()
		def.CreatedAt = now
		def.UpdatedAt = now
		version, err := builtinVersion(def, now)
		if err != nil {
			return seeded, fmt.Errorf("builtin %s: %w", def.ID, err)
		}
		created, err := s.store.SeedBuiltin(ctx, def, version)
		if err != nil {
			return seeded, fmt.Errorf("builtin %s: %w", def.ID, err)
		}
		if created {
			if err := s.recordFactorySeed(ctx, def.ID, version.VersionHash, now); err != nil {
				return seeded, fmt.Errorf("builtin %s: %w", def.ID, err)
			}
			seeded++
			continue
		}
		existing, getErr := s.store.GetDefinition(ctx, def.ID)
		if getErr != nil {
			return seeded, getErr
		}
		if existing.Carrier != def.Carrier || existing.Scope != def.Scope ||
			existing.ABIVersion != def.ABIVersion || existing.Entry != def.Entry {
			return seeded, fmt.Errorf("%w: builtin %q is already registered with a different shape (carrier=%s scope=%s entry=%s)",
				ErrArtifactConflict, def.ID, existing.Carrier, existing.Scope, existing.Entry)
		}
	}
	return seeded, nil
}

// recordFactorySeed 追加一条审计行（人=factory-seed、时间、结论=promoted）。
//
// 写不进去就启动失败：种子已经落库、审计却悄悄丢了，会让「谁激活的」看起来
// 有答案。与 promote 端点 audit_failed 的口径一致。
func (s *Service) recordFactorySeed(ctx context.Context, strategyID, versionHash string, at time.Time) error {
	if s.promotionLog == nil {
		return nil
	}
	return s.promotionLog.RecordPromotion(ctx, PromotionEvent{
		StrategyID:   strategyID,
		VersionHash:  versionHash,
		ArtifactHash: "", // builtin 无 artifact（ADR-0006）
		Actor:        FactorySeedActor,
		Outcome:      string(StatusPromoted),
		DecidedAt:    at,
	})
}
