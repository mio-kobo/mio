package aot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"Mio/internal/compiler"
	"Mio/internal/mochi"
)

func m19Metrics(t *testing.T, data []byte) map[string]uint64 {
	t.Helper()
	fields := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "mio-gc "), "=")
		if !ok || !strings.HasPrefix(line, "mio-gc ") {
			t.Fatalf("invalid telemetry %q", line)
		}
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := fields[key]; exists {
			t.Fatalf("duplicate field %s", key)
		}
		fields[key] = n
	}
	return fields
}

func m19Run(t *testing.T, image []byte) ([]byte, []byte, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image")
	if err := os.WriteFile(path, image, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/codesign", "-s", "-", path).CombinedOutput(); err != nil {
		t.Fatalf("sign: %v %s", err, out)
	}
	cmd := exec.Command(path)
	var out, diagnostics bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostics
	err := cmd.Run()
	return out.Bytes(), diagnostics.Bytes(), err
}

// Execute the pure Mio lowering implementation, not a Go replacement for it.
func m19MioImage(t *testing.T, p *compiler.Bytecode, capacity int, stress bool, observation bool, vmWitness bool, adaptive ...bool) []byte {
	t.Helper()
	dir := t.TempDir()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	input, output := filepath.Join(dir, "input.mochi"), filepath.Join(dir, "image")
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	mode, flag := "compile_with_heap", "iie"
	if observation {
		mode = "compile_with_observation"
	}
	if stress {
		flag = "hai"
	}
	if len(adaptive) > 0 && adaptive[0] {
		mode = "compile_with_adaptive_heap"
		if observation {
			flag += ", hai"
		} else {
			flag += ", iie"
		}
	}
	builder := m17CompileMio(t, root, fmt.Sprintf(`yobu "kiso/aot/lower";
yobu "kiso/aot/macho";
yobu "kiso/encoding/mochi";
yobu "kiso/fs";
waza run() -> unit {
 kaku decoded = mochi.decode_file(%q); kaku file = decoded.value;
 moshi (file != mu) {
  kaku result = lower.%s(file, %d, %s);
  kaku problem = result.error;
  moshi (problem != mu) { chant(lower.error_code(problem)); chant(lower.error_detail(problem)); }
  sore {
   kaku image = macho.executable(macho.ImageSpec { words: result.words, constants: result.constants, globals: result.globals, patches: result.patches });
   moshi (image.error != mu) { chant(-2); }
   sore { kaku written = fs.write_all(%q, image.bytes); moshi (written.error != mu) { chant(-3); } }
  }
 } sore { chant(-1); }
}
run();`, input, mode, capacity, flag, output))
	var vmImage []byte
	if vmWitness {
		if out := m17RunVM(t, builder, 500_000_000); len(out) != 0 {
			t.Fatalf("VM builder: %s", out)
		}
		vmImage, err = os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(output); err != nil {
			t.Fatal(err)
		}
	}
	image, err := CompileDarwinARM64WithOptions(builder, DarwinARM64Options{HeapCapacity: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	out, diagnostics, err := m19Run(t, image)
	if err != nil || len(out) != 0 || len(diagnostics) != 0 {
		t.Fatalf("Native Mio builder: %v stdout=%s stderr=%s", err, out, diagnostics)
	}
	result, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if vmWitness && !bytes.Equal(vmImage, result) {
		t.Fatal("Mio lowering VM/native images differ")
	}
	return result
}

func m19CheckMetrics(t *testing.T, fields map[string]uint64) {
	t.Helper()
	for _, key := range []string{"gc_count", "gc_last_ticks", "gc_total_ticks", "gc_max_ticks", "timer_frequency_hz", "runtime_ticks", "allocated_bytes", "heap_live_bytes", "heap_peak_live_bytes", "heap_capacity"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
	var count uint64
	for i := 0; i < 64; i++ {
		key := fmt.Sprintf("gc_pause_bucket_%d", i)
		n, ok := fields[key]
		if !ok {
			t.Fatalf("missing %s", key)
		}
		count += n
	}
	if count != fields["gc_count"] {
		t.Fatalf("histogram=%d count=%d", count, fields["gc_count"])
	}
	if count > 0 {
		maxTicks := fields["gc_max_ticks"]
		index := 0
		if maxTicks > 1 {
			index = bits.Len64(maxTicks - 1)
		}
		if index > 63 {
			index = 63
		}
		if fields[fmt.Sprintf("gc_pause_bucket_%d", index)] == 0 {
			t.Fatal("maximum pause missing from histogram")
		}
		for i := index + 1; i < 64; i++ {
			if fields[fmt.Sprintf("gc_pause_bucket_%d", i)] != 0 {
				t.Fatal("bucket exceeds maximum pause")
			}
		}
	}
	if fields["timer_frequency_hz"] == 0 || fields["gc_last_ticks"] > fields["gc_max_ticks"] || fields["gc_max_ticks"] > fields["gc_total_ticks"] || fields["gc_total_ticks"] > fields["runtime_ticks"] {
		t.Fatalf("invalid timers: %v", fields)
	}
	if fields["heap_live_bytes"] > fields["heap_peak_live_bytes"] || fields["heap_peak_live_bytes"] > fields["heap_capacity"] || fields["allocated_bytes"] < fields["heap_peak_live_bytes"] {
		t.Fatalf("invalid heap accounting: %v", fields)
	}
}

func TestM19Observation(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin ARM64")
	}
	root, _ := filepath.Abs("../..")
	for _, engine := range []string{"go", "mio"} {
		for _, scenario := range []struct {
			name, source, want string
			capacity           int
			stress, gc, oom    bool
		}{
			{"idle", `yobu "kiso/array"; kaku a = array.make[uint8](3, uint8(7)); chant(a[1]);`, "7\n", 4096, false, false, false},
			{"stress", `yobu "kiso/array"; waza run() -> unit { kaku live = array.make[int64?](2, mu); live[1] = 42; kaku i = 0; aida (i < 40) { kaku a = array.make[uint8](64, uint8(7)); i = i + 1; } chant(live[0] == mu); kaku n = live[1]; moshi (n != mu) { chant(n); } } run();`, "hai\n42\n", 4096, true, true, false},
			{"churn", `yobu "kiso/array"; waza run() -> unit { kaku i = 0; aida (i < 400) { kaku a = array.make[uint8](64, uint8(7)); i = i + 1; } chant(i); } run();`, "400\n", 4096, false, true, false},
			{"oom", `yobu "kiso/array"; kaku a = array.make[uint8](8192, uint8(7)); chant(len(a));`, "", 4096, false, true, true},
		} {
			t.Run(engine+"/"+scenario.name, func(t *testing.T) {
				p := m17CompileMio(t, root, scenario.source)
				var image []byte
				var err error
				if engine == "go" {
					image, err = CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: uint64(scenario.capacity), GCStress: scenario.stress, GCTelemetry: true})
				} else {
					image = m19MioImage(t, p, scenario.capacity, scenario.stress, true, scenario.name == "stress")
				}
				if err != nil {
					t.Fatal(err)
				}
				out, diagnostics, err := m19Run(t, image)
				if (err != nil) != scenario.oom {
					t.Fatalf("execution: %v stderr=%s", err, diagnostics)
				}
				if scenario.oom {
					exit, ok := err.(*exec.ExitError)
					wantCode := 2
					if engine == "go" {
						wantCode = 1
					}
					if !ok || exit.ExitCode() != wantCode {
						t.Fatalf("unexpected OOM exit: %v", err)
					}
				}
				if engine == "go" && scenario.oom { // existing Go OOM diagnostics precede metrics
					at := bytes.Index(diagnostics, []byte("mio-gc "))
					if at < 0 {
						t.Fatalf("OOM lacks metrics: %s", diagnostics)
					}
					diagnostics = diagnostics[at:]
				}
				if string(out) != scenario.want {
					t.Fatalf("stdout=%q want=%q", out, scenario.want)
				}
				fields := m19Metrics(t, diagnostics)
				m19CheckMetrics(t, fields)
				if (fields["gc_count"] > 0) != scenario.gc {
					t.Fatalf("gc_count=%d", fields["gc_count"])
				}
				if scenario.name == "idle" && (fields["allocated_objects"] != 2 || fields["allocated_bytes"] != 128 || fields["heap_live_bytes"] != 128) {
					t.Fatalf("descriptor + storage must occupy 128 bytes: %v", fields)
				}
				if scenario.name == "churn" && fields["allocated_bytes"] <= fields["heap_capacity"] {
					t.Fatal("fixture did not exercise heap reuse")
				}
			})
		}
	}
}

// Opt-in artifact builder used by scripts/benchmark-native-gc.py. No compiler
// or signing work occurs inside a timed workload invocation.
func TestM19BuildBenchmarks(t *testing.T) {
	dir := os.Getenv("MIO_M19_BENCH_DIR")
	if dir == "" {
		t.Skip("opt in with MIO_M19_BENCH_DIR")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Fatal("requires Darwin ARM64")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs("../..")
	type benchFixture struct {
		Name, Source, Want string
		Heap               int
	}
	fixtures := []benchFixture{
		{"compute", `waza run() -> unit { kaku i = 0; kaku sum = 0; aida (i < 1000000) { sum = sum + i; i = i + 1; } chant(sum); } run();`, "499999500000\n", 1 << 20},
		{"churn", `yobu "kiso/array"; waza run() -> unit { kaku i = 0; kaku sum = 0; aida (i < 30000) { kaku a = array.make[uint8](256, uint8(7)); sum = sum + int64(a[0]); i = i + 1; } chant(sum); } run();`, "210000\n", 1 << 20},
		{"retained", `yobu "kiso/array"; kata Box { value: int64; } waza run() -> unit { kaku live = array.with_capacity[Box](256); kaku i = 0; aida (i < 256) { array.push(live, Box { value: i }); i = i + 1; } i = 0; aida (i < 20000) { kaku a = array.make[Box](64, live[i - (i / 256) * 256]); i = i + 1; } chant(live[255].value); } run();`, "255\n", 1 << 20},
		{"buffers", `yobu "kiso/array"; waza run() -> unit { kaku i = 0; kaku sum = 0; aida (i < 200) { kaku a = array.make[uint8](131072, uint8(3)); sum = sum + int64(a[131071]); i = i + 1; } chant(sum); } run();`, "600\n", 1 << 20},
	}
	if os.Getenv("MIO_M19C_BENCH") == "1" {
		fixtures = append(fixtures,
			benchFixture{"los-2m", `yobu "kiso/array"; waza run()->unit {kaku i=0;kaku sum=0;aida(i<200){kaku a=array.make[uint8](2097152,uint8(3));sum=sum+int64(a[2097151]);i=i+1;}chant(sum);}run();`, "600\n", 8 << 20},
			benchFixture{"los-references", `yobu "kiso/array"; kata Box {value:int64;} waza run()->unit {kaku a=array.make[Box?](20000,mu);kaku i=0;aida(i<200){a[19999]=Box {value:i};kaku j=0;aida(j<20){kaku b=array.make[uint8](16384,uint8(7));j=j+1;}i=i+1;}kaku v=a[19999];moshi(v!=mu){chant(v.value);}}run();`, "199\n", 8 << 20},
			benchFixture{"adaptive-arena", `yobu "kiso/array"; kata Box {value:int64;} waza run()->unit {kaku a=array.with_capacity[Box](0);kaku i=0;aida(i<22000){array.push(a,Box {value:i});i=i+1;}chant(a[0].value);chant(a[21999].value);chant(len(a));}run();`, "0\n21999\n22000\n", 8 << 20})
	}
	type entry struct {
		Name        string `json:"name"`
		Engine      string `json:"engine"`
		Observation bool   `json:"observation"`
		Heap        int    `json:"heap_capacity"`
		Path        string `json:"path"`
		Want        string `json:"expected_stdout"`
	}
	var manifest []entry
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			p := m17CompileMio(t, root, f.Source)
			if err := os.WriteFile(filepath.Join(dir, f.Name+".mio"), []byte(f.Source+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var encoded bytes.Buffer
			if err := mochi.Encode(&encoded, p); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, f.Name+".mochi"), encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			for _, engine := range []string{"go", "mio"} {
				for _, observation := range []bool{false, true} {
					if observation && os.Getenv("MIO_M19_BASELINE") == "1" {
						continue
					}
					// A no-heap compute workload legitimately produces no GC telemetry.
					var image []byte
					var err error
					if engine == "go" {
						image, err = CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: uint64(f.Heap), GCTelemetry: observation, AdaptiveHeap: f.Name == "adaptive-arena"})
					} else {
						image = m19MioImage(t, p, f.Heap, false, observation, false, f.Name == "adaptive-arena")
					}
					if err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(dir, fmt.Sprintf("%s-%s-%t.native", f.Name, engine, observation))
					if err := os.WriteFile(path, image, 0700); err != nil {
						t.Fatal(err)
					}
					if out, err := exec.Command("/usr/bin/codesign", "-s", "-", path).CombinedOutput(); err != nil {
						t.Fatalf("sign: %v %s", err, out)
					}
					manifest = append(manifest, entry{f.Name, engine, observation, f.Heap, path, f.Want})
				}
			}
		})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
