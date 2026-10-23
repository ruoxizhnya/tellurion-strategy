// Command compiler 是 QT-41b 的独立编译进程：读一个编译 job，产出 artifact，
// 通过资产服务的内部端点登记。
//
// 为什么独立进程而不是库里一个函数：编译不可信源码是特权操作（toolchain 镜像
// 固定版本、env 白名单），把它和资产服务的 DB 连接放在同一个进程里，等于让
// 编译失败可以拖垮资产面（D 裁决，2026-10-21）。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"tellurion-strategy/internal/asset"
	"tellurion-strategy/internal/compile"
	"tellurion-strategy/internal/compilerjob"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "compiler: %v\n", err)
		os.Exit(1)
	}
}

type repeatableFlag []string

func (f *repeatableFlag) String() string { return strings.Join(*f, " ") }

func (f *repeatableFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

func run(args []string) error {
	fs := flag.NewFlagSet("compiler", flag.ContinueOnError)
	jobPath := fs.String("job", "", "path to the compile job JSON file (required)")
	assetURL := fs.String("asset-url", "", "base URL of the strategy asset service (required)")
	name := fs.String("toolchain-name", "", "toolchain name, e.g. tinygo (required)")
	version := fs.String("toolchain-version", "", "pinned toolchain version (required)")
	path := fs.String("toolchain-path", "", "absolute path to the toolchain binary (required)")
	lockPath := fs.String("toolchain-lock", "", "path to toolchains.lock; name+version must be pinned and the binary sha256 must match (required)")
	memoryLimit := fs.Int64("memory-limit", asset.MaxMemoryLimitBytes, "wasm memory limit in bytes")
	timeout := fs.Duration("timeout", 5*time.Minute, "compile timeout")
	var toolchainArgs repeatableFlag
	var envAllow repeatableFlag
	fs.Var(&toolchainArgs, "arg", "toolchain argument template (repeatable; must include {src}, {out} and -target=wasm-unknown)")
	fs.Var(&envAllow, "env", "environment variable to pass through to the toolchain (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, required := range []struct {
		name  string
		value string
	}{{"job", *jobPath}, {"asset-url", *assetURL}, {"toolchain-name", *name},
		{"toolchain-version", *version}, {"toolchain-path", *path}, {"toolchain-lock", *lockPath}} {
		if strings.TrimSpace(required.value) == "" {
			return fmt.Errorf("-%s is required", required.name)
		}
	}

	// 锁先于一切编译工作：清单里没有这个 名字+版本，或二进制 sha256 对不上，
	// 一个 job 都不许跑（无锁不编译）。
	lock, err := compile.LoadLock(*lockPath)
	if err != nil {
		return err
	}
	pin, err := lock.Pin(*name, *version)
	if err != nil {
		return err
	}
	if err := pin.VerifyBinary(*path); err != nil {
		return err
	}

	raw, err := os.ReadFile(*jobPath)
	if err != nil {
		return fmt.Errorf("read job: %w", err)
	}
	var job compilerjob.Job
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&job); err != nil {
		return fmt.Errorf("decode job: %w", err)
	}

	runner, err := compile.NewRunner(compile.Config{
		Name:         *name,
		Version:      *version,
		Path:         *path,
		Args:         toolchainArgs,
		EnvAllowlist: envAllow,
		Timeout:      *timeout,
		MaxBytes:     asset.MaxArtifactBytes,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout+time.Minute)
	defer cancel()
	artifact, err := compilerjob.Run(ctx, compilerjob.Config{
		AssetURL:         *assetURL,
		Runner:           runner,
		MemoryLimitBytes: *memoryLimit,
		BuiltBy:          "cmd/compiler " + runner.ToolchainID(),
	}, job)
	if err != nil {
		return err
	}
	out, err := json.Marshal(map[string]string{
		"strategy_id":   artifact.StrategyID,
		"version_hash":  artifact.VersionHash,
		"artifact_hash": artifact.ArtifactHash,
		"toolchain":     artifact.Toolchain,
		"status":        string(artifact.Status),
	})
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	fmt.Println(string(out))
	return nil
}
