package asset

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// ImportPolicy 是 import 白名单：按声明的 abi_version 从 contracts 的冻结函数表
// 派生，不在本仓手抄一份函数名清单（手抄必然漂移）。
type ImportPolicy struct {
	allowed map[string]struct{}
}

// ImportPolicyFor 返回 abi_version 对应的白名单。
//
// 接受两种写法：裸函数名（`get_bar`）与 `env.` 前缀（`env.get_bar`）——wasm
// import section 里带模块名，而 artifact 元数据可能只存函数名；两种都算同一件事。
func ImportPolicyFor(abiVersion string) (ImportPolicy, error) {
	version, err := contracts.HostAbiVersionFromName(abiVersion)
	if err != nil {
		return ImportPolicy{}, err
	}
	allowed := make(map[string]struct{})
	for _, fn := range contracts.HostABIContracts() {
		if fn.Since > version {
			continue
		}
		allowed[fn.Name] = struct{}{}
		allowed["env."+fn.Name] = struct{}{}
	}
	return ImportPolicy{allowed: allowed}, nil
}

// Validate 校验 imports 全在白名单内；返回越界项（排序、去重）。
func (p ImportPolicy) Validate(imports []string) error {
	if len(p.allowed) == 0 {
		return fmt.Errorf("%w: empty import policy", ErrInvalidArtifact)
	}
	var bad []string
	seen := make(map[string]struct{}, len(imports))
	for _, raw := range imports {
		name := strings.TrimSpace(raw)
		if name == "" {
			bad = append(bad, raw)
			continue
		}
		if _, ok := p.allowed[name]; ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		bad = append(bad, name)
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("%w: %s", contracts.ErrImportNotWhitelisted, strings.Join(bad, ", "))
}

// Verify 是五道门里的 Verify：字节 → 校验结论。
//
// 失败一律 fail-closed，不许「校验不过就先跑起来」。检查项与设计 §4 第 3 门一致：
// 字节 hash、体积、内存上限、imports 白名单、abi_version 可解析。
//
// wasm 的 imports **从字节里解析**（WasmImports），再与元数据声明比对：
// 拿元数据验元数据等于没验。so 载体校验导出符号表（entry 必须是一个已定义的
// GLOBAL/WEAK 符号）：只比体积和 hash 回答不了「挂载时能不能找到入口」。
func Verify(a Artifact, entry string) error {
	if a.Storage == StorageURL {
		return fmt.Errorf("%w: artifact %s uses storage=url", ErrStorageUnsupported, a.ArtifactHash)
	}
	if len(a.Bytes) == 0 {
		return fmt.Errorf("%w: artifact %s has no bytes", ErrArtifactFetchFailed, a.ArtifactHash)
	}
	size := int64(len(a.Bytes))
	if size > MaxArtifactBytes {
		return fmt.Errorf("%w: %d bytes > %d", ErrArtifactTooLarge, size, MaxArtifactBytes)
	}
	if a.SizeBytes != 0 && a.SizeBytes != size {
		return fmt.Errorf("%w: size_bytes %d != actual %d", ErrInvalidArtifact, a.SizeBytes, size)
	}
	if got := sha256Hex(a.Bytes); got != a.ArtifactHash {
		return fmt.Errorf("%w: got %s want %s", ErrArtifactHashMismatch, got, a.ArtifactHash)
	}
	if _, err := contracts.HostAbiVersionFromName(a.ABIVersion); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidABIVersion, err)
	}
	if a.MemoryLimitBytes <= 0 || a.MemoryLimitBytes > MaxMemoryLimitBytes {
		return fmt.Errorf("%w: memory_limit_bytes %d out of (0, %d]",
			ErrInvalidArtifact, a.MemoryLimitBytes, MaxMemoryLimitBytes)
	}
	if err := normalizeImports(&a); err != nil {
		return err
	}
	if a.Carrier == CarrierSO {
		return validateSOExports(a.Bytes, entry)
	}
	if a.Carrier != CarrierWASM {
		return nil
	}
	policy, err := ImportPolicyFor(a.ABIVersion)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidABIVersion, err)
	}
	return policy.Validate(a.Imports)
}

// normalizeImports 让 artifact.Imports 以字节里的 import section 为准。
//
// 声明为空 = 由本函数回填；声明非空但与字节不一致 = fail-closed（元数据在撒谎）。
func normalizeImports(a *Artifact) error {
	if a.Carrier != CarrierWASM {
		return nil
	}
	derived, err := WasmImports(a.Bytes)
	if err != nil {
		return err
	}
	if len(a.Imports) > 0 && !sameStringSet(a.Imports, derived) {
		return fmt.Errorf("%w: declared imports %v do not match the wasm import section %v",
			ErrInvalidArtifact, a.Imports, derived)
	}
	a.Imports = derived
	return nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

// builtinEntryPattern 是 builtin 入口名的符号规则：运行时要拿它去二进制里找
// 已注册的实现，所以必须是一个标识符（允许 `.` 分隔的命名空间写法）。
var builtinEntryPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// validateBuiltinActivation 校验 builtin 的「可激活」契约（ADR-0006）。
//
// builtin 是编译进二进制的内置实现指针：不产 artifact 是它的本质。因此这里
// 不要求字节码，也不发明源语言，只钉住三件可机械检查的事：
//   - entry 是合法符号名（运行时按名字解析实现）；
//   - 版本记录与定义形状一致（carrier / abi_version / scope / entry 不许漂移）；
//   - capability 声明闭合（无空白项、无重复项），source 是合法 UTF-8 文本
//     （builtin 的 source 是登记说明/指针，不是可执行代码；「能解析」= 不是乱码）。
func validateBuiltinActivation(def Definition, version Version) error {
	if def.Carrier != CarrierBuiltin {
		return fmt.Errorf("%w: carrier %s has no builtin activation path", ErrUnsupportedCarrier, def.Carrier)
	}
	if err := def.Validate(); err != nil {
		return err
	}
	if !builtinEntryPattern.MatchString(def.Entry) {
		return fmt.Errorf("%w: builtin entry %q is not a valid symbol name", ErrInvalidDefinition, def.Entry)
	}
	if version.Carrier != def.Carrier || version.Entry != def.Entry ||
		version.ABIVersion != def.ABIVersion || version.Scope != def.Scope {
		return fmt.Errorf("%w: version %s does not match the definition shape",
			ErrInvalidDefinition, version.VersionHash)
	}
	if err := validateCapabilityClosed(def.Capability); err != nil {
		return err
	}
	return validateBuiltinSource(version.Source)
}

func validateCapabilityClosed(c Capability) error {
	groups := []struct {
		name   string
		values []string
	}{
		{"frequencies", c.Frequencies},
		{"markets", c.Markets},
		{"features", c.Features},
	}
	for _, group := range groups {
		seen := make(map[string]struct{}, len(group.values))
		for _, raw := range group.values {
			v := strings.TrimSpace(raw)
			if v == "" {
				return fmt.Errorf("%w: capability.%s contains a blank entry", ErrInvalidDefinition, group.name)
			}
			if _, dup := seen[v]; dup {
				return fmt.Errorf("%w: capability.%s repeats %q", ErrInvalidDefinition, group.name, v)
			}
			seen[v] = struct{}{}
		}
	}
	return nil
}

func validateBuiltinSource(source string) error {
	if !utf8.ValidString(source) {
		return fmt.Errorf("%w: builtin source is not valid UTF-8", ErrInvalidDefinition)
	}
	if strings.ContainsRune(source, 0) {
		return fmt.Errorf("%w: builtin source contains a NUL byte", ErrInvalidDefinition)
	}
	if strings.TrimSpace(source) == "" {
		return fmt.Errorf("%w: builtin source is required (it records the in-binary implementation pointer)", ErrInvalidDefinition)
	}
	return nil
}
