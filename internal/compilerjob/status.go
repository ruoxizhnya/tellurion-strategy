package compilerjob

import (
	"bytes"
	"context"
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

// BuildReporter 把队列的 running / 终态回写到资产服务。
//
// 抽成接口只为测试注入替身：生产实现是下面的 HTTPReporter，Queue 不直连 DB。
type BuildReporter interface {
	Report(ctx context.Context, job Job, state, artifactHash, errorCode, message string) error
}

// StatusReporter 经内部端点 POST /internal/strategies/{id}/builds/{job_id}
// 回写作业状态。Job.JobID 为空时跳过——现有一批 job 文件的用法不依赖资产
// 服务签发的 job_id，不能因为没上报就不编译。
type StatusReporter struct {
	AssetURL   string
	HTTPClient *http.Client
}

// Report 发送一次状态变更；非 2xx 视为失败（由调用方决定是否重试/记账）。
func (r *StatusReporter) Report(ctx context.Context, job Job, state, artifactHash, errorCode, message string) error {
	if strings.TrimSpace(job.JobID) == "" {
		return nil
	}
	base, err := url.Parse(r.AssetURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("%w: asset url %q", asset.ErrInvalidParameter, r.AssetURL)
	}
	body := asset.BuildJobReport{
		State:        state,
		ArtifactHash: artifactHash,
		ErrorCode:    errorCode,
		Error:        message,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("compiler: encode build report: %w", err)
	}
	endpoint := strings.TrimRight(r.AssetURL, "/") + "/internal/strategies/" +
		url.PathEscape(job.StrategyID) + "/builds/" + url.PathEscape(job.JobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("compiler: build report request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("compiler: report build job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("compiler: read build report response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("compiler: report build job %s: HTTP %d: %s",
			job.JobID, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// ErrorCodeFor 把编译链路的错误映射成机器可读归因码（wire `error_code`）。
func ErrorCodeFor(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, asset.ErrCompileUnavailable):
		return asset.ErrorCodeCompileUnavailable
	case errors.Is(err, asset.ErrSourceTooLarge), errors.Is(err, asset.ErrArtifactTooLarge):
		return asset.ErrorCodePayloadTooLarge
	case errors.Is(err, asset.ErrArtifactHashMismatch), errors.Is(err, asset.ErrInvalidArtifact):
		return asset.ErrorCodeArtifactInvalid
	default:
		return asset.ErrorCodeCompileFailed
	}
}
