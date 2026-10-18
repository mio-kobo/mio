package aot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func m14Stub(id int, name string, params int) compiler.CompiledFunction {
	return compiler.CompiledFunction{
		ID: id, Name: "$mma::mio/fs/runtime::" + name + "::test",
		ParameterCount: params, LocalCount: params, ReturnsValue: true,
	}
}

func TestM14NativeFileOpenStatusClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(path, []byte("mio"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpCall, 0)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpChant)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpArrayTyped, 1, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpCall, 1)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpCall, 2)
	a.emit(code.OpChant)
	// A stale token cannot close a reused/closed slot.
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpCall, 2)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	p := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewString(path), value.NewInt(0), value.NewUint8(0)},
		Instructions: a.build(),
		Functions: []compiler.CompiledFunction{
			m14Stub(0, "open", 2), m14Stub(1, "file_status", 2), m14Stub(2, "close", 2),
		},
	}
	got, err := runM12Native(t, p)
	if err != nil {
		t.Fatalf("native execution failed: %v, output=%q", err, got)
	}
	if want := []byte("257\n\n0\n\n\n3\n"); !bytes.Equal(got, want) {
		t.Fatalf("native output=%q, want=%q", got, want)
	}
}

func TestM14NativeDirectoryIteration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "entry.mio"), []byte("mio"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpCall, 0)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArrayTyped, 1, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpCall, 1)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpCall, 2)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpCall, 3)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpCall, 4)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpCall, 5)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpCall, 6)
	a.emit(code.OpChant)
	p := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewString(dir), value.NewUint8(0), value.NewInt(0)},
		Instructions: a.build(),
		Functions: []compiler.CompiledFunction{
			m14Stub(0, "open_dir", 1), m14Stub(1, "dir_status", 2),
			m14Stub(2, "dir_next", 1), m14Stub(3, "dir_name", 1),
			m14Stub(4, "dir_is_file", 1), m14Stub(5, "dir_is_dir", 1),
			m14Stub(6, "dir_close", 2),
		},
	}
	got, err := runM12Native(t, p)
	if err != nil {
		t.Fatalf("native execution failed: %v, output=%q", err, got)
	}
	if want := []byte("\n0\nhai\nentry.mio\nhai\niie\n\n"); !bytes.Equal(got, want) {
		t.Fatalf("native output=%q, want=%q", got, want)
	}
}

func TestM14NativeFileWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundtrip.bin")
	a := newM3TestAsm()
	// handle = open(path, write); data = ['M','i','o']; status = byte[10]
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpCall, 0)
	a.emit(code.OpSetGlobal, 0)
	for _, id := range []int{2, 3, 4} {
		a.emit(code.OpConstant, id)
	}
	a.emit(code.OpArrayTyped, 3, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 1)
	for range 10 {
		a.emit(code.OpConstant, 9)
	}
	a.emit(code.OpArrayTyped, 10, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 2)
	for _, op := range []struct {
		global   int
		constant int
	}{{0, -1}, {1, -1}, {-1, 5}, {-1, 6}, {2, -1}} {
		if op.global >= 0 {
			a.emit(code.OpGetGlobal, op.global)
		} else {
			a.emit(code.OpConstant, op.constant)
		}
	}
	a.emit(code.OpCall, 1)
	a.emit(code.OpChant)
	// close writer
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpCall, 2)
	a.emit(code.OpChant)
	// reopen for reading and overwrite data with the file contents.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 5)
	a.emit(code.OpCall, 0)
	a.emit(code.OpSetGlobal, 0)
	for _, op := range []struct {
		global   int
		constant int
	}{{0, -1}, {1, -1}, {-1, 5}, {-1, 6}, {2, -1}} {
		if op.global >= 0 {
			a.emit(code.OpGetGlobal, op.global)
		} else {
			a.emit(code.OpConstant, op.constant)
		}
	}
	a.emit(code.OpCall, 3)
	a.emit(code.OpChant)
	for _, indexConstant := range []int{5, 7, 8} {
		a.emit(code.OpGetGlobal, 1)
		a.emit(code.OpConstant, indexConstant)
		a.emit(code.OpIndex)
		a.emit(code.OpChant)
	}
	p := &compiler.Bytecode{
		GlobalsCount: 3,
		Constants: []value.Value{
			value.NewString(path), value.NewInt(1), value.NewUint8('M'), value.NewUint8('i'), value.NewUint8('o'), value.NewInt(0), value.NewInt(3), value.NewInt(1), value.NewInt(2), value.NewUint8(0),
		},
		Instructions: a.build(),
		Functions: []compiler.CompiledFunction{
			m14Stub(0, "open", 2), m14Stub(1, "write", 5), m14Stub(2, "close", 2), m14Stub(3, "read", 5),
		},
	}
	got, err := runM12Native(t, p)
	if err != nil {
		t.Fatalf("native execution failed: %v, output=%q", err, got)
	}
	if want := []byte("\n\n\n77\n105\n111\n"); !bytes.Equal(got, want) {
		t.Fatalf("native output=%q, want=%q", got, want)
	}
}

func TestM14NativeStatSemantics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.mio")
	childDir := filepath.Join(dir, "child")
	link := filepath.Join(dir, "file-link")
	locked := filepath.Join(dir, "locked")
	if err := os.WriteFile(file, []byte("mio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	lockedChild := filepath.Join(locked, "hidden")
	if err := os.WriteFile(lockedChild, []byte("mio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	paths := []string{file, childDir, link, filepath.Join(dir, "missing"), lockedChild, "bad\x00path"}
	constants := make([]value.Value, 0, len(paths)+3)
	for _, path := range paths {
		constants = append(constants, value.NewString(path))
	}
	zeroByte := len(constants)
	constants = append(constants, value.NewUint8(0))
	zeroIndex := len(constants)
	constants = append(constants, value.NewInt(0))
	oneIndex := len(constants)
	constants = append(constants, value.NewInt(1))

	a := newM3TestAsm()
	a.emit(code.OpConstant, zeroByte)
	a.emit(code.OpConstant, zeroByte)
	a.emit(code.OpArrayTyped, 2, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 0)
	for pathIndex := range paths {
		a.emit(code.OpConstant, pathIndex)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpCall, 0)
		a.emit(code.OpChant)
		for _, index := range []int{zeroIndex, oneIndex} {
			a.emit(code.OpGetGlobal, 0)
			a.emit(code.OpConstant, index)
			a.emit(code.OpIndex)
			a.emit(code.OpChant)
		}
	}
	p := &compiler.Bytecode{
		GlobalsCount: 1,
		Constants:    constants,
		Instructions: a.build(),
		Functions:    []compiler.CompiledFunction{m14Stub(0, "stat", 2)},
	}
	got, err := runM12Native(t, p)
	if err != nil {
		t.Fatalf("native execution failed: %v, output=%q", err, got)
	}
	if want := []byte("\n1\n0\n\n2\n0\n\n1\n0\n\n0\n1\n\n0\n2\n\n0\n7\n"); !bytes.Equal(got, want) {
		t.Fatalf("native output=%q, want=%q", got, want)
	}
}

func TestM14NativeMkdirSemantics(t *testing.T) {
	dir := t.TempDir()
	created := filepath.Join(dir, "created")
	existing := filepath.Join(dir, "existing")
	link := filepath.Join(dir, "existing-link")
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(existing, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	paths := []string{created, existing, filepath.Join(dir, "missing", "child"), link, filepath.Join(locked, "denied"), "bad\x00path"}
	constants := make([]value.Value, 0, len(paths)+2)
	for _, path := range paths {
		constants = append(constants, value.NewString(path))
	}
	zeroByte := len(constants)
	constants = append(constants, value.NewUint8(0))
	zeroIndex := len(constants)
	constants = append(constants, value.NewInt(0))

	a := newM3TestAsm()
	a.emit(code.OpConstant, zeroByte)
	a.emit(code.OpArrayTyped, 1, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 0)
	for pathIndex := range paths {
		a.emit(code.OpConstant, pathIndex)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpCall, 0)
		a.emit(code.OpChant)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpConstant, zeroIndex)
		a.emit(code.OpIndex)
		a.emit(code.OpChant)
	}
	p := &compiler.Bytecode{
		GlobalsCount: 1,
		Constants:    constants,
		Instructions: a.build(),
		Functions:    []compiler.CompiledFunction{m14Stub(0, "mkdir", 2)},
	}
	got, err := runM12Native(t, p)
	if err != nil {
		t.Fatalf("native execution failed: %v, output=%q", err, got)
	}
	if want := []byte("\n0\n\n5\n\n1\n\n5\n\n2\n\n7\n"); !bytes.Equal(got, want) {
		t.Fatalf("native output=%q, want=%q", got, want)
	}
	info, err := os.Stat(created)
	if err != nil || !info.IsDir() {
		t.Fatalf("mkdir did not create a directory: info=%v err=%v", info, err)
	}
}
