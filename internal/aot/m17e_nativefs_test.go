package aot

import (
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/stdlib"
	"Mio/internal/value"
	"Mio/internal/vm"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestM17ePureMioNativeFSReadWrite(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	for _, data := range [][]byte{{}, {77, 0, 255, 228, 184, 173, 10}} {
		t.Run(fmt.Sprintf("bytes%d", len(data)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "二進制-é.bin")
			p := &compiler.Bytecode{GlobalsCount: 6}
			emit := func(op code.Opcode, args ...int) {
				inst, err := code.Make(op, args...)
				if err != nil {
					t.Fatal(err)
				}
				p.Instructions = append(p.Instructions, inst...)
			}
			constant := func(v value.Value) { p.Constants = append(p.Constants, v); emit(code.OpConstant, len(p.Constants)-1) }
			n := func(v int) { constant(value.NewInt(int64(v))) }
			g := func(i int) { emit(code.OpGetGlobal, i) }
			binding := func(name, hash string, argc int) int {
				id := len(p.Functions)
				p.Functions = append(p.Functions, compiler.CompiledFunction{ID: id, Name: "$mma::mio/fs/runtime::" + name + "::" + hash, ParameterCount: argc, LocalCount: argc, ReturnsValue: true})
				return id
			}
			open := binding("open", "1192df94a5bdf48f4df10a0aa4b61147cfc2e6f2c2f8ea1248826e517f716633", 2)
			write := binding("write", "e8706d18fa932e83ff56c940a8552084a64d75a1d0629364439b5e079ca29430", 5)
			read := binding("read", "e8706d18fa932e83ff56c940a8552084a64d75a1d0629364439b5e079ca29430", 5)
			close := binding("close", "513a084e5a0291e2893cce23d944461f99797716df1ab69f0acf49b3f195be93", 2)
			call := func(id int) { emit(code.OpCall, id); emit(code.OpSetGlobal, 5) }
			show := func(i int) { g(1); n(i); emit(code.OpIndex); emit(code.OpChant) }
			// A rooted, mutable 10-byte status buffer plus a compact source literal.
			n(10)
			constant(value.NewUint8(0))
			emit(code.OpArrayMake, int(value.KindUint8))
			emit(code.OpSetGlobal, 1)
			for _, b := range data {
				constant(value.NewUint8(b))
			}
			emit(code.OpArrayTyped, len(data), int(value.KindUint8))
			emit(code.OpSetGlobal, 2)
			constant(value.NewString(path))
			n(1)
			emit(code.OpCall, open)
			emit(code.OpSetGlobal, 0)
			split := len(data) / 2
			for _, r := range [][2]int{{0, split}, {split, len(data) - split}} {
				g(0)
				g(2)
				n(r[0])
				n(r[1])
				g(1)
				call(write)
				show(0)
				show(9)
			}
			g(0)
			g(1)
			call(close)
			show(0)
			constant(value.NewString(path))
			n(0)
			emit(code.OpCall, open)
			emit(code.OpSetGlobal, 3)
			n(10)
			constant(value.NewUint8(0))
			emit(code.OpArrayMake, int(value.KindUint8))
			emit(code.OpSetGlobal, 4)
			g(3)
			g(4)
			n(0)
			n(10)
			g(1)
			call(read)
			show(0)
			show(8)
			show(9)
			for i := range data {
				g(4)
				n(i)
				emit(code.OpIndex)
				emit(code.OpChant)
			}
			g(3)
			g(4)
			n(0)
			n(10)
			g(1)
			call(read)
			show(0)
			show(8)
			show(9)
			// Invalid range and closed handles must set status rather than touch storage.
			g(3)
			g(4)
			n(-1)
			n(1)
			g(1)
			call(read)
			show(9)
			g(3)
			g(4)
			n(1<<63 - 1)
			n(1<<63 - 1)
			g(1)
			call(read)
			show(9)
			g(3)
			g(1)
			call(close)
			show(0)
			want := m17RunVM(t, p, 100_000)
			checkFile := func() {
				t.Helper()
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("file: %v %v want %v", err, got, data)
				}
			}
			checkFile()
			ref, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
				return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 1 << 20, GCStress: true})
			})
			if err != nil || !bytes.Equal(ref, want) {
				t.Fatalf("Go reference: %v %q VM=%q", err, ref, want)
			}
			checkFile()
			out := m17eArrayImage(t, p)
			got, err := m17eExecute(t, out)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("pure native: %v %q VM=%q", err, got, want)
			}
			checkFile()
		})
	}
}

func TestM17ePureMioNativeFSDirectoryAndPaths(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	dir := t.TempDir()
	name := "日本語-é.bin"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte{0, 255}, 0600); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(t.TempDir(), "作成")
	p := &compiler.Bytecode{GlobalsCount: 5}
	e := func(op code.Opcode, args ...int) {
		inst, err := code.Make(op, args...)
		if err != nil {
			t.Fatal(err)
		}
		p.Instructions = append(p.Instructions, inst...)
	}
	c := func(v value.Value) { p.Constants = append(p.Constants, v); e(code.OpConstant, len(p.Constants)-1) }
	n := func(v int) { c(value.NewInt(int64(v))) }
	g := func(i int) { e(code.OpGetGlobal, i) }
	binding := func(name, hash string, argc int) int {
		id := len(p.Functions)
		p.Functions = append(p.Functions, compiler.CompiledFunction{ID: id, Name: "$mma::mio/fs/runtime::" + name + "::" + hash, ParameterCount: argc, LocalCount: argc, ReturnsValue: true})
		return id
	}
	open := binding("open_dir", "7128dadd824bee38c385b052b0d86ff37b801ae61b35c36db664b2d310b6dbdf", 1)
	next := binding("dir_next", "c9ff387755a05bfa435b60b204111ffc83a5f199b21f569f6673482e3a103ca6", 1)
	getname := binding("dir_name", "ca5c10cb23a1dc4f8d7120a05626b664f700151ff09870e6a2a9204b890efa7c", 1)
	isfile := binding("dir_is_file", "c9ff387755a05bfa435b60b204111ffc83a5f199b21f569f6673482e3a103ca6", 1)
	isdir := binding("dir_is_dir", "c9ff387755a05bfa435b60b204111ffc83a5f199b21f569f6673482e3a103ca6", 1)
	close := binding("dir_close", "513a084e5a0291e2893cce23d944461f99797716df1ab69f0acf49b3f195be93", 2)
	status := binding("dir_status", "513a084e5a0291e2893cce23d944461f99797716df1ab69f0acf49b3f195be93", 2)
	stat := binding("stat", "6f2c95561ed64afe3034886f256d86934e0ad43ef1d605224b7f6d40167a9946", 2)
	mkdir := binding("mkdir", "6f2c95561ed64afe3034886f256d86934e0ad43ef1d605224b7f6d40167a9946", 2)
	call := func(id int) { e(code.OpCall, id); e(code.OpSetGlobal, 4) }
	show := func(i int) { g(1); n(i); e(code.OpIndex); e(code.OpChant) }
	n(2)
	c(value.NewUint8(0))
	e(code.OpArrayMake, int(value.KindUint8))
	e(code.OpSetGlobal, 1)
	c(value.NewString(dir))
	e(code.OpCall, open)
	e(code.OpSetGlobal, 0)
	g(0)
	e(code.OpCall, next)
	e(code.OpChant)
	g(0)
	e(code.OpCall, isfile)
	e(code.OpChant)
	g(0)
	e(code.OpCall, isdir)
	e(code.OpChant)
	g(0)
	e(code.OpCall, getname)
	e(code.OpSetGlobal, 2)
	// Both NativeFS name allocations and every subsequent allocation collect.
	for range 300 {
		n(77)
		e(code.OpStruct, 1)
		e(code.OpSetGlobal, 3)
	}
	g(2)
	e(code.OpChant)
	g(2)
	e(code.OpLen)
	e(code.OpChant)
	g(0)
	e(code.OpCall, next)
	e(code.OpChant)
	g(0)
	g(1)
	call(status)
	show(0)
	g(0)
	g(1)
	call(close)
	show(0)
	for _, s := range []string{path, dir, path + "\x00bad"} {
		c(value.NewString(s))
		g(1)
		call(stat)
		show(0)
		show(1)
	}
	c(value.NewString(created))
	g(1)
	call(mkdir)
	show(0)
	want := m17RunVM(t, p, 100_000)
	if err := os.Remove(created); err != nil {
		t.Fatal(err)
	}
	ref, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 1 << 20, GCStress: true})
	})
	if err != nil || !bytes.Equal(ref, want) {
		t.Fatalf("Go reference: %v %q VM=%q", err, ref, want)
	}
	if err := os.Remove(created); err != nil {
		t.Fatal(err)
	}
	out := m17eArrayImage(t, p)
	got, err := m17eExecute(t, out)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("pure native: %v %q VM=%q", err, got, want)
	}
	if st, err := os.Stat(created); err != nil || !st.IsDir() {
		t.Fatalf("mkdir: %v", err)
	}
}

func TestM17ePureMioNativeFSStaleHandleBoundary(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	path := filepath.Join(t.TempDir(), "generation.bin")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	p := &compiler.Bytecode{GlobalsCount: 4, Constants: []value.Value{value.NewString(path), value.NewInt(0), value.NewUint8(0)}}
	p.Functions = []compiler.CompiledFunction{
		{ID: 0, Name: "$mma::mio/fs/runtime::open::1192df94a5bdf48f4df10a0aa4b61147cfc2e6f2c2f8ea1248826e517f716633", ParameterCount: 2, LocalCount: 2, ReturnsValue: true},
		{ID: 1, Name: "$mma::mio/fs/runtime::close::513a084e5a0291e2893cce23d944461f99797716df1ab69f0acf49b3f195be93", ParameterCount: 2, LocalCount: 2, ReturnsValue: true},
		{ID: 2, Name: "$mma::mio/fs/runtime::file_status::513a084e5a0291e2893cce23d944461f99797716df1ab69f0acf49b3f195be93", ParameterCount: 2, LocalCount: 2, ReturnsValue: true}}
	e := func(op code.Opcode, args ...int) {
		inst, err := code.Make(op, args...)
		if err != nil {
			t.Fatal(err)
		}
		p.Instructions = append(p.Instructions, inst...)
	}
	e(code.OpConstant, 2)
	e(code.OpArrayTyped, 1, int(value.KindUint8))
	e(code.OpSetGlobal, 0)
	e(code.OpConstant, 0)
	e(code.OpConstant, 1)
	e(code.OpCall, 0)
	e(code.OpSetGlobal, 1)
	e(code.OpGetGlobal, 1)
	e(code.OpGetGlobal, 0)
	e(code.OpCall, 1)
	e(code.OpSetGlobal, 3)
	// More than the native 32-slot limit: closing must release each slot.
	for range 40 {
		e(code.OpConstant, 0)
		e(code.OpConstant, 1)
		e(code.OpCall, 0)
		e(code.OpSetGlobal, 2)
		e(code.OpGetGlobal, 2)
		e(code.OpGetGlobal, 0)
		e(code.OpCall, 1)
		e(code.OpSetGlobal, 3)
	}
	e(code.OpConstant, 0)
	e(code.OpConstant, 1)
	e(code.OpCall, 0)
	e(code.OpSetGlobal, 2)
	e(code.OpGetGlobal, 2)
	e(code.OpGetGlobal, 0)
	e(code.OpCall, 2)
	e(code.OpSetGlobal, 3)
	e(code.OpGetGlobal, 0)
	e(code.OpConstant, 1)
	e(code.OpIndex)
	e(code.OpChant)
	e(code.OpGetGlobal, 1)
	e(code.OpGetGlobal, 0)
	e(code.OpCall, 2)
	e(code.OpSetGlobal, 3)
	e(code.OpGetGlobal, 0)
	e(code.OpConstant, 1)
	e(code.OpIndex)
	e(code.OpChant)
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var vmOutput bytes.Buffer
	machine := vm.NewWithRegistryAndOutput(p, registry, &vmOutput)
	if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: 1000}); err == nil || !strings.Contains(err.Error(), "closed_resource") {
		t.Fatalf("VM stale-resource boundary: %v", err)
	}
	if vmOutput.String() != "0\n" {
		t.Fatalf("VM generation equality: %q", vmOutput.String())
	}
	ref, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 1 << 20, GCStress: true})
	})
	if err != nil || !bytes.Equal(ref, []byte("0\n4\n")) {
		t.Fatalf("Go stale reference: %v %q", err, ref)
	}
	out := m17eArrayImage(t, p)
	got, err := m17eExecute(t, out)
	if err != nil || !bytes.Equal(got, ref) {
		t.Fatalf("pure stale handle: %v %q Go=%q", err, got, ref)
	}
}
