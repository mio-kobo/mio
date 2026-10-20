package aot

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/stdlib"
	"Mio/internal/value"
	"Mio/internal/vm"
)

// Execute both outcomes across real >1 MiB gaps, in both directions. An
// ADRP/ADD/BR entry relocation crosses the reserved slots too. Each destination
// checks the original comparison flags and register before returning its own
// exit code, so a reversed predicate or clobbered NZCV cannot pass unnoticed.
func TestM17ePureMioFarBranchExecutionAndRelocation(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"b_cond", "cbz_x", "cbnz_x"} {
		for _, backward := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/backward=%v", kind, backward), func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "far-native")
				branch := fmt.Sprintf("arm64.assembler_%s(a, 9, \"target\");", kind)
				if kind == "b_cond" {
					branch = "arm64.assembler_b_cond(a, 0, \"target\");"
				}
				target := `arm64.assembler_label(a, "target"); arm64.assembler_emit(a, arm64.movz_x(0, 42, 0)); arm64.assembler_b(a, "validate");`
				setup := `arm64.assembler_label(a, "entry");
arm64.assembler_emit(a, arm64.orr_x(20, 31, 9));
arm64.assembler_emit(a, arm64.movz_x(21, 1, 0));
arm64.assembler_emit(a, arm64.sub_x(21, 21, 9));
arm64.assembler_emit(a, arm64.cmp_imm_x(9, 0, iie));` + branch + `
arm64.assembler_emit(a, arm64.movz_x(0, 7, 0)); arm64.assembler_b(a, "validate");`
				padding := `array.append_all(a.words, array.make[uint32](262145, uint32(3573751839)));`
				middle := setup + padding + target
				if backward {
					middle = target + padding + setup
				}
				source := fmt.Sprintf(`yobu "kiso/aot/arm64";
yobu "kiso/aot/macho";
yobu "kiso/array";
yobu "kiso/fs";
waza build() -> unit {
kaku a = arm64.assembler();
arm64.assembler_emit(a, arm64.movz_x(9, 0, 0));
arm64.assembler_emit(a, arm64.movz_x(12, 0, 0));
arm64.assembler_emit(a, arm64.movz_x(12, 0, 0));
arm64.assembler_emit(a, arm64.br_x(12));
%s
arm64.assembler_label(a, "validate");
arm64.assembler_emit(a, arm64.cset_w(11, 0));
arm64.assembler_emit(a, arm64.cmp_x(9, 20)); arm64.assembler_b_cond(a, 1, "bad");
arm64.assembler_emit(a, arm64.cmp_x(11, 21)); arm64.assembler_b_cond(a, 1, "bad");
arm64.assembler_b(a, "exit");
arm64.assembler_label(a, "bad"); arm64.assembler_emit(a, arm64.movz_x(0, 99, 0));
arm64.assembler_label(a, "exit"); arm64.assembler_emit(a, arm64.movz_x(16, 1, 0)); arm64.assembler_emit(a, arm64.svc(128));
kaku result = arm64.assembler_assemble(a);
kaku patches = [macho.AddressPatch { at_word: 1, add_at_word: 2, target: macho.text_target(), target_offset: arm64.assembler_label_word(a, "entry") * 4, register: 12 }];
moshi (result.error != mu) { chant(-1); }
sore { kaku image = macho.executable(macho.ImageSpec { words: result.words, constants: [], globals: 0, patches: patches });
 moshi (image.error != mu) { chant(-2); } sore { fs.write_all(%q, image.bytes); }
}
}
build();
`, middle, path)
				builder := m17CompileMio(t, root, source)
				if out := m17RunVM(t, builder, 500_000_000); len(out) != 0 {
					t.Fatalf("VM builder: %q", out)
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				out, err := runM12NativeCompile(t, builder, func(p *compiler.Bytecode) ([]byte, error) {
					return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 128 << 20})
				})
				if err != nil || len(out) != 0 {
					t.Fatalf("native builder: %v %q", err, out)
				}
				native, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(raw, native) {
					t.Fatal("VM/native builders differ")
				}
				f, err := macho.NewFile(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				text := f.Section("__text")
				if text == nil || text.Size < 1<<20 {
					t.Fatal("missing far text")
				}
				offset := int(text.Offset)
				f.Close()
				for input := 0; input < 2; input++ {
					image := bytes.Clone(raw)
					binary.LittleEndian.PutUint32(image[offset:], uint32(0xd2800009+input*32)) // MOVZ X9,input
					if err := os.WriteFile(path, image, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(path, 0700); err != nil {
						t.Fatal(err)
					}
					for _, args := range [][]string{{"--force", "--sign", "-", path}, {"--verify", "--strict", path}} {
						if out, err := exec.Command("/usr/bin/codesign", args...).CombinedOutput(); err != nil {
							t.Fatalf("codesign: %v %s", err, out)
						}
					}
					cmd := exec.Command(path)
					out, err := cmd.CombinedOutput()
					want := 7
					if (input == 0 && kind != "cbnz_x") || (input == 1 && kind == "cbnz_x") {
						want = 42
					}
					if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != want || len(out) != 0 {
						t.Fatalf("input=%d want=%d execution=%v output=%q state=%v", input, want, err, out, cmd.ProcessState)
					}
				}
			})
		}
	}
}

// Put the real lowering error checks over 1 MiB from runtime_error, then test
// both a successful and failing divisor. Harmless calls retain padding under
// function reachability analysis and force real long control transfers.
func TestM17ePureMioFarRuntimeError(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(42), value.NewInt(0), value.NewInt(1)}}
	for i := 0; i < 30; i++ {
		body := code.Instructions{}
		for j := 0; j < 1500; j++ {
			body = append(body, byte(code.OpConstant), 0, 0, byte(code.OpPop))
		}
		body = append(body, byte(code.OpReturn))
		p.Functions = append(p.Functions, compiler.CompiledFunction{ID: i, Name: fmt.Sprintf("padding%d", i), Instructions: body})
	}
	p.Functions = append(p.Functions, compiler.CompiledFunction{ID: 30, Name: "divide", ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Instructions: code.Instructions{byte(code.OpConstant), 0, 0, byte(code.OpGetLocal), 0, 0, byte(code.OpDiv), byte(code.OpReturnValue)}})
	// Success first, error second; the first printed value must survive before
	// the runtime_error exit. This catches a wrong CBZ polarity in real codegen.
	for i := 0; i < 30; i++ {
		call, err := code.Make(code.OpCall, i)
		if err != nil {
			t.Fatal(err)
		}
		p.Instructions = append(p.Instructions, call...)
	}
	p.Instructions = append(p.Instructions, code.Instructions{byte(code.OpConstant), 0, 2, byte(code.OpCall), 0, 30, byte(code.OpChant), byte(code.OpConstant), 0, 1, byte(code.OpCall), 0, 30, byte(code.OpChant)}...)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var vmOutput bytes.Buffer
	machine := vm.NewWithRegistryAndOutput(p, registry, &vmOutput)
	if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: 1_000_000}); err == nil || vmOutput.String() != "42\n" {
		t.Fatalf("VM division error=%v stdout=%q", err, vmOutput.String())
	}
	reference, refErr := runM12NativeCompile(t, p, CompileDarwinARM64WithGCStress)
	if refErr == nil || string(reference) != "42\n" {
		t.Fatalf("Go reference division error=%v stdout=%q", refErr, reference)
	}
	path := m17eArrayImageWithLimits(t, p, false, 1_500_000_000, 512<<20)
	f, err := macho.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Section("__text").Size < 1<<20 {
		t.Fatal("fixture too small for runtime_error far branch")
	}
	f.Close()
	cmd := exec.Command(path)
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 1 || string(out) != "42\n" {
		t.Fatalf("runtime_error execution=%v state=%v output=%q root=%s", err, cmd.ProcessState, out, root)
	}
}

// Regression for the original one-page heap blocker. Even a one-byte file
// requires a 32800-byte Storage payload and must now succeed across OS pages.
func TestM17ePureMioReadAllBoundedHeap(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "one-byte")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	p := m17CompileMio(t, root, fmt.Sprintf(`yobu "kiso/fs"; kaku result=fs.read_all(%q); chant(len(result.data));`, path))
	if out := m17RunVM(t, p, 1_000_000); string(out) != "1\n" {
		t.Fatalf("VM=%q", out)
	}
	if out, err := runM12NativeCompile(t, p, CompileDarwinARM64); err != nil || string(out) != "1\n" {
		t.Fatalf("Go reference: %v %q", err, out)
	}
	cmd := exec.Command(m17eArrayImageMode(t, p, false))
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 0 || string(out) != "1\n" {
		t.Fatalf("bounded target heap: %v %q state=%v", err, out, cmd.ProcessState)
	}
	t.Log("fs.read_all(one byte) succeeds with cross-page managed Storage")
}
