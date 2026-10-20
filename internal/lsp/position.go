package lsp

import (
	"sort"
	"unicode/utf8"
)

// textBuffer 保存文本与行起点，用于 UTF-8 字节偏移与 UTF-16 坐标互转。
type textBuffer struct {
	text       string
	lineStarts []int
}

func newTextBuffer(text string) *textBuffer {
	starts := make([]int, 1, 16)
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '\n':
			starts = append(starts, index+1)
		case '\r':
			// CRLF 在 '\n' 处换行；单独的 '\r' 与 Mio Lexer 一样视为换行。
			if index+1 < len(text) && text[index+1] == '\n' {
				continue
			}
			starts = append(starts, index+1)
		}
	}
	return &textBuffer{text: text, lineStarts: starts}
}

// positionAt 将字节偏移转换为 LSP 位置。
// 偏移会被夹取到 [0, len(text)]；CRLF 中的 '\r' 不计入行字符数。
func (b *textBuffer) positionAt(byteOffset int) Position {
	if byteOffset < 0 {
		byteOffset = 0
	}
	if byteOffset > len(b.text) {
		byteOffset = len(b.text)
	}
	line := sort.Search(len(b.lineStarts), func(index int) bool {
		return b.lineStarts[index] > byteOffset
	}) - 1
	if line < 0 {
		line = 0
	}
	start := b.lineStarts[line]
	end := byteOffset
	if end > start && b.text[end-1] == '\r' {
		end--
	}
	if end < start {
		end = start
	}
	return Position{Line: line, Character: utf16Length(b.text[start:end])}
}

// offsetAt 将 LSP 位置转换为字节偏移。
// 越界位置会被夹取到行内容末尾或文件末尾。
func (b *textBuffer) offsetAt(position Position) int {
	if position.Line < 0 || position.Character < 0 {
		return 0
	}
	if position.Line >= len(b.lineStarts) {
		return len(b.text)
	}
	start := b.lineStarts[position.Line]
	contentEnd := len(b.text)
	if position.Line+1 < len(b.lineStarts) {
		contentEnd = b.lineStarts[position.Line+1] - 1
	}
	if contentEnd > start && b.text[contentEnd-1] == '\r' {
		contentEnd--
	}
	if position.Character == 0 {
		return start
	}

	target := position.Character
	units := 0
	offset := start
	for offset < contentEnd {
		r, size := utf8.DecodeRuneInString(b.text[offset:contentEnd])
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units+width > target {
			return offset
		}
		units += width
		offset += size
		if units == target {
			return offset
		}
	}
	return contentEnd
}

// utf16Length 返回字符串的 UTF-16 code unit 数量。
// 非法 UTF-8 字节按一个 code unit 计算，与 utf8.DecodeRune 的行为一致。
func utf16Length(value string) int {
	units := 0
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
		value = value[size:]
	}
	return units
}
