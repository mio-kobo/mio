package aot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"Mio/internal/compiler"
	"Mio/internal/mochi"
)

func m18Evidence(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("MIO_M18_EVIDENCE_DIR")
	if dir == "" {
		t.Skip("opt in with MIO_M18_EVIDENCE_DIR from check-native-aot-fixed-point.py")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Fatal("M18 acceptance requires Apple Silicon")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// This uses the exact compiler Mochi used by all native generations. It never
// substitutes a host code generator for the Mio lowering/encoder/writer.
func TestM18SelfCompilerVMDeterminism(t *testing.T) {
	dir := m18Evidence(t)
	data, err := os.ReadFile(filepath.Join(dir, "native-aot.mochi"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := mochi.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "vm-work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(work, "image.unsigned")
	for name, value := range map[string]string{"input.config": filepath.Join(dir, "native-aot.mochi"), "output.config": output} {
		if err := os.WriteFile(filepath.Join(work, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
	start := time.Now()
	stdout := m17RunVM(t, p, 2_000_000_000)
	image, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("VM stdout=%q: %v", stdout, err)
	}
	want, err := os.ReadFile(filepath.Join(dir, "stage2.unsigned"))
	if err != nil || !bytes.Equal(image, want) {
		t.Fatalf("VM/native self-compiler images differ: %v", err)
	}
	m17eWriteJSON(t, filepath.Join(work, "result.json"), map[string]any{
		"images_identical": true, "sha256": fmt.Sprintf("%x", sha256.Sum256(image)),
		"stdout": string(stdout), "wall_seconds": time.Since(start).Seconds(),
	})
}

// The fixed-point Stage 4 must compile other Mochi inputs too. Each output is
// signed, verified, executed, and compared to VM and independent Go Native.
func TestM18Stage4Differential(t *testing.T) {
	dir := m18Evidence(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]*compiler.Bytecode{
		"closure-cell": m15bCounterProgram(),
		"abi-12":       m15SumProgram(12),
		"types": m17CompileMio(t, root, `yobu "kiso/array";
sentaku Item { Empty; Value(int64); }
kata Box { value: int64; }
waza maybe(flag: bool) -> int64? { moshi (flag) { kaesu 0; } kaesu mu; }
waza run() -> unit {
 kaku live = Box { value: 73 };
 kaku a = array.with_capacity[uint8](0);
 kaku i = 0; aida (i < 65537) { array.push(a, uint8(i - (i / 256) * 256)); i = i + 1; }
 kaku b = array.clone(a); array.append_all(a, b);
 chant(len(a)); chant(a[65536]); chant(a[65537]);
 kaku n = array.make[int64?](3, mu); n[1] = maybe(hai); n[2] = 42;
 chant(n[0] == mu); chant(n[1] == mu);
 kaku v = n[2]; moshi (v != mu) { chant(v + 1); }
 kaku e = Item.Value(91);
 awase (e) { baai Empty { chant(-1); } baai Value(value) { chant(value); } }
 i = 0; aida (i < 1000) { kaku garbage = array.make[Box](32, live); i = i + 1; }
 chant(live.value);
}
run();`),
	}
	fsdir := filepath.Join(dir, "fs-fixture")
	if err := os.Mkdir(fsdir, 0700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 2<<20)
	for i := range data {
		data[i] = byte(i * 37)
	}
	input, copied := filepath.Join(fsdir, "input.bin"), filepath.Join(fsdir, "copied.bin")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	fixtures["nativefs"] = m17CompileMio(t, root, fmt.Sprintf(`yobu "kiso/fs";
waza run() -> unit {
 kaku r = fs.read_all(%q); chant(r.error == mu); chant(len(r.data));
 kaku w = fs.write_all(%q, r.data); chant(w.error == mu);
 kaku again = fs.read_all(%q); chant(again.error == mu); chant(len(again.data));
 kaku i = 0; kaku total = 0;
 aida (i < len(again.data)) { total = total + int64(again.data[i]); i = i + 1; }
 chant(total);
 kaku missing = fs.read_all(%q); chant(missing.error != mu);
}
run();`, input, copied, copied, filepath.Join(fsdir, "missing")))
	for name, p := range fixtures {
		t.Run(name, func(t *testing.T) {
			work := filepath.Join(dir, "differential-"+name)
			if err := os.Mkdir(work, 0700); err != nil {
				t.Fatal(err)
			}
			input, output := filepath.Join(work, "input.mochi"), filepath.Join(work, "image.unsigned")
			var encoded bytes.Buffer
			if err := mochi.Encode(&encoded, p); err != nil {
				t.Fatal(err)
			}
			for path, data := range map[string][]byte{input: encoded.Bytes(), filepath.Join(work, "input.config"): []byte(input), filepath.Join(work, "output.config"): []byte(output)} {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			image := filepath.Join(dir, "stage4.native")
			policy := filepath.Join(work, "sandbox.sb")
			if err := os.WriteFile(policy, []byte(`(version 1)(allow default)(deny process-fork)(deny process-exec)(allow process-exec (literal `+strconv.Quote(image)+`))`), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-f", policy, image)
			cmd.Dir, cmd.Env = work, []string{"PATH="}
			stdout, err := cmd.CombinedOutput()
			if err != nil || !bytes.HasPrefix(stdout, []byte("native-aot: ok\n")) {
				t.Fatalf("Stage 4 compile: %v %q", err, stdout)
			}
			unsigned, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(work, "image.native")
			if err := os.WriteFile(path, unsigned, 0700); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"--force", "--sign", "-", path}, {"--verify", "--strict", path}} {
				if out, err := exec.Command("/usr/bin/codesign", args...).CombinedOutput(); err != nil {
					t.Fatalf("codesign: %v %s", err, out)
				}
			}
			want := m17RunVM(t, p, 500_000_000)
			goOut, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
				return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 128 << 20, GCStress: true})
			})
			if err != nil || !bytes.Equal(goOut, want) {
				t.Fatalf("Go Native=%q VM=%q err=%v", goOut, want, err)
			}
			out, err := exec.CommandContext(ctx, path).CombinedOutput()
			if err != nil || !bytes.Equal(out, want) {
				t.Fatalf("Stage4 target=%q VM=%q err=%v", out, want, err)
			}
			if name == "nativefs" {
				got, err := os.ReadFile(copied)
				if err != nil || !bytes.Equal(got, data) {
					t.Fatal("NativeFS copied bytes differ", err)
				}
			}
			m17eWriteJSON(t, filepath.Join(work, "result.json"), map[string]any{"compiler_stdout": string(stdout), "unsigned_sha256": fmt.Sprintf("%x", sha256.Sum256(unsigned)), "stdout": string(out), "exit_code": 0, "three_way_equal": true, "codesign_strict": true})
		})
	}
}
