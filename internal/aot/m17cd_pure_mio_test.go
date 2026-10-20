package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

func TestM17cdPureMioARM64EncoderGolden(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	source := `yobu "kiso/aot/arm64";
chant(int64(arm64.movz_x(0, 42, 0).word));
chant(int64(arm64.movk_x(3, 43981, 1).word));
chant(int64(arm64.add_x(2, 3, 4).word));
chant(int64(arm64.sub_imm_x(31, 31, 16, iie).word));
chant(int64(arm64.ldr_x(9, 8, 24).word));
chant(int64(arm64.stp_pre_x(29, 30, 31, -16).word));
chant(int64(arm64.b(-1).word));
chant(int64(arm64.b_cond(1, -2).word));
chant(int64(arm64.adrp_offset(8, 768, 16384).word));
chant(int64(arm64.svc(128).word));
chant(arm64.movz_x(32, 0, 0).error != mu);
chant(arm64.add_imm_x(0, 0, 4096, iie).error != mu);
chant(arm64.ldr_x(0, 1, 3).error != mu);
chant(arm64.b(33554432).error != mu);
`
	program := m17CompileMio(t, root, source)
	got := string(m17RunVM(t, program, 10_000_000))
	words := []uint32{
		0xD2800540, 0xF2B579A3, 0x8B040062, 0xD10043FF, 0xF9400D09,
		0xA9BF7BFD, 0x17FFFFFF, 0x54FFFFC1, 0x90000028, 0xD4001001,
	}
	var want strings.Builder
	for _, word := range words {
		fmt.Fprintf(&want, "%d\n", uint64(word))
	}
	want.WriteString("hai\nhai\nhai\nhai\n")
	if got != want.String() {
		t.Fatalf("pure Mio ARM64 encoding:\n%s\nwant:\n%s", got, want.String())
	}
}

func TestM17cdPureMioAssemblerErrors(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	source := `yobu "kiso/aot/arm64";
kaku duplicate = arm64.assembler();
arm64.assembler_label(duplicate, "same");
chant(arm64.assembler_label(duplicate, "same") != mu);
kaku missing = arm64.assembler();
arm64.assembler_b(missing, "absent");
chant(arm64.assembler_assemble(missing).error != mu);
kaku forward = arm64.assembler();
arm64.assembler_b(forward, "done");
arm64.assembler_emit(forward, arm64.movz_x(0, 1, 0));
arm64.assembler_label(forward, "done");
chant(arm64.assembler_assemble(forward).error == mu);
kaku backward = arm64.assembler();
arm64.assembler_label(backward, "again");
arm64.assembler_emit(backward, arm64.movz_x(0, 1, 0));
arm64.assembler_b(backward, "again");
chant(arm64.assembler_assemble(backward).error == mu);
`
	if got := m17RunVM(t, m17CompileMio(t, root, source), 10_000_000); !bytes.Equal(got, []byte("hai\nhai\nhai\nhai\n")) {
		t.Fatalf("assembler validation=%q", got)
	}
}

func TestM17cdPureMioMachOMatchesGoWriter(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pure-mio-mach-o")
	source := fmt.Sprintf(`yobu "kiso/aot/arm64";
yobu "kiso/aot/macho";
yobu "kiso/fs";
kaku words: []uint32 = [arm64.movz_x(0, 0, 0).word, arm64.movz_x(16, 1, 0).word, arm64.svc(128).word];
kaku constants: []uint8 = [77, 105, 111, 0, 228, 184, 173, 240, 159, 140, 159];
kaku patches: []macho.AddressPatch = [];
kaku image = macho.executable(macho.ImageSpec { words: words, constants: constants, globals: 1, patches: patches });
moshi (image.error == mu) { fs.write_all(%q, image.bytes); chant(len(image.bytes)); } sore { chant(-1); }
`, path)
	program := m17CompileMio(t, root, source)
	if got := m17RunVM(t, program, 20_000_000); !bytes.Equal(got, []byte("32768\n")) {
		t.Fatalf("writer output=%q", got)
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := darwinarm64.ExecutableWordsWithGlobalsAndConst(
		[]uint32{0xD2800000, 0xD2800030, 0xD4001001}, 1,
		[]byte{'M', 'i', 'o', 0, 0xe4, 0xb8, 0xad, 0xf0, 0x9f, 0x8c, 0x9f},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		for i := range min(len(actual), len(expected)) {
			if actual[i] != expected[i] {
				t.Fatalf("pure Mio Mach-O differs from Go writer at %#x: Mio=%#x Go=%#x", i, actual[i], expected[i])
			}
		}
		t.Fatalf("pure Mio Mach-O size=%d, Go=%d", len(actual), len(expected))
	}
}

func TestM17cdPureMioBuilderVMNativeAndExecutable(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	example, err := os.ReadFile(filepath.Join(root, "examples", "m17_native_builder", "build.mio"))
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "m17cd-raw")
	source := strings.Replace(string(example), `"build/m17cd-pure-mio"`, fmt.Sprintf("%q", outputPath), 1)
	program := m17CompileMio(t, root, source)

	started := time.Now()
	vmOutput := m17RunVM(t, program, 200_000_000)
	vmDuration := time.Since(started)
	vmImage, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	vmHash := sha256.Sum256(vmImage)

	started = time.Now()
	nativeOutput, err := runM12NativeCompile(t, program, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 128 << 20})
	})
	if err != nil {
		t.Fatalf("native pure-Mio builder: %v output=%q", err, nativeOutput)
	}
	nativeDuration := time.Since(started)
	nativeImage, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vmImage, nativeImage) {
		t.Fatalf("VM/native raw Mach-O mismatch: VM=%x native=%x", vmHash, sha256.Sum256(nativeImage))
	}
	if !bytes.Equal(vmOutput, nativeOutput) {
		t.Fatalf("builder output mismatch: VM=%q native=%q", vmOutput, nativeOutput)
	}
	if len(nativeImage) != 3*0x4000 {
		t.Fatalf("multi-page image size=%d, want=%d", len(nativeImage), 3*0x4000)
	}

	executable := filepath.Join(t.TempDir(), "m17cd-executable")
	if err := os.WriteFile(executable, nativeImage, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", executable).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, output)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--verbose=2", executable).CombinedOutput(); err != nil {
		t.Fatalf("codesign verify: %v: %s", err, output)
	}
	if output, err := exec.Command(executable).CombinedOutput(); err != nil {
		t.Fatalf("execute pure-Mio Mach-O: %v output=%q", err, output)
	}
	t.Logf("pure Mio raw Mach-O size=%d sha256=%x cmp=identical VM=%s Native=%s", len(vmImage), vmHash, vmDuration, nativeDuration)
}

func TestM17cdMochiV14DecoderToPureNativeImage(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	constant, _ := code.Make(code.OpConstant, 0)
	convert, _ := code.Make(code.OpDataConvert, code.DataStringToBytes)
	pop, _ := code.Make(code.OpPop)
	input := &compiler.Bytecode{Instructions: append(append(constant, convert...), pop...), Constants: []value.Value{value.NewString("Mio")}}
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, input); err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(encoded.Bytes()[5:7]); version != mochi.VersionV14 {
		t.Fatalf("fixture version=%d", version)
	}
	directory := t.TempDir()
	mochiPath := filepath.Join(directory, "input.mochi")
	imagePath := filepath.Join(directory, "decoded-image")
	if err := os.WriteFile(mochiPath, encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`yobu "kiso/aot/arm64";
yobu "kiso/aot/macho";
yobu "kiso/encoding/mochi";
yobu "kiso/fs";
kaku decoded = mochi.decode_file(%q);
kaku status = 1;
kaku file = decoded.value;
moshi (decoded.error == mu) { moshi (file != mu) { kaku summary = mochi.summarize(file); moshi (summary.version == 14 && summary.instruction_bytes > 0) { status = 0; } } }
kaku words: []uint32 = [arm64.movz_x(0, status, 0).word, arm64.movz_x(16, 1, 0).word, arm64.svc(128).word];
kaku constants: []uint8 = [];
kaku patches: []macho.AddressPatch = [];
kaku image = macho.executable(macho.ImageSpec { words: words, constants: constants, globals: 0, patches: patches });
moshi (image.error == mu) { fs.write_all(%q, image.bytes); }
`, mochiPath, imagePath)
	program := m17CompileMio(t, root, source)
	m17RunVM(t, program, 50_000_000)
	image, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(imagePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", imagePath).CombinedOutput(); err != nil {
		t.Fatalf("codesign decoder integration: %v: %s", err, output)
	}
	if output, err := exec.Command(imagePath).CombinedOutput(); err != nil {
		t.Fatalf("decoded v14 image exit: %v output=%q raw-size=%d", err, output, len(image))
	}
}
