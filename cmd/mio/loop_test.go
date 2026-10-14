package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// 测试 run 与 build 后 exec 的循环输出一致
func TestCLILoopRunAndExecMatch(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "loop.mio")
	mochiPath := filepath.Join(directory, "loop.mochi")
	source := `
kaku count = 0;
aida (count < 5) {
    count = count + 1;
    moshi (count == 2) { tsuzuku; }
    moshi (count == 5) { nukeru; }
    chant(count);
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	var runOutput bytes.Buffer
	var runError bytes.Buffer
	if code := runCLI([]string{"run", sourcePath}, &runOutput, &runError); code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, runError.String())
	}

	var buildOutput bytes.Buffer
	var buildError bytes.Buffer
	if code := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &buildOutput, &buildError); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q", code, buildError.String())
	}

	var execOutput bytes.Buffer
	var execError bytes.Buffer
	if code := runCLI([]string{"exec", mochiPath}, &execOutput, &execError); code != 0 {
		t.Fatalf("exec exit = %d, stderr = %q", code, execError.String())
	}

	if want := "1\n3\n4\n"; runOutput.String() != want {
		t.Fatalf("run output = %q, want %q", runOutput.String(), want)
	}
	if execOutput.String() != runOutput.String() {
		t.Fatalf("exec output = %q, run output = %q", execOutput.String(), runOutput.String())
	}
}
