// Package compilerjob 把「编译 → 校验 → 登记」串成 QT-41b 的一条管线。
//
// 它不直连 DB、不 promote：编译产物通过资产服务的内部端点登记（D 裁决：
// cmd/compiler 是独立进程 / 容器，只经 HTTP 与资产服务交互）。
package compilerjob

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tellurion-strategy/internal/asset"
)

// Job 是一次编译任务（由调用方从文件 / CI 提供）。
type Job struct {
	// JobID 是资产服务 strategy_build_jobs 里的作业 id（可空）。
	//
	// 非空时队列用它作记录主键，终态就能按同一个 id 检索；空则由队列自己生成。
	// 编译进程仍不直连 DB——这里只借用标识，不写那张表。
	JobID       string `json:"job_id,omitempty"`
	StrategyID  string `json:"strategy_id"`
	VersionHash string `json:"version_hash"`
	Carrier     string `json:"carrier"`
	ABIVersion  string `json:"abi_version"`
	Entry       string `json:"entry"`
	Source      string `json:"source"`
	BuiltBy     string `json:"built_by"`
}

// Validate 校验任务形状。
func (j Job) Validate() error {
	if strings.TrimSpace(j.StrategyID) == "" {
		return fmt.Errorf("%w: strategy_id is required", asset.ErrInvalidParameter)
	}
	if strings.TrimSpace(j.VersionHash) == "" {
		return fmt.Errorf("%w: version_hash is required", asset.ErrInvalidParameter)
	}
	carrier := asset.Carrier(j.Carrier)
	if carrier != asset.CarrierWASM && carrier != asset.CarrierSO {
		return fmt.Errorf("%w: carrier %q produces no artifact", asset.ErrUnsupportedCarrier, j.Carrier)
	}
	if strings.TrimSpace(j.Entry) == "" {
		return fmt.Errorf("%w: entry is required", asset.ErrInvalidParameter)
	}
	if strings.TrimSpace(j.Source) == "" {
		return asset.ErrSourceMissing
	}
	return nil
}

// Config 是管线装配。
type Config struct {
	AssetURL         string
	Runner           asset.Compiler
	MemoryLimitBytes int64
	BuiltBy          string
	HTTPClient       *http.Client
	// Reporter 是作业状态回写面（可选）：nil 时队列只在进程内记录状态。
	Reporter BuildReporter
}

// Run 执行一次编译并登记，返回服务端落库后的 artifact 行。
func Run(ctx context.Context, cfg Config, job Job) (asset.Artifact, error) {
	if err := job.Validate(); err != nil {
		return asset.Artifact{}, err
	}
	if cfg.Runner == nil {
		return asset.Artifact{}, asset.ErrCompileUnavailable
	}
	base, err := url.Parse(cfg.AssetURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return asset.Artifact{}, fmt.Errorf("%w: asset url %q", asset.ErrInvalidParameter, cfg.AssetURL)
	}
	limit := cfg.MemoryLimitBytes
	if limit == 0 {
		limit = asset.MaxMemoryLimitBytes
	}
	builtBy := cfg.BuiltBy
	if builtBy == "" {
		builtBy = "compiler:" + cfg.Runner.ToolchainID()
	}

	res, err := cfg.Runner.Compile(ctx, asset.CompileRequest{
		StrategyID:       job.StrategyID,
		VersionHash:      job.VersionHash,
		Carrier:          asset.Carrier(job.Carrier),
		ABIVersion:       job.ABIVersion,
		Entry:            job.Entry,
		Source:           job.Source,
		MemoryLimitBytes: limit,
	})
	if err != nil {
		return asset.Artifact{}, err
	}

	body := registerRequest{
		StrategyID:       job.StrategyID,
		VersionHash:      job.VersionHash,
		ArtifactHash:     asset.ArtifactHash(res.Bytes),
		Carrier:          job.Carrier,
		ABIVersion:       job.ABIVersion,
		Toolchain:        cfg.Runner.ToolchainID(),
		SizeBytes:        int64(len(res.Bytes)),
		MemoryLimitBytes: limit,
		Imports:          res.Imports,
		CompileLog:       res.Log,
		BuiltBy:          builtBy,
		BytesBase64:      base64.StdEncoding.EncodeToString(res.Bytes),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return asset.Artifact{}, fmt.Errorf("compiler: encode registration: %w", err)
	}

	endpoint := strings.TrimRight(cfg.AssetURL, "/") + "/internal/strategies/" +
		url.PathEscape(job.StrategyID) + "/artifacts"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return asset.Artifact{}, fmt.Errorf("compiler: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return asset.Artifact{}, fmt.Errorf("%w: register artifact: %v", asset.ErrArtifactFetchFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return asset.Artifact{}, fmt.Errorf("%w: read registration response: %v", asset.ErrArtifactFetchFailed, err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return asset.Artifact{}, fmt.Errorf("%w: register artifact: HTTP %d: %s",
			asset.ErrArtifactFetchFailed, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var registered artifactResponse
	if err := json.Unmarshal(raw, &registered); err != nil {
		return asset.Artifact{}, fmt.Errorf("%w: decode registration response: %v", asset.ErrInvalidParameter, err)
	}
	if registered.ArtifactHash == "" {
		return asset.Artifact{}, ErrEmptyServiceResponse
	}
	if registered.ArtifactHash != body.ArtifactHash {
		return asset.Artifact{}, fmt.Errorf("%w: service registered %s, compiled %s",
			asset.ErrArtifactHashMismatch, registered.ArtifactHash, body.ArtifactHash)
	}
	return asset.Artifact{
		StrategyID:       registered.StrategyID,
		VersionHash:      registered.VersionHash,
		ArtifactHash:     registered.ArtifactHash,
		Carrier:          asset.Carrier(registered.Carrier),
		ABIVersion:       registered.ABIVersion,
		Toolchain:        registered.Toolchain,
		SizeBytes:        registered.SizeBytes,
		MemoryLimitBytes: registered.MemoryLimitBytes,
		Imports:          registered.Imports,
		Status:           asset.Status(registered.Status),
	}, nil
}

type registerRequest struct {
	StrategyID       string   `json:"strategy_id"`
	VersionHash      string   `json:"version_hash"`
	ArtifactHash     string   `json:"artifact_hash"`
	Carrier          string   `json:"carrier"`
	ABIVersion       string   `json:"abi_version"`
	Toolchain        string   `json:"toolchain"`
	SizeBytes        int64    `json:"size_bytes"`
	MemoryLimitBytes int64    `json:"memory_limit_bytes"`
	Imports          []string `json:"imports"`
	CompileLog       string   `json:"compile_log"`
	BuiltBy          string   `json:"built_by"`
	BytesBase64      string   `json:"bytes_base64"`
}

type artifactResponse struct {
	StrategyID       string   `json:"strategy_id"`
	VersionHash      string   `json:"version_hash"`
	ArtifactHash     string   `json:"artifact_hash"`
	Carrier          string   `json:"carrier"`
	ABIVersion       string   `json:"abi_version"`
	Toolchain        string   `json:"toolchain"`
	SizeBytes        int64    `json:"size_bytes"`
	MemoryLimitBytes int64    `json:"memory_limit_bytes"`
	Imports          []string `json:"imports"`
	Status           string   `json:"status"`
}

// ErrEmptyServiceResponse 是服务端 2xx 但响应里没有 artifact_hash——契约说谎。
var ErrEmptyServiceResponse = errors.New("compiler: service returned an empty artifact_hash")
