package lsp

import (
	"testing"
	"unicode/utf8"
)

func TestPositionAtByteOffsets(t *testing.T) {
	text := "aé😀中\nb\r\nc"
	buffer := newTextBuffer(text)
	cases := []struct {
		offset int
		want   Position
	}{
		{0, Position{0, 0}},
		{1, Position{0, 1}},
		{3, Position{0, 2}},
		{7, Position{0, 4}},
		{10, Position{0, 5}},
		{11, Position{1, 0}},
		{12, Position{1, 1}},
		{13, Position{1, 1}},
		{14, Position{2, 0}},
		{15, Position{2, 1}},
		{-5, Position{0, 0}},
		{len(text) + 10, Position{2, 1}},
	}
	for _, test := range cases {
		if got := buffer.positionAt(test.offset); got != test.want {
			t.Errorf("positionAt(%d) = %#v, want %#v", test.offset, got, test.want)
		}
	}
}

func TestOffsetAtPositions(t *testing.T) {
	text := "aé😀中\nb\r\nc"
	buffer := newTextBuffer(text)
	cases := []struct {
		position Position
		want     int
	}{
		{Position{0, 0}, 0},
		{Position{0, 1}, 1},
		{Position{0, 2}, 3},
		{Position{0, 4}, 7},
		{Position{0, 5}, 10},
		{Position{0, 9}, 10},
		{Position{1, 0}, 11},
		{Position{1, 1}, 12},
		{Position{1, 5}, 12},
		{Position{2, 0}, 14},
		{Position{2, 1}, 15},
		{Position{9, 0}, 15},
		{Position{-1, 0}, 0},
		{Position{0, -3}, 0},
		// 落在代理对中间时向前取整，避免拆开 emoji。
		{Position{0, 3}, 3},
	}
	for _, test := range cases {
		if got := buffer.offsetAt(test.position); got != test.want {
			t.Errorf("offsetAt(%#v) = %d, want %d", test.position, got, test.want)
		}
	}
}

func TestPositionRoundTripAtRuneBoundaries(t *testing.T) {
	text := "func 名前😀\r\nx = '中'\n"
	buffer := newTextBuffer(text)
	offset := 0
	for offset <= len(text) {
		position := buffer.positionAt(offset)
		roundTrip := buffer.offsetAt(position)
		if buffer.positionAt(roundTrip) != position {
			t.Fatalf("offset %d -> %#v -> %d -> %#v is not stable", offset, position, roundTrip, buffer.positionAt(roundTrip))
		}
		if offset < len(text) {
			_, size := utf8.DecodeRuneInString(text[offset:])
			if size <= 0 {
				size = 1
			}
			offset += size
			continue
		}
		break
	}
}

func TestUTF16Length(t *testing.T) {
	cases := map[string]int{
		"":        0,
		"ascii":   5,
		"中文":      2,
		"😀":       2,
		"a😀b":     4,
		"e\u0301": 2,
	}
	for text, want := range cases {
		if got := utf16Length(text); got != want {
			t.Errorf("utf16Length(%q) = %d, want %d", text, got, want)
		}
	}
	if got := utf16Length(string([]byte{0x61, 0xff, 0x62})); got != 3 {
		t.Errorf("invalid UTF-8 length = %d, want 3", got)
	}
}

func TestInvalidUTF8RoundTrip(t *testing.T) {
	text := string([]byte{0x61, 0xff, 0x62, 0x0a, 0x63})
	buffer := newTextBuffer(text)
	if got := buffer.positionAt(2); got != (Position{0, 2}) {
		t.Fatalf("positionAt(2) = %#v", got)
	}
	if got := buffer.offsetAt(Position{0, 2}); got != 2 {
		t.Fatalf("offsetAt(0:2) = %d", got)
	}
}

func TestLoneCarriageReturnIsALineBreak(t *testing.T) {
	buffer := newTextBuffer("ab\rcd")
	if got := buffer.positionAt(2); got != (Position{0, 2}) {
		t.Fatalf("positionAt before CR = %#v", got)
	}
	if got := buffer.positionAt(3); got != (Position{1, 0}) {
		t.Fatalf("positionAt after lone CR = %#v", got)
	}
	if got := buffer.offsetAt(Position{1, 0}); got != 3 {
		t.Fatalf("offsetAt line 1 = %d", got)
	}
	if got := buffer.offsetAt(Position{1, 2}); got != 5 {
		t.Fatalf("offsetAt line 1 end = %d", got)
	}
	if got := buffer.positionAt(5); got != (Position{1, 2}) {
		t.Fatalf("positionAt EOF = %#v", got)
	}
}

func TestCRLFDoesNotCountCarriageReturn(t *testing.T) {
	buffer := newTextBuffer("ab\r\ncd")
	if got := buffer.positionAt(2); got != (Position{0, 2}) {
		t.Fatalf("positionAt before CR = %#v", got)
	}
	if got := buffer.positionAt(3); got != (Position{0, 2}) {
		t.Fatalf("positionAt after CR = %#v", got)
	}
	if got := buffer.offsetAt(Position{0, 2}); got != 2 {
		t.Fatalf("offsetAt line end = %d", got)
	}
}
