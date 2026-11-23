package codegen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
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
