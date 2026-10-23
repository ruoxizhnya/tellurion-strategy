package compile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tellurion-strategy/internal/asset"
)

// 真实的 TinyGo 工具链锁定值（2026-10-24 从官方镜像量的，不是占位值）：
//
//	image:            tinygo/tinygo:0.34.0
//	image digest:     sha256:79c73c4246a1079e648d6e8425bae4654f26d2baed24c96537a8893b41c04d26
//	binary:           /usr/local/tinygo/bin/tinygo
//	binary sha256:    14408311f6cd58c61d406776417cb8dd07728aa4f00ededfbdd1a544405d2394
const (
	tinyGoName           = "tinygo"
	tinyGoVersion        = "0.34.0"
	tinyGoBinarySHA256   = "14408311f6cd58c61d406776417cb8dd07728aa4f00ededfbdd1a544405d2394"
	tinyGoArtifactSHA256 = "fd0ecc264521683eaa7ba0819cb9d41a187e54fc5bcfb2332fe0e9aa1f9f7aea"
	tinyGoArtifactBytes  = 270
)

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// 部署侧锁文件必须是真实值：名字+版本能取到，sha256 与官方镜像里的二进制一致。
func TestPinnedToolchainLockHasRealTinyGo(t *testing.T) {
	lock, err := LoadLock("toolchains.lock")
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	pin, err := lock.Pin(tinyGoName, tinyGoVersion)
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if !strings.EqualFold(pin.SHA256, tinyGoBinarySHA256) {
		t.Fatalf("pinned sha256 = %s, want %s", pin.SHA256, tinyGoBinarySHA256)
	}
}

// 离线可复现：仓库里保存了真镜像产出的 artifact，任何人只用 sha256 就能核对
// 它的 artifact_hash；同时它必须过得了本仓的 Verify 门。
func TestPinnedTinyGoArtifactIsOfflineVerifiable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "tinygo", "artifact.wasm"))
	if err != nil {
		t.Fatalf("read pinned artifact: %v", err)
	}
	if len(raw) != tinyGoArtifactBytes {
		t.Fatalf("artifact is %d bytes, want %d", len(raw), tinyGoArtifactBytes)
	}
	if got := sha256Hex(raw); got != tinyGoArtifactSHA256 {
		t.Fatalf("artifact sha256 = %s, want %s", got, tinyGoArtifactSHA256)
	}
	if got := asset.ArtifactHash(raw); got != tinyGoArtifactSHA256 {
		t.Fatalf("contracts.ArtifactHash = %s, want %s", got, tinyGoArtifactSHA256)
	}
	a := asset.Artifact{
		StrategyID: "tinygo-pinned", VersionHash: strings.Repeat("a", 64),
		ArtifactHash: tinyGoArtifactSHA256, Carrier: asset.CarrierWASM, ABIVersion: "on_bar/2",
		Toolchain: tinyGoName + " " + tinyGoVersion, SizeBytes: int64(len(raw)),
		MemoryLimitBytes: 64 << 20, Status: asset.StatusBuilt, Storage: asset.StorageInline,
		Bytes: raw,
	}
	if err := asset.Verify(a, "OnBar"); err != nil {
		t.Fatalf("pinned artifact does not pass Verify: %v", err)
	}
}

// 真镜像编译：需要本机有 docker 且允许拉/用 tinygo/tinygo:0.34.0。
// 默认 Skip；显式 STRATEGY_TINYGO_IMAGE=tinygo/tinygo:0.34.0 时运行。
func TestCompile_RealTinyGoImage(t *testing.T) {
	image := os.Getenv("STRATEGY_TINYGO_IMAGE")
	if strings.TrimSpace(image) == "" {
		t.Skip("STRATEGY_TINYGO_IMAGE is not set; skipping the real toolchain test")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join("testdata", "tinygo", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), source, 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := contextWithTimeout(t, 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	// 镜像里的二进制 sha256 必须与 toolchains.lock 一致。
	out := run("run", "--rm", image, "sha256sum", "/usr/local/tinygo/bin/tinygo")
	if !strings.Contains(strings.ToLower(out), tinyGoBinarySHA256) {
		t.Fatalf("image binary does not match the lock file:\n%s", out)
	}

	compile := func(name string) []byte {
		t.Helper()
		run("run", "--rm", "--network", "none",
			"-v", dir+":/work", "-w", "/work", image,
			"tinygo", "build", "-no-debug", "-target=wasm-unknown", "-o", "/work/"+name, "main.go")
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return raw
	}
	first := compile("artifact-1.wasm")
	second := compile("artifact-2.wasm")
	if sha256Hex(first) != sha256Hex(second) {
		t.Fatalf("same source + same pinned image produced different bytes:\n%s\n%s",
			sha256Hex(first), sha256Hex(second))
	}
	if got := sha256Hex(first); got != tinyGoArtifactSHA256 {
		t.Fatalf("artifact sha256 = %s, want the pinned %s", got, tinyGoArtifactSHA256)
	}
	if len(first) != tinyGoArtifactBytes {
		t.Fatalf("artifact is %d bytes, want %d", len(first), tinyGoArtifactBytes)
	}

	// 真产物必须过资产侧的门，并且 imports 为空（bare wasm-unknown 不引 host）。
	if err := asset.Verify(asset.Artifact{
		StrategyID: "tinygo-real", VersionHash: strings.Repeat("a", 64),
		ArtifactHash: tinyGoArtifactSHA256, Carrier: asset.CarrierWASM, ABIVersion: "on_bar/2",
		Toolchain: tinyGoName + " " + tinyGoVersion, SizeBytes: int64(len(first)),
		MemoryLimitBytes: 64 << 20, Status: asset.StatusBuilt, Storage: asset.StorageInline,
		Bytes: first,
	}, "OnBar"); err != nil {
		t.Fatalf("real tinygo artifact does not pass Verify: %v", err)
	}
	imports, err := asset.WasmImports(first)
	if err != nil {
		t.Fatalf("WasmImports: %v", err)
	}
	if len(imports) != 0 {
		t.Fatalf("bare wasm-unknown artifact must not import host functions, got %v", imports)
	}
}

// contextWithTimeout 只做测试超时，避免 docker 卡住时整个包不退出。
func contextWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), d)
}
