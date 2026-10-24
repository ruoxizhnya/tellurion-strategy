package main

import (
	"testing"
	"time"
)

func fullCompilerConfig() externalCompilerConfig {
	return externalCompilerConfig{
		Bin:              "C:/tools/compiler.exe",
		AssetURL:         "http://127.0.0.1:8085",
		ToolchainName:    "tinygo",
		ToolchainVersion: "0.34.0",
		ToolchainPath:    "C:/tools/tinygo.exe",
		ToolchainLock:    "C:/tools/toolchains.lock",
		ToolchainArgs:    []string{"-target=wasm-unknown"},
		EnvAllowlist:     []string{"PATH"},
		Timeout:          30 * time.Second,
		MaxAttempts:      2,
		RetryBackoff:     time.Second,
	}
}

// -compiler-bin 为空 = 不装配：Options.Compiler 保持 nil，build 端点仍 503。
func TestNewExternalCompiler_EmptyBinIsDisabled(t *testing.T) {
	compiler, err := newExternalCompiler(externalCompilerConfig{})
	if err != nil {
		t.Fatalf("newExternalCompiler: %v", err)
	}
	if compiler != nil {
		t.Fatalf("compiler = %v, want nil when -compiler-bin is empty", compiler)
	}
}

// 配置齐全时装配出 ToolchainID 一致的端口。
func TestNewExternalCompiler_Configured(t *testing.T) {
	compiler, err := newExternalCompiler(fullCompilerConfig())
	if err != nil {
		t.Fatalf("newExternalCompiler: %v", err)
	}
	if compiler == nil {
		t.Fatal("compiler = nil, want an external compiler")
	}
	if got := compiler.ToolchainID(); got != "tinygo 0.34.0" {
		t.Fatalf("ToolchainID = %q, want \"tinygo 0.34.0\"", got)
	}
}

// bin 非空但必填项缺失 → 启动期 fail-loud。
func TestNewExternalCompiler_MissingRequiredFieldFailsLoud(t *testing.T) {
	for _, missing := range []string{"asset-url", "toolchain-name", "toolchain-version", "toolchain-path", "toolchain-lock"} {
		t.Run(missing, func(t *testing.T) {
			cfg := fullCompilerConfig()
			switch missing {
			case "asset-url":
				cfg.AssetURL = ""
			case "toolchain-name":
				cfg.ToolchainName = ""
			case "toolchain-version":
				cfg.ToolchainVersion = ""
			case "toolchain-path":
				cfg.ToolchainPath = ""
			case "toolchain-lock":
				cfg.ToolchainLock = ""
			}
			if _, err := newExternalCompiler(cfg); err == nil {
				t.Fatalf("newExternalCompiler accepted a config without %s", missing)
			}
		})
	}
}
