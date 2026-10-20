package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const brokenSource = "waza broken() -> int64 {\n    kaesu missing_value;\n}\n"
const validSource = "waza add(a: int64, b: int64) -> int64 {\n    kaesu a + b;\n}\n"

func analyze(t *testing.T, uri, text string) []Diagnostic {
	t.Helper()
	instance := newAnalyzer("")
	return instance.diagnostics(context.Background(), &document{URI: uri, LanguageID: "mio", Version: 1, Text: text})
}

func requireSingleDiagnostic(t *testing.T, diagnostics []Diagnostic, code string) Diagnostic {
	t.Helper()
	if len(diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %#v", len(diagnostics), diagnostics)
	}
	if diagnostics[0].Code != code {
		t.Fatalf("code = %q, want %q (%#v)", diagnostics[0].Code, code, diagnostics[0])
	}
	if diagnostics[0].Source != diagnosticSource {
		t.Fatalf("source = %q", diagnostics[0].Source)
	}
	return diagnostics[0]
}

func TestAnalyzeValidProgram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "valid.mio")
	if err := os.WriteFile(path, []byte(validSource), 0o644); err != nil {
		t.Fatal(err)
	}
	diagnostics := analyze(t, uriFromPath(path), validSource)
	if diagnostics == nil {
		t.Fatal("diagnostics must be an empty slice, not nil")
	}
	if len(diagnostics) != 0 {
		t.Fatalf("valid program produced %#v", diagnostics)
	}
}

func TestAnalyzeUndefinedVariable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.mio")
	diagnostics := analyze(t, uriFromPath(path), brokenSource)
	diagnostic := requireSingleDiagnostic(t, diagnostics, "E0201")
	if diagnostic.Range.Start.Line != 1 || diagnostic.Range.Start.Character != 10 {
		t.Fatalf("range = %#v", diagnostic.Range)
	}
	if !strings.Contains(diagnostic.Message, "help:") {
		t.Fatalf("message should include compiler help: %q", diagnostic.Message)
	}
}

func TestAnalyzeTypeMismatch(t *testing.T) {
	source := "waza f() -> int64 {\n    kaesu \"text\";\n}\n"
	path := filepath.Join(t.TempDir(), "types.mio")
	diagnostics := analyze(t, uriFromPath(path), source)
	diagnostic := requireSingleDiagnostic(t, diagnostics, "E0304")
	if diagnostic.Range.Start.Line != 1 {
		t.Fatalf("range = %#v", diagnostic.Range)
	}
	if !strings.Contains(diagnostic.Message, "expected/actual") {
		t.Fatalf("message should include expected/actual: %q", diagnostic.Message)
	}
}

func TestAnalyzeSyntaxError(t *testing.T) {
	source := "waza f() -> int64 {\n    kaesu 1\n}\n"
	path := filepath.Join(t.TempDir(), "syntax.mio")
	diagnostics := analyze(t, uriFromPath(path), source)
	requireSingleDiagnostic(t, diagnostics, "E0101")
}

func TestAnalyzeLexicalError(t *testing.T) {
	source := "kaku x = @;\n"
	path := filepath.Join(t.TempDir(), "lexical.mio")
	diagnostics := analyze(t, uriFromPath(path), source)
	diagnostic := requireSingleDiagnostic(t, diagnostics, "E0001")
	if diagnostic.Range.Start != (Position{0, 9}) {
		t.Fatalf("range = %#v", diagnostic.Range)
	}
}

func TestAnalyzeUTF16PositionAfterEmoji(t *testing.T) {
	source := "kaku s = \"😀\";\nkaku x = missing_value;\n"
	path := filepath.Join(t.TempDir(), "emoji.mio")
	diagnostics := analyze(t, uriFromPath(path), source)
	diagnostic := requireSingleDiagnostic(t, diagnostics, "E0201")
	index := strings.Index(source, "missing_value")
	buffer := newTextBuffer(source)
	want := buffer.positionAt(index)
	if diagnostic.Range.Start != want {
		t.Fatalf("range start = %#v, want %#v", diagnostic.Range.Start, want)
	}
}

func TestAnalyzeBunkoImport(t *testing.T) {
	root := t.TempDir()
	packageDirectory := filepath.Join(root, "bunko", "kiso", "sample")
	if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(packageDirectory, "sample.mio"),
		[]byte("koukai waza value() -> int64 {\n    kaesu 41;\n}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "main.mio")
	source := "yobu \"kiso/sample\";\n\nwaza f() -> int64 {\n    kaesu sample.value() + 1;\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIO_BUNKO", filepath.Join(root, "bunko"))
	if diagnostics := analyze(t, uriFromPath(sourcePath), source); len(diagnostics) != 0 {
		t.Fatalf("valid Bunko import produced %#v", diagnostics)
	}

	broken := "yobu \"kiso/sample\";\n\nwaza f() -> int64 {\n    kaesu sample.missing();\n}\n"
	if diagnostics := analyze(t, uriFromPath(sourcePath), broken); len(diagnostics) == 0 {
		t.Fatal("missing exported member should produce a diagnostic")
	}
}

func TestAnalyzeUnsavedBufferOverridesDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overlay.mio")
	if err := os.WriteFile(path, []byte(validSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := uriFromPath(path)
	if diagnostics := analyze(t, uri, brokenSource); len(diagnostics) != 1 || diagnostics[0].Code != "E0201" {
		t.Fatalf("unsaved buffer should override disk: %#v", diagnostics)
	}
	if err := os.WriteFile(path, []byte(brokenSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if diagnostics := analyze(t, uri, validSource); len(diagnostics) != 0 {
		t.Fatalf("unsaved buffer should hide disk errors: %#v", diagnostics)
	}
}

func TestAnalyzeNonexistentFileUsesOverlay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created.mio")
	diagnostics := analyze(t, uriFromPath(path), brokenSource)
	requireSingleDiagnostic(t, diagnostics, "E0201")
}

func TestAnalyzeUntitledDocument(t *testing.T) {
	diagnostics := analyze(t, "untitled:Untitled-1", brokenSource)
	requireSingleDiagnostic(t, diagnostics, "E0201")
}

func TestAnalyzeCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	diagnostics := newAnalyzer("").diagnostics(ctx, &document{
		URI: "untitled:cancelled", LanguageID: "mio", Version: 1, Text: brokenSource,
	})
	if diagnostics != nil {
		t.Fatalf("cancelled analysis = %#v, want nil", diagnostics)
	}
}

func TestAnalyzeEmptyDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.mio")
	if diagnostics := analyze(t, uriFromPath(path), ""); len(diagnostics) != 0 {
		t.Fatalf("empty document produced %#v", diagnostics)
	}
}

func TestBunkoRootPrecedence(t *testing.T) {
	source := filepath.Join(t.TempDir(), "src", "a.mio")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIO_BUNKO", "/env/bunko")
	if got := bunkoRootFor(source, "/init/bunko"); got != "/env/bunko" {
		t.Fatalf("environment should win: %q", got)
	}
	t.Setenv("MIO_BUNKO", "")
	if got := bunkoRootFor(source, "/init/bunko"); got != "/init/bunko" {
		t.Fatalf("initialization option should be used: %q", got)
	}
	if got := bunkoRootFor(source, ""); got != "bunko" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestDiagnosticsEnabledFromSettings(t *testing.T) {
	cases := []struct {
		settings string
		want     bool
		found    bool
	}{
		{`{"mio":{"diagnostics":{"enabled":false}}}`, false, true},
		{`{"mio":{"diagnostics":{"enabled":true}}}`, true, true},
		{`{"diagnosticsEnabled":false}`, false, true},
		{`{"mio":{"diagnosticsEnabled":true}}`, true, true},
		{`{"unrelated":1}`, false, false},
		{`not-json`, false, false},
	}
	for _, test := range cases {
		got, found := diagnosticsEnabledFromSettings([]byte(test.settings))
		if got != test.want || found != test.found {
			t.Errorf("settings %s = (%t,%t), want (%t,%t)", test.settings, got, found, test.want, test.found)
		}
	}
}
