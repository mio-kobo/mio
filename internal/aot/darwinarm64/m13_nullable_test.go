package darwinarm64

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestM13NullableGCProbe(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	image, err := M13NullableGCProbe()
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/m13-nullable-probe"
	if err := os.WriteFile(path, image, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("codesign", "-s", "-", path).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, output)
	}
	output, err := exec.Command(path).CombinedOutput()
	if err != nil || !bytes.Equal(output, []byte("M13 nullable PASS\n")) {
		t.Fatalf("probe: %v: %q", err, output)
	}
}
