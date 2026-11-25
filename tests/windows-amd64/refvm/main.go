// Development-only reference host (Go): compiles a .mio file with the Go frontend and runs it in the Go VM with a
// large instruction budget, or writes its Mochi. It exists because `mio run` has a fixed 10M-instruction limit and the
// pure-Mio lowering needs more. It is an oracle/host, never part of the compile path of the Mio toolchain.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"Mio/internal/bunko"
	"Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/diagnostic"
	"Mio/internal/mochi"
	"Mio/internal/resolver"
	"Mio/internal/stdlib"
	"Mio/internal/typecheck"
	"Mio/internal/vm"
)

func compile(path, bunkoRoot string) (*compiler.Bytecode, error) {
	registry, err := stdlib.NewRegistry()
	if err != nil {
		return nil, err
	}
	loaded, err := bunko.LoadProgramWithRegistry(path, bunkoRoot, registry)
	if err != nil {
		return nil, err
	}
	if err := constant.Fold(loaded.AST); err != nil {
		return nil, err
	}
	r := resolver.New()
	if err := r.ResolveProgram(loaded.AST); err != nil {
		return nil, err
	}
	if err := typecheck.NewWithResolver(r).CheckProgram(loaded.AST); err != nil {
		return nil, err
	}
	c := compiler.NewWithResolver(r)
	if err := c.Compile(loaded.AST); err != nil {
		return nil, err
	}
	return c.Bytecode(), nil
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: refvm <bunko-root> run <file.mio> | mochi <file.mio> <out.mochi> | check <file.mio>")
		os.Exit(2)
	}
	root, command, path := os.Args[1], os.Args[2], os.Args[3]
	program, err := compile(path, root)
	if err != nil {
		var diag *diagnostic.Diagnostic
		if command == "check" && errors.As(err, &diag) {
			if encodeErr := json.NewEncoder(os.Stderr).Encode(diag); encodeErr == nil {
				os.Exit(1)
			}
		}
		fmt.Fprintln(os.Stderr, "refvm:", err)
		os.Exit(1)
	}
	switch command {
	case "check":
		return
	case "mochi":
		var encoded bytes.Buffer
		if err := mochi.Validate(program); err != nil {
			fmt.Fprintln(os.Stderr, "refvm: validate:", err)
			os.Exit(1)
		}
		if err := mochi.Encode(&encoded, program); err != nil {
			fmt.Fprintln(os.Stderr, "refvm: encode:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(os.Args[4], encoded.Bytes(), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "refvm:", err)
			os.Exit(1)
		}
	case "run":
		registry, err := stdlib.NewRegistry()
		if err != nil {
			fmt.Fprintln(os.Stderr, "refvm:", err)
			os.Exit(1)
		}
		limit := uint64(8_000_000_000)
		if v := os.Getenv("REFVM_LIMIT"); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				limit = n
			}
		}
		machine := vm.NewWithRegistryAndOutput(program, registry, os.Stdout)
		if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: limit}); err != nil {
			fmt.Fprintln(os.Stderr, "refvm: runtime:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "refvm: unknown command", command)
		os.Exit(2)
	}
}
