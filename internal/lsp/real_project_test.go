package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"Mio/internal/bunko"
)

// TestSemanticRealBunkoSource 用仓库内真实的 Bunko 源码验证跨包语义。
func TestSemanticRealBunkoSource(t *testing.T) {
	sourcePath := filepath.Join("..", "..", "bunko", "kiso", "path", "path.mio")
	text, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Skipf("real Bunko source is unavailable: %v", err)
	}
	absolute, err := filepath.Abs(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	instance := newAnalyzer("")
	doc := &document{URI: uriFromPath(absolute), LanguageID: "mio", Version: 1, Text: string(text)}
	result := instance.analyze(context.Background(), doc, bunko.NewOverlay(), nil)
	if result == nil {
		t.Fatal("analysis is nil")
	}
	if result.Partial {
		t.Fatalf("real source is partial: %#v", result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		t.Fatalf("real source diagnostic: %s %s", diagnostic.Code, diagnostic.Message)
	}
	if len(result.Files) < 2 {
		t.Fatalf("expected imported Bunko packages, files = %#v", result.Files)
	}

	source := string(text)
	arrayCall := offsetOf(t, source, "array.with_capacity") + len("array.")
	hover := occurrenceHoverMarkdown(result.Index, result.DocKey, arrayCall)
	if hover == nil {
		t.Fatal("hover on imported generic function is nil")
	}
	// 类型实参已在调用点提供，签名仍来自 kiso/array。
	if !containsAll(hover.Contents.Value, []string{"with_capacity", "int64", "T"}) {
		t.Fatalf("hover = %q", hover.Contents.Value)
	}
	definition := definitionSite(t, result, arrayCall)
	if definition == nil {
		t.Fatal("imported definition is nil")
	}
	expected := uriFromPath(canonicalPath(filepath.Join("..", "..", "bunko", "kiso", "array", "array.mio")))
	if definition.FileKey != expected {
		t.Fatalf("definition file = %q, want %q", definition.FileKey, expected)
	}

	symbols := result.documentSymbols()
	names := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		names[symbol.Name] = true
	}
	for _, want := range []string{"clean", "join", "SplitResult"} {
		if !names[want] {
			t.Fatalf("document symbols missing %q: %v", want, names)
		}
	}
}

func containsAll(value string, wants []string) bool {
	for _, want := range wants {
		found := false
		for index := 0; index+len(want) <= len(value); index++ {
			if value[index:index+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
