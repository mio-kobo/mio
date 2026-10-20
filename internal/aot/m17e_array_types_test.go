package aot

import (
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
	"bytes"
	"debug/macho"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"Mio/internal/mochi"
)

func TestM17eCompilerAArrayTypeProvenance(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "build/compiler-A.mochi")
	data, err := os.ReadFile(input)
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
	for _, id := range []int{80, 82, 125} {
		t.Logf("origin function %d %s\n%s", id, p.Functions[id].Name, p.Functions[id].Instructions.String())
	}
	f := p.Functions[117]
	t.Logf("function 117: %+v\n%s", f, f.Instructions.String())
	a, err := analyzeM6(p)
	if err != nil {
		t.Fatal(err)
	}
	n := a.returns[117]
	el, err := a.u.arrayElement(n)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Go AOT return Value=%d root=%d element Value=%d root=%d kind=%v nullable=%v width=%d reference=%v", n, a.u.root(n), el, a.u.root(el), a.u.get(el), a.u.isNullable(el), m13ElementBytes(a.u.get(el), a.u.isNullable(el)), m13StoredReference(a.u.get(el), a.u.isNullable(el)))
	source := fmt.Sprintf(`yobu "kiso/aot/lower/types";
yobu "kiso/encoding/mochi";
kaku decoded = mochi.decode_file(%q); kaku file = decoded.value;
moshi (file != mu) {
 kaku a = types.analyze_types_traced(file, hai);
 chant(a.error_detail);
 kaku result = a.returns[117]; kaku root = a.arena.parent[result]; aida (a.arena.parent[root] != root) { root = a.arena.parent[root]; }
 kaku element = a.arena.fields[root][0]; kaku eroot = a.arena.parent[element]; aida (a.arena.parent[eroot] != eroot) { eroot = a.arena.parent[eroot]; }
 chant(result); chant(root); chant(element); chant(eroot); chant(a.arena.nullable[eroot]);
 kaku i = 0; aida (i < len(a.arena.nullable_origins)) {
  kaku event = a.arena.nullable_origins[i]; kaku r = event[1]; aida (a.arena.parent[r] != r) { r = a.arena.parent[r]; }
  moshi (r == eroot) { chant(event[0]); chant(event[1]); chant(event[2]); chant(event[3]); }
  i = i + 1;
 }
 chant("parse_prefix Slot3/Slot4 and non-nil GetLocal result at offset63");
 chant(a.locals[125][3]); chant(types.type_nullable(a, a.locals[125][3]));
 chant(a.locals[125][4]); chant(types.type_nullable(a, a.locals[125][4]));
 chant(a.blocks[126].entry[63][0]); chant(types.type_nullable(a, a.blocks[126].entry[63][0]));
}
`, input)
	out := m17RunVM(t, m17CompileMio(t, root, source), 1_000_000_000)
	t.Logf("Mio return/element roots and nullable provenance:\n%s", out)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 13 || lines[0] != "0" || lines[5] != "iie" || lines[8] != "hai" || lines[10] != "iie" || lines[12] != "iie" {
		t.Fatalf("children element and narrowed Slot4 must remain raw int64 while Slot3 stays nullable: %q", out)
	}
}

func TestM17ePureMioArrayScalarAndNullableFlow(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	source := `yobu "kiso/array";
waza maybe(flag: bool) -> int64? { moshi (flag) { kaesu 42; } kaesu mu; }
waza children() -> []int64 { kaesu array.with_capacity[int64](0); }
waza boxed() -> []int64? { kaesu array.with_capacity[int64?](0); }
waza run() -> unit {
 kaku a = children(); kaku b = boxed();
 chant(len(a)); chant(array.capacity(a)); chant(len(b)); chant(array.capacity(b));
 kaku empty = array.make[int64](0, 0); kaku empty_box = array.make[int64?](0, mu); chant(len(empty)); chant(len(empty_box));
 kaku n = maybe(hai); kaku raw = 0;
 moshi (n != mu) { raw = n; }
 array.push(a, raw); array.push(a, 0);
 array.push(b, n); array.push(b, mu); array.push(b, 0);
 a[0] = 43; b[1] = 44;
 array.reserve(a, 12); array.reserve(b, 12);
 kaku ac = array.clone(a); kaku bc = array.clone(b);
 array.append_all(a, ac); array.append_all(b, bc);
 kaku read = waza() -> []int64 { kaesu a; };
 kaku read_box = waza() -> []int64? { kaesu b; };
 kaku i = 0; aida (i < 300) { kaku garbage = array.make[int64?](4, i); i = i + 1; }
 kaku returned = read(); kaku returned_box = read_box();
 chant(returned[0]); chant(returned[2]); chant(returned_box[0] == mu); chant(returned_box[2] == mu);
 kaku value = returned_box[0]; moshi (value != mu) { chant(value + 1); }
 b[0] = mu; chant(b[0] == mu); chant(bc[0] == mu); chant(b[2] == mu);
}
run();`
	requireM17eClosure(t, m17CompileMio(t, root, source))
}

func TestM17ePureMioArrayNullableReferenceGC(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	p := m17CompileMio(t, root, `yobu "kiso/array"; yobu "kiso/string";
kata Box { text: string; }
waza run() -> unit {
 kaku a = array.with_capacity[Box?](0); array.push(a, mu);
 array.push(a, Box { text: string.concat("保留", "对象") });
 kaku b = array.clone(a); array.append_all(a, b); array.reserve(a, 20);
 kaku i = 0; aida (i < 400) { kaku garbage = Box { text: "garbage" }; i = i + 1; }
 chant(a[0] == mu); kaku v = a[1]; moshi (v != mu) { chant(v.text); }
 kaku w = a[3]; moshi (w != mu) { chant(w.text); }
 a[1] = mu; chant(a[1] == mu); kaku original = b[1]; moshi (original != mu) { chant(original.text); }
}
run();`)
	requireM17eClosure(t, p)
}

func TestM17ePureMioArrayRejectsAmbiguousAndMixedLayouts(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	for _, descriptor := range []int{0, 129} {
		t.Run(fmt.Sprint(descriptor), func(t *testing.T) {
			a := newM3TestAsm()
			a.emit(code.OpConstant, 0)
			a.emit(code.OpArrayWithCapacity, 1)
			a.emit(code.OpConstant, 0)
			a.emit(code.OpArrayWithCapacity, descriptor)
			a.emit(code.OpArrayAppendAll)
			if descriptor == 0 {
				a = newM3TestAsm()
				a.emit(code.OpConstant, 0)
				a.emit(code.OpArrayWithCapacity, 0)
				a.emit(code.OpPop)
			}
			p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(0)}, Instructions: a.build()}
			if got := m17eClosureLowerDiagnostic(t, p); !strings.HasPrefix(string(got), "8\n") {
				t.Fatalf("unsafe layout accepted: %q", got)
			}
		})
	}
}

// Deliberately use a raw integer equal to an interior address in this image's
// managed heap. It must neither enter a Root map nor the Storage scanner.
func TestM17ePureMioArrayHeapAddressInteger(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	for _, descriptor := range []int{1, 129} {
		t.Run(fmt.Sprint(descriptor), func(t *testing.T) {
			a := newM3TestAsm()
			a.emit(code.OpConstant, 0)
			a.emit(code.OpArrayWithCapacity, descriptor)
			a.emit(code.OpSetGlobal, 0)
			a.emit(code.OpGetGlobal, 0)
			a.emit(code.OpConstant, 1)
			a.emit(code.OpArrayPush)
			// IsNil is an observation, not evidence that an int64 needs a box.
			a.emit(code.OpGetGlobal, 0)
			a.emit(code.OpConstant, 0)
			a.emit(code.OpIndex)
			a.emit(code.OpIsNil)
			a.emit(code.OpChant)
			for range 350 {
				a.emit(code.OpConstant, 0)
				a.emit(code.OpStruct, 1)
				a.emit(code.OpSetGlobal, 1)
			}
			a.emit(code.OpGetGlobal, 0)
			a.emit(code.OpConstant, 0)
			a.emit(code.OpIndex)
			if descriptor == 129 {
				a.emit(code.OpNeg)
				a.emit(code.OpNeg)
			}
			a.emit(code.OpChant)
			p := &compiler.Bytecode{GlobalsCount: 2, Constants: []value.Value{value.NewInt(0), value.NewInt(1)}, Instructions: a.build()}
			// move_bits always emits four words, so changing constant bits keeps the
			// same image layout. Darwin's writer uses fixed virtual addresses (no PIE).
			image := m17eArrayImage(t, p)
			f, err := macho.Open(image)
			if err != nil {
				t.Fatal(err)
			}
			data := f.Section("__data")
			if data == nil {
				t.Fatal("missing data section")
			}
			address := data.Addr + 2*8 + 288 + 24
			f.Close()
			p.Constants[1] = value.NewInt(int64(address))
			requireM17eClosure(t, p)
			rebuilt := m17eArrayImage(t, p)
			f, err = macho.Open(rebuilt)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if f.Section("__data").Addr+2*8+288+24 != address {
				t.Fatal("heap address fixture changed layout")
			}
			t.Logf("descriptor=%d int64=%#x, inside managed heap; forced GC leaves value intact", descriptor, address)
		})
	}
}
