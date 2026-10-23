package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// toolchain 锁定（QT-41b D 裁决「toolchain 镜像固定版本」的可执行形态）：
//
// `toolchains.lock` 是部署侧的固定清单——名字 + 版本 + **二进制 sha256**。
// 编译进程启动时必须先过锁：清单里没有这个 名字+版本 ⇒ 不编译；版本匹配但
// 二进制字节对不上 ⇒ 不编译。caller 自己在命令行报一个版本字符串不算「锁」
// ——锁是部署物的一部分，不是调用参数。
var (
	// ErrToolchainNotPinned 是 名字+版本 不在锁清单里。
	ErrToolchainNotPinned = errors.New("compile: toolchain is not pinned in the lock file")
	// ErrToolchainBinaryMismatch 是 版本在清单里但二进制 sha256 对不上
	//（装错了版本 / 二进制被换）。与「没锁」必须分开：前者改清单，后者查机器。
	ErrToolchainBinaryMismatch = errors.New("compile: toolchain binary does not match the pinned sha256")
)

// Pin 是锁清单的一条记录。
type Pin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// SHA256 是 toolchain 二进制的 sha256（hex，大小写不敏感）。
	SHA256 string `json:"sha256"`
}

// LockFile 是 `toolchains.lock` 的形状。
type LockFile struct {
	Toolchains []Pin `json:"toolchains"`
}

// LoadLock 读锁文件。严格解码：未知字段一律拒——锁文件里混进看不懂的
// 键说明它不是这份代码认识的格式，静默忽略等于锁了个寂寞。
func LoadLock(path string) (*LockFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("compile: read toolchain lock: %w", err)
	}
	var lock LockFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lock); err != nil {
		return nil, fmt.Errorf("compile: decode toolchain lock: %w", err)
	}
	for i, p := range lock.Toolchains {
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Version) == "" {
			return nil, fmt.Errorf("compile: toolchains[%d] has empty name or version", i)
		}
		if _, err := hex.DecodeString(p.SHA256); err != nil || len(p.SHA256) != 64 {
			return nil, fmt.Errorf("compile: toolchains[%d] (%s %s) sha256 is not 64 hex chars", i, p.Name, p.Version)
		}
	}
	return &lock, nil
}

// Pin 按名字+版本取锁记录；没有 ⇒ ErrToolchainNotPinned。
func (l *LockFile) Pin(name, version string) (Pin, error) {
	for _, p := range l.Toolchains {
		if p.Name == name && p.Version == version {
			return p, nil
		}
	}
	return Pin{}, fmt.Errorf("%w: %s %s", ErrToolchainNotPinned, name, version)
}

// VerifyBinary 核对二进制的 sha256 与锁记录一致。
func (p Pin) VerifyBinary(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("compile: open pinned toolchain binary: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("compile: hash pinned toolchain binary: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, p.SHA256) {
		return fmt.Errorf("%w: %s %s binary hashes to %s, lock says %s",
			ErrToolchainBinaryMismatch, p.Name, p.Version, got, p.SHA256)
	}
	return nil
}
