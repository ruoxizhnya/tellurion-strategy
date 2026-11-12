package asset

import (
	"errors"
	"strings"
	"testing"
)

// validateBuiltinActivation 是 builtin 版本级激活的结构闸（ADR-0006）：
// 不要求 artifact，但定义、entry 符号、capability 闭合与 source 文本都要过关。
func TestValidateBuiltinActivation(t *testing.T) {
	def := Definition{
		ID:      "st_builtin",
		Carrier: CarrierBuiltin,
		Scope:   "market",
		Entry:   "momentum",
		Capability: Capability{
			Frequencies: []string{"1d"},
			Markets:     []string{"crypto"},
			Features:    []string{"close"},
		},
	}
	version := Version{
		StrategyID:  def.ID,
		VersionHash: strings.Repeat("a", 64),
		Source:      "momentum@builtin (in-binary implementation)",
		Carrier:     def.Carrier,
		Scope:       def.Scope,
		Entry:       def.Entry,
	}

	if err := validateBuiltinActivation(def, version); err != nil {
		t.Fatalf("valid builtin rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*Definition, *Version)
		wantErr error
	}{
		{"entry is not a symbol", func(d *Definition, v *Version) {
			d.Entry, v.Entry = "9momentum", "9momentum"
		}, ErrInvalidDefinition},
		{"version entry drifted", func(d *Definition, v *Version) { v.Entry = "other" }, ErrInvalidDefinition},
		{"blank capability entry", func(d *Definition, v *Version) {
			d.Capability.Markets = []string{"crypto", " "}
		}, ErrInvalidDefinition},
		{"duplicate capability entry", func(d *Definition, v *Version) {
			d.Capability.Features = []string{"close", "close"}
		}, ErrInvalidDefinition},
		{"blank source", func(d *Definition, v *Version) { v.Source = "   " }, ErrInvalidDefinition},
		{"source with NUL", func(d *Definition, v *Version) { v.Source = "momentum\x00" }, ErrInvalidDefinition},
		{"source is not UTF-8", func(d *Definition, v *Version) { v.Source = string([]byte{0xff, 0xfe}) }, ErrInvalidDefinition},
		{"non-builtin carrier", func(d *Definition, v *Version) {
			d.Carrier, v.Carrier = CarrierDSL, CarrierDSL
		}, ErrUnsupportedCarrier},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, v := def, version
			tc.mutate(&d, &v)
			if err := validateBuiltinActivation(d, v); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
