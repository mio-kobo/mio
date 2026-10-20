package bunko

import (
	"Mio/internal/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProgramWithOverlayOverridesRootSource(t *testing.T) {
	directory := t.TempDir()
	bunkoRoot := filepath.Join(directory, "bunko")
	if err := os.MkdirAll(bunkoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(directory, "main.mio")
	if err := os.WriteFile(sourcePath, []byte("waza broken( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProgram(sourcePath, bunkoRoot); err == nil {
		t.Fatal("disk source should fail to parse")
	}

	overlay := NewOverlay()
	overlay.Set(sourcePath, []byte("waza ok() -> int64 {\n    kaesu 1;\n}\n"))
	loaded, err := LoadProgramWithOverlay(sourcePath, bunkoRoot, nil, overlay)
	if err != nil {
		t.Fatalf("overlay source: %v", err)
	}
	if len(loaded.AST.Statements) != 1 {
		t.Fatalf("statements = %d", len(loaded.AST.Statements))
	}
	function, ok := loaded.AST.Statements[0].(*ast.FunctionStatement)
	if !ok || function.Name.Name != "ok" {
		t.Fatalf("unexpected statement %#v", loaded.AST.Statements[0])
	}
}

func TestLoadProgramWithOverlayCoversMissingFile(t *testing.T) {
	directory := t.TempDir()
	bunkoRoot := filepath.Join(directory, "bunko")
	if err := os.MkdirAll(bunkoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(directory, "missing.mio")
	overlay := NewOverlay()
	overlay.Set(sourcePath, []byte("kaku x = 1;\n"))
	loaded, err := LoadProgramWithOverlay(sourcePath, bunkoRoot, nil, overlay)
	if err != nil {
		t.Fatalf("missing file with overlay: %v", err)
	}
	if len(loaded.AST.Statements) != 1 {
		t.Fatalf("statements = %d", len(loaded.AST.Statements))
	}
}

func TestLoadProgramWithOverlayAppliesToImports(t *testing.T) {
	directory := t.TempDir()
	bunkoRoot := filepath.Join(directory, "bunko")
	packageDirectory := filepath.Join(bunkoRoot, "kiso", "sample")
	if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	packageSource := filepath.Join(packageDirectory, "sample.mio")
	if err := os.WriteFile(packageSource, []byte("koukai waza value() -> int64 {\n    kaesu 1;\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(directory, "main.mio")
	source := "yobu \"kiso/sample\";\n\nkaku x = sample.value();\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProgram(sourcePath, bunkoRoot); err != nil {
		t.Fatalf("disk program: %v", err)
	}

	overlay := NewOverlay()
	overlay.Set(packageSource, []byte("koukai waza value() -> int64 {\n    kaesu 1\n}\n"))
	if _, err := LoadProgramWithOverlay(sourcePath, bunkoRoot, nil, overlay); err == nil {
		t.Fatal("overlay dependency should fail to parse")
	}
}

func TestOverlayCopiesContent(t *testing.T) {
	overlay := NewOverlay()
	content := []byte("kaku x = 1;\n")
	overlay.Set("/tmp/mio-overlay-copy.mio", content)
	content[0] = 'X'
	stored, ok := overlay.Lookup("/tmp/mio-overlay-copy.mio")
	if !ok || strings.HasPrefix(string(stored), "X") {
		t.Fatalf("overlay stored aliased content: %q", stored)
	}
}
