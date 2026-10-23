// faketc 是 internal/compile 的测试用假工具链：它只做两件事 —— 把
// {src} 读出来加前缀写成 {out}，以及在 FAKETC_* 控制下制造失败/超时/超限。
//
// 独立可执行文件（而不是进程内 stub）是关键：Compile 的价值在于「真起子进程、
// 真过滤环境、真读回产物」，进程内打桩测不到这些。
package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	var src, out string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 < len(args) {
				out = args[i+1]
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "-") {
				src = args[i]
			}
		}
	}

	// 若环境过滤失效，这一行会出现在返回的编译日志里，测试据此判红。
	if secret := os.Getenv("TELLURION_TEST_SECRET"); secret != "" {
		fmt.Fprintf(os.Stdout, "leaked secret=%s\n", secret)
	}
	if n, _ := strconv.Atoi(os.Getenv("FAKETC_LOG")); n > 0 {
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("L"), n))
	}

	switch os.Getenv("FAKETC_MODE") {
	case "sleep":
		time.Sleep(10 * time.Second)
	case "fail":
		fmt.Fprintln(os.Stderr, "faketc: deliberate failure")
		os.Exit(2)
	case "noout":
		return
	}

	if src == "" || out == "" {
		fmt.Fprintln(os.Stderr, "faketc: need -o and a positional source")
		os.Exit(3)
	}
	source, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "faketc: read %s: %v\n", src, err)
		os.Exit(4)
	}
	data := append([]byte("artifact:"), source...)
	if n, _ := strconv.Atoi(os.Getenv("FAKETC_SIZE")); n > 0 {
		data = bytes.Repeat([]byte("A"), n)
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "faketc: write %s: %v\n", out, err)
		os.Exit(5)
	}
}
