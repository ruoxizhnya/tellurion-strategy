package asset

import (
	"container/list"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 缓存裁决（E/F，2026-10-21）：
//   - E：内存 LRU 一级 + 本地磁盘内容寻址二级；按容量与 TTL 驱逐。
//   - F：latest 用 TTL 300s；promote 响应携带新 hash 供本进程主动失效，
//     不引入 webhook / msgbus。
//
// 两种对象的失效语义不同，缓存里也分开：
//   - artifact 字节按 artifact_hash 内容寻址，**永不失效**（同 hash 同内容），
//     只按容量驱逐；TTL 对本层恒为 0，否则「离线跑历史回测」就不成立。
//   - latest 是可变引用，按 TTL 过期，promote/deprecate 时主动失效。
const DefaultLatestTTL = 300 * time.Second

// 默认容量：L1 32 MiB（单进程），L2 512 MiB（单机磁盘）。
const (
	DefaultMemoryCacheBytes int64 = 32 << 20
	DefaultDiskCacheBytes   int64 = 512 << 20
)

// CacheConfig 是两级缓存与 latest TTL 的装配面。
type CacheConfig struct {
	// Dir 为本地磁盘二级缓存的根目录；空串 = 只开 L1（内存）。
	Dir string
	// MemoryBytes / DiskBytes 是两级各自的容量上限（<=0 用默认值）。
	MemoryBytes int64
	DiskBytes   int64
	// LatestTTL 是 latest 解析结果的 TTL；<=0 用 DefaultLatestTTL。
	LatestTTL time.Duration
	// Now 仅测试注入（TTL 与磁盘 LRU 用）。
	Now func() time.Time
}

type memoryEntry struct {
	hash  string
	bytes []byte
}

type diskEntry struct {
	hash    string
	size    int64
	modTime time.Time
}

// Cache 是 artifact 字节的两级内容寻址缓存 + latest 解析结果缓存。
type Cache struct {
	mu        sync.Mutex
	lru       *list.List // front = 最近使用
	entries   map[string]*list.Element
	memory    int64
	memoryMax int64

	dir         string
	diskMax     int64
	diskUsed    int64
	diskTracked map[string]diskEntry

	latestMu  sync.Mutex
	latest    map[string]latestEntry
	latestTTL time.Duration
	now       func() time.Time
}

type latestEntry struct {
	result  ResolveResult
	expires time.Time
}

// NewCache 建缓存；Dir 非空时扫描已有文件（跨重启复用）并按容量先做一次驱逐。
func NewCache(cfg CacheConfig) (*Cache, error) {
	if cfg.MemoryBytes <= 0 {
		cfg.MemoryBytes = DefaultMemoryCacheBytes
	}
	if cfg.DiskBytes <= 0 {
		cfg.DiskBytes = DefaultDiskCacheBytes
	}
	if cfg.LatestTTL <= 0 {
		cfg.LatestTTL = DefaultLatestTTL
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	c := &Cache{
		lru:         list.New(),
		entries:     make(map[string]*list.Element),
		memoryMax:   cfg.MemoryBytes,
		dir:         strings.TrimSpace(cfg.Dir),
		diskMax:     cfg.DiskBytes,
		diskTracked: make(map[string]diskEntry),
		latest:      make(map[string]latestEntry),
		latestTTL:   cfg.LatestTTL,
		now:         cfg.Now,
	}
	if c.dir == "" {
		return c, nil
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return nil, fmt.Errorf("asset: cache dir: %w", err)
	}
	if err := c.scanDisk(); err != nil {
		return nil, err
	}
	if err := c.evictDisk(); err != nil {
		return nil, err
	}
	return c, nil
}

// Get 先查 L1，再查 L2；L2 命中会回填 L1。返回副本，调用方改不动缓存。
//
// L2 读出的字节必须重新算 sha256 与文件名（hash）一致：磁盘是可被外部改写的
// 介质，读到一个「名字对、内容错」的文件必须当 miss 并删掉，绝不返回。
func (c *Cache) Get(hash string) ([]byte, bool) {
	if !isLowerHex64(hash) {
		return nil, false
	}
	c.mu.Lock()
	if el, ok := c.entries[hash]; ok {
		c.lru.MoveToFront(el)
		raw := el.Value.(*memoryEntry).bytes
		out := append([]byte(nil), raw...)
		c.mu.Unlock()
		return out, true
	}
	c.mu.Unlock()

	if c.dir == "" {
		return nil, false
	}
	raw, err := os.ReadFile(c.path(hash))
	if err != nil {
		return nil, false
	}
	if sha256Hex(raw) != hash {
		_ = os.Remove(c.path(hash))
		c.forgetDisk(hash)
		return nil, false
	}
	_ = os.Chtimes(c.path(hash), c.now(), c.now())
	c.touchDisk(hash)
	c.putMemory(hash, raw)
	return append([]byte(nil), raw...), true
}

// Put 写两级缓存；hash 与字节不符 fail-closed（内容寻址的根）。
func (c *Cache) Put(hash string, bytes []byte) error {
	if !isLowerHex64(hash) {
		return errors.New("asset: cache key must be 64 lowercase hex")
	}
	if got := sha256Hex(bytes); got != hash {
		return fmt.Errorf("asset: cache key %s does not match bytes (%s)", hash, got)
	}
	c.putMemory(hash, bytes)
	if c.dir == "" || len(bytes) == 0 {
		return nil
	}
	path := c.path(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("asset: cache dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, bytes, 0o600); err != nil {
		return fmt.Errorf("asset: write cache entry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("asset: commit cache entry: %w", err)
	}
	c.mu.Lock()
	if prev, ok := c.diskTracked[hash]; ok {
		c.diskUsed -= prev.size
	}
	c.diskUsed += int64(len(bytes))
	c.diskTracked[hash] = diskEntry{hash: hash, size: int64(len(bytes)), modTime: c.now()}
	c.mu.Unlock()
	return c.evictDisk()
}

// Latest 读 latest 解析缓存；过期即删（TTL 300s）。
func (c *Cache) Latest(id string) (ResolveResult, bool) {
	c.latestMu.Lock()
	defer c.latestMu.Unlock()
	entry, ok := c.latest[id]
	if !ok {
		return ResolveResult{}, false
	}
	if !c.now().Before(entry.expires) {
		delete(c.latest, id)
		return ResolveResult{}, false
	}
	return cloneResolveResult(entry.result), true
}

// PutLatest 写 latest 解析缓存。
func (c *Cache) PutLatest(id string, result ResolveResult) {
	c.latestMu.Lock()
	defer c.latestMu.Unlock()
	c.latest[id] = latestEntry{result: cloneResolveResult(result), expires: c.now().Add(c.latestTTL)}
}

// InvalidateLatest 主动失效（promote / deprecate 的同进程效应）。
func (c *Cache) InvalidateLatest(id string) {
	c.latestMu.Lock()
	defer c.latestMu.Unlock()
	delete(c.latest, id)
}

func cloneResolveResult(r ResolveResult) ResolveResult {
	r.Capability = r.Capability.Clone()
	r.Imports = append([]string(nil), r.Imports...)
	return r
}

func (c *Cache) path(hash string) string {
	return filepath.Join(c.dir, hash[:2], hash)
}

func (c *Cache) putMemory(hash string, bytes []byte) {
	cp := append([]byte(nil), bytes...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[hash]; ok {
		entry := el.Value.(*memoryEntry)
		c.memory += int64(len(cp)) - int64(len(entry.bytes))
		entry.bytes = cp
		c.lru.MoveToFront(el)
	} else {
		el := c.lru.PushFront(&memoryEntry{hash: hash, bytes: cp})
		c.entries[hash] = el
		c.memory += int64(len(cp))
	}
	for c.memory > c.memoryMax && c.lru.Len() > 1 {
		back := c.lru.Back()
		if back == nil {
			break
		}
		entry := back.Value.(*memoryEntry)
		c.lru.Remove(back)
		delete(c.entries, entry.hash)
		c.memory -= int64(len(entry.bytes))
	}
}

func (c *Cache) scanDisk() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return filepath.Walk(c.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		hash := filepath.Base(path)
		if !isLowerHex64(hash) || strings.HasSuffix(hash, ".tmp") {
			// 非内容寻址文件（临时文件 / 外部杂物）不参与缓存。
			return nil
		}
		c.diskUsed += info.Size()
		c.diskTracked[hash] = diskEntry{hash: hash, size: info.Size(), modTime: info.ModTime()}
		return nil
	})
}

func (c *Cache) forgetDisk(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.diskTracked[hash]; ok {
		c.diskUsed -= entry.size
		delete(c.diskTracked, hash)
	}
}

func (c *Cache) touchDisk(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.diskTracked[hash]; ok {
		entry.modTime = c.now()
		c.diskTracked[hash] = entry
	}
}

// evictDisk 按 mtime 从旧到新删，直到总量回到上限内。
func (c *Cache) evictDisk() error {
	if c.dir == "" {
		return nil
	}
	c.mu.Lock()
	if c.diskUsed <= c.diskMax {
		c.mu.Unlock()
		return nil
	}
	entries := make([]diskEntry, 0, len(c.diskTracked))
	for _, e := range c.diskTracked {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].modTime.Equal(entries[j].modTime) {
			return entries[i].modTime.Before(entries[j].modTime)
		}
		return entries[i].hash < entries[j].hash
	})
	var firstErr error
	for _, e := range entries {
		if c.diskUsed <= c.diskMax {
			break
		}
		if err := os.Remove(c.path(e.hash)); err != nil && !errors.Is(err, os.ErrNotExist) {
			if firstErr == nil {
				firstErr = fmt.Errorf("asset: evict cache entry: %w", err)
			}
			continue
		}
		c.diskUsed -= e.size
		delete(c.diskTracked, e.hash)
	}
	c.mu.Unlock()
	return firstErr
}
