package lsp

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func requestPosition(text string, offset int) Position {
	return newTextBuffer(text).positionAt(offset)
}

func TestServerSemanticCapabilities(t *testing.T) {
	h := newHarness(t, Options{})
	response := h.request(1, "initialize", map[string]any{
		"processId":    nil,
		"rootUri":      nil,
		"capabilities": map[string]any{},
	})
	if response.Error != nil {
		t.Fatalf("initialize failed: %#v", response.Error)
	}
	var raw struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(response.Result, &raw); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"hoverProvider",
		"definitionProvider",
		"referencesProvider",
		"documentSymbolProvider",
		"workspaceSymbolProvider",
		"renameProvider",
		"documentFormattingProvider",
		"completionProvider",
		"signatureHelpProvider",
	} {
		value, ok := raw.Capabilities[required]
		if !ok {
			t.Fatalf("capability %q is missing", required)
		}
		if string(value) == "false" {
			t.Fatalf("capability %q is false", required)
		}
	}
	for _, forbidden := range []string{
		"semanticTokensProvider",
		"documentRangeFormattingProvider",
		"documentOnTypeFormattingProvider",
		"codeActionProvider",
	} {
		if _, ok := raw.Capabilities[forbidden]; ok {
			t.Fatalf("capability %q must not be advertised", forbidden)
		}
	}
	h.notify("initialized", map[string]any{})
	h.shutdown()
	h.exit()
}

func TestServerHoverDefinitionReferencesSymbols(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	text := `waza add(a: int64, b: int64) -> int64 {
    kaku sum = a + b;
    kaesu sum;
}
`
	uri := uriFromPath(filepath.Join(t.TempDir(), "sample.mio"))
	h.open(uri, text, 1)

	useOffset := offsetOf(t, text, "kaesu sum") + len("kaesu ")
	position := requestPosition(text, useOffset)
	positionParams := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	}

	hoverResponse := h.request(2, "textDocument/hover", positionParams)
	if hoverResponse.Error != nil {
		t.Fatalf("hover failed: %#v", hoverResponse.Error)
	}
	var hover Hover
	if err := json.Unmarshal(hoverResponse.Result, &hover); err != nil {
		t.Fatal(err)
	}
	if hover.Contents.Kind != "markdown" || !strings.Contains(hover.Contents.Value, "kaku sum: int64") {
		t.Fatalf("hover = %#v", hover)
	}
	if hover.Range == nil || hover.Range.Start.Line != 2 {
		t.Fatalf("hover range = %#v", hover.Range)
	}

	definitionResponse := h.request(3, "textDocument/definition", positionParams)
	var locations []Location
	if err := json.Unmarshal(definitionResponse.Result, &locations); err != nil {
		t.Fatal(err)
	}
	if len(locations) != 1 {
		t.Fatalf("definition locations = %#v", locations)
	}
	if locations[0].URI != uri {
		t.Fatalf("definition uri = %q", locations[0].URI)
	}
	if locations[0].Range.Start.Line != 1 || locations[0].Range.Start.Character != 9 {
		t.Fatalf("definition range = %#v", locations[0].Range)
	}

	referenceParams := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": true},
	}
	referencesResponse := h.request(4, "textDocument/references", referenceParams)
	if err := json.Unmarshal(referencesResponse.Result, &locations); err != nil {
		t.Fatal(err)
	}
	if len(locations) != 2 {
		t.Fatalf("references = %#v", locations)
	}

	symbolsResponse := h.request(5, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var symbols []*DocumentSymbol
	if err := json.Unmarshal(symbolsResponse.Result, &symbols); err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 1 || symbols[0].Name != "add" || symbols[0].Kind != symbolKindFunction {
		t.Fatalf("symbols = %#v", symbols)
	}
	if len(symbols[0].Children) != 2 || symbols[0].Children[0].Name != "a" {
		t.Fatalf("symbol children = %#v", symbols[0].Children)
	}
	if symbols[0].SelectionRange.Start.Character != 5 {
		t.Fatalf("selection range = %#v", symbols[0].SelectionRange)
	}

	h.shutdown()
	h.exit()
}

func TestServerCompletionAndSignatureHelp(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	text := `waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}

waza main() -> int64 {
    kaku value = 1;
` + "    \n" + `    kaesu add(1, 2);
}
`
	uri := uriFromPath(filepath.Join(t.TempDir(), "completion.mio"))
	h.open(uri, text, 1)

	completionOffset := offsetOf(t, text, "    \n    kaesu add")
	completionPosition := requestPosition(text, completionOffset)
	completionResponse := h.request(2, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     completionPosition,
		"context":      map[string]any{"triggerKind": 1},
	})
	var completion CompletionList
	if err := json.Unmarshal(completionResponse.Result, &completion); err != nil {
		t.Fatal(err)
	}
	labels := make(map[string]CompletionItem, len(completion.Items))
	for _, item := range completion.Items {
		labels[item.Label] = item
	}
	for _, want := range []string{"value", "add", "main", "kaku", "int64"} {
		if _, ok := labels[want]; !ok {
			t.Fatalf("completion missing %q", want)
		}
	}

	signatureOffset := offsetOf(t, text, "add(1, ") + len("add(1, ")
	signaturePosition := requestPosition(text, signatureOffset)
	signatureResponse := h.request(3, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     signaturePosition,
	})
	var help SignatureHelp
	if err := json.Unmarshal(signatureResponse.Result, &help); err != nil {
		t.Fatal(err)
	}
	if len(help.Signatures) != 1 || help.Signatures[0].Label != "add(a: int64, b: int64) -> int64" {
		t.Fatalf("signature = %#v", help)
	}
	if help.ActiveParameter != 1 {
		t.Fatalf("active parameter = %d", help.ActiveParameter)
	}

	h.shutdown()
	h.exit()
}

func TestServerSemanticResultsFollowDocumentVersion(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	uri := uriFromPath(filepath.Join(t.TempDir(), "version.mio"))
	first := `waza main() -> int64 {
    kaku value = 1;
    kaesu value;
}
`
	h.open(uri, first, 1)
	position := requestPosition(first, offsetOf(t, first, "value;"))
	hoverParams := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	}
	response := h.request(2, "textDocument/hover", hoverParams)
	var hover Hover
	if err := json.Unmarshal(response.Result, &hover); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents.Value, "int64") {
		t.Fatalf("first hover = %#v", hover)
	}

	second := strings.Replace(first, "kaku value = 1;", "kaku value = \"text\";", 1)
	h.change(uri, 2, second)
	response = h.request(3, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     requestPosition(second, offsetOf(t, second, "value;")),
	})
	hover = Hover{}
	if err := json.Unmarshal(response.Result, &hover); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents.Value, "string") {
		t.Fatalf("updated hover = %#v", hover)
	}

	h.shutdown()
	h.exit()
}

func TestServerSemanticUnknownSymbolsReturnEmpty(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	text := `waza main() -> int64 {
    kaesu 1;
}
`
	uri := uriFromPath(filepath.Join(t.TempDir(), "unknown.mio"))
	h.open(uri, text, 1)
	position := requestPosition(text, offsetOf(t, text, "kaesu"))
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	}

	hover := h.request(2, "textDocument/hover", params)
	if string(hover.Result) != "null" {
		t.Fatalf("hover result = %s", hover.Result)
	}
	definition := h.request(3, "textDocument/definition", params)
	if string(definition.Result) != "null" {
		t.Fatalf("definition result = %s", definition.Result)
	}
	references := h.request(4, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": true},
	})
	var locations []Location
	if err := json.Unmarshal(references.Result, &locations); err != nil {
		t.Fatal(err)
	}
	if len(locations) != 0 {
		t.Fatalf("references = %#v", locations)
	}
	signature := h.request(5, "textDocument/signatureHelp", params)
	if string(signature.Result) != "null" {
		t.Fatalf("signature result = %s", signature.Result)
	}

	invalid := h.request(6, "textDocument/hover", map[string]any{})
	if invalid.Error == nil || invalid.Error.Code != codeInvalidParams {
		t.Fatalf("invalid params error = %#v", invalid.Error)
	}

	h.shutdown()
	h.exit()
}

func TestServerSemanticRequestCancellation(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	text := validSource
	uri := uriFromPath(filepath.Join(t.TempDir(), "cancel.mio"))
	h.open(uri, text, 1)
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     requestPosition(text, 0),
	}

	// 预取消：取消通知先于请求到达，请求必须以 -32800 结束。
	h.notify("$/cancelRequest", map[string]any{"id": 21})
	response := h.request(21, "textDocument/hover", params)
	if response.Error == nil || response.Error.Code != codeRequestCancelled {
		t.Fatalf("cancel response = %#v", response.Error)
	}

	h.shutdown()
	h.exit()
}

func TestServerSemanticConcurrentRequests(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	text := `waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}
`
	uri := uriFromPath(filepath.Join(t.TempDir(), "concurrent.mio"))
	h.open(uri, text, 1)
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     requestPosition(text, offsetOf(t, text, "add(a")),
	}
	ids := []int{31, 32, 33, 34, 35, 36}
	for _, id := range ids {
		h.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "textDocument/hover", "params": params})
	}
	pending := make(map[string]bool, len(ids))
	for _, id := range ids {
		pending[strconv.Itoa(id)] = true
	}
	for len(pending) > 0 {
		message := h.next(5 * time.Second)
		if message.Method != "" || len(message.ID) == 0 {
			continue
		}
		key := string(message.ID)
		if !pending[key] {
			continue
		}
		delete(pending, key)
		if message.Error != nil {
			t.Fatalf("hover %s failed: %#v", key, message.Error)
		}
		var hover Hover
		if err := json.Unmarshal(message.Result, &hover); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(hover.Contents.Value, "waza add") {
			t.Fatalf("hover result = %#v", hover)
		}
	}

	h.shutdown()
	h.exit()
}

func TestServerSemanticUnicodeHoverOverProtocol(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	text := "waza main() -> int64 {\r\n    kaku note = \"😀こんにちは\"; kaesu note;\r\n}\r\n"
	uri := uriFromPath(filepath.Join(t.TempDir(), "unicode.mio"))
	h.open(uri, text, 1)
	offset := offsetOf(t, text, "kaesu note") + len("kaesu ")
	response := h.request(2, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     requestPosition(text, offset),
	})
	var hover Hover
	if err := json.Unmarshal(response.Result, &hover); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents.Value, "kaku note: string") {
		t.Fatalf("hover = %#v", hover)
	}
	if hover.Range == nil || hover.Range.Start.Line != 1 {
		t.Fatalf("hover range = %#v", hover.Range)
	}

	h.shutdown()
	h.exit()
}

func TestServerReferencesAcrossOpenDocuments(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "bunko", "util", "util.mio")
	libSource := `koukai waza visible(value: int64) -> int64 {
    kaesu value;
}
`
	writeMioFile(t, libPath, libSource)
	mainPath := filepath.Join(root, "main.mio")
	mainSource := `yobu "util";

waza main() -> int64 {
    kaesu util.visible(1);
}
`
	writeMioFile(t, mainPath, mainSource)

	h := newHarness(t, Options{})
	h.initialize()
	h.open(uriFromPath(libPath), libSource, 1)
	h.open(uriFromPath(mainPath), mainSource, 1)

	position := requestPosition(libSource, offsetOf(t, libSource, "visible(value"))
	response := h.request(2, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uriFromPath(libPath)},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": true},
	})
	var locations []Location
	if err := json.Unmarshal(response.Result, &locations); err != nil {
		t.Fatal(err)
	}
	mainFound := false
	for _, location := range locations {
		if location.URI == uriFromPath(mainPath) {
			mainFound = true
		}
	}
	if !mainFound {
		t.Fatalf("references from importing document are missing: %#v", locations)
	}

	h.shutdown()
	h.exit()
}

func TestServerSemanticIncrementalEditUpdatesHover(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	text := `kata Point {
    x: int64;
    y: int64;
}

sentaku Color {
    Red;
    Green;
}

// 两个数相加。
waza add(a: int64, b: int64) -> int64 {
    kaku sum = a + b;
    kaesu sum;
}

waza main() -> int64 {
    kaku point = Point { x: 1, y: 2 };
    kaku color: Color = Color.Red();
    kaesu add(point.x, point.y);
}
`
	uri := uriFromPath(filepath.Join(t.TempDir(), "incremental-semantic.mio"))
	h.open(uri, text, 1)
	sumUse := requestPosition(text, offsetOf(t, text, "kaesu sum")+len("kaesu "))

	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     sumUse,
	}
	first := h.request(2, "textDocument/hover", params)
	var hover Hover
	if err := json.Unmarshal(first.Result, &hover); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents.Value, "int64") {
		t.Fatalf("initial hover = %#v", hover)
	}

	// 与 VS Code editor.edit 相同的增量替换：只覆盖行首到分号前。
	h.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{
			{
				"range": map[string]any{
					"start": map[string]any{"line": 12, "character": 0},
					"end":   map[string]any{"line": 12, "character": len("kaku sum = a + b;")},
				},
				"text": `    kaku sum = "text";`,
			},
		},
	})
	updated := h.request(3, "textDocument/hover", params)
	hover = Hover{}
	if err := json.Unmarshal(updated.Result, &hover); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents.Value, "string") {
		t.Fatalf("updated hover = %#v", hover)
	}

	h.shutdown()
	h.exit()
}

func TestServerRespectsClientContentCapabilities(t *testing.T) {
	h := newHarness(t, Options{})
	response := h.request(1, "initialize", map[string]any{
		"processId": nil,
		"rootUri":   nil,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"hover": map[string]any{"contentFormat": []string{"plaintext"}},
				"completion": map[string]any{
					"completionItem": map[string]any{"snippetSupport": false},
				},
			},
		},
	})
	if response.Error != nil {
		t.Fatalf("initialize failed: %#v", response.Error)
	}
	h.notify("initialized", map[string]any{})

	text := `waza add(a: int64, b: int64) -> int64 {
    kaku sum = a + b;
    kaesu sum;
}
`
	uri := uriFromPath(filepath.Join(t.TempDir(), "capabilities.mio"))
	h.open(uri, text, 1)
	hoverResponse := h.request(2, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     requestPosition(text, offsetOf(t, text, "kaesu sum")+len("kaesu ")),
	})
	var hover Hover
	if err := json.Unmarshal(hoverResponse.Result, &hover); err != nil {
		t.Fatal(err)
	}
	if hover.Contents.Kind != "plaintext" {
		t.Fatalf("hover kind = %q", hover.Contents.Kind)
	}
	if strings.Contains(hover.Contents.Value, "```") {
		t.Fatalf("plaintext hover contains markdown fence: %q", hover.Contents.Value)
	}
	if !strings.Contains(hover.Contents.Value, "kaku sum: int64") {
		t.Fatalf("plaintext hover = %q", hover.Contents.Value)
	}

	completionResponse := h.request(3, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     requestPosition(text, offsetOf(t, text, "    kaesu sum")),
	})
	var completion CompletionList
	if err := json.Unmarshal(completionResponse.Result, &completion); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range completion.Items {
		if item.Label != "add" {
			continue
		}
		found = true
		if item.InsertTextFormat != 0 || item.InsertText != "add" {
			t.Fatalf("non-snippet completion item = %#v", item)
		}
	}
	if !found {
		t.Fatalf("completion missing add: %#v", completion.Items)
	}

	h.shutdown()
	h.exit()
}

func TestServerReferencesAcrossOpenDocumentsForFields(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "bunko", "util", "util.mio")
	libSource := `koukai kata Point { x: int64; y: int64; }
`
	writeMioFile(t, libPath, libSource)
	mainPath := filepath.Join(root, "main.mio")
	mainSource := `yobu "util";

waza main() -> int64 {
    kaku point = util.Point { x: 1, y: 2 };
    kaesu point.x;
}
`
	writeMioFile(t, mainPath, mainSource)

	h := newHarness(t, Options{})
	h.initialize()
	h.open(uriFromPath(libPath), libSource, 1)
	h.open(uriFromPath(mainPath), mainSource, 1)

	position := requestPosition(libSource, offsetOf(t, libSource, "x: int64"))
	response := h.request(2, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uriFromPath(libPath)},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": true},
	})
	var locations []Location
	if err := json.Unmarshal(response.Result, &locations); err != nil {
		t.Fatal(err)
	}
	mainLocations := 0
	for _, location := range locations {
		if location.URI == uriFromPath(mainPath) {
			mainLocations++
		}
	}
	if mainLocations != 2 {
		t.Fatalf("cross-document field references in main = %d, want 2 (%#v)", mainLocations, locations)
	}

	h.shutdown()
	h.exit()
}

func TestServerWorkspaceSymbols(t *testing.T) {
	root := t.TempDir()
	// 路径含空格与中日文字符，另有应被跳过的隐藏目录与产物目录。
	dir := filepath.Join(root, "src 日本語")
	writeMioFile(t, filepath.Join(dir, "a.mio"), "waza helloWorld() -> unit {\n}\nkata Point { x: int64; }\n")
	writeMioFile(t, filepath.Join(dir, "b.mio"), "waza other() -> unit {\n}\n")
	writeMioFile(t, filepath.Join(root, ".hidden", "c.mio"), "waza hiddenFn() -> unit {\n}\n")
	writeMioFile(t, filepath.Join(root, "node_modules", "d.mio"), "waza vendored() -> unit {\n}\n")

	h := newHarness(t, Options{})
	response := h.request(1, "initialize", map[string]any{
		"processId":    nil,
		"rootUri":      uriFromPath(root),
		"capabilities": map[string]any{},
	})
	if response.Error != nil {
		t.Fatalf("initialize failed: %#v", response.Error)
	}
	h.notify("initialized", map[string]any{})

	query := func(id int, q string) []SymbolInformation {
		t.Helper()
		r := h.request(id, "workspace/symbol", map[string]any{"query": q})
		if r.Error != nil {
			t.Fatalf("workspace/symbol failed: %#v", r.Error)
		}
		var out []SymbolInformation
		if err := json.Unmarshal(r.Result, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	names := func(symbols []SymbolInformation) string {
		list := make([]string, len(symbols))
		for i, s := range symbols {
			list[i] = s.Name
		}
		return strings.Join(list, ",")
	}

	all := query(3, "")
	if got := names(all); got != "helloWorld,other,Point" && got != "Point,helloWorld,other" {
		t.Fatalf("all symbols = %q", got)
	}
	for _, symbol := range all {
		if symbol.Name == "hiddenFn" || symbol.Name == "vendored" {
			t.Fatalf("skipped directory leaked: %#v", symbol)
		}
	}
	fuzzy := query(4, "hw")
	if len(fuzzy) != 1 || fuzzy[0].Name != "helloWorld" {
		t.Fatalf("fuzzy = %#v", fuzzy)
	}
	if point := query(7, "Point"); len(point) != 1 || point[0].Kind != symbolKindStruct {
		t.Fatalf("struct symbol = %#v", point)
	}
	if !strings.HasSuffix(fuzzy[0].Location.URI, "a.mio") || fuzzy[0].Location.Range.Start.Line != 0 {
		t.Fatalf("location = %#v", fuzzy[0].Location)
	}

	// 未保存的打开文档优先于磁盘内容。
	uri := uriFromPath(filepath.Join(dir, "b.mio"))
	h.open(uri, "waza renamedInEditor() -> unit {\n}\n", 1)
	if got := query(5, "renamedInEditor"); len(got) != 1 {
		t.Fatalf("unsaved symbol missing: %#v", got)
	}
	if got := query(6, "other"); len(got) != 0 {
		t.Fatalf("stale on-disk symbol returned: %#v", got)
	}
	h.shutdown()
	h.exit()
}

func TestServerRename(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()
	text := `waza f(a: int64) -> int64 {
    kaku sum = a;
    // sum in a comment
    kaku s = "sum";
    kaesu sum + sum;
}
`
	uri := uriFromPath(filepath.Join(t.TempDir(), "rename.mio"))
	h.open(uri, text, 1)
	position := requestPosition(text, offsetOf(t, text, "kaesu sum")+len("kaesu "))
	params := func(name string) map[string]any {
		return map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     position,
			"newName":      name,
		}
	}

	prepare := h.request(2, "textDocument/prepareRename", params(""))
	if prepare.Error != nil {
		t.Fatalf("prepareRename: %#v", prepare.Error)
	}
	var prepared prepareRenameResult
	if err := json.Unmarshal(prepare.Result, &prepared); err != nil || prepared.Placeholder != "sum" {
		t.Fatalf("prepare = %s (%v)", prepare.Result, err)
	}

	response := h.request(3, "textDocument/rename", params("total"))
	if response.Error != nil {
		t.Fatalf("rename: %#v", response.Error)
	}
	var edit WorkspaceEdit
	if err := json.Unmarshal(response.Result, &edit); err != nil {
		t.Fatal(err)
	}
	edits := edit.Changes[uri]
	if len(edits) != 3 {
		t.Fatalf("edits = %#v", edits)
	}
	for _, item := range edits {
		if item.NewText != "total" || (item.Range.Start.Line != 1 && item.Range.Start.Line != 4) {
			t.Fatalf("unexpected edit %#v", item)
		}
	}

	for index, bad := range []string{"", "kaku", "1abc", "a b", "a.b"} {
		r := h.request(10+index, "textDocument/rename", params(bad))
		if r.Error == nil || r.Error.Code != codeInvalidParams {
			t.Fatalf("newName %q must be rejected, got %#v", bad, r.Error)
		}
	}
	// 与同作用域已有符号冲突（参数 a）。
	conflict := h.request(20, "textDocument/rename", params("a"))
	if conflict.Error == nil || conflict.Error.Code != codeRequestFailed {
		t.Fatalf("conflict must be refused: %#v", conflict.Error)
	}
	// 导出的顶层函数不能重命名（其它文件可能使用）。
	exportedText := "koukai waza g() -> unit {\n}\n"
	exportedURI := uriFromPath(filepath.Join(t.TempDir(), "exported.mio"))
	h.open(exportedURI, exportedText, 1)
	exported := h.request(21, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": exportedURI},
		"position":     requestPosition(exportedText, offsetOf(t, exportedText, "g()")),
		"newName":      "h",
	})
	if exported.Error == nil || exported.Error.Code != codeRequestFailed {
		t.Fatalf("exported rename must be refused: %#v", exported.Error)
	}
	// 有语法错误的文件拒绝重命名。
	brokenText := "waza f() -> unit {\n    kaku x = ;\n}\n"
	brokenURI := uriFromPath(filepath.Join(t.TempDir(), "broken.mio"))
	h.open(brokenURI, brokenText, 1)
	broken := h.request(22, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": brokenURI},
		"position":     requestPosition(brokenText, offsetOf(t, brokenText, "f()")),
		"newName":      "g",
	})
	if broken.Error == nil || broken.Error.Code != codeRequestFailed {
		t.Fatalf("rename in broken file must be refused: %#v", broken.Error)
	}
	h.shutdown()
	h.exit()
}

func TestServerRenameTopLevelAndFields(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()
	text := `kata Point { x: int64; }
waza helper(p: Point) -> int64 {
    kaesu p.x;
}
waza main2() -> int64 {
    kaku p = Point { x: 1 };
    kaesu helper(p);
}
`
	uri := uriFromPath(filepath.Join(t.TempDir(), "top.mio"))
	h.open(uri, text, 1)
	rename := func(id int, anchor string, delta int, name string) []TextEdit {
		t.Helper()
		r := h.request(id, "textDocument/rename", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     requestPosition(text, offsetOf(t, text, anchor)+delta),
			"newName":      name,
		})
		if r.Error != nil {
			t.Fatalf("rename %q: %#v", anchor, r.Error)
		}
		var edit WorkspaceEdit
		if err := json.Unmarshal(r.Result, &edit); err != nil {
			t.Fatal(err)
		}
		return edit.Changes[uri]
	}
	if got := rename(2, "helper(p)", 0, "assist"); len(got) != 2 {
		t.Fatalf("function rename edits = %#v", got)
	}
	if got := rename(3, "x: int64", 0, "px"); len(got) != 3 {
		t.Fatalf("field rename edits = %#v", got)
	}
	h.shutdown()
	h.exit()
}

func TestServerDocumentFormatting(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()
	text := "waza f()->int64{\nkaesu 1;\n}\n// 日本語 🙂\n"
	uri := uriFromPath(filepath.Join(t.TempDir(), "fmt.mio"))
	h.open(uri, text, 1)
	response := h.request(2, "textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 8, "insertSpaces": false},
	})
	if response.Error != nil {
		t.Fatalf("formatting: %#v", response.Error)
	}
	var edits []TextEdit
	if err := json.Unmarshal(response.Result, &edits); err != nil {
		t.Fatal(err)
	}
	want := "waza f() -> int64 {\n    kaesu 1;\n}\n// 日本語 🙂\n"
	if len(edits) != 1 || edits[0].NewText != want {
		t.Fatalf("edits = %#v", edits)
	}
	// 范围的结尾必须使用 UTF-16 位置（🙂 占两个单元）。
	if edits[0].Range.Start != (Position{}) || edits[0].Range.End.Line != 4 || edits[0].Range.End.Character != 0 {
		t.Fatalf("range = %#v", edits[0].Range)
	}

	formattedURI := uriFromPath(filepath.Join(t.TempDir(), "ok.mio"))
	h.open(formattedURI, want, 1)
	again := h.request(3, "textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": formattedURI}})
	if string(again.Result) != "[]" {
		t.Fatalf("already formatted must return [], got %s", again.Result)
	}

	brokenURI := uriFromPath(filepath.Join(t.TempDir(), "broken.mio"))
	h.open(brokenURI, "kaku x = ;\n", 1)
	broken := h.request(4, "textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": brokenURI}})
	if broken.Error == nil || broken.Error.Code != codeRequestFailed {
		t.Fatalf("unparseable buffer must be refused: %#v", broken.Error)
	}
	h.shutdown()
	h.exit()
}
