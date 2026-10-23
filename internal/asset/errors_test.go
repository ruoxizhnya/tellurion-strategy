package asset

import (
	"errors"
	"fmt"
	"testing"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// 编译 / 资产四条哨兵自 contracts v0.4.0 起由契约仓定义，本仓只留同名别名。
// 钉子盯两件事：两条 err 必须是**同一个值**（不是同名同文案的两份），
// 且跨仓 `%w` 包装后仍能用本仓别名 `errors.Is` 判定。
func TestCompileErrorFamilyIsContractAlias(t *testing.T) {
	pairs := []struct {
		name     string
		alias    error
		contract error
	}{
		{"ErrCompileFailed", ErrCompileFailed, contracts.ErrCompileFailed},
		{"ErrCompileUnavailable", ErrCompileUnavailable, contracts.ErrCompileUnavailable},
		{"ErrSourceTooLarge", ErrSourceTooLarge, contracts.ErrSourceTooLarge},
		{"ErrArtifactTooLarge", ErrArtifactTooLarge, contracts.ErrArtifactTooLarge},
	}
	for _, tc := range pairs {
		t.Run(tc.name, func(t *testing.T) {
			if tc.alias != tc.contract {
				t.Fatalf("%s is a separate value from the contract sentinel (two sources of truth)", tc.name)
			}
			if tc.alias.Error() != tc.contract.Error() {
				t.Fatalf("%s text drifted: %q vs %q", tc.name, tc.alias.Error(), tc.contract.Error())
			}
			wrapped := fmt.Errorf("compiler: toolchain faketc: %w", tc.contract)
			if !errors.Is(wrapped, tc.alias) {
				t.Fatalf("%s must stay recognizable through %%w wrapping", tc.name)
			}
		})
	}
}
