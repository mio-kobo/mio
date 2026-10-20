package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/bunko"
)

// semanticAnalyze 用非文件 URI 分析单文件源码（不经过 Bunko 合并）。
func semanticAnalyze(t *testing.T, text string) *analysis {
	t.Helper()
	instance := newAnalyzer("")
	doc := &document{URI: "untitled:semantic-test.mio", LanguageID: "mio", Version: 1, Text: text}
	result := instance.analyze(context.Background(), doc, bunko.NewOverlay(), nil)
	if result == nil {
		t.Fatal("analysis was cancelled")
	}
	return result
}

// semanticAnalyzeFile 写入临时文件并经过 Bunko 合并分析。
func semanticAnalyzeFile(t *testing.T, root, path string) *analysis {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	instance := newAnalyzer(filepath.Join(root, "bunko"))
	doc := &document{URI: uriFromPath(path), LanguageID: "mio", Version: 1, Text: string(text)}
	result := instance.analyze(context.Background(), doc, bunko.NewOverlay(), nil)
	if result == nil {
		t.Fatal("analysis was cancelled")
	}
	return result
}

func writeMioFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func offsetOf(t *testing.T, text, needle string) int {
	t.Helper()
	index := strings.Index(text, needle)
	if index < 0 {
		t.Fatalf("needle %q not found", needle)
	}
	return index
}

func offsetOfN(t *testing.T, text, needle string, occurrence int) int {
	t.Helper()
	start := 0
	for index := 0; index <= occurrence; index++ {
		found := strings.Index(text[start:], needle)
		if found < 0 {
			t.Fatalf("needle %q occurrence %d not found", needle, occurrence)
		}
		if index == occurrence {
			return start + found
		}
		start += found + len(needle)
	}
	return -1
}

func requireHover(t *testing.T, result *analysis, offset int, wants ...string) {
	t.Helper()
	hover := occurrenceHoverMarkdown(result.Index, result.DocKey, offset)
	if hover == nil {
		t.Fatalf("hover at offset %d is nil", offset)
	}
	for _, want := range wants {
		if !strings.Contains(hover.Contents.Value, want) {
			t.Fatalf("hover at offset %d = %q, want substring %q", offset, hover.Contents.Value, want)
		}
	}
}

func requireNoHover(t *testing.T, result *analysis, offset int) {
	t.Helper()
	if hover := occurrenceHoverMarkdown(result.Index, result.DocKey, offset); hover != nil {
		t.Fatalf("hover at offset %d = %#v, want nil", offset, hover)
	}
}

func definitionSite(t *testing.T, result *analysis, offset int) *site {
	t.Helper()
	symbol, _ := result.Index.symbolAt(result.DocKey, offset)
	if symbol == nil {
		return nil
	}
	location := symbol.nameSite()
	return &location
}

func referenceSites(t *testing.T, result *analysis, offset int) []site {
	t.Helper()
	symbol, _ := result.Index.symbolAt(result.DocKey, offset)
	if symbol == nil {
		return nil
	}
	return result.Index.references(symbol)
}

func TestSemanticHoverCore(t *testing.T) {
	text := `waza add(a: int64, b: int64) -> int64 {
    kaku sum = a + b;
    kaesu sum;
}
`
	result := semanticAnalyze(t, text)
	if result.Partial {
		t.Fatal("valid source must not be partial")
	}
	requireHover(t, result, offsetOf(t, text, "add(a"), "waza add(a: int64, b: int64) -> int64")
	requireHover(t, result, offsetOf(t, text, "a + b"), "a: int64")
	requireHover(t, result, offsetOf(t, text, "sum ="), "kaku sum: int64")
	requireHover(t, result, offsetOf(t, text, "sum;"), "kaku sum: int64")
	// 表达式悬停：字面量所在的加法表达式也有真实类型。
	requireHover(t, result, offsetOf(t, text, "a + b")+len("a + "), "int64")
}

func TestSemanticDefinitionAndShadowing(t *testing.T) {
	text := `waza outer(x: int64) -> int64 {
    kaku value = x;
    moshi (value > 0) {
        kaku value = 1;
        kaesu value;
    }
    kaesu value;
}
`
	result := semanticAnalyze(t, text)
	innerUse := offsetOf(t, text, "kaesu value;")
	outerUse := strings.LastIndex(text, "kaesu value;")

	innerDefinition := definitionSite(t, result, innerUse+len("kaesu "))
	if innerDefinition == nil {
		t.Fatal("inner definition not found")
	}
	if innerDefinition.Span.Start.Offset != offsetOf(t, text, "value = 1") {
		t.Fatalf("inner definition points to offset %d", innerDefinition.Span.Start.Offset)
	}
	outerDefinition := definitionSite(t, result, outerUse+len("kaesu "))
	if outerDefinition == nil {
		t.Fatal("outer definition not found")
	}
	if outerDefinition.Span.Start.Offset != offsetOf(t, text, "value = x") {
		t.Fatalf("outer definition points to offset %d", outerDefinition.Span.Start.Offset)
	}
	if innerDefinition.Span.Start.Offset == outerDefinition.Span.Start.Offset {
		t.Fatal("shadowed variables must not share a declaration")
	}

	// 引用必须按绑定区分：内层 1 处、外层 2 处（声明 + 末尾使用）。
	innerRefs := referenceSites(t, result, innerUse+len("kaesu "))
	if len(innerRefs) != 1 {
		t.Fatalf("inner refs = %d, want 1 (%#v)", len(innerRefs), innerRefs)
	}
	// 外层变量在 moshi 条件与末尾 kaesu 中各被引用一次。
	outerRefs := referenceSites(t, result, outerUse+len("kaesu "))
	if len(outerRefs) != 2 {
		t.Fatalf("outer refs = %d, want 2 (%#v)", len(outerRefs), outerRefs)
	}
}

func TestSemanticStructFields(t *testing.T) {
	text := `kata Point { x: int64; y: int64; }

waza main() -> int64 {
    kaku p = Point { x: 1, y: 2 };
    kaesu p.x;
}
`
	result := semanticAnalyze(t, text)
	fieldUse := offsetOf(t, text, "p.x") + len("p.")
	requireHover(t, result, fieldUse, "x: int64")

	definition := definitionSite(t, result, fieldUse)
	if definition == nil {
		t.Fatal("field definition not found")
	}
	if definition.Span.Start.Offset != offsetOf(t, text, "x: int64") {
		t.Fatalf("field definition offset = %d", definition.Span.Start.Offset)
	}
	// 字段声明、结构体字面量字段与读取都属于同一字段身份。
	fieldDeclaration := offsetOf(t, text, "x: int64")
	refs := referenceSites(t, result, fieldDeclaration)
	if len(refs) != 2 {
		t.Fatalf("field refs = %d, want 2 (%#v)", len(refs), refs)
	}

	// 结构体类型本身可跳转到声明名称。
	pointDefinition := definitionSite(t, result, offsetOf(t, text, "Point { x"))
	if pointDefinition == nil || pointDefinition.Span.Start.Offset != offsetOf(t, text, "Point") {
		t.Fatalf("struct literal type definition = %#v", pointDefinition)
	}
}

func TestSemanticEnumVariants(t *testing.T) {
	text := `sentaku Color { Red; Green; }

waza name(c: Color) -> int64 {
    awase (c) {
        baai Red { kaesu 1; }
        baai Green { kaesu 2; }
    }
}

waza make() -> Color {
    kaesu Color.Red();
}
`
	result := semanticAnalyze(t, text)
	if result.Diagnostics == nil {
		t.Fatal("diagnostics must be an empty slice")
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", result.Diagnostics)
	}
	matchRed := offsetOf(t, text, "baai Red") + len("baai ")
	requireHover(t, result, matchRed, "Color.Red")
	matchDefinition := definitionSite(t, result, matchRed)
	if matchDefinition == nil || matchDefinition.Span.Start.Offset != offsetOf(t, text, "Red;") {
		t.Fatalf("match variant definition = %#v", matchDefinition)
	}

	constructor := offsetOf(t, text, "Color.Red()") + len("Color.")
	requireHover(t, result, constructor, "Color.Red")
	constructorDefinition := definitionSite(t, result, constructor)
	if constructorDefinition == nil || constructorDefinition.Span.Start.Offset != offsetOf(t, text, "Red;") {
		t.Fatalf("constructor variant definition = %#v", constructorDefinition)
	}
}

func TestSemanticGenericSignatures(t *testing.T) {
	text := `waza identity<T>(value: T) -> T {
    kaesu value;
}

waza main() -> int64 {
    kaesu identity(7);
}
`
	result := semanticAnalyze(t, text)
	requireHover(t, result, offsetOf(t, text, "identity<T>"), "waza identity<T>(value: T) -> T")
	requireHover(t, result, offsetOf(t, text, "value: T")+len("value: "), "类型参数 `T`")

	callOffset := offsetOf(t, text, "identity(7)") + len("identity(")
	help := result.signatureHelpAt(callOffset)
	if help == nil {
		t.Fatal("signature help is nil")
	}
	signature := help.Signatures[0]
	if signature.Label != "identity<T>(value: T) -> T" {
		t.Fatalf("signature label = %q", signature.Label)
	}
	if len(signature.Parameters) != 1 || signature.Parameters[0].Label != "value: T" {
		t.Fatalf("parameters = %#v", signature.Parameters)
	}
	if help.ActiveParameter != 0 {
		t.Fatalf("active parameter = %d", help.ActiveParameter)
	}
}

func TestSemanticClosureCapture(t *testing.T) {
	text := `waza main() -> int64 {
    kaku base = 10;
    kaku add = waza (delta: int64) -> int64 {
        kaesu base + delta;
    };
    kaesu add(5);
}
`
	result := semanticAnalyze(t, text)
	captureUse := offsetOf(t, text, "base + delta")
	requireHover(t, result, captureUse, "kaku base: int64")
	definition := definitionSite(t, result, captureUse)
	if definition == nil || definition.Span.Start.Offset != offsetOf(t, text, "base = 10") {
		t.Fatalf("captured variable definition = %#v", definition)
	}
	refs := referenceSites(t, result, offsetOf(t, text, "base = 10"))
	if len(refs) != 1 {
		t.Fatalf("captured refs = %d, want 1 (%#v)", len(refs), refs)
	}
}

func TestSemanticConstantHoverAndDefinition(t *testing.T) {
	text := `teisu Limit: int64 = 10;

waza main() -> int64 {
    kaesu Limit;
}
`
	result := semanticAnalyze(t, text)
	use := offsetOf(t, text, "Limit;")
	requireHover(t, result, use, "teisu Limit: int64")
	definition := definitionSite(t, result, use)
	if definition == nil || definition.Span.Start.Offset != offsetOf(t, text, "Limit") {
		t.Fatalf("constant definition = %#v", definition)
	}
}

func TestSemanticDocumentSymbols(t *testing.T) {
	text := `kata Point {
    x: int64;
    y: int64;
}

sentaku Color { Red; Green; }

waza main(p: Point) -> int64 {
    kaesu p.x;
}
`
	result := semanticAnalyze(t, text)
	symbols := result.documentSymbols()
	if len(symbols) != 3 {
		t.Fatalf("document symbols = %d, want 3 (%#v)", len(symbols), symbols)
	}
	point := symbols[0]
	if point.Name != "Point" || point.Kind != symbolKindStruct {
		t.Fatalf("first symbol = %#v", point)
	}
	if len(point.Children) != 2 || point.Children[0].Name != "x" || point.Children[1].Name != "y" {
		t.Fatalf("struct children = %#v", point.Children)
	}
	if point.Children[0].SelectionRange.Start.Line != 1 || point.Children[0].SelectionRange.Start.Character != 4 {
		t.Fatalf("field selection range = %#v", point.Children[0].SelectionRange)
	}
	color := symbols[1]
	if color.Name != "Color" || color.Kind != symbolKindEnum || len(color.Children) != 2 {
		t.Fatalf("enum symbol = %#v", color)
	}
	main := symbols[2]
	if main.Name != "main" || main.Kind != symbolKindFunction {
		t.Fatalf("function symbol = %#v", main)
	}
	if len(main.Children) != 1 || main.Children[0].Name != "p" {
		t.Fatalf("function children = %#v", main.Children)
	}
	// selectionRange 必须指向名称而不是整个声明。
	if main.SelectionRange.Start.Character != 5 || main.SelectionRange.End.Character != 9 {
		t.Fatalf("function selection range = %#v", main.SelectionRange)
	}
}

func TestSemanticCrossFile(t *testing.T) {
	root := t.TempDir()
	writeMioFile(t, filepath.Join(root, "bunko", "lib", "lib.mio"), `koukai kata Point {
    x: int64;
    y: int64;
}

koukai waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}

koukai waza twice(a: int64) -> int64 {
    kaesu add(a, a);
}
`)
	mainPath := filepath.Join(root, "main.mio")
	writeMioFile(t, mainPath, `yobu "lib";

waza main() -> int64 {
    kaku point = lib.Point { x: 1, y: 2 };
    kaesu lib.add(point.x, point.y);
}
`)
	libSource, err := os.ReadFile(filepath.Join(root, "bunko", "lib", "lib.mio"))
	if err != nil {
		t.Fatal(err)
	}
	result := semanticAnalyzeFile(t, root, mainPath)
	if result.Partial {
		t.Fatalf("cross-file analysis is partial: %#v", result.Diagnostics)
	}
	if len(result.Files) < 2 {
		t.Fatalf("merged files = %#v", result.Files)
	}
	callOffset := offsetOf(t, string(result.Text), "lib.add") + len("lib.")
	requireHover(t, result, callOffset, "waza add(a: int64, b: int64) -> int64")

	definition := definitionSite(t, result, callOffset)
	if definition == nil {
		t.Fatal("imported function definition not found")
	}
	libPath := filepath.Join(root, "bunko", "lib", "lib.mio")
	if definition.FileKey != uriFromPath(canonicalPath(libPath)) {
		t.Fatalf("definition file = %q, want %q", definition.FileKey, uriFromPath(canonicalPath(libPath)))
	}
	if definition.Span.Start.Offset != offsetOf(t, string(libSource), "add") {
		t.Fatalf("definition offset = %d", definition.Span.Start.Offset)
	}

	// 从入口文档查找 add 的引用：应同时包含入口调用与库内调用。
	callSymbol, _ := result.Index.symbolAt(result.DocKey, callOffset)
	if callSymbol == nil {
		t.Fatal("imported symbol not found")
	}
	refs := result.Index.references(callSymbol)
	if len(refs) != 2 {
		t.Fatalf("cross-module refs = %d, want 2 (%#v)", len(refs), refs)
	}
	mainRefs := 0
	libRefs := 0
	for _, ref := range refs {
		switch ref.FileKey {
		case uriFromPath(mainPath):
			mainRefs++
		case uriFromPath(canonicalPath(libPath)):
			libRefs++
		}
	}
	if mainRefs != 1 || libRefs != 1 {
		t.Fatalf("ref distribution main=%d lib=%d (%#v)", mainRefs, libRefs, refs)
	}
}

func TestSemanticInvalidSourceDoesNotPanic(t *testing.T) {
	cases := []string{
		"waza broken( -> int64 {",
		"waza broken() -> int64 {\n    kaesu missing_value;\n}\n",
		"kaku value = \"unterminated\n",
		"kaku values = [1, 2,\n",
		"yobu \"lib\"\n",
		"waza generic<T,>(value: T) -> T { kaesu value; }",
		"~~~",
		"",
	}
	for _, source := range cases {
		result := semanticAnalyze(t, source)
		if result.Index == nil {
			t.Fatalf("index is nil for %q", source)
		}
		// 查询任意位置都不能 panic。
		offset := len(source) / 2
		_ = occurrenceHoverMarkdown(result.Index, result.DocKey, offset)
		_, _ = result.Index.symbolAt(result.DocKey, offset)
		_ = result.documentSymbols()
		_ = result.signatureHelpAt(offset)
		_ = result.completionAt(result.Buffer.positionAt(offset))
	}
}

func TestSemanticRecursionAndMutualCalls(t *testing.T) {
	text := `waza fact(n: int64) -> int64 {
    moshi (n <= 1) {
        kaesu 1;
    }
    kaesu n * fact(n - 1);
}

waza is_even(n: int64) -> bool {
    moshi (n == 0) { kaesu hai; }
    kaesu is_odd(n - 1);
}

waza is_odd(n: int64) -> bool {
    moshi (n == 0) { kaesu iie; }
    kaesu is_even(n - 1);
}
`
	result := semanticAnalyze(t, text)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	recursiveCall := offsetOf(t, text, "fact(n - 1)")
	definition := definitionSite(t, result, recursiveCall)
	if definition == nil || definition.Span.Start.Offset != offsetOf(t, text, "fact(n") {
		t.Fatalf("recursive definition = %#v", definition)
	}
	mutualCall := offsetOf(t, text, "is_odd(n - 1)")
	definition = definitionSite(t, result, mutualCall)
	if definition == nil || definition.Span.Start.Offset != offsetOf(t, text, "is_odd(n:") {
		t.Fatalf("mutual definition = %#v", definition)
	}
	// 互相调用的引用必须落在同一个绑定上。
	refs := referenceSites(t, result, offsetOf(t, text, "is_odd(n:"))
	if len(refs) != 1 {
		t.Fatalf("mutual refs = %d (%#v)", len(refs), refs)
	}
}

func TestSemanticDocumentationHover(t *testing.T) {
	text := `// add 返回两个数之和。
// 第二行文档。
waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}
`
	result := semanticAnalyze(t, text)
	hover := occurrenceHoverMarkdown(result.Index, result.DocKey, offsetOf(t, text, "add(a"))
	if hover == nil {
		t.Fatal("hover is nil")
	}
	for _, want := range []string{"add 返回两个数之和", "第二行文档", "```text"} {
		if !strings.Contains(hover.Contents.Value, want) {
			t.Fatalf("hover = %q, want %q", hover.Contents.Value, want)
		}
	}
}
