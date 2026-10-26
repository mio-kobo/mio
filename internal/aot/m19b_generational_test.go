package aot

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestM19bGenerationalDifferential(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Darwin ARM64")
	}
	root, _ := filepath.Abs("../..")
	const common = `yobu "kiso/array";
kata Box { value: int64; }
kata Wrap { items: []Box?; }
waza churn(n: int64) -> unit { kaku i=0; aida (i<n) { kaku a=array.make[uint8](64,uint8(19)); i=i+1; } }
`
	cases := []struct{ name, source string }{
		{"storage-shared-nullable", common + `waza run() -> unit {
 kaku a=array.make[Box?](4,mu); kaku alias=a; kaku wrap=Wrap {items:a}; churn(20);
 kaku i=0; aida(i<30) { a[1]=Box {value:i}; a[2]=a[1]; churn(5);
 kaku x=wrap.items[1]; kaku y=alias[2]; moshi(x!=mu) { moshi(y!=mu) { chant(x.value+y.value); } }
 a[1]=mu; a[2]=mu; churn(5); i=i+1; }
 chant(a[1]==mu); } run();`},
		{"push-append-reserve", common + `waza run() -> unit {
 kaku a=array.with_capacity[Box](256); kaku b=array.with_capacity[Box](0); churn(20);
 kaku i=0; aida(i<30) { array.push(a,Box {value:i}); churn(5); i=i+1; }
 kaku c=array.clone(a); array.append_all(a,c); array.reserve(a,400); array.append_all(b,a); churn(20);
 chant(a[29].value); chant(a[59].value); chant(b[59].value); chant(len(b));
 array.truncate(a,0); array.push(a,Box {value:99}); churn(2000); chant(a[0].value);
 } run();`},
		{"closure-cell", common + `waza run() -> unit {
 kaku current=Box {value:0};
 kaku get=waza() -> int64 { kaesu current.value; };
 kaku set=waza(v: Box) -> unit { current=v; };
 churn(20); kaku i=0; aida(i<40) { set(Box {value:i}); churn(5); chant(get()); i=i+1; }
 churn(1000); chant(get()); } run();`},
		{"large-major-pressure", common + `waza run() -> unit {
 kaku refs=array.with_capacity[Box](0); kaku i=0; aida(i<40) { array.push(refs,Box {value:i}); i=i+1; }
 churn(1000); chant(refs[0].value); chant(refs[39].value);
 kaku b=array.make[uint8](131072,uint8(219)); churn(500); chant(b[131071]);
 } run();`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := m17CompileMio(t, root, c.source)
			want := m17RunVM(t, p, 50_000_000)
			for _, engine := range []string{"go", "mio"} {
				for _, stress := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/stress=%v", engine, stress), func(t *testing.T) {
						var image []byte
						capacity := 16 << 10
						if c.name == "push-append-reserve" {
							capacity = 64 << 10
						}
						if c.name == "large-major-pressure" {
							capacity = 192 << 10
						}
						if engine == "go" {
							var err error
							image, err = CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: uint64(capacity), GCStress: stress, GCTelemetry: true})
							if err != nil {
								t.Fatal(err)
							}
						} else {
							image = m19MioImage(t, p, capacity, stress, true, false)
						}
						out, diagnostics, err := m19Run(t, image)
						if err != nil || !bytes.Equal(out, want) {
							t.Fatalf("execution %v\nwant=%s\nstdout=%s\nstderr=%s", err, want, out, diagnostics)
						}
						m := m19Metrics(t, diagnostics)
						m19CheckMetrics(t, m)
						m19bCheckMetrics(t, m)
						if m["minor_gc_count"] == 0 {
							t.Fatal("no actual minor GC")
						}
						if stress && m["promoted_objects"] == 0 {
							t.Fatal("no actual promotion")
						}
					})
				}
			}
		})
	}
}

func m19bCheckMetrics(t *testing.T, m map[string]uint64) {
	t.Helper()
	if m["minor_gc_count"]+m["major_gc_count"] != m["gc_count"] {
		t.Fatal("collection kinds do not sum")
	}
	if m["minor_gc_total_ticks"]+m["major_gc_total_ticks"] != m["gc_total_ticks"] {
		t.Fatal("kind timings do not sum")
	}
	if m["young_live_bytes"]+m["old_live_bytes"] != m["heap_live_bytes"] {
		t.Fatalf("generation bytes do not sum: %v", m)
	}
	if m["promotion_threshold"] < 2 || m["promotion_threshold"] > 8 {
		t.Fatal("unbounded threshold")
	}
	for _, kind := range []string{"minor", "major"} {
		var n uint64
		for i := 0; i < 64; i++ {
			n += m[fmt.Sprintf("%s_gc_pause_bucket_%d", kind, i)]
		}
		if n != m[kind+"_gc_count"] {
			t.Fatalf("%s histogram count", kind)
		}
	}
	for i := 0; i < 64; i++ {
		if m[fmt.Sprintf("gc_pause_bucket_%d", i)] != m[fmt.Sprintf("minor_gc_pause_bucket_%d", i)]+m[fmt.Sprintf("major_gc_pause_bucket_%d", i)] {
			t.Fatal("kind histograms do not sum")
		}
	}
}

// Compile the actual Mio runtime emitters into a test builder, then execute
// their emitted ARM64 directly. Concatenation only gives this test access to
// package-private emitters; no replacement collector or public test API.
func TestM19bPureMioNativeMetadata(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Darwin ARM64")
	}
	root, _ := filepath.Abs("../..")
	var packageSource strings.Builder
	imports := map[string]bool{}
	for _, name := range []string{"lower.mio", "observation.mio", "generational.mio", "large_objects.mio", "memory_runtime.mio", "memory_policy.mio"} {
		data, err := os.ReadFile(filepath.Join(root, "bunko/kiso/aot/lower", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "yobu ") {
				imports[line] = true
			} else {
				packageSource.WriteString(line + "\n")
			}
		}
	}
	imports[`yobu "kiso/fs";`] = true
	var header strings.Builder
	names := make([]string, 0, len(imports))
	for name := range imports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header.WriteString(name + "\n")
	}
	for _, mode := range []string{"ages", "remembered", "promotion-edge", "major", "precision", "adaptive", "cross-graph", "alternating", "minor-scan", "precision-storage"} {
		t.Run(mode, func(t *testing.T) {
			var source strings.Builder
			source.WriteString(header.String())
			source.WriteString(packageSource.String())
			source.WriteString("\nwaza probe_builder() -> unit { kaku code=arm64.assembler(); emit_runtime_init(code,4096,3072);\n")
			emit := func(s string) { fmt.Fprintf(&source, "emit(code, arm64.%s);\n", s) }
			bl := func(s string) { fmt.Fprintf(&source, "arm64.assembler_bl(code,%q);\n", s) }
			eq := func(r, n int) {
				emit(fmt.Sprintf("movz_x(11,%d,0)", n))
				emit(fmt.Sprintf("cmp_x(%d,11)", r))
				source.WriteString("arm64.assembler_b_cond(code,1,\"runtime_error\");\n")
			}
			metric := func(r int, name string) { fmt.Fprintf(&source, "emit(code,arm64.ldr_x(%d,23,gen_%s));\n", r, name) }
			flags := func(p, r int) {
				emit(fmt.Sprintf("sub_imm_x(9,%d,16,iie)", p))
				emit(fmt.Sprintf("ldr_x(%d,9,0)", r))
				emit(fmt.Sprintf("lsr_x(%d,%d,32)", r, r))
			}
			rootSlot := func(slot, r int) { emit(fmt.Sprintf("str_x(%d,31,%d)", r, 16+slot*8)) }
			alloc := func(dst int, ref bool) {
				emit("movz_x(0,16,0)")
				emit("movz_x(1,105,0)")
				bl("runtime_alloc")
				source.WriteString("arm64.assembler_cbz_x(code,0,\"runtime_error\");\n")
				fmt.Fprintf(&source, "move_x(code,%d,0);\n", dst)
				kind := 6
				if ref {
					kind += 256
				}
				emit(fmt.Sprintf("movz_x(9,%d,0)", kind))
				emit(fmt.Sprintf("str_x(9,%d,0)", dst))
				emit(fmt.Sprintf("str_x(31,%d,8)", dst))
			}
			minor := func() { bl("gen_minor") }
			major := func() { bl("gen_major") }
			remember := func(r int) { fmt.Fprintf(&source, "emit_gen_remember(code,%d);\n", r) }
			emit("sub_imm_x(31,31,64,iie)")
			emit("str_x(31,31,0)")
			emit("movz_x(9,4,0)")
			emit("str_x(9,31,8)")
			for i := 0; i < 4; i++ {
				rootSlot(i, 31)
			}
			emit("add_imm_x(24,31,0,iie)")
			switch mode {
			case "ages":
				alloc(20, false)
				rootSlot(0, 20)
				flags(20, 10)
				eq(10, 0)
				for i := 1; i <= 3; i++ {
					minor()
					flags(20, 10)
					n := i * 16
					if i == 3 {
						n += 8 + 256
					}
					eq(10, n)
				}
				emit("ldr_x(10,31,16)")
				emit("cmp_x(10,20)")
				source.WriteString("arm64.assembler_b_cond(code,1,\"runtime_error\");\n")
				metric(10, "PromotedObjects")
				eq(10, 1)
			case "remembered", "precision", "minor-scan":
				alloc(20, mode != "precision")
				rootSlot(0, 20)
				if mode == "minor-scan" {
					for i := 0; i < 63; i++ {
						alloc(21, true)
						emit("str_x(20,21,8)")
						source.WriteString("move_x(code,20,21);\n")
						rootSlot(0, 20)
					}
				}
				for i := 0; i < 4; i++ {
					minor()
				}
				metric(10, "RememberedCount")
				eq(10, 0)
				alloc(21, false)
				emit("movz_x(9,77,0)")
				emit("str_x(9,21,8)")
				emit("str_x(21,20,8)")
				remember(20)
				remember(20)
				metric(10, "RememberedCount")
				eq(10, 1)
				minor()
				if mode != "precision" {
					emit("ldr_x(9,20,8)")
					emit("ldr_x(10,9,8)")
					eq(10, 77)
				} else {
					metric(10, "YoungBytes")
					eq(10, 0)
				}
				if mode == "minor-scan" {
					metric(10, "ScannedOld")
					eq(10, 65)
				}
				emit("str_x(31,20,8)")
				remember(20)
				minor()
				metric(10, "RememberedCount")
				eq(10, 0)
				metric(10, "YoungBytes")
				eq(10, 0)
			case "promotion-edge":
				alloc(20, true)
				rootSlot(0, 20)
				minor()
				minor()
				alloc(21, false)
				emit("movz_x(9,91,0)")
				emit("str_x(9,21,8)")
				emit("str_x(21,20,8)")
				minor()
				flags(20, 10)
				eq(10, 48+8+256)
				minor()
				emit("ldr_x(9,20,8)")
				emit("ldr_x(10,9,8)")
				eq(10, 91)
			case "major":
				alloc(20, true)
				rootSlot(0, 20)
				for i := 0; i < 3; i++ {
					minor()
				}
				rootSlot(0, 31)
				minor()
				metric(10, "OldBytes")
				eq(10, 32)
				major()
				metric(10, "OldBytes")
				eq(10, 0)
				alloc(21, false)
				rootSlot(0, 21)
				minor()
				flags(21, 10)
				eq(10, 16)
			case "precision-storage":
				emit("movz_x(0,40,0)")
				emit("movz_x(1,102,0)")
				bl("runtime_alloc")
				source.WriteString("move_x(code,20,0);\n")
				emit("movz_x(9,2051,0)")
				emit("str_x(9,20,0)")
				emit("movz_x(9,1,0)")
				emit("str_x(9,20,8)")
				emit("str_x(9,20,16)")
				emit("str_x(31,20,24)")
				emit("str_x(31,20,32)")
				rootSlot(0, 20)
				for i := 0; i < 3; i++ {
					minor()
				}
				alloc(21, false)
				emit("str_x(21,20,32)")
				remember(20)
				minor()
				metric(10, "YoungBytes")
				eq(10, 0)
				metric(10, "OldBytes")
				eq(10, 64)
				emit("ldr_x(10,20,32)")
				emit("cmp_x(10,21)")
				source.WriteString("arm64.assembler_b_cond(code,1,\"runtime_error\");\n")
			case "adaptive":
				for i := 0; i < 18; i++ {
					alloc(20, false)
					minor()
				}
				metric(10, "Threshold")
				eq(10, 8)
				alloc(20, false)
				rootSlot(0, 20)
				for i := 0; i < 4; i++ {
					minor()
				}
				metric(10, "Threshold")
				eq(10, 7)
			case "cross-graph":
				alloc(20, true)
				rootSlot(0, 20)
				alloc(21, true)
				rootSlot(1, 21)
				emit("str_x(21,20,8)")
				for i := 0; i < 3; i++ {
					minor()
				}
				alloc(22, true)
				emit("str_x(22,20,8)")
				emit("str_x(22,21,8)")
				remember(20)
				remember(21)
				alloc(25, false)
				emit("movz_x(9,123,0)")
				emit("str_x(9,25,8)")
				emit("str_x(25,22,8)")
				minor()
				alloc(22, true)
				rootSlot(0, 22)
				rootSlot(1, 31)
				emit("str_x(21,22,8)")
				minor()
				emit("ldr_x(9,22,8)")
				emit("ldr_x(9,9,8)")
				emit("ldr_x(9,9,8)")
				emit("ldr_x(10,9,8)")
				eq(10, 123)
			case "alternating":
				alloc(20, true)
				rootSlot(0, 20)
				for i := 0; i < 40; i++ {
					alloc(21, false)
					emit(fmt.Sprintf("movz_x(9,%d,0)", i))
					emit("str_x(9,21,8)")
					emit("str_x(21,20,8)")
					remember(20)
					minor()
					if i%4 == 0 {
						major()
					}
					emit("ldr_x(9,20,8)")
					emit("ldr_x(10,9,8)")
					eq(10, i)
				}
				rootSlot(0, 31)
				major()
				metric(10, "OldBytes")
				eq(10, 0)
				metric(10, "YoungBytes")
				eq(10, 0)
			}
			emit("movz_x(0,0,0)")
			emit("movz_x(16,1,0)")
			emit("svc(128)")
			for _, label := range []string{"runtime_error", "runtime_oom", "runtime_gc_bad_type", "runtime_gc_bad_kind", "runtime_gc_bad_count"} {
				fmt.Fprintf(&source, "arm64.assembler_label(code,%q);\n", label)
			}
			emit("movz_x(0,1,0)")
			emit("movz_x(16,1,0)")
			emit("svc(128)")
			source.WriteString("emit_managed_runtime(code,iie,3072,iie); kaku result=arm64.assembler_assemble(code); moshi(result.error!=mu) { chant(-1); } sore { kaku patches: []macho.AddressPatch=[]; kaku image=macho.executable(macho.ImageSpec {words:result.words,constants:[],globals:0,patches:patches});\n")
			output := filepath.Join(t.TempDir(), "pure-mio-probe")
			fmt.Fprintf(&source, "kaku written=fs.write_all(%q,image.bytes); moshi(written.error!=mu){chant(-2);} } } probe_builder();", output)
			p := m17CompileMio(t, root, source.String())
			if out := m17RunVM(t, p, 500_000_000); len(out) != 0 {
				t.Fatalf("Mio emitter: %s", out)
			}
			image, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			out, diagnostics, err := m19Run(t, image)
			if err != nil || len(out) != 0 || len(diagnostics) != 0 {
				t.Fatalf("Mio runtime %s: %v %s %s", mode, err, out, diagnostics)
			}
		})
	}
}
