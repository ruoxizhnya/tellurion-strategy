package compilerjob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"tellurion-strategy/internal/asset"
)

// 作业状态词表。取值**直接取自** asset 包的 job 常量，不另抄一遍字符串：
// 它们与 strategy.strategy_build_jobs.state 的 CHECK 是同一套
// （queued → running → succeeded | failed），钉子在 queue_test.go。
const (
	StateQueued    = asset.JobQueued
	StateRunning   = asset.JobRunning
	StateSucceeded = asset.JobSucceeded
	StateFailed    = asset.JobFailed
)

// ErrQueueClosed 是队列已开始执行后仍提交作业（Submit 必须在 Run 之前）。
var ErrQueueClosed = errors.New("compiler: queue has already started")

// Record 是一条作业的可检索快照。终态（尤其 failed）连 error 一起留在队列里，
// 调用方按 job_id 取回，不必翻进程日志。
type Record struct {
	JobID        string     `json:"job_id"`
	StrategyID   string     `json:"strategy_id"`
	VersionHash  string     `json:"version_hash"`
	Carrier      string     `json:"carrier"`
	Entry        string     `json:"entry"`
	State        string     `json:"state"`
	Attempts     int        `json:"attempts"`
	ArtifactHash string     `json:"artifact_hash,omitempty"`
	Error        string     `json:"error,omitempty"`
	QueuedAt     time.Time  `json:"queued_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// QueueOptions 是队列的执行参数。
//
// MaxAttempts <= 0 视为 1（不重试）；非可重试的错误即使还有剩余次数也立即终态，
// 不拿「重试」掩盖确定性失败。
type QueueOptions struct {
	Workers     int
	MaxAttempts int
	Backoff     time.Duration
	Retryable   func(error) bool
	IDGen       func() string
	Now         func() time.Time
}

// Queue 是编译作业的排队 / 重试 / 限并发执行器。
//
// 批次语义：Submit 若干作业 → Run 由 Workers 个 worker 取队首执行 → 全部终态后
// 返回。它不是常驻 worker：与资产服务 strategy_build_jobs 行的跨进程派发 / 回写
// 是另一条边（见 docs/TASKS.md QT-41b 余项），本队列只保证**状态词表与终态语义**
// 和那张表一致，并接受资产服务签发的 job_id 作为记录主键。
type Queue struct {
	cfg  Config
	opts QueueOptions

	mu      sync.Mutex
	order   []*queueEntry
	pending []*queueEntry
	started bool
	done    chan struct{}
}

type queueEntry struct {
	job Job
	rec Record
}

// NewQueue 装配队列。Runner 缺失在构造期 fail-closed（等于每个作业都会失败）。
func NewQueue(cfg Config, opts QueueOptions) (*Queue, error) {
	if cfg.Runner == nil {
		return nil, asset.ErrCompileUnavailable
	}
	if opts.Workers <= 0 {
		opts.Workers = 1
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 1
	}
	if opts.Retryable == nil {
		opts.Retryable = DefaultRetryable
	}
	if opts.IDGen == nil {
		opts.IDGen = randomJobID
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Queue{cfg: cfg, opts: opts, done: make(chan struct{})}, nil
}

// Submit 校验并登记一个作业（状态 queued），返回记录用的 job_id。
//
// 作业自带的 JobID（资产服务签发的 job_id）优先作为记录主键，重复即拒——
// 两条不同作业共用一个 id 会让终态检索张冠李戴。
func (q *Queue) Submit(job Job) (string, error) {
	if err := job.Validate(); err != nil {
		return "", err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started {
		return "", ErrQueueClosed
	}
	id := job.JobID
	if id == "" {
		id = q.opts.IDGen()
	}
	for _, e := range q.order {
		if e.rec.JobID == id {
			return "", fmt.Errorf("%w: duplicate job id %q", asset.ErrInvalidParameter, id)
		}
	}
	entry := &queueEntry{
		job: job,
		rec: Record{
			JobID:       id,
			StrategyID:  job.StrategyID,
			VersionHash: job.VersionHash,
			Carrier:     job.Carrier,
			Entry:       job.Entry,
			State:       StateQueued,
			QueuedAt:    q.opts.Now(),
		},
	}
	q.order = append(q.order, entry)
	q.pending = append(q.pending, entry)
	return id, nil
}

// Run 由至多 Workers 个 worker 处理全部已提交作业，全部终态后返回。
//
// 队列一旦开始执行就不再接受新作业；并发的第二次 Run 会等第一次结束后返回，
// 所以「Run 返回」恒等于「全部作业已终态」。
func (q *Queue) Run(ctx context.Context) {
	q.mu.Lock()
	if q.started {
		done := q.done
		q.mu.Unlock()
		<-done
		return
	}
	q.started = true
	workers := q.opts.Workers
	if workers > len(q.order) {
		workers = len(q.order)
	}
	q.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				entry, ok := q.next()
				if !ok {
					return
				}
				q.execute(ctx, entry)
			}
		}()
	}
	wg.Wait()
	close(q.done)
}

func (q *Queue) next() (*queueEntry, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil, false
	}
	entry := q.pending[0]
	q.pending = q.pending[1:]
	return entry, true
}

func (q *Queue) execute(ctx context.Context, entry *queueEntry) {
	q.mu.Lock()
	now := q.opts.Now()
	entry.rec.State = StateRunning
	entry.rec.StartedAt = &now
	q.mu.Unlock()

	var last error
	for attempt := 1; attempt <= q.opts.MaxAttempts; attempt++ {
		if attempt > 1 && !sleepCtx(ctx, q.opts.Backoff) {
			break
		}
		q.mu.Lock()
		entry.rec.Attempts = attempt
		q.mu.Unlock()

		artifact, err := Run(ctx, q.cfg, entry.job)
		if err == nil {
			q.finish(entry, StateSucceeded, artifact.ArtifactHash, nil)
			return
		}
		last = err
		if ctx.Err() != nil || !q.opts.Retryable(err) {
			break
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil && last != nil {
		// 保留被中断前的最后一次错误链，同时把「为什么停」写进终态。
		last = fmt.Errorf("%w; stopped early: %v", last, ctxErr)
	}
	q.finish(entry, StateFailed, "", last)
}

func (q *Queue) finish(entry *queueEntry, state, artifactHash string, cause error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.opts.Now()
	entry.rec.State = state
	entry.rec.ArtifactHash = artifactHash
	entry.rec.FinishedAt = &now
	if cause != nil {
		entry.rec.Error = cause.Error()
	}
}

// Record 按 job_id 返回快照。
func (q *Queue) Record(jobID string) (Record, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, entry := range q.order {
		if entry.rec.JobID == jobID {
			return cloneRecord(entry.rec), nil
		}
	}
	return Record{}, fmt.Errorf("%w: %s", asset.ErrJobNotFound, jobID)
}

// Records 按提交顺序返回全部作业快照（终态失败可检索）。
func (q *Queue) Records() []Record {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Record, 0, len(q.order))
	for _, entry := range q.order {
		out = append(out, cloneRecord(entry.rec))
	}
	return out
}

func cloneRecord(rec Record) Record {
	if rec.StartedAt != nil {
		v := *rec.StartedAt
		rec.StartedAt = &v
	}
	if rec.FinishedAt != nil {
		v := *rec.FinishedAt
		rec.FinishedAt = &v
	}
	return rec
}

// DefaultRetryable 区分「再试一次可能好」与「重试只是把同一个确定性失败再跑一遍」。
//
// 可重试：编译失败（工具链超时 / 容器抖动）、登记时取字节失败、其余未知错误。
// 不可重试：作业形状 / 源码超限 / 产物超限 / hash 不符 / 服务未配编译器 /
// 服务撒谎——这些重试一万次也是同一个结论，只会把终态报告拖晚。
func DefaultRetryable(err error) bool {
	switch {
	case errors.Is(err, asset.ErrInvalidParameter),
		errors.Is(err, asset.ErrInvalidDefinition),
		errors.Is(err, asset.ErrUnsupportedCarrier),
		errors.Is(err, asset.ErrSourceMissing),
		errors.Is(err, asset.ErrSourceTooLarge),
		errors.Is(err, asset.ErrArtifactTooLarge),
		errors.Is(err, asset.ErrArtifactHashMismatch),
		errors.Is(err, asset.ErrCompileUnavailable),
		errors.Is(err, ErrEmptyServiceResponse):
		return false
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func randomJobID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("job_%d", time.Now().UnixNano())
	}
	return "job_" + hex.EncodeToString(buf[:])
}
