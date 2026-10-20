package aot

import (
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/macho"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestM17eCompilerAProgressAfterClosures(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	stage := os.Getenv("MIO_M17E_BOOTSTRAP_STAGE")
	if stage == "" {
		stage = "A"
	}
	nextStage := map[string]string{"A": "B", "B": "C", "C": "D"}[stage]
	if nextStage == "" {
		t.Fatalf("unsupported bootstrap stage %q", stage)
	}
	compilerA := filepath.Join(root, "build", "compiler-A.mochi")
	if stage != "A" {
		compilerA = os.Getenv("MIO_M17E_BOOTSTRAP_INPUT")
		if compilerA == "" {
			t.Fatal("bootstrap B/C requires an explicit Mochi input")
		}
		data, err := os.ReadFile(compilerA)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != "b44a55bf3f9020e6b0ff83ad137b25774814a32dfe8acaf070abcd167bfd1465" {
			t.Fatalf("stage %s input hash=%s", stage, got)
		}
	}
	if _, err := os.Stat(compilerA); os.IsNotExist(err) {
		t.Skip("compiler-A.mochi is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if evidence := m17eEvidenceDir(t); evidence != "" {
		dir = evidence
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	imagePath := filepath.Join(dir, "compiler-"+stage+".native")
	source := fmt.Sprintf(`yobu "kiso/aot/lower";
yobu "kiso/aot/macho";
yobu "kiso/fs";
yobu "kiso/encoding/mochi";
waza show(problem: lower.LowerError) -> unit { chant(lower.error_code(problem)); chant(lower.error_detail(problem)); }
kaku decoded = mochi.decode_file(%q);
kaku decoded_file = decoded.value;
moshi (decoded.error != mu) { chant(-1); }
sore {
  moshi (decoded_file == mu) { chant(-1); }
  sore {
    // Match the repository native bootstrap protocol: bounded 1 GiB target arena.
    kaku lowered = lower.compile_with_heap(decoded_file, 1073741824, iie);
    kaku problem = lowered.error;
    moshi (problem != mu) { show(problem); }
    sore {
      kaku image = macho.executable(macho.ImageSpec { words: lowered.words, constants: lowered.constants, globals: lowered.globals, patches: lowered.patches });
      moshi (image.error != mu) { chant(-3); }
      sore { kaku written = fs.write_all(%q, image.bytes); moshi (written.error != mu) { chant(-4); } sore { chant(len(lowered.words)); chant(len(image.bytes)); } }
    }
  }
}
`, compilerA, imagePath)
	program := m17CompileMio(t, root, source)
	output := m17RunVM(t, program, 1_000_000_000)
	vmImage, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatalf("Compiler A builder: %q: %v", output, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "builder.mio"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		start := time.Now()
		native, err := runM12NativeCompile(t, program, func(p *compiler.Bytecode) ([]byte, error) {
			return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 512 << 20})
		})
		if err != nil || !bytes.Equal(native, output) {
			t.Fatalf("Compiler A probe VM=%q native=%q err=%v", output, native, err)
		}
		t.Logf("Compiler A native Mio lowering probe agrees; seed build/sign/run %.3fs", time.Since(start).Seconds())
	}
	if err := os.WriteFile(filepath.Join(dir, "compiler-"+stage+".unsigned"), vmImage, 0600); err != nil {
		t.Fatal(err)
	}
	nativeImage, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nativeImage, vmImage) {
		t.Fatal("Compiler A VM/native builder images differ")
	}
	f, err := macho.Open(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	text := f.Section("__text")
	if text == nil || text.Size <= 2<<20 || f.Cpu != macho.CpuArm64 {
		t.Fatalf("invalid full Compiler A Mach-O: %v", f)
	}
	if !bytes.Equal(output, []byte(fmt.Sprintf("%d\n%d\n", text.Size/4, len(vmImage)))) {
		t.Fatalf("Compiler A build: %q", output)
	}
	t.Logf("complete pure Mio Compiler %s: %s words=%d bytes=%d unsigned sha256=%x", stage, imagePath, text.Size/4, len(vmImage), sha256.Sum256(vmImage))
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if err := os.Chmod(imagePath, 0700); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"--force", "--sign", "-", imagePath}, {"--verify", "--strict", imagePath}} {
			if out, err := exec.Command("/usr/bin/codesign", args...).CombinedOutput(); err != nil {
				t.Fatalf("codesign %v: %v %s", args, err, out)
			}
		}
		// Opt in because bootstrap.config belongs to the working tree and a full
		// bootstrap must run alone. Always restore its exact original bytes.
		if os.Getenv("MIO_M17E_BOOTSTRAP_RUN") == "1" {
			config := filepath.Join(root, "selfhost/bootstrap/bootstrap.config")
			original, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(config, original, 0644); err != nil {
					t.Error(err)
				}
			})
			outputB := filepath.Join(dir, "compiler-"+nextStage+".mochi")
			if _, err := os.Stat(outputB); !os.IsNotExist(err) {
				t.Fatalf("refusing to overwrite %s", outputB)
			}
			if err := os.WriteFile(config, []byte(outputB), 0644); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, imagePath)
			cmd.Dir = root
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			start := time.Now()
			runErr := cmd.Run()
			m17eWriteJSON(t, filepath.Join(dir, "execution.json"), map[string]any{"stdout": stdout.String(), "stderr": stderr.String(), "exit_code": cmd.ProcessState.ExitCode(), "wall_seconds": time.Since(start).Seconds()})
			if runErr != nil {
				t.Fatalf("real pure Mio Compiler %s -> %s: %v stdout=%q stderr=%q", stage, nextStage, runErr, stdout.Bytes(), stderr.Bytes())
			}
			b, err := os.ReadFile(outputB)
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(b)); got != "b44a55bf3f9020e6b0ff83ad137b25774814a32dfe8acaf070abcd167bfd1465" {
				t.Fatalf("Compiler B hash=%s", got)
			}
			t.Logf("real %s -> %s matches bootstrap golden (%d bytes)", stage, nextStage, len(b))
		}
	}
}

func TestM17eCompilerAArrayAudit(t *testing.T) {
	data, err := os.ReadFile("../../build/compiler-A.mochi")
	if os.IsNotExist(err) {
		t.Skip("Compiler A absent")
	}
	if err != nil {
		t.Fatal(err)
	}
	p, err := mochi.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[code.Opcode]int)
	descriptors := make(map[int]int)
	blocks := []code.Instructions{p.Instructions}
	for _, f := range p.Functions {
		blocks = append(blocks, f.Instructions)
	}
	for _, block := range blocks {
		instructions, _, err := decodeM4Block(block)
		if err != nil {
			t.Fatal(err)
		}
		for _, ins := range instructions {
			counts[ins.op]++
			if ins.op == code.OpArrayTyped {
				descriptors[ins.operands[1]]++
			}
			if ins.op == code.OpArrayMake || ins.op == code.OpArrayWithCapacity {
				descriptors[ins.operands[0]]++
			}
		}
	}
	t.Logf("Compiler A sha256=%x functions=%d", sha256.Sum256(data), len(p.Functions))
	for op := code.Opcode(1); op <= code.OpDataConvert; op++ {
		def, err := code.Lookup(op)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d %s %d", op, def.Name, counts[op])
	}
	for descriptor := 0; descriptor < 256; descriptor++ {
		if count := descriptors[descriptor]; count > 0 {
			t.Logf("array descriptor=%d count=%d", descriptor, count)
		}
	}
	block, _, err := decodeM4Block(p.Functions[19].Instructions)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ins := range block {
		if ins.offset == 67 {
			found = ins.op == code.OpCallValue && ins.operands[0] == 3
		}
	}
	if !found || p.Functions[19].Name != "kiso/io::write_all" {
		t.Fatal("Compiler A call-value blocker moved")
	}
	for id, f := range p.Functions {
		if len(f.Captures) > 0 || strings.Contains(f.Name, "writer") {
			t.Logf("closure context function %d %s captures=%v", id, f.Name, f.Captures)
		}
	}
	f := p.Functions[19]
	t.Logf("current blocker: function 19 %s\n%s", f.Name, f.Instructions.String())
}

// The representation is established from the actual producer and type graph,
// rather than inferred from the spelling of OpCallValue.
func TestM17eCompilerAWriterClosureOrigin(t *testing.T) {
	data, err := os.ReadFile("../../build/compiler-A.mochi")
	if os.IsNotExist(err) {
		t.Skip("Compiler A absent")
	}
	if err != nil {
		t.Fatal(err)
	}
	p, err := mochi.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	a, err := analyzeM6(p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Functions[28].Name != "kiso/fs::writer" || len(p.Functions[376].Captures) != 1 || p.Functions[376].Captures[0] != (compiler.Capture{Local: true, Index: 0}) {
		t.Fatal("Writer producer/capture metadata changed")
	}
	producerFound := false
	for _, ins := range a.blocks[29].inst {
		if ins.op == code.OpClosure && ins.operands[0] == 376 {
			producerFound = true
		}
	}
	if !producerFound {
		t.Fatal("fs.writer does not create closure 376")
	}
	capture := a.captures[376][0]
	if a.u.get(capture) != m6Struct || a.u.structFieldCount(capture) != 1 || !m13ReferenceNode(a.u, capture) {
		t.Fatal("captured File is not a rooted one-field Struct")
	}
	handle, err := a.u.structField(capture, 0)
	if err != nil || a.u.get(handle) != m6Resource || m13ReferenceNode(a.u, handle) {
		t.Fatalf("File.handle kind=%v err=%v", a.u.get(handle), err)
	}
	b := a.blocks[20]
	found := false
	for i, ins := range b.inst {
		if ins.offset != 67 {
			continue
		}
		found = true
		st := b.entry[i]
		if ins.op != code.OpCallValue || ins.operands[0] != 3 || len(st) != 4 {
			t.Fatalf("call stack=%v instruction=%v", st, ins)
		}
		targets := a.u.functionTargets(st[0])
		if len(targets) != 1 || targets[0] != 376 {
			t.Fatalf("targets=%v", targets)
		}
		if a.u.get(st[0]) != m6Function || a.u.get(st[1]) != m6Array || a.u.get(st[2]) != m6Int || a.u.get(st[3]) != m6Int {
			t.Fatal("Writer argument representations changed")
		}
		if a.u.arrayElementKind(st[1]) != m6Uint8 {
			t.Fatal("Writer source is not a compact uint8 array")
		}
		exit := b.exit[i]
		if len(exit) != 1 || a.u.get(exit[0]) != m6Struct || a.u.structFieldCount(exit[0]) != 2 {
			t.Fatal("WriteResult return shape changed")
		}
	}
	if !found {
		t.Fatal("write_all@67 absent")
	}
}
