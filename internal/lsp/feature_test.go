package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

// withCursor 移除源码中的 @@ 标记并返回其字节偏移。
func withCursor(t *testing.T, source string) (string, int) {
	t.Helper()
	index := strings.Index(source, "@@")
	if index < 0 {
		t.Fatal("cursor marker @@ is missing")
	}
	return strings.Replace(source, "@@", "", 1), index
}

func completionLabels(items []CompletionItem) map[string]CompletionItem {
	labels := make(map[string]CompletionItem, len(items))
	for _, item := range items {
		labels[item.Label] = item
	}
	return labels
}

func completionAtOffset(result *analysis, offset int) []CompletionItem {
	return result.completionAt(result.Buffer.positionAt(offset)).Items
}

func TestSemanticCompletionScope(t *testing.T) {
	source, cursor := withCursor(t, `waza helper(value: int64) -> int64 {
    kaesu value;
}

waza main() -> int64 {
    kaku local = 1;
@@
    kaesu local;
}
`)
	result := semanticAnalyze(t, source)
	labels := completionLabels(completionAtOffset(result, cursor))
	for _, want := range []string{"local", "main", "helper", "kaku", "int64", "chant", "len"} {
		if _, ok := labels[want]; !ok {
			t.Fatalf("missing completion %q in %v", want, keysOfCompletion(labels))
		}
	}
	if labels["local"].Kind != completionKindVariable {
		t.Fatalf("local kind = %d", labels["local"].Kind)
	}
	helper := labels["helper"]
	if helper.InsertTextFormat != insertTextFormatSnippet || helper.InsertText != "helper(${1:value})" {
		t.Fatalf("function completion = %#v", helper)
	}
	if labels["local"].Detail != "int64" {
		t.Fatalf("local detail = %q", labels["local"].Detail)
	}
}

func keysOfCompletion(labels map[string]CompletionItem) []string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	return keys
}

func TestSemanticCompletionShadowing(t *testing.T) {
	source, cursor := withCursor(t, `waza main() -> int64 {
    kaku value: int64 = 1;
    moshi (value > 0) {
        kaku value = "text";
@@
        kaesu 1;
    }
    kaesu value;
}
`)
	result := semanticAnalyze(t, source)
	labels := completionLabels(completionAtOffset(result, cursor))
	value, ok := labels["value"]
	if !ok {
		t.Fatalf("missing shadowed completion: %v", keysOfCompletion(labels))
	}
	if value.Detail != "string" {
		t.Fatalf("shadowed completion detail = %q, want string", value.Detail)
	}
}

func TestSemanticCompletionFieldsAndVariants(t *testing.T) {
	source, cursor := withCursor(t, `kata Point { x: int64; y: int64; }
sentaku Color { Red; Green; }

waza main() -> int64 {
    kaku p = Point { x: 1, y: 2 };
    kaku c: Color = Color.Red();
    kaesu p.@@x;
}
`)
	result := semanticAnalyze(t, source)
	labels := completionLabels(completionAtOffset(result, cursor))
	for _, want := range []string{"x", "y"} {
		if _, ok := labels[want]; !ok {
			t.Fatalf("missing field completion %q: %v", want, keysOfCompletion(labels))
		}
	}
	if labels["x"].Kind != completionKindField {
		t.Fatalf("field kind = %d", labels["x"].Kind)
	}

	enumSource, enumCursor := withCursor(t, `sentaku Color { Red; Green; }

waza main() -> int64 {
    kaesu Color.@@Red().value;
}
`)
	enumResult := semanticAnalyze(t, enumSource)
	enumLabels := completionLabels(completionAtOffset(enumResult, enumCursor))
	for _, want := range []string{"Red", "Green"} {
		if _, ok := enumLabels[want]; !ok {
			t.Fatalf("missing variant completion %q: %v", want, keysOfCompletion(enumLabels))
		}
	}
	if enumLabels["Red"].Kind != completionKindEnumMember {
		t.Fatalf("variant kind = %d", enumLabels["Red"].Kind)
	}
	if enumLabels["Red"].InsertText != "Red()" {
		t.Fatalf("variant insert = %q", enumLabels["Red"].InsertText)
	}
}

func TestSemanticCompletionModuleExports(t *testing.T) {
	root := t.TempDir()
	writeMioFile(t, filepath.Join(root, "bunko", "util", "util.mio"), `koukai waza visible(value: int64) -> int64 {
    kaesu hidden(value);
}

waza hidden(value: int64) -> int64 {
    kaesu value;
}
`)
	mainPath := filepath.Join(root, "main.mio")
	mainSource, cursor := withCursor(t, `yobu "util";

waza main() -> int64 {
    kaesu util.@@visible(1);
}
`)
	writeMioFile(t, mainPath, mainSource)
	result := semanticAnalyzeFile(t, root, mainPath)
	labels := completionLabels(completionAtOffset(result, cursor))
	if _, ok := labels["visible"]; !ok {
		t.Fatalf("missing exported completion: %v", keysOfCompletion(labels))
	}
	if _, ok := labels["hidden"]; ok {
		t.Fatalf("private symbol leaked into completion: %v", keysOfCompletion(labels))
	}
	if labels["visible"].InsertText != "visible" {
		t.Fatalf("module export insert = %q", labels["visible"].InsertText)
	}

	// 顶层补全不能把导入成员当成可直接引用的名字。
	topSource, topCursor := withCursor(t, `yobu "util";

waza main() -> int64 {
@@
    kaesu 1;
}
`)
	topPath := filepath.Join(root, "top.mio")
	writeMioFile(t, topPath, topSource)
	topResult := semanticAnalyzeFile(t, root, topPath)
	topLabels := completionLabels(completionAtOffset(topResult, topCursor))
	if _, ok := topLabels["visible"]; ok {
		t.Fatalf("imported name must not appear as a bare completion: %v", keysOfCompletion(topLabels))
	}
}

func TestSemanticCompletionSuppressedInCommentsAndStrings(t *testing.T) {
	source, cursor := withCursor(t, `waza main() -> int64 {
    // 注释里的 @@ 不提供补全
    kaesu 1;
}
`)
	result := semanticAnalyze(t, source)
	if items := completionAtOffset(result, cursor); len(items) != 0 {
		t.Fatalf("comment completion = %#v", items)
	}

	stringSource, stringCursor := withCursor(t, `waza main() -> int64 {
    kaku text = "hello @@ world";
    kaesu 1;
}
`)
	stringResult := semanticAnalyze(t, stringSource)
	if items := completionAtOffset(stringResult, stringCursor); len(items) != 0 {
		t.Fatalf("string completion = %#v", items)
	}
}

func TestSemanticCompletionLocalShadowsBuiltin(t *testing.T) {
	source, cursor := withCursor(t, `waza main() -> int64 {
    kaku len = 1;
@@
    kaesu len;
}
`)
	result := semanticAnalyze(t, source)
	items := completionAtOffset(result, cursor)
	count := 0
	for _, item := range items {
		if item.Label != "len" {
			continue
		}
		count++
		if item.Kind != completionKindVariable {
			t.Fatalf("len completion kind = %d, want variable", item.Kind)
		}
	}
	if count != 1 {
		t.Fatalf("len completion count = %d, want 1", count)
	}
}

func TestSemanticSignatureHelpNested(t *testing.T) {
	source, cursor := withCursor(t, `waza inner(a: int64, b: int64) -> int64 {
    kaesu a + b;
}

waza outer(first: string, second: int64, third: int64) -> int64 {
    kaesu second + third;
}

waza main() -> int64 {
    kaesu outer("x,y", inner(1, @@2), 3);
}
`)
	result := semanticAnalyze(t, source)
	help := result.signatureHelpAt(cursor)
	if help == nil {
		t.Fatal("signature help is nil")
	}
	if got := help.Signatures[0].Label; got != "inner(a: int64, b: int64) -> int64" {
		t.Fatalf("signature = %q", got)
	}
	if help.ActiveParameter != 1 {
		t.Fatalf("active parameter = %d, want 1", help.ActiveParameter)
	}

	outerCursor := offsetOf(t, source, "3)")
	outerHelp := result.signatureHelpAt(outerCursor)
	if outerHelp == nil {
		t.Fatal("outer signature help is nil")
	}
	if got := outerHelp.Signatures[0].Label; got != "outer(first: string, second: int64, third: int64) -> int64" {
		t.Fatalf("outer signature = %q", got)
	}
	if outerHelp.ActiveParameter != 2 {
		t.Fatalf("outer active parameter = %d, want 2", outerHelp.ActiveParameter)
	}
}

func TestSemanticSignatureHelpIncompleteCall(t *testing.T) {
	source, cursor := withCursor(t, `waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}

waza main() -> int64 {
    kaesu add(1, @@
}
`)
	result := semanticAnalyze(t, source)
	if !result.Partial {
		t.Fatal("unterminated call should be partial")
	}
	help := result.signatureHelpAt(cursor)
	if help == nil {
		t.Fatal("incomplete call signature help is nil")
	}
	if got := help.Signatures[0].Label; got != "add(a: int64, b: int64) -> int64" {
		t.Fatalf("signature = %q", got)
	}
	if help.ActiveParameter != 1 {
		t.Fatalf("active parameter = %d, want 1", help.ActiveParameter)
	}
}

func TestSemanticSignatureHelpIgnoresNonCalls(t *testing.T) {
	source, cursor := withCursor(t, `waza main() -> int64 {
    moshi (@@1 > 0) {
        kaesu 1;
    }
    kaesu 0;
}
`)
	result := semanticAnalyze(t, source)
	if help := result.signatureHelpAt(cursor); help != nil {
		t.Fatalf("signature help for condition = %#v", help)
	}
}

func TestSemanticCompletionStructLiteralFields(t *testing.T) {
	source, cursor := withCursor(t, `kata Point { x: int64; y: int64; }

waza main() -> int64 {
    kaku p = Point { x: 1, @@ };
    kaesu p.x;
}
`)
	result := semanticAnalyze(t, source)
	labels := completionLabels(completionAtOffset(result, cursor))
	if _, ok := labels["y"]; !ok {
		t.Fatalf("missing pending literal field: %v", keysOfCompletion(labels))
	}
	if _, ok := labels["x"]; ok {
		t.Fatalf("already initialized field must not be offered: %v", keysOfCompletion(labels))
	}

	valueSource, valueCursor := withCursor(t, `kata Point { x: int64; y: int64; }

waza main() -> int64 {
    kaku other = 1;
    kaku p = Point { x: other@@, y: 2 };
    kaesu p.x;
}
`)
	valueResult := semanticAnalyze(t, valueSource)
	valueLabels := completionLabels(completionAtOffset(valueResult, valueCursor))
	if _, ok := valueLabels["x"]; ok {
		t.Fatalf("field names must not replace value completion: %v", keysOfCompletion(valueLabels))
	}
	if _, ok := valueLabels["other"]; !ok {
		t.Fatalf("value completion lost inside literal: %v", keysOfCompletion(valueLabels))
	}
}

func TestSemanticEnumConstructorSignature(t *testing.T) {
	source, cursor := withCursor(t, `sentaku Shape { Circle(float64); Empty(); }

waza main() -> Shape {
    kaesu Shape.Circle(@@1.0);
}
`)
	result := semanticAnalyze(t, source)
	help := result.signatureHelpAt(cursor)
	if help == nil {
		t.Fatal("enum constructor signature help is nil")
	}
	label := help.Signatures[0].Label
	if !strings.HasPrefix(label, "Circle(float64)") {
		t.Fatalf("variant signature = %q", label)
	}
	if help.ActiveParameter != 0 {
		t.Fatalf("active parameter = %d", help.ActiveParameter)
	}
}

func TestSemanticCompletionEnumValueHasNoMembers(t *testing.T) {
	source, cursor := withCursor(t, `sentaku Color { Red; Green; }

waza main() -> int64 {
    kaku color: Color = Color.Red();
    kaesu color.@@x;
}
`)
	result := semanticAnalyze(t, source)
	for _, item := range completionAtOffset(result, cursor) {
		if item.Label == "Red" || item.Label == "Green" {
			t.Fatalf("enum value must not offer variant construction: %#v", item)
		}
	}
}

func TestSemanticCompletionAliasShadowedByLocal(t *testing.T) {
	root := t.TempDir()
	writeMioFile(t, filepath.Join(root, "bunko", "util", "util.mio"), `koukai kata Point { x: int64; }

koukai waza visible(value: int64) -> int64 {
    kaesu value;
}
`)
	mainPath := filepath.Join(root, "main.mio")
	source, cursor := withCursor(t, `yobu "util";

waza main() -> int64 {
    kaku util = util.Point { x: 1 };
    kaesu util.@@visible(1);
}
`)
	writeMioFile(t, mainPath, source)
	result := semanticAnalyzeFile(t, root, mainPath)
	labels := completionLabels(completionAtOffset(result, cursor))
	if _, ok := labels["visible"]; ok {
		t.Fatalf("shadowed alias must not expose module exports: %v", keysOfCompletion(labels))
	}
	if _, ok := labels["x"]; !ok {
		t.Fatalf("shadowing value should offer its fields: %v", keysOfCompletion(labels))
	}
}

func TestSemanticSignatureHelpForFunctionValue(t *testing.T) {
	source, cursor := withCursor(t, `waza apply(callback: (int64) -> int64) -> int64 {
    kaesu callback(1);
}

waza main() -> int64 {
    kaku increment = waza (value: int64) -> int64 { kaesu value + 1; };
    kaesu increment(@@
}
`)
	result := semanticAnalyze(t, source)
	help := result.signatureHelpAt(cursor)
	if help == nil {
		t.Fatal("function value signature help is nil")
	}
	if !strings.HasPrefix(help.Signatures[0].Label, "increment(int64)") {
		t.Fatalf("function value signature = %q", help.Signatures[0].Label)
	}
}
