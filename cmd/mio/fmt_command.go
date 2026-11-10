package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"Mio/internal/format"
)

const fmtUsage = `Usage: mio fmt [--check] <path>...
       mio fmt --stdin [--check]

Format Mio source files. Paths may be files or directories (searched
recursively for *.mio; hidden, node_modules, vendor, build, dist, out and
target directories and symbolic links are skipped).

Options:
  --check    Do not write files; list files that need formatting and exit 1.
  --stdin    Read source from standard input and write the result to standard
             output (with --check: print nothing, exit 1 when formatting changes it).
  -h, --help Show this help.

Files that do not parse are never modified; they are reported on standard
error and the exit status is 2.

Exit status: 0 success, 1 --check found files to format, 2 usage or I/O error
or an unformattable file.
`

type fmtOptions struct {
	check bool
	stdin bool
	paths []string
}

func parseFmtArgs(args []string) (fmtOptions, bool) {
	var options fmtOptions
	rest := false
	for _, argument := range args {
		if !rest {
			switch argument {
			case "--":
				rest = true
				continue
			case "--check":
				options.check = true
				continue
			case "--stdin":
				options.stdin = true
				continue
			}
			if strings.HasPrefix(argument, "-") && argument != "-" {
				return options, false
			}
		}
		options.paths = append(options.paths, argument)
	}
	if options.stdin {
		return options, len(options.paths) == 0
	}
	return options, len(options.paths) > 0
}

// runFmt implements `mio fmt`.
func runFmt(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	for _, argument := range args {
		if argument == "-h" || argument == "--help" {
			fmt.Fprint(stdout, fmtUsage)
			return 0
		}
	}
	options, ok := parseFmtArgs(args)
	if !ok {
		fmt.Fprint(stderr, fmtUsage)
		return 2
	}
	if options.stdin {
		source, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "<stdin>: %v\n", err)
			return 2
		}
		formatted, err := format.Format(source)
		if err != nil {
			fmt.Fprintf(stderr, "<stdin>: %v\n", err)
			return 2
		}
		if options.check {
			if !bytes.Equal(source, formatted) {
				return 1
			}
			return 0
		}
		if _, err := stdout.Write(formatted); err != nil {
			return 2
		}
		return 0
	}

	files, failed := collectMioFiles(options.paths, stderr)
	status := 0
	if failed {
		status = 2
	}
	changed := 0
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			status = 2
			continue
		}
		formatted, err := format.Format(source)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			status = 2
			continue
		}
		if bytes.Equal(source, formatted) {
			continue
		}
		changed++
		fmt.Fprintln(stdout, path)
		if options.check {
			continue
		}
		if err := writeFileAtomically(path, formatted); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			status = 2
		}
	}
	if options.check && changed > 0 && status == 0 {
		status = 1
	}
	return status
}

func skippedFmtDirectory(name string) bool {
	if strings.HasPrefix(name, ".") && name != "." && name != ".." {
		return true
	}
	switch name {
	case "node_modules", "vendor", "build", "dist", "out", "target":
		return true
	}
	return false
}

// collectMioFiles expands files and directories into a sorted, de-duplicated list.
func collectMioFiles(paths []string, stderr io.Writer) ([]string, bool) {
	seen := make(map[string]struct{})
	var files []string
	failed := false
	add := func(path string) {
		clean := filepath.Clean(path)
		if _, dup := seen[clean]; !dup {
			seen[clean] = struct{}{}
			files = append(files, clean)
		}
	}
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", root, err)
			failed = true
			continue
		}
		if !info.IsDir() {
			add(root)
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				fmt.Fprintf(stderr, "%s: %v\n", path, walkErr)
				failed = true
				return nil
			}
			if entry.IsDir() {
				if path != root && skippedFmtDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".mio") {
				add(path)
			}
			return nil
		})
	}
	sort.Strings(files)
	return files, failed
}

// writeFileAtomically replaces path through a temporary file in the same
// directory so an interrupted run never leaves a truncated source file.
func writeFileAtomically(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".mio-fmt-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, target)
}
