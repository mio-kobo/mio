package diagnostic

import (
	"fmt"

	"Mio/internal/token"
)

// Severity 表示诊断严重程度
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// SourceLocation 表示源码中的字节位置
type SourceLocation struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Span 表示左闭右开的源码范围
type Span struct {
	Start SourceLocation `json:"start"`
	End   SourceLocation `json:"end"`
}

// Label 表示源码范围及其说明
type Label struct {
	Span    Span   `json:"span"`
	Message string `json:"message,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// Note 表示补充说明
type Note struct {
	Message string `json:"message"`
}

// Help 表示可执行的修复建议
type Help struct {
	Message string `json:"message"`
}

// Diagnostic 保存一条结构化诊断
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Path     string   `json:"path,omitempty"`
	Labels   []Label  `json:"labels,omitempty"`
	Notes    []Note   `json:"notes,omitempty"`
	Helps    []Help   `json:"helps,omitempty"`
	Expected string   `json:"expected,omitempty"`
	Actual   string   `json:"actual,omitempty"`
	Cause    error    `json:"-"`
}

// Error 返回不含源码片段的简短错误
func (d *Diagnostic) Error() string {
	if d == nil {
		return "<nil>"
	}
	message := d.Message
	if d.Cause != nil && d.Cause.Error() != message {
		message += ": " + d.Cause.Error()
	}
	if d.Code == "" {
		return message
	}
	return fmt.Sprintf("%s: %s", d.Code, message)
}

// Unwrap 返回原始错误原因
func (d *Diagnostic) Unwrap() error {
	if d == nil {
		return nil
	}
	return d.Cause
}

// PrimaryLabel 返回主要错误标签
func (d *Diagnostic) PrimaryLabel() (Label, bool) {
	if d == nil {
		return Label{}, false
	}
	for _, label := range d.Labels {
		if label.Primary {
			return label, true
		}
	}
	if len(d.Labels) == 0 {
		return Label{}, false
	}
	return d.Labels[0], true
}

// NewError 创建带主要源码范围的错误诊断
func NewError(code, message string, span Span) *Diagnostic {
	return &Diagnostic{
		Code:     code,
		Severity: SeverityError,
		Message:  message,
		Labels:   []Label{{Span: span, Primary: true}},
	}
}

// FromPositions 将 Token 位置转换为诊断范围
func FromPositions(start, end token.Position) Span {
	return Span{
		Start: SourceLocation{Offset: start.Offset, Line: start.Line, Column: start.Column},
		End:   SourceLocation{Offset: end.Offset, Line: end.Line, Column: end.Column},
	}
}

// WithLabel 添加源码标签
func (d *Diagnostic) WithLabel(span Span, message string) *Diagnostic {
	d.Labels = append(d.Labels, Label{Span: span, Message: message})
	return d
}

// WithHelp 添加修复建议
func (d *Diagnostic) WithHelp(message string) *Diagnostic {
	d.Helps = append(d.Helps, Help{Message: message})
	return d
}

// WithNote 添加补充说明
func (d *Diagnostic) WithNote(message string) *Diagnostic {
	d.Notes = append(d.Notes, Note{Message: message})
	return d
}

// WithTypes 保存期望类型和实际类型
func (d *Diagnostic) WithTypes(expected, actual string) *Diagnostic {
	d.Expected = expected
	d.Actual = actual
	return d
}

// WithCause 保存原始错误原因
func (d *Diagnostic) WithCause(err error) *Diagnostic {
	d.Cause = err
	return d
}
