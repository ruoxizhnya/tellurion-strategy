package asset

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// wasmImportSectionID 是 WebAssembly 二进制格式里的 import section id。
const wasmImportSectionID = 2

// WasmImports 从 wasm 字节的 import section 里抽出 `module.field` 列表（排序）。
//
// 为什么本仓自己解析而不是信 artifact 元数据里的 imports 列：元数据是**被校验
// 对象**，拿它验它自己等于没验。字节里的 import section 才是引擎实际会链接的
// 东西；两边不一致就 fail-closed（ADR-027 §2.6.1 的 Verify 门）。
func WasmImports(b []byte) ([]string, error) {
	r := &wasmReader{b: b}
	magic, err := r.bytes(4)
	if err != nil {
		return nil, fmt.Errorf("%w: wasm magic: %v", ErrInvalidArtifact, err)
	}
	if string(magic) != "\x00asm" {
		return nil, fmt.Errorf("%w: missing wasm magic", ErrInvalidArtifact)
	}
	version, err := r.bytes(4)
	if err != nil {
		return nil, fmt.Errorf("%w: wasm version: %v", ErrInvalidArtifact, err)
	}
	if binary.LittleEndian.Uint32(version) != 1 {
		return nil, fmt.Errorf("%w: unsupported wasm binary version %d", ErrInvalidArtifact,
			binary.LittleEndian.Uint32(version))
	}

	var imports []string
	for r.remaining() > 0 {
		id, err := r.u8()
		if err != nil {
			return nil, fmt.Errorf("%w: section id: %v", ErrInvalidArtifact, err)
		}
		size, err := r.u32()
		if err != nil {
			return nil, fmt.Errorf("%w: section %d size: %v", ErrInvalidArtifact, id, err)
		}
		payload, err := r.bytes(int(size))
		if err != nil {
			return nil, fmt.Errorf("%w: section %d payload: %v", ErrInvalidArtifact, id, err)
		}
		if int(id) != wasmImportSectionID {
			continue
		}
		imports, err = parseImportSection(payload)
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(imports)
	return imports, nil
}

func parseImportSection(payload []byte) ([]string, error) {
	r := &wasmReader{b: payload}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("%w: import count: %v", ErrInvalidArtifact, err)
	}
	out := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		module, err := r.name()
		if err != nil {
			return nil, err
		}
		field, err := r.name()
		if err != nil {
			return nil, err
		}
		kind, err := r.u8()
		if err != nil {
			return nil, fmt.Errorf("%w: import kind: %v", ErrInvalidArtifact, err)
		}
		if err := r.skipImportDesc(kind); err != nil {
			return nil, err
		}
		out = append(out, module+"."+field)
	}
	return out, nil
}

type wasmReader struct {
	b   []byte
	pos int
}

func (r *wasmReader) remaining() int { return len(r.b) - r.pos }

func (r *wasmReader) bytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.b) {
		return nil, fmt.Errorf("truncated (%d bytes wanted, %d left)", n, r.remaining())
	}
	out := r.b[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *wasmReader) u8() (byte, error) {
	b, err := r.bytes(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// u32 读 LEB128 无符号整数（wasm 的长度与索引编码）。
func (r *wasmReader) u32() (uint32, error) {
	var result uint32
	var shift uint
	for i := 0; i < 5; i++ {
		b, err := r.u8()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
	}
	return 0, fmt.Errorf("LEB128 u32 is longer than 5 bytes")
}

// name 读一个 vec(byte)：长度 + UTF-8 字节。
func (r *wasmReader) name() (string, error) {
	n, err := r.u32()
	if err != nil {
		return "", fmt.Errorf("%w: name length: %v", ErrInvalidArtifact, err)
	}
	b, err := r.bytes(int(n))
	if err != nil {
		return "", fmt.Errorf("%w: name: %v", ErrInvalidArtifact, err)
	}
	return string(b), nil
}

// skipImportDesc 按 import kind 跳过类型描述：
//
//	0x00 func → typeidx
//	0x01 table → reftype + limits
//	0x02 memory → limits
//	0x03 global → valtype + mut
//	0x04 tag → attribute + typeidx
func (r *wasmReader) skipImportDesc(kind byte) error {
	skipLimits := func() error {
		flags, err := r.u8()
		if err != nil {
			return err
		}
		if _, err := r.u32(); err != nil {
			return err
		}
		if flags&0x01 != 0 {
			if _, err := r.u32(); err != nil {
				return err
			}
		}
		return nil
	}
	switch kind {
	case 0x00:
		_, err := r.u32()
		return err
	case 0x01:
		if _, err := r.u8(); err != nil { // reftype
			return err
		}
		return skipLimits()
	case 0x02:
		return skipLimits()
	case 0x03:
		if _, err := r.u8(); err != nil { // valtype
			return err
		}
		_, err := r.u8() // mut
		return err
	case 0x04:
		if _, err := r.u8(); err != nil { // attribute
			return err
		}
		_, err := r.u32()
		return err
	default:
		return fmt.Errorf("%w: unknown import kind 0x%02x", ErrInvalidArtifact, kind)
	}
}
