package asset

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

func mustCache(t *testing.T, cfg CacheConfig) *Cache {
	t.Helper()
	c, err := NewCache(cfg)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	return c
}

func TestCache_MemoryLRU(t *testing.T) {
	c := mustCache(t, CacheConfig{MemoryBytes: 10})
	a, b, d := []byte("aaaa"), []byte("bbbb"), []byte("cccc")
	ha, hb, hd := ArtifactHash(a), ArtifactHash(b), ArtifactHash(d)
	if err := c.Put(ha, a); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(hb, b); err != nil {
		t.Fatal(err)
	}
	// 命中 a，让 b 成为最久未用；再放 d（总量 12 > 10）应驱逐 b。
	if _, ok := c.Get(ha); !ok {
		t.Fatal("a must be a hit")
	}
	if err := c.Put(hd, d); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(hb); ok {
		t.Fatal("b should have been evicted (LRU)")
	}
	if got, ok := c.Get(ha); !ok || !bytes.Equal(got, a) {
		t.Fatal("a must survive")
	}
	if got, ok := c.Get(hd); !ok || !bytes.Equal(got, d) {
		t.Fatal("d must be present")
	}
}

func TestCache_DiskTierSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	raw := wasmWithImports("env.get_bar2")
	hash := ArtifactHash(raw)

	first := mustCache(t, CacheConfig{Dir: dir, MemoryBytes: 1 << 20, DiskBytes: 1 << 20})
	if err := first.Put(hash, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, hash[:2], hash)); err != nil {
		t.Fatalf("content-addressed file missing: %v", err)
	}

	// 新实例 = 进程重启：L1 是空的，字节只能来自 L2。
	second := mustCache(t, CacheConfig{Dir: dir, MemoryBytes: 1 << 20, DiskBytes: 1 << 20})
	got, ok := second.Get(hash)
	if !ok || !bytes.Equal(got, raw) {
		t.Fatalf("disk tier did not survive restart (ok=%v, %d bytes)", ok, len(got))
	}
}

func TestCache_FailsClosedOnHashMismatchAndCorruption(t *testing.T) {
	raw := []byte("payload")
	if err := mustCache(t, CacheConfig{}).Put(ArtifactHash([]byte("other")), raw); err == nil {
		t.Fatal("Put with a key that does not match the bytes must fail")
	}

	dir := t.TempDir()
	hash := ArtifactHash([]byte("expected"))
	path := filepath.Join(dir, hash[:2], hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := mustCache(t, CacheConfig{Dir: dir})
	if _, ok := c.Get(hash); ok {
		t.Fatal("tampered disk entry must be a miss")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tampered entry must be removed, stat err = %v", err)
	}
}

func TestCache_DiskCapacityEviction(t *testing.T) {
	dir := t.TempDir()
	values := [][]byte{[]byte("aaaa"), []byte("bbbb"), []byte("cccc")}
	times := []time.Time{
		time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 24, 9, 0, 1, 0, time.UTC),
		time.Date(2026, 10, 24, 9, 0, 2, 0, time.UTC),
	}
	for i, v := range values {
		hash := ArtifactHash(v)
		path := filepath.Join(dir, hash[:2], hash)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, v, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, times[i], times[i]); err != nil {
			t.Fatal(err)
		}
	}
	// 启动扫描时总量 12 > 8，按 mtime 从旧到新删；aaaa 最新一次都没被用过。
	mustCache(t, CacheConfig{Dir: dir, MemoryBytes: 1, DiskBytes: 8})
	oldest := filepath.Join(dir, ArtifactHash(values[0])[:2], ArtifactHash(values[0]))
	if _, err := os.Stat(oldest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest entry should be evicted, stat err = %v", err)
	}
	for _, v := range values[1:] {
		hash := ArtifactHash(v)
		if _, err := os.Stat(filepath.Join(dir, hash[:2], hash)); err != nil {
			t.Fatalf("%x should survive: %v", v, err)
		}
	}
}

func TestCache_LatestTTLAndInvalidation(t *testing.T) {
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	c := mustCache(t, CacheConfig{
		LatestTTL: 300 * time.Second,
		Now:       func() time.Time { return now },
	})
	result := ResolveResult{StrategyID: "s1", ArtifactHash: "h1", Imports: []string{"env.get_bar"}}
	c.PutLatest("s1", result)
	if _, ok := c.Latest("s1"); !ok {
		t.Fatal("latest should be cached")
	}
	now = now.Add(299 * time.Second)
	if _, ok := c.Latest("s1"); !ok {
		t.Fatal("latest must still be valid before the 300s TTL")
	}
	now = now.Add(2 * time.Second)
	if _, ok := c.Latest("s1"); ok {
		t.Fatal("latest must expire after the TTL")
	}
	c.PutLatest("s1", result)
	c.InvalidateLatest("s1")
	if _, ok := c.Latest("s1"); ok {
		t.Fatal("InvalidateLatest must drop the entry")
	}
}

// countingStore 统计回源次数，用来证明缓存命中没有偷偷查库。
type countingStore struct {
	Store
	defs      int32
	artifacts int32
}

func (s *countingStore) GetDefinition(ctx context.Context, id string) (Definition, error) {
	atomic.AddInt32(&s.defs, 1)
	return s.Store.GetDefinition(ctx, id)
}

func (s *countingStore) GetArtifact(ctx context.Context, id, hash string) (Artifact, error) {
	atomic.AddInt32(&s.artifacts, 1)
	return s.Store.GetArtifact(ctx, id, hash)
}

func TestService_LatestCacheTTLAndPromoteInvalidation(t *testing.T) {
	ctx := context.Background()
	mem := NewMemory()
	store := &countingStore{Store: mem}
	now := fixedNow()
	cache := mustCache(t, CacheConfig{
		LatestTTL: 300 * time.Second,
		Now:       func() time.Time { return now },
	})
	svc, err := New(Options{Store: store, Cache: cache, IDGen: seqIDs(), Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	v1 := mustPutSource(t, svc, def.ID, "src-v1")
	// 直接登记 artifact（不需要编译器），再 validate/promote。
	raw := wasmWithImports("env.get_bar2")
	if _, err := svc.RegisterArtifact(ctx, Artifact{
		StrategyID: def.ID, VersionHash: v1.VersionHash, ArtifactHash: ArtifactHash(raw),
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: "tc",
		SizeBytes: int64(len(raw)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: raw,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v1.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	promoted, err := svc.Promote(ctx, def.ID, v1.VersionHash, "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.Resolve(ctx, def.ID, "latest")
	if err != nil {
		t.Fatal(err)
	}
	defsAfterFirst, artifactsAfterFirst := store.defs, store.artifacts
	if first.ArtifactHash != promoted.ArtifactHash {
		t.Fatalf("resolve = %+v", first)
	}
	if _, err := svc.Resolve(ctx, def.ID, "latest"); err != nil {
		t.Fatal(err)
	}
	if store.defs != defsAfterFirst || store.artifacts != artifactsAfterFirst {
		t.Fatal("a warm latest cache must not hit the store")
	}

	// TTL 过期后必须回源（值不变也必须重新确认）。
	now = now.Add(301 * time.Second)
	if _, err := svc.Resolve(ctx, def.ID, "latest"); err != nil {
		t.Fatal(err)
	}
	if store.defs == defsAfterFirst {
		t.Fatal("latest must be re-read after the TTL")
	}

	// 新版本 promote 必须当场失效 latest，而不是等 TTL 自然过期。
	v2 := mustPutSource(t, svc, def.ID, "src-v2")
	raw2 := wasmWithImports("env.get_bar2", "env.emit_target2")
	if _, err := svc.RegisterArtifact(ctx, Artifact{
		StrategyID: def.ID, VersionHash: v2.VersionHash, ArtifactHash: ArtifactHash(raw2),
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: "tc",
		SizeBytes: int64(len(raw2)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: raw2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v2.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	promoted2, err := svc.Promote(ctx, def.ID, v2.VersionHash, "")
	if err != nil {
		t.Fatal(err)
	}
	after, err := svc.Resolve(ctx, def.ID, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if after.ArtifactHash != promoted2.ArtifactHash || after.ArtifactHash == promoted.ArtifactHash {
		t.Fatalf("promote did not invalidate latest: %+v", after)
	}
}

// offlineBlobStore 模拟「进程重启后，元数据还在但字节回源不可用」。
type offlineBlobStore struct {
	Store
}

func (s offlineBlobStore) GetArtifact(context.Context, string, string) (Artifact, error) {
	return Artifact{}, errors.New("blob store is offline")
}

func TestService_CachedArtifactServedAfterRestartWithBlobStoreOffline(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mem := NewMemory()
	cache1 := mustCache(t, CacheConfig{Dir: dir, MemoryBytes: 1 << 20, DiskBytes: 1 << 20})
	svc1, err := New(Options{Store: mem, Cache: cache1, IDGen: seqIDs(), Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	def := mustCreateWasm(t, svc1, "on_bar/2", contracts.ScopeMarket)
	v := mustPutSource(t, svc1, def.ID, "src")
	raw := wasmWithImports("env.get_bar2")
	if _, err := svc1.RegisterArtifact(ctx, Artifact{
		StrategyID: def.ID, VersionHash: v.VersionHash, ArtifactHash: ArtifactHash(raw),
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: "tc",
		SizeBytes: int64(len(raw)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: raw,
	}); err != nil {
		t.Fatal(err)
	}
	online, err := svc1.ArtifactForVersion(ctx, def.ID, v.VersionHash, "")
	if err != nil {
		t.Fatal(err)
	}

	// 重启：L1 清空，L2 仍在；字节回源（GetArtifact）失败也不影响已缓存 hash。
	cache2 := mustCache(t, CacheConfig{Dir: dir, MemoryBytes: 1 << 20, DiskBytes: 1 << 20})
	svc2, err := New(Options{Store: offlineBlobStore{Store: mem}, Cache: cache2, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	offline, err := svc2.ArtifactForVersion(ctx, def.ID, v.VersionHash, "")
	if err != nil {
		t.Fatalf("cached artifact must survive a restart with the blob store offline: %v", err)
	}
	if !bytes.Equal(offline.Bytes, online.Bytes) || offline.ArtifactHash != online.ArtifactHash {
		t.Fatal("cached artifact bytes differ from the online read")
	}
}
