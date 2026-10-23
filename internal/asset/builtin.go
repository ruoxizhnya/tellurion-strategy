package asset

import (
	"context"
	"errors"
	"fmt"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// 内置策略登记（G 裁决，2026-10-21：保留 core 的 init 注册 + 在资产服务同步登记，
// 不做全量迁移）。
//
// 登记的是**定义**（carrier=builtin、artifact 为空）：字节码/符号在 core 二进制里，
// 本服务只回答「这个名字存在、它属于哪个 scope / 入口符号」，消除第二真相源。
//
// 元数据依据（不是猜的）：
//   - 名单：core `pkg/strategy/plugins/registration_test.go` 的 `builtinNames`（14 个），
//     `expression_template` 是 carrier=dsl 的另一条腿，不在此列；
//   - scope：core 的 `strategy.SignalGenerator` 一次拿整张 `bars map[string][]Bar`
//     与 portfolio（`pkg/strategy/interfaces.go`），所以内置策略是 market scope；
//   - entry：设计 `strategy-source-of-truth-and-cache.md` §4「entry 指向二进制内符号」，
//     登记时用策略名作为符号名（equals `Name()`，由 core 注册表保证）。
const builtinScope = contracts.ScopeMarket

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

// RegisterBuiltins 在启动时同步登记内置策略（幂等）。
//
// 已存在同名定义时只做**一致性核对**，不覆盖：如果同名 id 已经是 wasm/dsl 或
// scope 不同，那是命名空间冲突，必须 fail-loud——静默覆盖会把别人的 active
// 指针和版本历史一起改写。
func (s *Service) RegisterBuiltins(ctx context.Context) (int, error) {
	registered := 0
	for _, def := range BuiltinCatalog() {
		if err := def.Validate(); err != nil {
			return registered, fmt.Errorf("builtin %s: %w", def.ID, err)
		}
		now := s.now()
		def.CreatedAt = now
		def.UpdatedAt = now
		err := s.store.CreateDefinition(ctx, def)
		if err == nil {
			registered++
			continue
		}
		if !errors.Is(err, ErrStrategyExists) {
			return registered, err
		}
		existing, getErr := s.store.GetDefinition(ctx, def.ID)
		if getErr != nil {
			return registered, getErr
		}
		if existing.Carrier != def.Carrier || existing.Scope != def.Scope ||
			existing.ABIVersion != def.ABIVersion || existing.Entry != def.Entry {
			return registered, fmt.Errorf("%w: builtin %q is already registered with a different shape (carrier=%s scope=%s entry=%s)",
				ErrArtifactConflict, def.ID, existing.Carrier, existing.Scope, existing.Entry)
		}
	}
	return registered, nil
}
