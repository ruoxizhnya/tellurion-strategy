package asset

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"strings"
)

// validateSOExports 校验 carrier=so 的产物确实导出了定义里声明的入口符号。
//
// 只比体积与 sha256 回答不了「挂载时能不能找到入口」：一个字节完整、hash 正确
// 但 entry 拼错的 .so 会一直通过校验直到运行时才炸。这里用标准库 debug/elf
// 读符号表，要求：
//   - ET_DYN（共享对象），不是可执行文件 / 目标文件；
//   - entry 在 .dynsym（没有则退 .symtab）里，**已定义**（非 UND）；
//   - 绑定是 GLOBAL 或 WEAK（LOCAL 符号动态链接器看不到）；
//   - 类型是 FUNC 或 NOTYPE（C 导出常见 NOTYPE）。
func validateSOExports(raw []byte, entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return fmt.Errorf("%w: so artifact has no entry symbol to check", ErrInvalidArtifact)
	}
	file, err := elf.NewFile(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: so artifact is not a valid ELF: %v", ErrInvalidArtifact, err)
	}
	defer func() { _ = file.Close() }()

	if file.Type != elf.ET_DYN {
		return fmt.Errorf("%w: so artifact ELF type is %s, want ET_DYN", ErrInvalidArtifact, file.Type)
	}

	symbols, err := file.DynamicSymbols()
	if err != nil {
		if !errors.Is(err, elf.ErrNoSymbols) {
			return fmt.Errorf("%w: read dynamic symbols: %v", ErrInvalidArtifact, err)
		}
		symbols, err = file.Symbols()
		if err != nil && !errors.Is(err, elf.ErrNoSymbols) {
			return fmt.Errorf("%w: read symbols: %v", ErrInvalidArtifact, err)
		}
	}
	for _, sym := range symbols {
		if sym.Name != entry {
			continue
		}
		if sym.Section == elf.SHN_UNDEF {
			continue
		}
		bind := elf.ST_BIND(sym.Info)
		typ := elf.ST_TYPE(sym.Info)
		if bind != elf.STB_GLOBAL && bind != elf.STB_WEAK {
			continue
		}
		if typ != elf.STT_FUNC && typ != elf.STT_NOTYPE {
			continue
		}
		return nil
	}
	return fmt.Errorf("%w: so artifact does not export %q as a defined global function", ErrInvalidArtifact, entry)
}
