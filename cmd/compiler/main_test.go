package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tellurion-strategy/internal/compile"
)

// 无锁不编译：缺 --toolchain-lock 直接拒，job 都不读。
func TestRunRequiresToolchainLock(t *testing.T) {
	err := run([]string{
		"-job", "whatever.json",
		"-asset-url", "http://127.0.0.1:1",
		"-toolchain-name", "tinygo",
		"-toolchain-version", "0.33.0",
		"-toolchain-path", "tinygo",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-toolchain-lock is required")
}

// caller 报的版本不在锁清单里 ⇒ ErrToolchainNotPinned（锁是部署物，
// 不是调用参数）。
func TestRunRejectsUnpinnedVersion(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "toolchains.lock")
	sum := sha256.Sum256([]byte("x"))
	lockJSON := `{"toolchains":[{"name":"tinygo","version":"0.33.0","sha256":"` + hex.EncodeToString(sum[:]) + `"}]}`
	require.NoError(t, os.WriteFile(lockPath, []byte(lockJSON), 0o600))

	err := run([]string{
		"-job", "whatever.json",
		"-asset-url", "http://127.0.0.1:1",
		"-toolchain-name", "tinygo",
		"-toolchain-version", "0.99.0",
		"-toolchain-path", "tinygo",
		"-toolchain-lock", lockPath,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, compile.ErrToolchainNotPinned)
}

// 版本在清单但二进制对不上 ⇒ ErrToolchainBinaryMismatch。
func TestRunRejectsBinaryNotMatchingPin(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tinygo")
	require.NoError(t, os.WriteFile(bin, []byte("real pinned binary"), 0o700))
	lockPath := filepath.Join(dir, "toolchains.lock")
	sum := sha256.Sum256([]byte("real pinned binary"))
	lockJSON := `{"toolchains":[{"name":"tinygo","version":"0.33.0","sha256":"` + hex.EncodeToString(sum[:]) + `"}]}`
	require.NoError(t, os.WriteFile(lockPath, []byte(lockJSON), 0o600))
	require.NoError(t, os.WriteFile(bin, []byte("tampered"), 0o700))

	err := run([]string{
		"-job", "whatever.json",
		"-asset-url", "http://127.0.0.1:1",
		"-toolchain-name", "tinygo",
		"-toolchain-version", "0.33.0",
		"-toolchain-path", bin,
		"-toolchain-lock", lockPath,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, compile.ErrToolchainBinaryMismatch))
}
