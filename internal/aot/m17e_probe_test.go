package aot

import (
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestM17eCompilerAProgressAfterGetField(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	compilerA := filepath.Join(root, "build", "compiler-A.mochi")
	if _, err := os.Stat(compilerA); os.IsNotExist(err) {
		t.Skip("compiler-A.mochi is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`yobu "kiso/aot/lower";
yobu "kiso/encoding/mochi";
waza show(problem: lower.LowerError) -> unit { chant(lower.error_code(problem)); chant(lower.error_detail(problem)); }
kaku decoded = mochi.decode_file(%q);
kaku decoded_file = decoded.value;
moshi (decoded.error != mu) { chant(-1); }
sore {
  moshi (decoded_file == mu) { chant(-1); }
  sore {
    kaku lowered = lower.compile(decoded_file);
    kaku problem = lowered.error;
    moshi (problem != mu) { show(problem); }
    sore { chant(len(lowered.words)); }
  }
}
`, compilerA)
	program := m17CompileMio(t, root, source)
	output := m17RunVM(t, program, 250_000_000)
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		start := time.Now()
		native, err := runM12NativeCompile(t, program, func(p *compiler.Bytecode) ([]byte, error) {
			return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 128 << 20})
		})
		if err != nil || !bytes.Equal(native, output) {
			t.Fatalf("Compiler A probe VM=%q native=%q err=%v", output, native, err)
		}
		t.Logf("Compiler A native Mio lowering probe agrees; seed build/sign/run %.3fs", time.Since(start).Seconds())
	}
	if !bytes.Equal(output, []byte("2\n20006745\n")) {
		t.Fatalf("Compiler A first unsupported detail=%q, want function 19 kiso/io::write_all byte offset 67 OpCallValue (20006745)", output)
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
