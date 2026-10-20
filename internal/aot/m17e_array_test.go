package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

// Build the image through Mio in both execution engines. Go AOT only hosts
// the seed builder and supplies an independent semantic reference.
func m17eArrayImage(t *testing.T, p *compiler.Bytecode) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, p); err != nil {
		t.Fatal(err)
	}
	in, out := filepath.Join(dir, "input.mochi"), filepath.Join(dir, "native")
	if err := os.WriteFile(in, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`yobu "kiso/aot/lower";
yobu "kiso/aot/macho";
yobu "kiso/encoding/mochi";
yobu "kiso/fs";
kaku decoded = mochi.decode_file(%q);
kaku file = decoded.value;
moshi (file != mu) {
 kaku lowered = lower.compile_gc_stress(file);
 kaku problem = lowered.error;
 moshi (problem != mu) { chant(lower.error_detail(problem)); }
 sore {
  kaku image = macho.executable(macho.ImageSpec { words: lowered.words, constants: lowered.constants, globals: lowered.globals, patches: lowered.patches });
  moshi (image.error != mu) { chant(-3); }
  sore { kaku result = fs.write_all(%q, image.bytes); moshi (result.error != mu) { chant(-4); } }
 }
} sore { chant(-1); }
`, in, out)
	builder := m17CompileMio(t, root, src)
	vmStart := time.Now()
	if got := m17RunVM(t, builder, 500_000_000); len(got) != 0 {
		t.Fatalf("Mio builder: %s", got)
	}
	vmElapsed := time.Since(vmStart)
	vmImage, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	seedStart := time.Now()
	if got, err := runM12NativeCompile(t, builder, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 128 << 20})
	}); err != nil || len(got) != 0 {
		t.Fatalf("Native Mio builder: %v %s", err, got)
	}
	seedElapsed := time.Since(seedStart)
	nativeImage, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vmImage, nativeImage) {
		t.Fatal("VM/native Mio builder images differ")
	}
	if err := os.Chmod(out, 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--force", "--sign", "-", out}, {"--verify", "--strict", out}} {
		if got, err := exec.Command("/usr/bin/codesign", args...).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %v %s", err, got)
		}
	}
	if evidence := m17eEvidenceDir(t); evidence != "" {
		if err := os.MkdirAll(evidence, 0700); err != nil {
			t.Fatal(err)
		}
		signed, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		persistentSource := strings.ReplaceAll(strings.ReplaceAll(src, in, filepath.Join(evidence, "input.mochi")), out, filepath.Join(evidence, "rebuilt.native"))
		for name, data := range map[string][]byte{"input.mochi": encoded.Bytes(), "builder.mio": []byte(persistentSource), "image.unsigned": vmImage, "image.native": signed} {
			if err := os.WriteFile(filepath.Join(evidence, name), data, 0700); err != nil {
				t.Fatal(err)
			}
		}
		manifest := map[string]any{"unsigned_sha256": fmt.Sprintf("%x", sha256.Sum256(vmImage)), "image_bytes": len(vmImage), "pure_mio_vm_decode_lower_write_seconds": vmElapsed.Seconds(), "go_seed_build_sign_and_native_mio_run_seconds": seedElapsed.Seconds(), "builder_images_identical": true, "codesign_strict": true, "gc_stress": true}
		m17eWriteJSON(t, filepath.Join(evidence, "build.json"), manifest)
	}
	t.Logf("unsigned image: %d bytes; pure Mio VM build %.3fs", len(vmImage), vmElapsed.Seconds())
	return out
}

func m17eEvidenceDir(t *testing.T) string {
	t.Helper()
	root := os.Getenv("MIO_M17E_EVIDENCE_DIR")
	if root == "" {
		return ""
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(absolute, strings.ReplaceAll(t.Name(), "/", "-"))
}

func m17eWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func m17eExecute(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(path)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if evidence := m17eEvidenceDir(t); evidence != "" {
		result := map[string]any{"wall_seconds": elapsed.Seconds(), "stdout_stderr": string(out), "exit_code": cmd.ProcessState.ExitCode(), "sys_usage_darwin_maxrss_bytes": cmd.ProcessState.SysUsage(), "gc_count": "not exported", "gc_seconds": "not exported", "allocated_bytes": "not exported"}
		m17eWriteJSON(t, filepath.Join(evidence, "execution.json"), result)
	}
	return out, err
}

func TestM17ePureMioArrayAppendAndGC(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	for _, kind := range []value.Kind{value.KindUint8, value.KindInt, value.KindInt8, value.KindInt16, value.KindInt32, value.KindUint16, value.KindUint32, value.KindStruct} {
		t.Run(kind.String(), func(t *testing.T) {
			elem := value.NewInt(77)
			switch kind {
			case value.KindUint8:
				elem = value.NewUint8(255)
			case value.KindInt8:
				elem = value.NewInt8(-77)
			case value.KindInt16:
				elem = value.NewInt16(-1777)
			case value.KindInt32:
				elem = value.NewInt32(-177777)
			case value.KindUint16:
				elem = value.NewUint16(65535)
			case value.KindUint32:
				elem = value.NewUint32(4294967295)
			}
			p := &compiler.Bytecode{GlobalsCount: 4, Constants: []value.Value{value.NewInt(0), elem, value.NewInt(1)}}
			emit := func(op code.Opcode, args ...int) {
				inst, err := code.Make(op, args...)
				if err != nil {
					t.Fatal(err)
				}
				p.Instructions = append(p.Instructions, inst...)
			}
			c := func(i int) { emit(code.OpConstant, i) }
			g := func(i int) { emit(code.OpGetGlobal, i) }
			// Both empty, then cross 0 -> 4 -> 8; self append via descriptor alias.
			for i := 0; i < 2; i++ {
				c(0)
				emit(code.OpArrayWithCapacity, int(kind))
				emit(code.OpSetGlobal, i)
			}
			g(0)
			g(1)
			emit(code.OpArrayAppendAll)
			for i := 0; i < 5; i++ {
				g(1)
				c(1)
				if kind == value.KindStruct {
					emit(code.OpStruct, 1)
				}
				emit(code.OpArrayPush)
			}
			g(0)
			g(1)
			emit(code.OpArrayAppendAll)
			g(0)
			emit(code.OpSetGlobal, 2)
			g(0)
			g(2)
			emit(code.OpArrayAppendAll)
			g(0)
			emit(code.OpLen)
			emit(code.OpChant)
			g(0)
			emit(code.OpArrayCapacity)
			emit(code.OpChant)
			// Remove the source reference. Subsequent collections must retain the
			// shallow-copied elements solely through the destination storage bitmap.
			g(1)
			c(0)
			emit(code.OpArrayTruncate)
			for i := 0; i < 500; i++ {
				c(1)
				emit(code.OpStruct, 1)
				emit(code.OpSetGlobal, 3)
			}
			for i := 0; i < 10; i++ {
				g(0)
				p.Constants = append(p.Constants, value.NewInt(int64(i)))
				c(len(p.Constants) - 1)
				emit(code.OpIndex)
				if kind == value.KindStruct {
					emit(code.OpGetField, 0)
				}
				emit(code.OpChant)
			}
			want := m17RunVM(t, p, 100_000)
			ref, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
				return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 1 << 20, GCStress: true})
			})
			if err != nil || !bytes.Equal(ref, want) {
				t.Fatalf("Go reference: %v %q VM=%q", err, ref, want)
			}
			out := m17eArrayImage(t, p)
			got, err := m17eExecute(t, out)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("pure native: %v %q VM=%q", err, got, want)
			}
		})
	}
}

func TestM17ePureMioArrayConstructCloneNullableAndBulk(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	p := &compiler.Bytecode{GlobalsCount: 5, Constants: []value.Value{value.NewUint8(255), value.NewUint8(0), value.NewInt(0), value.NewInt(1), value.NewInt(2048)}}
	emit := func(op code.Opcode, args ...int) {
		inst, err := code.Make(op, args...)
		if err != nil {
			t.Fatal(err)
		}
		p.Instructions = append(p.Instructions, inst...)
	}
	c := func(i int) { emit(code.OpConstant, i) }
	g := func(i int) { emit(code.OpGetGlobal, i) }
	// Compact literal and independent clone: changing clone leaves source intact.
	c(0)
	c(1)
	emit(code.OpArrayTyped, 2, int(value.KindUint8))
	emit(code.OpSetGlobal, 0)
	g(0)
	emit(code.OpArrayClone)
	emit(code.OpSetGlobal, 1)
	g(1)
	c(2)
	c(1)
	emit(code.OpSetIndex)
	for _, i := range []int{0, 1} {
		g(i)
		c(2)
		emit(code.OpIndex)
		emit(code.OpChant)
	}
	// Nullable scalar slots use the established type-106 boxes, including Some(0).
	c(3)
	emit(code.OpNil)
	emit(code.OpArrayMake, 128+int(value.KindInt))
	emit(code.OpSetGlobal, 2)
	g(2)
	c(2)
	emit(code.OpIndex)
	emit(code.OpIsNil)
	emit(code.OpChant)
	g(2)
	c(2)
	c(2)
	emit(code.OpSetNullableIndex)
	g(2)
	c(2)
	emit(code.OpIndex)
	emit(code.OpIsNil)
	emit(code.OpChant)
	// 4096 actual data bytes, checked in full by a runtime checksum after GC.
	c(4)
	c(0)
	emit(code.OpArrayMake, int(value.KindUint8))
	emit(code.OpSetGlobal, 3)
	c(2)
	emit(code.OpArrayWithCapacity, int(value.KindUint8))
	emit(code.OpSetGlobal, 4)
	for range 2 {
		g(4)
		g(3)
		emit(code.OpArrayAppendAll)
	}
	// Use a function for verification to keep code size and operand depth bounded.
	body := code.Instructions{}
	f := func(op code.Opcode, args ...int) {
		inst, err := code.Make(op, args...)
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, inst...)
	}
	f(code.OpConstant, 2)
	f(code.OpSetLocal, 1)
	f(code.OpConstant, 2)
	f(code.OpSetLocal, 2)
	loop := len(body)
	f(code.OpGetLocal, 1)
	f(code.OpGetLocal, 0)
	f(code.OpLen)
	f(code.OpLess)
	jump := len(body)
	f(code.OpJumpIfFalse, 0)
	f(code.OpGetLocal, 2)
	f(code.OpGetLocal, 0)
	f(code.OpGetLocal, 1)
	f(code.OpIndex)
	f(code.OpIntegerConvert, 4)
	f(code.OpAdd)
	f(code.OpSetLocal, 2)
	f(code.OpGetLocal, 1)
	f(code.OpConstant, 3)
	f(code.OpAdd)
	f(code.OpSetLocal, 1)
	f(code.OpJump, loop)
	end := len(body)
	patch, _ := code.Make(code.OpJumpIfFalse, end)
	copy(body[jump:], patch)
	f(code.OpGetLocal, 2)
	f(code.OpReturnValue)
	p.Functions = []compiler.CompiledFunction{{ID: 0, Name: "checksum", ParameterCount: 1, LocalCount: 3, ReturnsValue: true, Instructions: body}}
	g(4)
	emit(code.OpCall, 0)
	emit(code.OpChant)
	want := m17RunVM(t, p, 200_000)
	ref, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 1 << 20, GCStress: true})
	})
	if err != nil || !bytes.Equal(ref, want) {
		t.Fatalf("Go reference: %v %q VM=%q", err, ref, want)
	}
	out := m17eArrayImage(t, p)
	got, err := m17eExecute(t, out)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("pure native: %v %q VM=%q", err, got, want)
	}
}

func TestM17ePureMioArrayOverflow(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	for _, tc := range []struct {
		name                 string
		capacity, additional int64
		reserve              bool
		exit                 int
	}{{"negative_capacity", -1, 0, false, 1}, {"capacity_multiply_overflow", 1 << 62, 0, false, 2}, {"negative_reserve", 1, -1, true, 1}, {"length_add_overflow", 1, 1<<63 - 1, true, 1}, {"bounded_heap_oom", 1 << 20, 0, false, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			p := &compiler.Bytecode{GlobalsCount: 1, Constants: []value.Value{value.NewInt(tc.capacity), value.NewInt(tc.additional), value.NewInt(77)}}
			e := func(op code.Opcode, args ...int) {
				inst, err := code.Make(op, args...)
				if err != nil {
					t.Fatal(err)
				}
				p.Instructions = append(p.Instructions, inst...)
			}
			e(code.OpConstant, 0)
			e(code.OpArrayWithCapacity, int(value.KindInt))
			e(code.OpSetGlobal, 0)
			if tc.reserve {
				e(code.OpGetGlobal, 0)
				e(code.OpConstant, 2)
				e(code.OpArrayPush)
				e(code.OpGetGlobal, 0)
				e(code.OpConstant, 1)
				e(code.OpArrayReserve)
			}
			out := m17eArrayImage(t, p)
			got, err := m17eExecute(t, out)
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != tc.exit || len(got) != 0 {
				t.Fatalf("native error: %v stdout=%q want exit %d", err, got, tc.exit)
			}
		})
	}
}

func TestM17ePureMioEnumAllocationAndTagGC(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	p := &compiler.Bytecode{GlobalsCount: 2, Constants: []value.Value{value.NewInt(77), value.NewString("引用")}}
	e := func(op code.Opcode, args ...int) {
		inst, err := code.Make(op, args...)
		if err != nil {
			t.Fatal(err)
		}
		p.Instructions = append(p.Instructions, inst...)
	}
	e(code.OpConstant, 0)
	e(code.OpStruct, 1)
	e(code.OpConstant, 1)
	e(code.OpEnum, 42, 2)
	e(code.OpSetGlobal, 0)
	for range 400 {
		e(code.OpConstant, 0)
		e(code.OpStruct, 1)
		e(code.OpSetGlobal, 1)
	}
	e(code.OpGetGlobal, 0)
	e(code.OpEnumTag)
	e(code.OpChant)
	want := m17RunVM(t, p, 100_000)
	ref, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 1 << 20, GCStress: true})
	})
	if err != nil || !bytes.Equal(ref, want) {
		t.Fatalf("Go reference: %v %q VM=%q", err, ref, want)
	}
	out := m17eArrayImage(t, p)
	got, err := m17eExecute(t, out)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("pure native: %v %q VM=%q", err, got, want)
	}
}
