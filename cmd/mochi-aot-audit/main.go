// mochi-aot-audit 只读扫描 Mochi 指令，定位 Native AOT 自举的实现缺口。
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"Mio/internal/aot/bootstrapaudit"
	"Mio/internal/mochi"
)

const maxInputSize = 128 << 20

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("mochi-aot-audit", flag.ContinueOnError)
	flags.SetOutput(errOut)
	input := flags.String("in", "build/compiler-A.mochi", "需要扫描的 Mochi 文件")
	jsonPath := flags.String("json", "build/aot-bootstrap-audit.json", "JSON 报告路径；设置为空则不写文件")
	top := flags.Int("top", 20, "显示的主要缺口及函数数量")
	strict := flags.Bool("strict", false, "发现缺失 Opcode、外部函数或 ABI 限制则以状态码 3 退出")
	if parseErr := flags.Parse(args); parseErr != nil {
		if errors.Is(parseErr, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(flags.Args()) != 0 || *top < 0 || *top > 1000 {
		fmt.Fprintln(errOut, "参数错误：-top 应在 0..1000 之间，且不接受额外的位置参数")
		return 2
	}
	artifact, e := readMochi(*input)
	if e != nil {
		fmt.Fprintf(errOut, "读取失败：%v\n", e)
		return 1
	}
	program, e := mochi.Decode(bytes.NewReader(artifact))
	if e != nil {
		fmt.Fprintf(errOut, "解码 Mochi 失败：%v\n", e)
		return 1
	}
	if e = mochi.Validate(program); e != nil {
		fmt.Fprintf(errOut, "Mochi 结构校验失败：%v\n", e)
		return 1
	}
	report, e := bootstrapaudit.Analyze(program)
	if e != nil {
		fmt.Fprintf(errOut, "扫描失败：%v\n", e)
		return 1
	}
	report.ProgramSizeBytes = len(artifact)
	digest := sha256.Sum256(artifact)
	report.SourceSHA256 = hex.EncodeToString(digest[:])
	if len(artifact) >= 7 && string(artifact[:5]) == "MOCHI" {
		report.MochiVersion = int(binary.BigEndian.Uint16(artifact[5:7]))
	}
	if *jsonPath != "" {
		if e = writeJSON(*jsonPath, report); e != nil {
			fmt.Fprintf(errOut, "无法写入报告：%v\n", e)
			return 1
		}
	}
	printReport(out, *input, *jsonPath, report, *top)
	if *strict && (report.MissingInstructions != 0 || len(report.NativeCandidates) != 0 || len(report.StructuralIssues) != 0) {
		return 3
	}
	return 0
}

func readMochi(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 %q: %w", path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxInputSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取 %q: %w", path, err)
	}
	if len(data) > maxInputSize {
		return nil, fmt.Errorf("Mochi 输入超过 %d MiB 上限", maxInputSize>>20)
	}
	return data, nil
}

func writeJSON(path string, r *bootstrapaudit.Report) error {
	data, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	data = append(data, '\n')
	parent := filepath.Dir(path)
	temp, e := os.CreateTemp(parent, ".aot-bootstrap-audit-*")
	if e != nil {
		return e
	}
	defer os.Remove(temp.Name())
	if _, e = temp.Write(data); e != nil {
		temp.Close()
		return e
	}
	if e = temp.Chmod(0644); e != nil {
		temp.Close()
		return e
	}
	if e = temp.Close(); e != nil {
		return e
	}
	return os.Rename(temp.Name(), path)
}

func printReport(w io.Writer, input, jsonPath string, r *bootstrapaudit.Report, top int) {
	fmt.Fprintln(w, "===== Mio Native AOT 自举兼容性扫描 =====")
	fmt.Fprintf(w, "输入：%s\nSHA256：%s\n", input, r.SourceSHA256)
	if r.MochiVersion > 0 {
		fmt.Fprintf(w, "Mochi 格式版本：v%d\n", r.MochiVersion)
	}
	fmt.Fprintf(w, "字节数：%d　函数数：%d　全局槽：%d　最大函数局部槽：%d\n", r.ProgramSizeBytes, r.FunctionCount, r.GlobalsCount, r.MaxLocalSlots)
	fmt.Fprintf(w, "带 Captures 的函数：%d　无函数体的函数：%d\n", r.FunctionsWithCaptures, r.FunctionsWithoutBody)
	fmt.Fprintf(w, "字节码：%d 条；不同 Opcode：%d 种\n", r.TotalInstructions, r.DistinctOpcodes)
	fmt.Fprintf(w, "已有原生处理路径：%d；受类型/范围限制：%d；缺少当前实现：%d\n", r.LoweredInstructions, r.LimitedInstructions, r.MissingInstructions)
	fmt.Fprintln(w, "注意：这些是静态出现次数，不是 Native 自举完成度或实际可编译率。")
	fmt.Fprintln(w, "\n===== 常量类型（总数 / 已引用条目数） =====")
	for _, c := range r.Constants {
		fmt.Fprintf(w, "%-16s %5d / %5d\n", c.Kind, c.Count, c.Referenced)
	}

	missing := make([]bootstrapaudit.OpcodeStat, 0)
	limited := make([]bootstrapaudit.OpcodeStat, 0)
	for _, op := range r.Opcodes {
		if op.Missing > 0 {
			missing = append(missing, op)
		} else if op.Limited > 0 {
			limited = append(limited, op)
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Missing != missing[j].Missing {
			return missing[i].Missing > missing[j].Missing
		}
		return missing[i].Opcode < missing[j].Opcode
	})
	sort.Slice(limited, func(i, j int) bool {
		if limited[i].Limited != limited[j].Limited {
			return limited[i].Limited > limited[j].Limited
		}
		return limited[i].Opcode < limited[j].Opcode
	})
	fmt.Fprintln(w, "\n===== 尚缺实现的 Opcode（按出现次数排序） =====")
	if len(missing) == 0 {
		fmt.Fprintln(w, "未发现明确缺失的 Opcode（仍有类型与 Runtime 限制）")
	}
	for i, op := range missing {
		if i >= top {
			fmt.Fprintf(w, "... 其余 %d 项详见 JSON\n", len(missing)-top)
			break
		}
		fmt.Fprintf(w, "%-22s 缺口 %6d / 出现 %6d  类别=%s\n", op.Name, op.Missing, op.Count, op.Category)
		for _, site := range op.ExampleSites {
			if site.Reason != "" {
				fmt.Fprintf(w, "    %s @%d: %s\n", site.Block, site.Offset, site.Reason)
			}
		}
	}
	fmt.Fprintln(w, "\n===== 已有路径但仍受限的 Opcode =====")
	if len(limited) == 0 {
		fmt.Fprintln(w, "无")
	}
	for i, op := range limited {
		if i >= top {
			fmt.Fprintf(w, "... 其余 %d 项详见 JSON\n", len(limited)-top)
			break
		}
		fmt.Fprintf(w, "%-22s %6d  %s\n", op.Name, op.Limited, op.Notes)
	}
	funcs := append([]bootstrapaudit.FunctionStat(nil), r.Functions...)
	sort.Slice(funcs, func(i, j int) bool {
		if funcs[i].Missing != funcs[j].Missing {
			return funcs[i].Missing > funcs[j].Missing
		}
		if funcs[i].Limited != funcs[j].Limited {
			return funcs[i].Limited > funcs[j].Limited
		}
		return funcs[i].FunctionID < funcs[j].FunctionID
	})
	fmt.Fprintln(w, "\n===== 缺口最多的函数 / 指令块 =====")
	for i, f := range funcs {
		if i >= top {
			break
		}
		fmt.Fprintf(w, "[%d] %-38s 缺口=%d 受限=%d 总数=%d\n", f.FunctionID, shorten(f.Name, 38), f.Missing, f.Limited, f.Instructions)
	}
	fmt.Fprintf(w, "\n结构性 ABI/资源缺口：%d 项\n", len(r.StructuralIssues))
	for i, issue := range r.StructuralIssues {
		if i >= top {
			fmt.Fprintf(w, "... 其余 %d 项详见 JSON\n", len(r.StructuralIssues)-top)
			break
		}
		fmt.Fprintf(w, "  %s: %s=%d (上限=%d) — %s\n", shorten(issue.Block, 38), issue.Kind, issue.Value, issue.Limit, issue.Reason)
	}
	fmt.Fprintf(w, "\n潜在外部/MMA 函数：%d 项\n", len(r.NativeCandidates))
	for i, c := range r.NativeCandidates {
		if i >= top {
			fmt.Fprintf(w, "... 其余 %d 项详见 JSON\n", len(r.NativeCandidates)-top)
			break
		}
		fmt.Fprintf(w, "  [%d] %s：%s\n", c.FunctionID, shorten(c.Name, 72), c.Reason)
	}
	fmt.Fprintln(w, "\n===== 解读边界 =====")
	for _, msg := range r.Notices {
		fmt.Fprintln(w, "- "+msg)
	}
	if jsonPath != "" {
		fmt.Fprintf(w, "\n完整机器可读报告：%s\n", jsonPath)
	}
}

func shorten(s string, n int) string {
	if n < 4 {
		return s
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n-3]) + "..."
}
