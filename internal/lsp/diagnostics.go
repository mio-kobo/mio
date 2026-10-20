package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"Mio/internal/ast"
	"Mio/internal/bunko"
	"Mio/internal/diagnostic"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/stdlib"
	"Mio/internal/token"
	"Mio/mma"
)

const diagnosticSource = "mio"

// analyzer 复用标准库注册表，把源码转换为 LSP 诊断。
type analyzer struct {
	bunkoRoot string

	once     sync.Once
	registry *mma.ModuleRegistry
	initErr  error
}

func newAnalyzer(bunkoRoot string) *analyzer {
	return &analyzer{bunkoRoot: bunkoRoot}
}

func (a *analyzer) modules() (*mma.ModuleRegistry, error) {
	a.once.Do(func() {
		a.registry, a.initErr = stdlib.NewRegistry()
	})
	return a.registry, a.initErr
}

// diagnostics 运行与 mio check 相同的静态流水线，最多返回一条诊断。
// 上下文取消时返回 nil，调用方不得发布结果。
// 它复用 analyze：诊断与语义导航来自同一次带版本的快照构建。
func (a *analyzer) diagnostics(ctx context.Context, doc *document) []Diagnostic {
	result := a.analyze(ctx, doc, bunko.NewOverlay(), nil)
	if result == nil {
		return nil
	}
	return result.Diagnostics
}

// parseSource 直接解析单个源码缓冲区，不经过 Bunko 包合并。
func parseSource(text string) (*ast.Program, error) {
	lex, err := lexer.NewChecked(text, token.V05)
	if err != nil {
		return nil, err
	}
	program, err := parser.New(lex).ParseProgram()
	if err != nil {
		return nil, err
	}
	if err := lex.Err(); err != nil {
		return nil, err
	}
	return program, nil
}

// bunkoRootFor 与 CLI 保持一致：MIO_BUNKO > 初始化选项 > 向上查找 bunko/ > 相对路径。
func bunkoRootFor(sourcePath, override string) string {
	if configured := os.Getenv("MIO_BUNKO"); configured != "" {
		return configured
	}
	if override != "" {
		return override
	}
	absolute, err := filepath.Abs(sourcePath)
	if err == nil {
		current := filepath.Dir(absolute)
		for {
			candidate := filepath.Join(current, "bunko")
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return "bunko"
}

// diagnosticsFromError 将编译器错误转换为 LSP 诊断。
func diagnosticsFromError(err error, text, uri string) []Diagnostic {
	var compilerDiagnostic *diagnostic.Diagnostic
	if errors.As(err, &compilerDiagnostic) {
		return []Diagnostic{fromCompilerDiagnostic(compilerDiagnostic, text, uri)}
	}
	var lexical *lexer.LexicalError
	if errors.As(err, &lexical) {
		start := lexical.Position
		end := token.Position{Offset: start.Offset + 1, Line: start.Line, Column: start.Column + 1}
		converted := diagnostic.NewError("E0001", "无法识别此处的源码", diagnostic.FromPositions(start, end)).WithCause(err)
		return []Diagnostic{fromCompilerDiagnostic(converted, text, uri)}
	}
	return []Diagnostic{internalFailure(text, err)}
}

func internalFailure(text string, err error) Diagnostic {
	message := "编译失败"
	if err != nil {
		message += ": " + err.Error()
	}
	return Diagnostic{
		Range:    rangeForOffsets(text, 0, 0),
		Severity: severityError,
		Code:     "E0401",
		Source:   diagnosticSource,
		Message:  message,
	}
}

func fromCompilerDiagnostic(value *diagnostic.Diagnostic, text, uri string) Diagnostic {
	severity := severityError
	if value.Severity == diagnostic.SeverityWarning {
		severity = severityWarning
	}
	primary, hasPrimary := value.PrimaryLabel()
	startOffset, endOffset := 0, 0
	if hasPrimary {
		startOffset = primary.Span.Start.Offset
		endOffset = primary.Span.End.Offset
	}
	message := diagnosticMessage(value)
	if value.Path != "" {
		if documentPath, ok := pathFromURI(uri); !ok || filepath.Clean(documentPath) != filepath.Clean(value.Path) {
			message = fmt.Sprintf("reported in %s\n%s", value.Path, message)
		}
	}
	result := Diagnostic{
		Range:    rangeForOffsets(text, startOffset, endOffset),
		Severity: severity,
		Code:     value.Code,
		Source:   diagnosticSource,
		Message:  message,
	}
	if hasPrimary {
		for _, label := range value.Labels {
			if label.Primary || label.Message == "" {
				continue
			}
			result.RelatedInformation = append(result.RelatedInformation, DiagnosticRelatedInformation{
				Location: Location{
					URI:   uri,
					Range: rangeForOffsets(text, label.Span.Start.Offset, label.Span.End.Offset),
				},
				Message: label.Message,
			})
		}
	}
	return result
}

// diagnosticMessage 合并编译器附加说明，保持 CLI JSON 的字段语义。
func diagnosticMessage(value *diagnostic.Diagnostic) string {
	message := strings.TrimSpace(value.Message)
	if message == "" {
		message = "Mio compiler error"
	}
	var extra []string
	if value.Expected != "" || value.Actual != "" {
		parts := make([]string, 0, 2)
		if value.Expected != "" {
			parts = append(parts, value.Expected)
		}
		if value.Actual != "" {
			parts = append(parts, value.Actual)
		}
		extra = append(extra, "expected/actual: "+strings.Join(parts, " / "))
	}
	for _, note := range value.Notes {
		if strings.TrimSpace(note.Message) != "" {
			extra = append(extra, "note: "+note.Message)
		}
	}
	for _, help := range value.Helps {
		if strings.TrimSpace(help.Message) != "" {
			extra = append(extra, "help: "+help.Message)
		}
	}
	if len(extra) > 0 {
		message += "\n" + strings.Join(extra, "\n")
	}
	return message
}

// rangeForOffsets 把 UTF-8 字节范围转换为 UTF-16 Range。
// 零宽度范围会扩展到下一个字符，避免编辑器不显示标记。
func rangeForOffsets(text string, start, end int) Range {
	if start < 0 {
		start = 0
	}
	if start > len(text) {
		start = len(text)
	}
	if end < start {
		end = start
	}
	if end > len(text) {
		end = len(text)
	}
	if start == end && start < len(text) {
		_, size := utf8.DecodeRuneInString(text[start:])
		if size <= 0 {
			size = 1
		}
		end = start + size
	}
	buffer := newTextBuffer(text)
	return Range{Start: buffer.positionAt(start), End: buffer.positionAt(end)}
}
