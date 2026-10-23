// Command compiler 是 QT-41b 的独立编译进程：读若干编译 job，产出 artifact，
// 通过资产服务的内部端点登记。
//
// 为什么独立进程而不是库里一个函数：编译不可信源码是特权操作（toolchain 镜像
// 固定版本、env 白名单），把它和资产服务的 DB 连接放在同一个进程里，等于让
// 编译失败可以拖垮资产面（D 裁决，2026-10-21）。
//
// 一个进程可以吃一批 job：internal/compilerjob.Queue 负责排队、限并发与重试，
// 终态（含失败原因）随 JSON 报告输出，一个坏 job 不再带走整批。
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

// report 是批次的终端结果。作业级失败不影响其他作业，整批退出码由 Failed 决定。
type report struct {
	Succeeded int                  `json:"succeeded"`
	Failed    int                  `json:"failed"`
	Jobs      []compilerjob.Record `json:"jobs"`
}

func run(args []string) error {
	fs := flag.NewFlagSet("compiler", flag.ContinueOnError)
	var jobPaths repeatableFlag
	fs.Var(&jobPaths, "job", "path to a compile job JSON file (repeatable; each file is queued)")
	assetURL := fs.String("asset-url", "", "base URL of the strategy asset service (required)")
	name := fs.String("toolchain-name", "", "toolchain name, e.g. tinygo (required)")
	version := fs.String("toolchain-version", "", "pinned toolchain version (required)")
	path := fs.String("toolchain-path", "", "absolute path to the toolchain binary (required)")
	lockPath := fs.String("toolchain-lock", "", "path to toolchains.lock; name+version must be pinned and the binary sha256 must match (required)")
	memoryLimit := fs.Int64("memory-limit", asset.MaxMemoryLimitBytes, "wasm memory limit in bytes")
	timeout := fs.Duration("timeout", 5*time.Minute, "per-attempt compile timeout")
	workers := fs.Int("workers", 1, "number of jobs compiled concurrently")
	maxAttempts := fs.Int("max-attempts", 3, "attempts per job before the terminal failed state (1 disables retries)")
	retryBackoff := fs.Duration("retry-backoff", 2*time.Second, "delay between attempts of one job")
	reportPath := fs.String("report", "", "optional path to write the JSON job report")
	var toolchainArgs repeatableFlag
	var envAllow repeatableFlag
	fs.Var(&toolchainArgs, "arg", "toolchain argument template (repeatable; must include {src}, {out} and -target=wasm-unknown; tinygo also requires -no-debug)")
	fs.Var(&envAllow, "env", "environment variable to pass through to the toolchain (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, required := range []struct {
		name  string
		value string
	}{{"job", strings.Join(jobPaths, "")}, {"asset-url", *assetURL}, {"toolchain-name", *name},
		{"toolchain-version", *version}, {"toolchain-path", *path}, {"toolchain-lock", *lockPath}} {
		if strings.TrimSpace(required.value) == "" {
			return fmt.Errorf("-%s is required", required.name)
		}
	}
	if *workers <= 0 {
		return fmt.Errorf("-workers must be positive")
	}
	if *maxAttempts <= 0 {
		return fmt.Errorf("-max-attempts must be positive")
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

	// 先把整批 job 读完并严格解码：输入坏了要整批拒，不能跑一半才发现。
	jobs := make([]compilerjob.Job, 0, len(jobPaths))
	for _, jobPath := range jobPaths {
		raw, err := os.ReadFile(jobPath)
		if err != nil {
			return fmt.Errorf("read job %s: %w", jobPath, err)
		}
		var job compilerjob.Job
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&job); err != nil {
			return fmt.Errorf("decode job %s: %w", jobPath, err)
		}
		jobs = append(jobs, job)
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

	queue, err := compilerjob.NewQueue(compilerjob.Config{
		AssetURL:         *assetURL,
		Runner:           runner,
		MemoryLimitBytes: *memoryLimit,
		BuiltBy:          "cmd/compiler " + runner.ToolchainID(),
	}, compilerjob.QueueOptions{
		Workers:     *workers,
		MaxAttempts: *maxAttempts,
		Backoff:     *retryBackoff,
	})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if _, err := queue.Submit(job); err != nil {
			return fmt.Errorf("submit job: %w", err)
		}
	}

	// 批次预算：每个 worker 串行跑 ceil(N/workers) 个作业，每个作业最多
	// MaxAttempts 次、每次 ≤ timeout + backoff；在此之上留一分钟给登记。
	batches := (len(jobs) + *workers - 1) / *workers
	budget := time.Duration(batches)*time.Duration(*maxAttempts)*(*timeout+*retryBackoff) + time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	queue.Run(ctx)

	result := report{Jobs: queue.Records()}
	for _, rec := range result.Jobs {
		if rec.State == compilerjob.StateFailed {
			result.Failed++
		} else if rec.State == compilerjob.StateSucceeded {
			result.Succeeded++
		}
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	fmt.Println(string(out))
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, append(out, '\n'), 0o600); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	if result.Failed > 0 {
		return fmt.Errorf("%d of %d build jobs failed", result.Failed, len(jobs))
	}
	return nil
}
