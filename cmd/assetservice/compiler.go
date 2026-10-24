package main

import (
	"strings"
	"time"

	"tellurion-strategy/internal/asset"
	"tellurion-strategy/internal/compilerclient"
)

// externalCompilerConfig 是 `-compiler-*` 旗标的装配输入。
type externalCompilerConfig struct {
	Bin              string
	AssetURL         string
	ToolchainName    string
	ToolchainVersion string
	ToolchainPath    string
	ToolchainLock    string
	ToolchainArgs    []string
	EnvAllowlist     []string
	Timeout          time.Duration
	MaxAttempts      int
	RetryBackoff     time.Duration
}

// newExternalCompiler 装配外部编译进程端口。
//
// Bin 为空 = disabled（返回 nil，build 端点保持 503 compile_unavailable）；
// Bin 非空但必填项缺失 = 启动期 fail-loud，不把错误留到第一次 build。
func newExternalCompiler(cfg externalCompilerConfig) (asset.Compiler, error) {
	if strings.TrimSpace(cfg.Bin) == "" {
		return nil, nil
	}
	compiler, err := compilerclient.New(compilerclient.Config{
		Bin:              cfg.Bin,
		AssetURL:         cfg.AssetURL,
		ToolchainName:    cfg.ToolchainName,
		ToolchainVersion: cfg.ToolchainVersion,
		ToolchainPath:    cfg.ToolchainPath,
		ToolchainLock:    cfg.ToolchainLock,
		ToolchainArgs:    cfg.ToolchainArgs,
		EnvAllowlist:     cfg.EnvAllowlist,
		MemoryLimitBytes: asset.MaxMemoryLimitBytes,
		Timeout:          cfg.Timeout,
		MaxAttempts:      cfg.MaxAttempts,
		RetryBackoff:     cfg.RetryBackoff,
	})
	if err != nil {
		return nil, err
	}
	return compiler, nil
}
