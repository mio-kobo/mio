package playground

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"Mio/internal/mochi"
	"Mio/internal/stdlib"
)

func testLibrary() fs.FS { return Library(os.DirFS("../../bunko")) }

func execute(t *testing.T, request Request) Result {
	t.Helper()
	return Execute(request, testLibrary())
}

func TestRunCheckCompileExec(t *testing.T) {
	source := "yobu \"kiso/string\";\nchant(string.rune_count(\"ミオ🍡\"));\n1 + 2;\n"
	run := execute(t, Request{Operation: OpRun, Source: source})
	if run.Status != StatusOK || run.ExitCode != 0 || run.Stdout != "3\n3\n" {
		t.Fatalf("run=%+v", run)
	}
	check := execute(t, Request{Operation: OpCheck, Source: `chant("not executed");`})
	if check.Status != StatusChecked || check.Stdout != "" {
		t.Fatalf("check=%+v", check)
	}
	compiled := execute(t, Request{Operation: OpCompile, Source: source})
	if compiled.Status != StatusCompiled || !bytes.HasPrefix(compiled.Mochi, []byte("MOCHI")) {
		t.Fatalf("compile=%+v", compiled)
	}
	exec := execute(t, Request{Operation: OpExec, Mochi: compiled.Mochi})
	if exec.Status != StatusOK || exec.Stdout != run.Stdout || exec.MochiVersion != compiled.MochiVersion {
		t.Fatalf("exec=%+v", exec)
	}
}

func TestDiagnosticsAndRuntimeErrors(t *testing.T) {
	bad := execute(t, Request{Operation: OpRun, Source: "kaku n: int64 = \"x\";\n"})
	if bad.Status != StatusDiagnostic || bad.ExitCode != 1 || len(bad.Diagnostics) != 1 {
		t.Fatalf("diagnostic=%+v", bad)
	}
	if d := bad.Diagnostics[0]; d.Code != "E0301" || d.Line != 1 || d.Column != 17 || !strings.Contains(d.Rendered, "--> main.mio:1:17") {
		t.Fatalf("diagnostic=%+v", d)
	}
	crash := execute(t, Request{Operation: OpRun, Source: "kaku z = 0;\nchant(\"a\");\nchant(1 / z);\n"})
	if crash.Status != StatusRuntime || crash.Stdout != "a\n" || !strings.Contains(crash.Error, "division by zero") ||
		!strings.HasPrefix(crash.Diagnostics[0].Message, "运行时错误: mio: runtime: ") ||
		!strings.HasPrefix(crash.Diagnostics[0].Rendered, "error[E0501]: runtime error: mio: runtime: ") {
		t.Fatalf("runtime=%+v", crash)
	}
}

func TestLimits(t *testing.T) {
	budget := execute(t, Request{Operation: OpRun, Source: "aida (hai) {}", InstructionLimit: 500})
	if budget.Status != StatusRuntime || !strings.Contains(budget.Error, "instruction limit 500 exceeded") {
		t.Fatalf("budget=%+v", budget)
	}
	flood := execute(t, Request{Operation: OpRun, Source: `aida (hai) { chant("0123456789"); }`})
	if flood.Status != StatusRuntime || len(flood.Stdout) != MaxOutputBytes || !strings.Contains(flood.Error, "output limit exceeded") {
		t.Fatalf("flood status=%s len=%d err=%s", flood.Status, len(flood.Stdout), flood.Error)
	}
	for _, request := range []Request{
		{Operation: OpRun, Source: strings.Repeat(" ", MaxSourceBytes+1)},
		{Operation: OpExec, Mochi: make([]byte, MaxMochiBytes+1)},
		{Operation: OpRun, Source: "chant(1);", InstructionLimit: MaxInstructionLimit + 1},
		{Operation: "shell", Source: "chant(1);"},
	} {
		if result := execute(t, request); result.Status != StatusRejected || result.ExitCode != 1 {
			t.Fatalf("%s accepted: %+v", request.Operation, result)
		}
	}
}

func TestMalformedMochiIsRejected(t *testing.T) {
	compiled := execute(t, Request{Operation: OpCompile, Source: "waza f(x: int64) -> int64 { kaesu x * 2; }\nchant(f(4));\n"})
	valid := compiled.Mochi
	future := append([]byte(nil), valid...)
	binary.BigEndian.PutUint16(future[5:7], mochi.Version+1)
	for name, data := range map[string][]byte{
		"empty": nil, "signature": append([]byte("MOCHA"), valid[5:]...), "future version": future,
		"truncated": valid[:len(valid)-1], "trailing": append(append([]byte(nil), valid...), 0),
	} {
		result := execute(t, Request{Operation: OpExec, Mochi: data})
		if result.Status != StatusRejected || result.Stdout != "" {
			t.Fatalf("%s: %+v", name, result)
		}
	}
	// No single-byte corruption may crash the host; each either runs, fails, or is rejected.
	for index := 7; index < len(valid); index++ {
		for _, value := range []byte{0x00, 0xff, valid[index] ^ 0x5a} {
			data := append([]byte(nil), valid...)
			data[index] = value
			if result := execute(t, Request{Operation: OpExec, Mochi: data, InstructionLimit: 10000}); result.Status == StatusInternal {
				t.Fatalf("byte %d=%#x: %+v", index, value, result)
			}
		}
	}
}

func TestClosedLibrary(t *testing.T) {
	for _, source := range []string{`yobu "kiso/fs";`, `yobu "kiso/io";`, `yobu "kiso/os";`, `yobu "mio/fs/runtime";`,
		`yobu "kiso/testing";`, `yobu "../secrets";`, `yobu "kiso/aot/elf";`} {
		result := execute(t, Request{Operation: OpRun, Source: source})
		if result.Status != StatusDiagnostic {
			t.Fatalf("%s: %+v", source, result)
		}
	}
	library := testLibrary()
	if _, err := fs.ReadFile(library, "kiso/math/math_test.mio"); err == nil {
		t.Fatal("test source visible")
	}
	if _, err := fs.ReadFile(library, "kiso/fs/fs.mio"); err == nil {
		t.Fatal("kiso/fs visible")
	}
	for _, name := range LibraryPackages {
		if entries, err := fs.ReadDir(library, name); err != nil || len(entries) == 0 {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// The denied registry must expose exactly the reference CLI's host functions and signatures,
// otherwise Mochi files built by `mio build` or the native CLI would fail to bind.
func TestDeniedRegistryMatchesReference(t *testing.T) {
	reference, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	denied, err := DeniedRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for path := range hostModules {
		want, ok := reference.Module(path)
		got, _ := denied.Module(path)
		if !ok || len(want) != len(got) {
			t.Fatalf("%s: reference %d functions, denied %d", path, len(want), len(got))
		}
		for _, function := range want {
			replacement, ok := denied.Resolve(function.ID)
			if !ok || replacement.BindingKey != function.BindingKey || replacement.Public != function.Public {
				t.Fatalf("%s: %+v vs %+v", function.ID, function, replacement)
			}
			if err := replacement.Function(nil); !errors.Is(err, ErrCapability) {
				t.Fatalf("%s not denied: %v", function.ID, err)
			}
		}
	}
	for _, path := range []string{"mio/fs/runtime", "mio/sys/socket"} {
		if _, ok := hostModules[path]; !ok {
			t.Fatalf("%s is not covered", path)
		}
	}
}

func TestHostCallIsDenied(t *testing.T) {
	// Compile with the reference registry (as `mio build` does), then run in the playground.
	source := "yobu \"kiso/fs\";\nchant(\"before\");\nkaku r = fs.read_all(\"/etc/hostname\");\nchant(r.error == mu);\n"
	data := referenceBuild(t, source)
	result := execute(t, Request{Operation: OpExec, Mochi: data})
	if result.Status != StatusRuntime || result.Stdout != "before\n" || !strings.Contains(result.Error, "host capability is not available") {
		t.Fatalf("result=%+v", result)
	}
}
