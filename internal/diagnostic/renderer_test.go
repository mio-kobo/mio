package diagnostic

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 测试 Unicode、Tab 与 CRLF 的稳定渲染
func TestRendererUnicodeGolden(t *testing.T) {
	source := "\t日本語 = 1;\r\n"
	start := SourceLocation{Offset: 1, Line: 1, Column: 2}
	end := SourceLocation{Offset: 1 + len("日本語"), Line: 1, Column: 5}
	d := NewError("E0301", "类型不匹配", Span{Start: start, End: end}).WithTypes("string", "int64").WithHelp("请修改赋值类型")
	d.Labels[0].Message = "名称"

	var output bytes.Buffer
	r := Renderer{Writer: &output, Source: source, Path: "main.mio", Color: ColorNever}
	if err := r.Render(d); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/unicode.golden")
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Fatalf("诊断输出不匹配\n实际:\n%s\n期望:\n%s", output.String(), want)
	}
}

// 测试跨行范围与多个标签
func TestRendererMultilineLabels(t *testing.T) {
	source := "first\nsecond\nthird"
	d := NewError("E0101", "跨行错误", Span{Start: SourceLocation{Offset: 2}, End: SourceLocation{Offset: 10}})
	d.WithLabel(Span{Start: SourceLocation{Offset: 13}, End: SourceLocation{Offset: 18}}, "相关位置")
	var output bytes.Buffer
	if err := (Renderer{Writer: &output, Source: source, Path: "multi.mio", Color: ColorNever}).Render(d); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"multi.mio:1:3", "1 | first", "2 | second", "3 | third", "相关位置"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("输出缺少 %q:\n%s", want, output.String())
		}
	}
}

// 测试颜色模式与重定向自动禁用颜色
func TestRendererColorModes(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	d := NewError("E0001", "错误", Span{})
	var plain bytes.Buffer
	if err := (Renderer{Writer: &plain, Source: "x", Color: ColorAuto}).Render(d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatal("重定向输出不应包含 ANSI 颜色")
	}
	var colored bytes.Buffer
	if err := (Renderer{Writer: &colored, Source: "x", Color: ColorAlways}).Render(d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[1;31m") {
		t.Fatal("always 模式应包含 ANSI 颜色")
	}
	t.Setenv("NO_COLOR", "1")
	colored.Reset()
	if err := (Renderer{Writer: &colored, Source: "x", Color: ColorAlways}).Render(d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(colored.String(), "\x1b[") {
		t.Fatal("NO_COLOR 必须禁用 ANSI 颜色")
	}
}

// 测试 CRLF 后文件末尾的位置
func TestRendererEOFPosition(t *testing.T) {
	source := "一;\r\n\t"
	offset := len(source)
	d := NewError("E0101", "缺少 ';'", Span{Start: SourceLocation{Offset: offset}, End: SourceLocation{Offset: offset}})
	var output bytes.Buffer
	if err := (Renderer{Writer: &output, Source: source, Path: "eof.mio", Color: ColorNever}).Render(d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "eof.mio:2:5") || !strings.Contains(output.String(), "2 |     ") {
		t.Fatalf("EOF 位置错误:\n%s", output.String())
	}
}

// 测试短格式与 JSON 字段
func TestRendererMachineFormats(t *testing.T) {
	d := NewError("E0201", "未定义的变量", Span{Start: SourceLocation{Offset: 2, Line: 1, Column: 3}, End: SourceLocation{Offset: 3}})
	var short bytes.Buffer
	if err := (Renderer{Writer: &short, Source: "  x", Path: "main.mio", Format: FormatShort}).Render(d); err != nil {
		t.Fatal(err)
	}
	if short.String() != "main.mio:1:3: error[E0201]: 未定义的变量\n" {
		t.Fatalf("短格式错误: %q", short.String())
	}

	var encoded bytes.Buffer
	if err := (Renderer{Writer: &encoded, Source: "  x", Path: "main.mio", Format: FormatJSON}).Render(d); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value["code"] != "E0201" || value["path"] != "main.mio" || strings.Contains(encoded.String(), "\x1b[") {
		t.Fatalf("JSON 输出错误: %s", encoded.String())
	}
}
