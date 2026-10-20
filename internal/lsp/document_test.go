package lsp

import (
	"strings"
	"testing"
)

func openedStore(uri, text string, version int) *documentStore {
	store := newDocumentStore()
	store.open(TextDocumentItem{URI: uri, LanguageID: "mio", Version: version, Text: text})
	return store
}

func TestApplyFullReplacement(t *testing.T) {
	text, warnings := applyContentChanges("old", []TextDocumentContentChangeEvent{{Text: "new"}})
	if text != "new" || len(warnings) != 0 {
		t.Fatalf("text = %q, warnings = %v", text, warnings)
	}
}

func TestApplyIncrementalChange(t *testing.T) {
	text := "kaku message = \"hi\";\n"
	start := Position{0, 15}
	end := Position{0, 19}
	updated, warnings := applyContentChanges(text, []TextDocumentContentChangeEvent{
		{Range: &Range{Start: start, End: end}, Text: "\"bye\""},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	want := "kaku message = \"bye\";\n"
	if updated != want {
		t.Fatalf("text = %q, want %q", updated, want)
	}
}

func TestApplyIncrementalChangeWithEmoji(t *testing.T) {
	text := "kaku value = \"😀\";\n"
	// 替换 emoji 之后的右引号与分号，验证 UTF-16 位置换算。
	buffer := newTextBuffer(text)
	start := buffer.positionAt(len("kaku value = \"😀"))
	end := buffer.positionAt(len("kaku value = \"😀\";"))
	updated, warnings := applyContentChanges(text, []TextDocumentContentChangeEvent{
		{Range: &Range{Start: start, End: end}, Text: "!\";"},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if !strings.Contains(updated, "😀") || !strings.Contains(updated, "!\";") {
		t.Fatalf("unexpected text %q", updated)
	}
}

func TestApplyMultiChangeSequence(t *testing.T) {
	text := "abc\ndef\n"
	changes := []TextDocumentContentChangeEvent{
		{Range: &Range{Start: Position{0, 1}, End: Position{0, 2}}, Text: "X"},
		{Range: &Range{Start: Position{1, 0}, End: Position{1, 3}}, Text: "Y"},
	}
	updated, warnings := applyContentChanges(text, changes)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if updated != "aXc\nY\n" {
		t.Fatalf("text = %q", updated)
	}
}

func TestInvertedRangeIsSwapped(t *testing.T) {
	text := "abcdef"
	updated, warnings := applyContentChanges(text, []TextDocumentContentChangeEvent{
		{Range: &Range{Start: Position{0, 4}, End: Position{0, 1}}, Text: "X"},
	})
	if updated != "aXef" {
		t.Fatalf("text = %q", updated)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestOutOfRangeChangeIsClamped(t *testing.T) {
	text := "abc"
	updated, _ := applyContentChanges(text, []TextDocumentContentChangeEvent{
		{Range: &Range{Start: Position{0, 99}, End: Position{9, 9}}, Text: "Z"},
	})
	if updated != "abcZ" {
		t.Fatalf("text = %q", updated)
	}
}

func TestStoreVersionValidation(t *testing.T) {
	store := openedStore("file:///tmp/a.mio", "kaku x = 1;\n", 3)
	_, stale, warnings := store.change("file:///tmp/a.mio", 3, []TextDocumentContentChangeEvent{{Text: "kaku x = 2;\n"}})
	if !stale || len(warnings) == 0 {
		t.Fatalf("same version should be stale, got stale=%t warnings=%v", stale, warnings)
	}
	doc, ok := store.snapshot("file:///tmp/a.mio")
	if !ok || doc.Text != "kaku x = 1;\n" {
		t.Fatalf("document rolled back: %#v", doc)
	}

	updated, stale, warnings := store.change("file:///tmp/a.mio", 4, []TextDocumentContentChangeEvent{{Text: "kaku x = 2;\n"}})
	if stale || len(warnings) != 0 || updated == nil || updated.Version != 4 {
		t.Fatalf("version 4 change failed: %#v stale=%t warnings=%v", updated, stale, warnings)
	}
}

func TestStoreChangeForUnopenedDocument(t *testing.T) {
	store := newDocumentStore()
	doc, stale, warnings := store.change("file:///tmp/missing.mio", 1, []TextDocumentContentChangeEvent{{Text: "x"}})
	if doc != nil || stale || len(warnings) == 0 {
		t.Fatalf("unexpected result %#v %t %v", doc, stale, warnings)
	}
}

func TestStoreCloseAndReplaceText(t *testing.T) {
	store := openedStore("file:///tmp/a.mio", "a", 1)
	store.replaceText("file:///tmp/a.mio", "b")
	doc, ok := store.snapshot("file:///tmp/a.mio")
	if !ok || doc.Text != "b" || doc.Version != 1 {
		t.Fatalf("replaceText result %#v", doc)
	}
	if store.close("file:///tmp/a.mio") == nil {
		t.Fatal("close should return the previous document")
	}
	if _, ok := store.snapshot("file:///tmp/a.mio"); ok {
		t.Fatal("document should be removed")
	}
}
