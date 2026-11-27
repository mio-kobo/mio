package codegen_test

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	miolang "Mio"
	"Mio/internal/mochi"
)

// 同时验证两套前端的声明、符号绑定、原型元数据和 VM 拒绝执行行为。
func TestCMetadataDifferential(t *testing.T) {
	source := `waza mixed(a:int64,b:float64,c:int32,d:float32,p:cptr,u:uint64) -> float64 = c "fixture.dll" "mixed_export";
waza windows_default() -> uint32 = c "FIXTURE.DLL";
waza unix_symbol(a:uint64) -> uint64 = c "mio_unix_symbol";
waza unix_buffer(data:[]uint8,size:uint64,p:cptr) -> int64 = c "mio_unix_buffer";
waza unix_indirect(f:cptr,x:int32,y:float64) -> float64 = c "mio_ffi_indirect";
mixed(1,2.0,int32(3),float32(4.0),cptr(0),uint64(-1));
windows_default();
unix_symbol(uint64(-1));
unix_buffer([uint8(1)],uint64(1),cptr(0));
unix_indirect(cptr(0),int32(1),2.0);`
	pure, err := mochi.Decode(bytes.NewReader(compileWithPureMio(t, source)))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ffi.mio")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	env, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	program, err := env.CompileFile(path, filepath.Join(codegenRoot(t), "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := program.Encode(&encoded); err != nil {
		t.Fatal(err)
	}
	reference, err := mochi.Decode(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pure.Functions, reference.Functions) {
		t.Fatalf("C metadata differs: Mio=%+v Go=%+v", pure.Functions, reference.Functions)
	}
	expected := []string{
		"$win64c|fixture.dll|mixed_export|float64|int64|float64|int32|float32|cptr|uint64",
		"$win64c|FIXTURE.DLL|windows_default|uint32",
		"$cabi::mio_unix_symbol:hh",
		"$cabi::mio_unix_buffer:gmhk",
		"$cabi::mio_ffi_indirect:jkej",
	}
	if len(pure.Functions) != len(expected) {
		t.Fatalf("unexpected function count: %+v", pure.Functions)
	}
	for i, name := range expected {
		fn := pure.Functions[i]
		if fn.Name != name || len(fn.Instructions) != 0 {
			t.Fatalf("lost prototype %s: %+v", name, fn)
		}
	}
	var output bytes.Buffer
	if err := env.Run(program, &output); err == nil || !strings.Contains(err.Error(), "E_FFI_RUNTIME") {
		t.Fatalf("VM executed C stub: %v", err)
	}
}

func compileErrorWithPureMio(t testing.TB, source string) string {
	t.Helper()
	root := codegenRoot(t)
	harness := filepath.Join(t.TempDir(), "harness.mio")
	if err := os.WriteFile(harness, []byte(pureCodegenHarness(source)), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(&output)
	readInt := func() int {
		if !scanner.Scan() {
			t.Fatal("unexpected EOF in pure Mio codegen output")
		}
		v, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	success := readInt()
	byteLen := readInt()
	for i := 0; i < byteLen; i++ {
		readInt()
	}
	errorCount := readInt()
	var details []string
	for i := 0; i < errorCount && scanner.Scan(); i++ {
		details = append(details, scanner.Text())
	}
	if success == 1 {
		t.Fatalf("expected pure Mio compile failure, succeeded")
	}
	return strings.Join(details, "; ")
}

func TestFFIReservedSymbolDifferential(t *testing.T) {
	allowed := `waza ver() -> uint32 = c "mio_ffi_abi_version";
waza ind(f: cptr, x: int32) -> int32 = c "mio_ffi_indirect";
waza ord(x: int64) -> int64 = c "mio_ordinary_symbol";
chant(ver());
chant(ind(cptr(0), int32(1)));
chant(ord(1));`
	if _, err := mochi.Decode(bytes.NewReader(compileWithPureMio(t, allowed))); err != nil {
		t.Fatal(err)
	}
	env, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	allowedPath := filepath.Join(t.TempDir(), "allowed.mio")
	if err := os.WriteFile(allowedPath, []byte(allowed), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := env.CompileFile(allowedPath, filepath.Join(codegenRoot(t), "bunko")); err != nil {
		t.Fatalf("Go compiler rejected allowed FFI declarations: %v", err)
	}

	rejectedCases := []struct {
		name   string
		source string
	}{
		{"loader_intrinsic", `waza w(x: cptr) -> int64 = c "mio_ffi_load_word"; chant(w(cptr(0)));`},
		{"loader_call", `waza cg(x: cptr) -> int64 = c "mio_ffi_call_g"; chant(cg(cptr(0)));`},
		{"dynamic_bridge", `waza call(f: cptr, frame: cptr, stack: uint64) -> unit = c "mio_ffi_darwin_call"; call(cptr(0), cptr(0), uint64(0));`},
		{"v1_alias", `waza alias() -> int64 = c "mio_ffi_v1_g_foo"; chant(alias());`},
	}
	for _, tc := range rejectedCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name+".mio")
			if err := os.WriteFile(path, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := env.CompileFile(path, filepath.Join(codegenRoot(t), "bunko")); err == nil || !strings.Contains(err.Error(), "reserved Mio FFI runtime symbol") {
				t.Fatalf("Go compiler expected reserved symbol error, got: %v", err)
			}
			pureErr := compileErrorWithPureMio(t, tc.source)
			if !strings.Contains(pureErr, "reserved Mio FFI runtime symbol") {
				t.Fatalf("Pure Mio compiler expected reserved symbol error, got: %q", pureErr)
			}
		})
	}
}
