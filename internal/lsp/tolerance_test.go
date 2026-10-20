package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticUnicodeAndCRLFPositions(t *testing.T) {
	lf := "waza main() -> int64 {\n    kaku note = \"😀こんにちは\"; kaesu note;\n}\n"
	for name, text := range map[string]string{
		"LF":   lf,
		"CRLF": strings.ReplaceAll(lf, "\n", "\r\n"),
	} {
		t.Run(name, func(t *testing.T) {
			result := semanticAnalyze(t, text)
			use := offsetOf(t, text, "kaesu note") + len("kaesu ")
			position := result.Buffer.positionAt(use)
			wantCharacter := utf16Length("    kaku note = \"😀こんにちは\"; kaesu ")
			if position.Line != 1 || position.Character != wantCharacter {
				t.Fatalf("position = %#v, want line 1 character %d", position, wantCharacter)
			}
			if roundTrip := result.positionOffset(position); roundTrip != use {
				t.Fatalf("round trip = %d, want %d", roundTrip, use)
			}
			requireHover(t, result, use, "kaku note: string")

			definition := definitionSite(t, result, use)
			if definition == nil {
				t.Fatal("definition not found")
			}
			declarationPosition := result.Buffer.positionAt(definition.Span.Start.Offset)
			if declarationPosition.Line != 1 || declarationPosition.Character != utf16Length("    kaku ") {
				t.Fatalf("declaration position = %#v", declarationPosition)
			}

			symbols := result.documentSymbols()
			if len(symbols) != 1 || symbols[0].Name != "main" {
				t.Fatalf("document symbols = %#v", symbols)
			}
			if symbols[0].SelectionRange.Start.Line != 0 || symbols[0].SelectionRange.Start.Character != 5 {
				t.Fatalf("selection range = %#v", symbols[0].SelectionRange)
			}
		})
	}
}

func TestSemanticCRLFRangesKeepLineNumbers(t *testing.T) {
	text := strings.ReplaceAll(`kata Point {
    x: int64;
}

waza main() -> int64 {
    kaku p = Point { x: 1 };
    kaesu p.x;
}
`, "\n", "\r\n")
	result := semanticAnalyze(t, text)
	use := offsetOf(t, text, "p.x") + len("p.")
	definition := definitionSite(t, result, use)
	if definition == nil {
		t.Fatal("field definition not found")
	}
	position := result.Buffer.positionAt(definition.Span.Start.Offset)
	if position.Line != 1 || position.Character != 4 {
		t.Fatalf("field definition position = %#v", position)
	}
	hover := occurrenceHoverMarkdown(result.Index, result.DocKey, use)
	if hover == nil || !strings.Contains(hover.Contents.Value, "x: int64") {
		t.Fatalf("hover = %#v", hover)
	}
}

func TestSemanticToleranceMissingRightParen(t *testing.T) {
	text := `waza helper(a: int64) -> int64 {
    kaesu a;
}

waza main() -> int64 {
    kaku value = 1;
    kaesu helper(value;
}
`
	result := semanticAnalyze(t, text)
	if !result.Partial {
		t.Fatal("incomplete source must be marked partial")
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("strict diagnostics = %#v", result.Diagnostics)
	}
	requireHover(t, result, offsetOf(t, text, "value = 1"), "kaku value: int64")
	// 同一函数中合法语句仍然参与补全。
	source, cursor := withCursor(t, `waza helper(a: int64) -> int64 {
    kaesu a;
}

waza main() -> int64 {
    kaku value = 1;
    kaesu helper(value;
@@}
`)
	completion := semanticAnalyze(t, source)
	labels := completionLabels(completionAtOffset(completion, cursor))
	if _, ok := labels["value"]; !ok {
		t.Fatalf("local completion lost after broken statement: %v", keysOfCompletion(labels))
	}
}

func TestSemanticToleranceUnterminatedBlock(t *testing.T) {
	text := `waza main() -> int64 {
    kaku value = 1;
    kaesu value;
`
	result := semanticAnalyze(t, text)
	if !result.Partial {
		t.Fatal("unterminated block must be partial")
	}
	requireHover(t, result, offsetOf(t, text, "value;"), "kaku value: int64")
	if len(result.documentSymbols()) != 1 {
		t.Fatalf("document symbols = %#v", result.documentSymbols())
	}
}

func TestSemanticToleranceUnterminatedStringKeepsEarlierDeclarations(t *testing.T) {
	text := "kaku earlier = 1;\nkaku broken = \"unterminated\n"
	result := semanticAnalyze(t, text)
	if !result.Partial {
		t.Fatal("unterminated string must be partial")
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	requireHover(t, result, offsetOf(t, text, "earlier = 1"), "kaku earlier: int64")
}

func TestSemanticToleranceIncompleteGenericAndImport(t *testing.T) {
	sources := []string{
		"kaku value = 1;\nwaza broken<T(value: T) -> T {\n",
		"kaku value = 1;\nyobu \"lib\n",
		"kaku value = 1;\nkaku values = [1, 2,\n",
		"kaku value = 1;\nwaza broken() -> int64 { kaesu 1;\n",
	}
	for _, source := range sources {
		result := semanticAnalyze(t, source)
		if result.Index == nil {
			t.Fatalf("index is nil for %q", source)
		}
		// 已完整解析的前缀声明必须保留。
		hover := occurrenceHoverMarkdown(result.Index, result.DocKey, offsetOf(t, source, "value = 1"))
		if hover == nil || !strings.Contains(hover.Contents.Value, "kaku value: int64") {
			t.Fatalf("hover for %q = %#v", source, hover)
		}
	}
}

func TestSemanticUnknownSymbolReturnsNothing(t *testing.T) {
	text := `waza main() -> int64 {
    kaesu 1;
}
`
	result := semanticAnalyze(t, text)
	unknown := offsetOf(t, text, "kaesu")
	requireNoHover(t, result, unknown)
	if definition := definitionSite(t, result, unknown); definition != nil {
		t.Fatalf("unknown token definition = %#v", definition)
	}
	if refs := referenceSites(t, result, unknown); len(refs) != 0 {
		t.Fatalf("unknown token refs = %#v", refs)
	}
	if help := result.signatureHelpAt(unknown); help != nil {
		t.Fatalf("unknown token signature help = %#v", help)
	}
}

func TestSemanticCommentsAndStringsAreNotReferences(t *testing.T) {
	text := `waza main() -> int64 {
    kaku value = 1;
    // value value value
    kaku text = "value value";
    kaesu value;
}
`
	result := semanticAnalyze(t, text)
	declaration := offsetOf(t, text, "value = 1")
	refs := referenceSites(t, result, declaration)
	if len(refs) != 1 {
		t.Fatalf("refs = %d, want only the real reference (%#v)", len(refs), refs)
	}
	if refs[0].Span.Start.Offset != offsetOf(t, text, "value;") {
		t.Fatalf("ref offset = %d", refs[0].Span.Start.Offset)
	}
}

func TestSemanticToleranceFileDocumentKeys(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "partial.mio")
	source := "waza main() -> int64 {\n    kaku value = 1;\n    kaesu value\n}\n"
	writeMioFile(t, sourcePath, source)
	result := semanticAnalyzeFile(t, root, sourcePath)
	if !result.Partial {
		t.Fatal("missing semicolon must be partial")
	}
	if result.DocKey != uriFromPath(sourcePath) {
		t.Fatalf("doc key = %q, want %q", result.DocKey, uriFromPath(sourcePath))
	}
	requireHover(t, result, offsetOf(t, source, "value = 1"), "kaku value: int64")
	// 容错分析也必须为大文档提供层级符号。
	symbols := result.documentSymbols()
	if len(symbols) != 1 || symbols[0].Name != "main" {
		t.Fatalf("document symbols = %#v", symbols)
	}
}
