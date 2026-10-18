package darwinarm64

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestM12ArrayRuntimeProbe(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	image, err := M12ArrayProbe()
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/m12-array-probe"
	if err := os.WriteFile(path, image, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("codesign", "-s", "-", path).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, output)
	}
	output, err := exec.Command(path).CombinedOutput()
	if err != nil || !bytes.Equal(output, []byte("M12 array PASS\n")) {
		t.Fatalf("probe: %v: %q", err, output)
	}
}
