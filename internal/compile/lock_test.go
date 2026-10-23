package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sha256HexFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// 锁文件严格解码：未知字段拒收（混进看不懂的键 = 不是这份代码认识的格式）。
func TestLoadLockRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "toolchains.lock")
	require.NoError(t, os.WriteFile(path, []byte(`{"toolchains":[],"deployed_by":"someone"}`), 0o600))
	_, err := LoadLock(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode toolchain lock")
}

func TestLoadLockRejectsBadSha256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "toolchains.lock")
	bad := []byte(`{"toolchains":[{"name":"tinygo","version":"0.33.0","sha256":"zz"}]}`)
	require.NoError(t, os.WriteFile(path, bad, 0o600))
	_, err := LoadLock(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sha256")
}

// 钉住的 toolchain：版本在清单 + 二进制哈希一致 ⇒ 过锁。
func TestPinVerifyBinaryAcceptsPinnedBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tinygo")
	require.NoError(t, os.WriteFile(bin, []byte("fake toolchain bytes"), 0o700))

	lock := &LockFile{Toolchains: []Pin{{
		Name: "tinygo", Version: "0.33.0", SHA256: sha256HexFile(t, bin),
	}}}
	pin, err := lock.Pin("tinygo", "0.33.0")
	require.NoError(t, err)
	assert.NoError(t, pin.VerifyBinary(bin))
}

// 无锁不编译：名字或版本不在清单 ⇒ ErrToolchainNotPinned。
// caller 在命令行报什么版本都救不了——锁是部署物，不是调用参数。
func TestPinRejectsUnpinnedToolchain(t *testing.T) {
	lock := &LockFile{Toolchains: []Pin{{Name: "tinygo", Version: "0.33.0", SHA256: "00"}}}

	_, err := lock.Pin("tinygo", "0.34.0")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrToolchainNotPinned)

	_, err = lock.Pin("gccgo", "0.33.0")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrToolchainNotPinned)
}

// 二进制被换（哪怕版本号一样）⇒ ErrToolchainBinaryMismatch，与「没锁」分诊。
func TestPinRejectsSwappedBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tinygo")
	require.NoError(t, os.WriteFile(bin, []byte("original bytes"), 0o700))
	lock := &LockFile{Toolchains: []Pin{{
		Name: "tinygo", Version: "0.33.0", SHA256: sha256HexFile(t, bin),
	}}}
	pin, err := lock.Pin("tinygo", "0.33.0")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(bin, []byte("tampered bytes"), 0o700))
	err = pin.VerifyBinary(bin)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrToolchainBinaryMismatch)
	assert.NotErrorIs(t, err, ErrToolchainNotPinned)
}

// 二进制缺失当场失败，不给「文件读不到当没锁」留口子。
func TestPinRejectsMissingBinary(t *testing.T) {
	pin := Pin{Name: "tinygo", Version: "0.33.0", SHA256: "00"}
	err := pin.VerifyBinary(filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrToolchainBinaryMismatch)
}
