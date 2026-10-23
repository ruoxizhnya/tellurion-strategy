package asset

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// 造一个最小可被 debug/elf 读出的 ET_DYN：ELF header + .dynstr/.dynsym/.text/.shstrtab。
// 符号表由测试控制，避免把二进制 fixture 进库。
type testSymbol struct {
	name  string
	info  byte
	shndx uint16
}

func buildSharedObject(symbols []testSymbol) []byte {
	dynstr := []byte{0}
	nameOffset := make(map[string]uint32, len(symbols))
	for _, sym := range symbols {
		if _, ok := nameOffset[sym.name]; ok {
			continue
		}
		nameOffset[sym.name] = uint32(len(dynstr))
		dynstr = append(dynstr, []byte(sym.name)...)
		dynstr = append(dynstr, 0)
	}
	dynsym := make([]byte, 24*(len(symbols)+1))
	for i, sym := range symbols {
		off := 24 * (i + 1)
		binary.LittleEndian.PutUint32(dynsym[off:], nameOffset[sym.name])
		dynsym[off+4] = sym.info
		binary.LittleEndian.PutUint16(dynsym[off+6:], sym.shndx)
	}
	text := []byte{0xc3}

	shstr := []byte{0}
	addName := func(s string) uint32 {
		off := uint32(len(shstr))
		shstr = append(shstr, []byte(s)...)
		shstr = append(shstr, 0)
		return off
	}
	nameDynstr := addName(".dynstr")
	nameDynsym := addName(".dynsym")
	nameText := addName(".text")
	nameShstr := addName(".shstrtab")

	align8 := func(n int) int { return (n + 7) &^ 7 }
	out := make([]byte, 64)
	out[0], out[1], out[2], out[3] = 0x7f, 'E', 'L', 'F'
	out[4] = 2                                          // ELFCLASS64
	out[5] = 1                                          // ELFDATA2LSB
	out[6] = 1                                          // EV_CURRENT
	binary.LittleEndian.PutUint16(out[16:], uint16(3))  // ET_DYN
	binary.LittleEndian.PutUint16(out[18:], uint16(62)) // EM_X86_64
	binary.LittleEndian.PutUint32(out[20:], 1)          // EV_CURRENT
	binary.LittleEndian.PutUint16(out[52:], 64)         // e_ehsize
	binary.LittleEndian.PutUint16(out[58:], 64)         // e_shentsize
	binary.LittleEndian.PutUint16(out[60:], 5)          // e_shnum

	strOff := align8(len(out))
	out = append(out, make([]byte, strOff-len(out))...)
	out = append(out, dynstr...)
	symOff := align8(len(out))
	out = append(out, make([]byte, symOff-len(out))...)
	out = append(out, dynsym...)
	textOff := align8(len(out))
	out = append(out, make([]byte, textOff-len(out))...)
	out = append(out, text...)
	shstrOff := len(out)
	out = append(out, shstr...)
	shoff := align8(len(out))
	out = append(out, make([]byte, shoff-len(out))...)
	out = append(out, make([]byte, 5*64)...)
	binary.LittleEndian.PutUint64(out[40:], uint64(shoff)) // e_shoff

	writeSection := func(idx int, name uint32, typ uint32, flags uint64, off, size int,
		link, info uint32, align, entsize uint64) {
		base := shoff + idx*64
		binary.LittleEndian.PutUint32(out[base:], name)
		binary.LittleEndian.PutUint32(out[base+4:], typ)
		binary.LittleEndian.PutUint64(out[base+8:], flags)
		binary.LittleEndian.PutUint64(out[base+24:], uint64(off))
		binary.LittleEndian.PutUint64(out[base+32:], uint64(size))
		binary.LittleEndian.PutUint32(out[base+40:], link)
		binary.LittleEndian.PutUint32(out[base+44:], info)
		binary.LittleEndian.PutUint64(out[base+48:], align)
		binary.LittleEndian.PutUint64(out[base+56:], entsize)
	}
	writeSection(1, nameDynstr, 3, 0, strOff, len(dynstr), 0, 0, 1, 0)
	writeSection(2, nameDynsym, 11, 0, symOff, len(dynsym), 1, 1, 8, 24)
	writeSection(3, nameText, 1, 0x6, textOff, len(text), 0, 0, 16, 0)
	writeSection(4, nameShstr, 3, 0, shstrOff, len(shstr), 0, 0, 1, 0)
	return out
}

const (
	globalFunc byte = 0x12 // STB_GLOBAL | STT_FUNC
	weakFunc   byte = 0x22 // STB_WEAK   | STT_FUNC
	localFunc  byte = 0x02 // STB_LOCAL  | STT_FUNC
	globalObj  byte = 0x11 // STB_GLOBAL | STT_OBJECT
	globalNone byte = 0x10 // STB_GLOBAL | STT_NOTYPE
)

func TestValidateSOExports(t *testing.T) {
	cases := []struct {
		name    string
		symbols []testSymbol
		entry   string
		wantErr bool
	}{
		{"global function", []testSymbol{{"Strategy", globalFunc, 3}}, "Strategy", false},
		{"weak function", []testSymbol{{"Strategy", weakFunc, 3}}, "Strategy", false},
		{"global notype", []testSymbol{{"Strategy", globalNone, 3}}, "Strategy", false},
		{"missing entry", []testSymbol{{"Other", globalFunc, 3}}, "Strategy", true},
		{"local symbol", []testSymbol{{"Strategy", localFunc, 3}}, "Strategy", true},
		{"undefined symbol", []testSymbol{{"Strategy", globalFunc, 0}}, "Strategy", true},
		{"data symbol", []testSymbol{{"Strategy", globalObj, 3}}, "Strategy", true},
		{"empty entry", []testSymbol{{"Strategy", globalFunc, 3}}, "  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSOExports(buildSharedObject(tc.symbols), tc.entry)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidArtifact) {
					t.Fatalf("got %v, want ErrInvalidArtifact", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
	t.Run("not an ELF", func(t *testing.T) {
		if err := validateSOExports(wasmWithImports("env.get_bar"), "Strategy"); !errors.Is(err, ErrInvalidArtifact) {
			t.Fatalf("got %v, want ErrInvalidArtifact", err)
		}
	})
}

func TestVerify_SORequiresDeclaredExport(t *testing.T) {
	raw := buildSharedObject([]testSymbol{{"OnBar", globalFunc, 3}})
	a := Artifact{
		StrategyID: "so1", VersionHash: strings.Repeat("a", 64), ArtifactHash: ArtifactHash(raw),
		Carrier: CarrierSO, ABIVersion: "on_bar/2", Toolchain: "gcc 13",
		SizeBytes: int64(len(raw)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: raw,
	}
	if err := Verify(a, "OnBar"); err != nil {
		t.Fatalf("valid so rejected: %v", err)
	}
	if err := Verify(a, "Strategy"); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("entry mismatch must fail closed, got %v", err)
	}
}
