package aot

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
	"math"
)

func TestM17ePureMioReadAllCrossPage(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	root, _ := filepath.Abs("../..")
	dir := t.TempDir()
	var source, want strings.Builder
	source.WriteString(`yobu "kiso/fs"; yobu "kiso/array";
waza verify(data: []uint8, size: int64) -> int64 {
 moshi (len(data) != size) { kaesu -1; }
 kaku i = 0; aida (i < size) {
  moshi (data[i] != uint8((i * 37) - ((i * 37) / 256) * 256)) { kaesu -2; } i = i + 1;
 } kaesu size;
}
waza error_code(problem: fs.FsError?) -> int64 {
 moshi (problem == mu) { kaesu 0; }
 awase (problem) {
  baai NotFound(message) { kaesu 1; }
  baai PermissionDenied(message) { kaesu 2; }
  baai Closed(message) { kaesu 3; }
  baai InvalidArgument(message) { kaesu 4; }
  baai NoSpace(message) { kaesu 5; }
  baai InvalidPath(message) { kaesu 6; }
  baai IOFailure(message) { kaesu 7; }
 } kaesu -1;
}
waza read_check(path: string, size: int64) -> unit {
 kaku r = fs.read_all(path);
 moshi (r.error != mu) { chant(-3); } sore {
  // Force many allocations while retaining the original descriptor/Storage.
  kaku i = 0; aida (i < 12) { kaku garbage = array.make[uint8](65536, uint8(91)); i = i + 1; }
  chant(verify(r.data, size));
 }
}
`)
	// Includes physical header/alignment boundary around a 16 KiB allocation:
	// align16(capacity + 32 + 16) = 16384 for capacity 16321..16336.
	for _, n := range []int{0, 1, 127, 16320, 16321, 16336, 16337, 16383, 16384, 16385, 32768, 65536, 262144, 2 << 20} {
		path := filepath.Join(dir, fmt.Sprint(n))
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i * 37)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&source, "read_check(%q, %d);\n", path, n)
		fmt.Fprintln(&want, n)
	}
	// Repeat reads and exercise both open failure and a real read(2) failure on a directory.
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&source, "read_check(%q, 65536);\n", filepath.Join(dir, "65536"))
		fmt.Fprintln(&want, 65536)
	}
	for index, path := range []string{filepath.Join(dir, "missing"), dir} {
		fmt.Fprintf(&source, "kaku e%d = fs.read_all(%q); chant(error_code(e%d.error)); chant(len(e%d.data));\n", index, path, index, index)
		fmt.Fprintf(&want, "%d\n0\n", []int{1, 7}[index])
	}
	p := m17CompileMio(t, root, source.String())
	expected := []byte(want.String())
	if out := m17RunVM(t, p, 500_000_000); !bytes.Equal(out, expected) {
		t.Fatalf("VM=%q want=%q", out, expected)
	}
	out, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 128 << 20, GCStress: true})
	})
	if err != nil || !bytes.Equal(out, expected) {
		t.Fatalf("Go native=%q err=%v want=%q", out, err, expected)
	}
	for _, stress := range []bool{false, true} {
		path := m17eArrayImageWithHeap(t, p, stress, 1_500_000_000, 512<<20, 8<<20)
		out, err := exec.Command(path).CombinedOutput()
		if err != nil || !bytes.Equal(out, expected) {
			t.Fatalf("pure stress=%v: %v %q want=%q", stress, err, out, expected)
		}
	}
}

func TestM17ePureMioHeapAllocationBoundary(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	root, _ := filepath.Abs("../..")
	// One Storage + one descriptor, with exact 16-byte header/alignment accounting.
	for _, n := range []int{16320, 16321, 16336, 16337, 32768} {
		for _, fits := range []bool{true, false} {
			t.Run(fmt.Sprintf("%d-%v", n, fits), func(t *testing.T) {
				capacity := int64((n+48+15)&^15) + 64
				if !fits {
					capacity -= 16
				}
				p := m17CompileMio(t, root, fmt.Sprintf(`yobu "kiso/array"; kaku a=array.make[uint8](%d,uint8(173)); chant(len(a)); chant(a[%d]);`, n, n-1))
				path := m17eArrayImageWithHeap(t, p, true, 500_000_000, 128<<20, capacity)
				cmd := exec.Command(path)
				out, err := cmd.CombinedOutput()
				if fits {
					if err != nil || string(out) != fmt.Sprintf("%d\n173\n", n) {
						t.Fatalf("exact fit: %v %q", err, out)
					}
				} else if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 2 || len(out) != 0 {
					t.Fatalf("real exhaustion: %v %q", err, out)
				}
			})
		}
	}
}

func TestM17ePureMioHeapReuseAndReferenceStorage(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	root, _ := filepath.Abs("../..")
	source := `yobu "kiso/array";
kata Box { value: int64; }
waza churn() -> unit {
 kaku a = array.make[uint8](40000,uint8(219));
 kaku b = array.make[uint8](20000,uint8(17));
 moshi (a[39999] != uint8(219) || b[19999] != uint8(17)) { chant(-1); }
}
kaku refs = array.with_capacity[Box](3000);
kaku i=0; aida (i<3000) { array.push(refs,Box { value:i }); i=i+1; }
kaku j=0; aida (j<100) { churn(); j=j+1; }
i=0; kaku sum=0; aida (i<3000) { sum=sum+refs[i].value; i=i+1; }
chant(sum); chant(len(refs));`
	p := m17CompileMio(t, root, source)
	want := "4498500\n3000\n"
	if out := m17RunVM(t, p, 50_000_000); string(out) != want {
		t.Fatalf("VM=%q", out)
	}
	for _, stress := range []bool{false, true} {
		path := m17eArrayImageWithHeap(t, p, stress, 500_000_000, 128<<20, 320<<10)
		out, err := exec.Command(path).CombinedOutput()
		if err != nil || string(out) != want {
			t.Fatalf("stress=%v: %v %q", stress, err, out)
		}
	}
}

func TestM17ePureMioHeapCoalesceAndSplit(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	root, _ := filepath.Abs("../..")
	p := m17CompileMio(t, root, `yobu "kiso/array";
kata Marker { value: int64; }
waza make_hole() -> Marker {
 kaku i=0; aida (i<500) { kaku garbage=array.make[uint8](96,uint8(211)); i=i+1; }
 kaesu Marker { value:771 };
}
kaku marker=make_hole();
// The live marker pins the end of the arena. Only coalescing the dead interior
// can satisfy this Storage; splitting must preserve the remainder for descriptors.
kaku large=array.make[uint8](90000,uint8(143));
kaku second=array.make[uint8](10000,uint8(29));
chant(marker.value); chant(len(large)); chant(large[89999]); chant(second[9999]);`)
	want := "771\n90000\n143\n29\n"
	if out := m17RunVM(t, p, 5_000_000); string(out) != want {
		t.Fatalf("VM=%q", out)
	}
	path := m17eArrayImageWithHeap(t, p, false, 500_000_000, 128<<20, 104<<10)
	out, err := exec.Command(path).CombinedOutput()
	if err != nil || string(out) != want {
		t.Fatalf("coalescing/splitting: %v %q", err, out)
	}
}

func TestM17ePureMioFloatPredicateWidth(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	root, _ := filepath.Abs("../..")
	p := m17CompileMio(t, root, `yobu "kiso/math";
waza check64(value: float64) -> bool {
 kaku infinite=math.is_inf(value);
 kaesu !infinite && 1.0 / value > 0.0;
}
waza check32(value: float32) -> bool {
 kaku infinite=math.is_inf(value);
 kaesu !infinite && float32(1.0) / value > float32(0.0);
}
waza inf64(value: float64) -> bool { kaesu math.is_inf(1.0 / value); }
waza inf32(value: float32) -> bool { kaesu math.is_inf(float32(1.0) / value); }
chant(check64(0.0)); chant(check64(-0.0)); chant(check64(2.0));
chant(check32(float32(0.0))); chant(check32(float32(-0.0))); chant(check32(float32(2.0)));
chant(inf64(0.0)); chant(inf64(-0.0)); chant(inf32(float32(0.0))); chant(inf32(float32(-0.0)));
waza nan64(zero: float64) -> bool { kaesu math.is_nan(zero / zero); }
waza nan32(zero: float32) -> bool { kaesu math.is_nan(zero / zero); }
chant(nan64(0.0)); chant(nan32(float32(0.0))); chant(math.is_finite(2.0)); chant(math.is_finite(float32(2.0)));
chant(math.is_inf(0.0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001));`)
	// A finite binary64 subnormal whose raw bits equal binary32 +Inf must
	// remain finite: testing both widths at an erased call would misclassify it.
	replaced := false
	for i, constant := range p.Constants {
		if f, ok := constant.Float64(); ok && f > 0 && f < 1e-90 {
			p.Constants[i] = value.NewFloat(math.Float64frombits(0x7f800000))
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("missing finite collision fixture")
	}
	want := "hai\niie\nhai\nhai\niie\nhai\nhai\nhai\nhai\nhai\nhai\nhai\nhai\nhai\niie\n"
	if out := m17RunVM(t, p, 5_000_000); string(out) != want {
		t.Fatalf("VM=%q", out)
	}
	// Mixed-width calls retain their own concrete arithmetic representation.
	path := m17eArrayImage(t, p)
	out, err := exec.Command(path).CombinedOutput()
	if err != nil || string(out) != want {
		t.Fatalf("float predicate: %v %q", err, out)
	}
}

func TestM17ePureMioGenericArrayForwarding(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	p := &compiler.Bytecode{GlobalsCount: 4, Constants: []value.Value{value.NewInt(4), value.NewInt(42), value.NewUint8(193), value.NewInt(0), value.NewInt(1)}}
	emit := func(dst *code.Instructions, op code.Opcode, args ...int) {
		inst, err := code.Make(op, args...)
		if err != nil {
			t.Fatal(err)
		}
		*dst = append(*dst, inst...)
	}
	for id, op := range []code.Opcode{code.OpArrayPush, code.OpArrayClone, code.OpArrayReserve, code.OpArrayCapacity, code.OpArrayTruncate, code.OpArrayAppendAll, code.OpArrayWithCapacity, code.OpArrayMake} {
		n, ret := 2, false
		if op == code.OpArrayClone || op == code.OpArrayCapacity || op == code.OpArrayWithCapacity {
			n = 1
		}
		if op == code.OpArrayClone || op == code.OpArrayCapacity || op == code.OpArrayWithCapacity || op == code.OpArrayMake {
			ret = true
		}
		var body code.Instructions
		for i := 0; i < n; i++ {
			emit(&body, code.OpGetLocal, i)
		}
		if op == code.OpArrayWithCapacity || op == code.OpArrayMake {
			emit(&body, op, 0)
		} else {
			emit(&body, op)
		}
		if ret {
			emit(&body, code.OpReturnValue)
		} else {
			emit(&body, code.OpReturn)
		}
		p.Functions = append(p.Functions, compiler.CompiledFunction{ID: id, Name: []string{"push", "clone", "reserve", "capacity", "truncate", "append_all", "with_capacity", "make"}[id], ParameterCount: n, LocalCount: n, ReturnsValue: ret, Instructions: body})
	}
	e := func(op code.Opcode, args ...int) { emit(&p.Instructions, op, args...) }
	// Same forwarding functions operate on compact bytes and word integers.
	for global, descriptor := range []int{1, 15} {
		e(code.OpConstant, 0)
		e(code.OpArrayWithCapacity, descriptor)
		e(code.OpSetGlobal, global)
		e(code.OpGetGlobal, global)
		e(code.OpConstant, global+1)
		e(code.OpCall, 0)
		e(code.OpGetGlobal, global)
		e(code.OpCall, 1)
		e(code.OpSetGlobal, 2+global)
		e(code.OpGetGlobal, 2+global)
		e(code.OpConstant, 0)
		e(code.OpCall, 2)
		e(code.OpGetGlobal, 2+global)
		e(code.OpConstant, 3)
		e(code.OpIndex)
		e(code.OpChant)
		e(code.OpGetGlobal, global)
		e(code.OpGetGlobal, 2+global)
		e(code.OpCall, 5)
		e(code.OpGetGlobal, global)
		e(code.OpLen)
		e(code.OpChant)
		e(code.OpGetGlobal, global)
		e(code.OpConstant, 4)
		e(code.OpCall, 4)
		e(code.OpGetGlobal, global)
		e(code.OpLen)
		e(code.OpChant)
	}
	// Generic constructors infer the element at each call, never from the body.
	e(code.OpConstant, 0)
	e(code.OpCall, 6)
	e(code.OpSetGlobal, 0)
	e(code.OpGetGlobal, 0)
	e(code.OpConstant, 1)
	e(code.OpCall, 0)
	e(code.OpGetGlobal, 0)
	e(code.OpCall, 3)
	e(code.OpChant)
	e(code.OpConstant, 0)
	e(code.OpConstant, 2)
	e(code.OpCall, 7)
	e(code.OpSetGlobal, 1)
	e(code.OpGetGlobal, 1)
	e(code.OpConstant, 3)
	e(code.OpIndex)
	e(code.OpChant)
	want := m17RunVM(t, p, 1_000_000)
	if string(want) != "42\n2\n1\n193\n2\n1\n4\n193\n" {
		t.Fatalf("VM=%q", want)
	}
	path := m17eArrayImage(t, p)
	out, err := exec.Command(path).CombinedOutput()
	if err != nil || !bytes.Equal(out, want) {
		t.Fatalf("generic forwarding: %v %q VM=%q", err, out, want)
	}
}

func TestM17ePureMioFunctionReachability(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(42)}}
	emit := func(dst *code.Instructions, op code.Opcode, args ...int) {
		inst, err := code.Make(op, args...)
		if err != nil {
			t.Fatal(err)
		}
		*dst = append(*dst, inst...)
	}
	var unused, identity code.Instructions
	emit(&unused, code.OpGetLocal, 0)
	emit(&unused, code.OpGetLocal, 1)
	emit(&unused, code.OpAdd)
	emit(&unused, code.OpReturnValue)
	emit(&identity, code.OpGetLocal, 0)
	emit(&identity, code.OpReturnValue)
	p.Functions = []compiler.CompiledFunction{{ID: 0, Name: "unused_generic", ParameterCount: 2, LocalCount: 2, ReturnsValue: true, Instructions: unused}, {ID: 1, Name: "identity", ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Instructions: identity}}
	emit(&p.Instructions, code.OpConstant, 0)
	emit(&p.Instructions, code.OpCall, 1)
	emit(&p.Instructions, code.OpChant)
	want := m17RunVM(t, p, 100_000)
	path := m17eArrayImage(t, p)
	out, err := exec.Command(path).CombinedOutput()
	if err != nil || !bytes.Equal(out, want) || string(out) != "42\n" {
		t.Fatalf("reachability: %v %q VM=%q", err, out, want)
	}
}
