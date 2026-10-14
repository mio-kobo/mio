package diagnostic

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ColorMode 控制 ANSI 颜色输出
type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

// Format 控制诊断输出格式
type Format string

const (
	FormatHuman Format = "human"
	FormatShort Format = "short"
	FormatJSON  Format = "json"
)

// Renderer 将结构化诊断写入终端或机器可读流
type Renderer struct {
	Writer   io.Writer
	Source   string
	Path     string
	Color    ColorMode
	Format   Format
	Verbose  bool
	TabWidth int
}

type sourceLine struct {
	number int
	start  int
	end    int
	text   string
}

type jsonDiagnostic struct {
	Code     string         `json:"code"`
	Severity Severity       `json:"severity"`
	Message  string         `json:"message"`
	Path     string         `json:"path,omitempty"`
	Location SourceLocation `json:"location"`
	Labels   []Label        `json:"labels,omitempty"`
	Notes    []Note         `json:"notes,omitempty"`
	Helps    []Help         `json:"helps,omitempty"`
	Expected string         `json:"expected,omitempty"`
	Actual   string         `json:"actual,omitempty"`
}

// Render 输出一条诊断
func (r Renderer) Render(d *Diagnostic) error {
	if d == nil {
		return nil
	}
	if r.Writer == nil {
		return fmt.Errorf("诊断输出流为空")
	}
	if r.TabWidth <= 0 {
		r.TabWidth = 4
	}
	path := d.Path
	if path == "" {
		path = r.Path
	}
	label, hasLabel := d.PrimaryLabel()
	location := label.Span.Start
	if hasLabel && r.Source != "" {
		location = locateOffset(r.Source, clamp(label.Span.Start.Offset, 0, len(r.Source)), r.TabWidth)
	}

	switch r.Format {
	case FormatShort:
		displayPath := path
		if displayPath == "" {
			displayPath = "<runtime>"
		}
		_, err := fmt.Fprintf(r.Writer, "%s:%d:%d: %s[%s]: %s\n", displayPath, location.Line, location.Column, d.Severity, d.Code, d.Message)
		return err
	case FormatJSON:
		value := jsonDiagnostic{Code: d.Code, Severity: d.Severity, Message: d.Message, Path: path, Location: location, Labels: d.Labels, Notes: d.Notes, Helps: d.Helps, Expected: d.Expected, Actual: d.Actual}
		encoder := json.NewEncoder(r.Writer)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(value)
	default:
		return r.renderHuman(d, path, location, hasLabel)
	}
}

func (r Renderer) renderHuman(d *Diagnostic, path string, location SourceLocation, hasLabel bool) error {
	color := r.useColor()
	header := fmt.Sprintf("%s[%s]", d.Severity, d.Code)
	if color {
		header = "\x1b[1;31m" + header + "\x1b[0m"
	}
	if _, err := fmt.Fprintf(r.Writer, "%s: %s\n", header, d.Message); err != nil {
		return err
	}
	if hasLabel {
		if _, err := fmt.Fprintf(r.Writer, "  --> %s:%d:%d\n", path, location.Line, location.Column); err != nil {
			return err
		}
		if r.Source != "" {
			if err := r.renderLabels(d, color); err != nil {
				return err
			}
		}
	}
	if d.Expected != "" || d.Actual != "" {
		detail := fmt.Sprintf("期望 %s，实际 %s", d.Expected, d.Actual)
		if len(d.Helps) > 0 {
			detail += "；建议: " + d.Helps[0].Message
		}
		if _, err := fmt.Fprintf(r.Writer, "  = %s\n", detail); err != nil {
			return err
		}
	}
	if len(d.Helps) > 0 && d.Expected == "" && d.Actual == "" {
		if _, err := fmt.Fprintf(r.Writer, "  = 建议: %s\n", d.Helps[0].Message); err != nil {
			return err
		}
	}
	if len(d.Notes) > 0 {
		if _, err := fmt.Fprintf(r.Writer, "  = 说明: %s\n", d.Notes[0].Message); err != nil {
			return err
		}
	}
	if r.Verbose {
		for _, note := range d.Notes[1:] {
			if _, err := fmt.Fprintf(r.Writer, "  = 说明: %s\n", note.Message); err != nil {
				return err
			}
		}
		if d.Cause != nil {
			if _, err := fmt.Fprintf(r.Writer, "  = 原因: %v\n", d.Cause); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r Renderer) renderLabels(d *Diagnostic, color bool) error {
	labels := d.Labels
	lines := splitSource(r.Source)
	type mark struct {
		line       sourceLine
		start, end int
		message    string
		primary    bool
	}
	marks := make([]mark, 0, len(labels))
	for _, label := range labels {
		start := clamp(label.Span.Start.Offset, 0, len(r.Source))
		end := clamp(label.Span.End.Offset, start, len(r.Source))
		if end == start && end < len(r.Source) {
			_, size := utf8.DecodeRuneInString(r.Source[end:])
			end += size
		}
		for _, line := range lines {
			if start > line.end || end < line.start || (start == line.end && start != len(r.Source)) {
				continue
			}
			segmentStart := clamp(start, line.start, line.end)
			segmentEnd := clamp(end, segmentStart, line.end)
			startCol := displayWidth(r.Source[line.start:segmentStart], r.TabWidth) + 1
			endCol := displayWidth(r.Source[line.start:segmentEnd], r.TabWidth) + 1
			if endCol <= startCol {
				endCol = startCol + 1
			}
			message := label.Message
			if message == "" && label.Primary && d.Cause != nil {
				message = d.Cause.Error()
			}
			marks = append(marks, mark{line: line, start: startCol, end: endCol, message: message, primary: label.Primary})
		}
	}
	if len(marks) == 0 {
		return nil
	}
	sort.SliceStable(marks, func(i, j int) bool {
		if marks[i].line.number == marks[j].line.number {
			return marks[i].start < marks[j].start
		}
		return marks[i].line.number < marks[j].line.number
	})
	lastLine := -1
	for _, current := range marks {
		if current.line.number != lastLine {
			if _, err := fmt.Fprintf(r.Writer, "%4d | %s\n", current.line.number, expandTabs(current.line.text, r.TabWidth)); err != nil {
				return err
			}
			lastLine = current.line.number
		}
		underline := strings.Repeat(" ", current.start-1) + strings.Repeat("^", current.end-current.start)
		if current.message != "" {
			underline += " " + current.message
		}
		if color && current.primary {
			underline = "\x1b[31m" + underline + "\x1b[0m"
		}
		if _, err := fmt.Fprintf(r.Writer, "     | %s\n", underline); err != nil {
			return err
		}
	}
	return nil
}

func (r Renderer) useColor() bool {
	if r.Color == ColorNever || os.Getenv("NO_COLOR") != "" {
		return false
	}
	if r.Color == ColorAlways {
		return true
	}
	if os.Getenv("CI") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := r.Writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func splitSource(source string) []sourceLine {
	lines := make([]sourceLine, 0, strings.Count(source, "\n")+1)
	start := 0
	number := 1
	for start <= len(source) {
		newline := strings.IndexByte(source[start:], '\n')
		end := len(source)
		next := len(source) + 1
		if newline >= 0 {
			end = start + newline
			next = end + 1
		}
		textEnd := end
		if textEnd > start && source[textEnd-1] == '\r' {
			textEnd--
		}
		lines = append(lines, sourceLine{number: number, start: start, end: textEnd, text: source[start:textEnd]})
		if newline < 0 {
			break
		}
		start = next
		number++
	}
	return lines
}

func locateOffset(source string, offset, tabWidth int) SourceLocation {
	offset = clamp(offset, 0, len(source))
	line := 1
	lineStart := 0
	for i := 0; i < offset; i++ {
		if source[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	column := displayWidth(strings.TrimSuffix(source[lineStart:offset], "\r"), tabWidth) + 1
	return SourceLocation{Offset: offset, Line: line, Column: column}
}

func displayWidth(text string, tabWidth int) int {
	width := 0
	for _, current := range text {
		if current == '\t' {
			width += tabWidth - width%tabWidth
			continue
		}
		if unicode.Is(unicode.Mn, current) || unicode.Is(unicode.Me, current) || current == '\r' {
			continue
		}
		if isWide(current) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func expandTabs(text string, tabWidth int) string {
	var result strings.Builder
	width := 0
	for _, current := range text {
		if current == '\t' {
			count := tabWidth - width%tabWidth
			result.WriteString(strings.Repeat(" ", count))
			width += count
			continue
		}
		result.WriteRune(current)
		if isWide(current) {
			width += 2
		} else if !unicode.Is(unicode.Mn, current) && !unicode.Is(unicode.Me, current) {
			width++
		}
	}
	return result.String()
}

func isWide(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) || (r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) || (r >= 0x20000 && r <= 0x3fffd))
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
