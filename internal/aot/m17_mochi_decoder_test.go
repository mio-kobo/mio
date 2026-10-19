package aot

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"Mio/internal/bunko"
	"Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/mochi"
	"Mio/internal/resolver"
	"Mio/internal/stdlib"
	"Mio/internal/typecheck"
	"Mio/internal/vm"
)

type m17Golden struct {
	name string
	path string
	size int
	hash string
}

func m17CompileMio(t *testing.T, root, source string) *compiler.Bytecode {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "main.mio")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := bunko.LoadProgramWithRegistry(path, filepath.Join(root, "bunko"), registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := constant.Fold(loaded.AST); err != nil {
		t.Fatal(err)
	}
	r := resolver.New()
	if err := r.ResolveProgram(loaded.AST); err != nil {
		t.Fatal(err)
	}
	if err := typecheck.NewWithResolver(r).CheckProgram(loaded.AST); err != nil {
		t.Fatal(err)
	}
	c := compiler.NewWithResolver(r)
	if err := c.Compile(loaded.AST); err != nil {
		t.Fatal(err)
	}
	return c.Bytecode()
}

func m17InstructionBytes(program *compiler.Bytecode) int {
	total := len(program.Instructions)
	for i := range program.Functions {
		total += len(program.Functions[i].Instructions)
	}
	return total
}

func m17RunVM(t *testing.T, program *compiler.Bytecode, limit uint64) []byte {
	t.Helper()
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	machine := vm.NewWithRegistryAndOutput(program, registry, &output)
	if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: limit}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestM17PureMioDecoderMalformedVMAndNative(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("M17 native differential requires macOS ARM64")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	source := `yobu "kiso/encoding/mochi";
waza show(data: []uint8) -> unit {
    kaku problem = mochi.decode(data).error;
    moshi (problem != mu) { chant(mochi.error_code(problem)); } sore { chant(0); }
}
show([]);
show([0, 79, 67, 72, 73, 0, 14]);
show([77, 79, 67, 72, 73, 0, 15]);
show([77, 79, 67, 72, 73, 0, 14, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 255, 0, 0, 0, 0]);
`
	program := m17CompileMio(t, root, source)
	vmOutput := m17RunVM(t, program, 10_000_000)
	if want := []byte("1\n2\n3\n8\n"); !bytes.Equal(vmOutput, want) {
		t.Fatalf("malformed VM classifications=%q, want=%q", vmOutput, want)
	}
	nativeOutput, err := runM12Native(t, program)
	if err != nil {
		t.Fatalf("malformed native execution: %v output=%q", err, nativeOutput)
	}
	if !bytes.Equal(nativeOutput, vmOutput) {
		t.Fatalf("malformed native classifications=%q, VM=%q", nativeOutput, vmOutput)
	}
}

func TestM17PureMioDecoderGoldenVMAndNative(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("M17 native differential requires macOS ARM64")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	candidates := []m17Golden{
		{name: "A", path: "build/compiler-A.mochi", size: 152805, hash: "b5904e8fd51e96be16183826177504ed395be67143e0c78d7a4b5d304cfd00a1"},
		{name: "B", path: "build/compiler-B-native.mochi", size: 120707, hash: "b44a55bf3f9020e6b0ff83ad137b25774814a32dfe8acaf070abcd167bfd1465"},
		{name: "C", path: "build/native-bootstrap/compiler-C.mochi", size: 120707, hash: "b44a55bf3f9020e6b0ff83ad137b25774814a32dfe8acaf070abcd167bfd1465"},
		{name: "D", path: "build/native-bootstrap/compiler-D.mochi", size: 120707, hash: "b44a55bf3f9020e6b0ff83ad137b25774814a32dfe8acaf070abcd167bfd1465"},
	}
	var present []m17Golden
	for _, item := range candidates {
		if _, statErr := os.Stat(filepath.Join(root, item.path)); statErr == nil {
			present = append(present, item)
		} else if !os.IsNotExist(statErr) {
			t.Fatal(statErr)
		}
	}
	if len(present) == 0 {
		t.Skip("verified bootstrap artifacts are not present")
	}

	var source strings.Builder
	source.WriteString("yobu \"kiso/bytes\"; yobu \"kiso/encoding/mochi\"; yobu \"kiso/fs\";\n")
	source.WriteString(`waza inspect(path: string) -> unit {
    kaku loaded = fs.read_all(path); kaku read_error = loaded.error;
    moshi (read_error != mu) { chant(-1); kaesu; }
    kaku decoded = mochi.decode_file(path); kaku decode_error = decoded.error;
    moshi (decode_error != mu) { chant(mochi.error_code(decode_error)); kaesu; }
    kaku file = decoded.value;
    moshi (file != mu) {
        kaku summary = mochi.summarize(file);
        chant(summary.version); chant(summary.constants); chant(summary.functions); chant(summary.globals);
        chant(summary.instruction_bytes); chant(summary.canonical_size);
        chant(bytes.equal(loaded.data, mochi.canonical_bytes(file)));
    } sore { chant(-2); }
}
`)
	var expected bytes.Buffer
	for _, item := range present {
		absolute := filepath.Join(root, item.path)
		artifact, readErr := os.ReadFile(absolute)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(artifact) != item.size || fmt.Sprintf("%x", sha256.Sum256(artifact)) != item.hash {
			t.Fatalf("Compiler %s does not match verified golden identity", item.name)
		}
		decoded, decodeErr := mochi.Decode(bytes.NewReader(artifact))
		if decodeErr != nil {
			t.Fatalf("Go decode Compiler %s: %v", item.name, decodeErr)
		}
		var normalized bytes.Buffer
		if encodeErr := mochi.Encode(&normalized, decoded); encodeErr != nil {
			t.Fatalf("Go normalize Compiler %s: %v", item.name, encodeErr)
		}
		if !bytes.Equal(normalized.Bytes(), artifact) {
			t.Fatalf("Go normalized structure differs for Compiler %s", item.name)
		}
		fmt.Fprintf(&expected, "14\n%d\n%d\n%d\n%d\n%d\nhai\n",
			len(decoded.Constants), len(decoded.Functions), decoded.GlobalsCount,
			m17InstructionBytes(decoded), len(artifact))
		fmt.Fprintf(&source, "inspect(%q);\n", absolute)
	}

	program := m17CompileMio(t, root, source.String())
	started := time.Now()
	vmOutput := m17RunVM(t, program, 500_000_000)
	t.Logf("pure Mio VM decoded %d golden files in %s", len(present), time.Since(started))
	if !bytes.Equal(vmOutput, expected.Bytes()) {
		t.Fatalf("pure Mio VM summary=%q, Go normalized summary=%q", vmOutput, expected.Bytes())
	}

	started = time.Now()
	nativeOutput, err := runM12NativeCompile(t, program, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 256 << 20})
	})
	if err != nil {
		t.Fatalf("pure Mio decoder native execution: %v output=%q", err, nativeOutput)
	}
	t.Logf("pure Mio native decoded %d golden files in %s", len(present), time.Since(started))
	if !bytes.Equal(nativeOutput, vmOutput) {
		t.Fatalf("native summary=%q, VM summary=%q", nativeOutput, vmOutput)
	}
}
