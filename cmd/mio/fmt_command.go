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

	"Mio/internal/bunko"
	"Mio/internal/format"
	"Mio/internal/i18n"
)

func printFmtHelp(out io.Writer, language i18n.Language) {
	fmt.Fprintf(out, "%s: mio fmt [--check] [--diff] [--no-organize-imports] <path>...\n", i18n.Text(language, i18n.KeyHelpUsage))
	fmt.Fprintln(out, "       mio fmt --stdin [--stdin-filepath <path>] [--check] [--diff]")
	fmt.Fprintln(out, i18n.Text(language, i18n.KeyCmdFmt))
	fmt.Fprintln(out, i18n.Text(language, i18n.KeyFmtBehavior))
	fmt.Fprintln(out, i18n.Text(language, i18n.KeyFmtStatus))
}

type fmtOptions struct {
	check          bool
	diff           bool
	stdin          bool
	filename       string
	help           bool
	whitespaceOnly bool
	bunkoRoot      string
	paths          []string
}

func parseFmtArgs(args []string) (fmtOptions, bool) {
	var options fmtOptions
	for i := 0; i < len(args); i++ {
		switch argument := args[i]; argument {
		case "--":
			options.paths = append(options.paths, args[i+1:]...)
			i = len(args)
		case "--help", "-h":
			options.help = true
			return options, true
		case "--check":
			options.check = true
		case "--diff":
			options.diff = true
		case "--no-organize-imports":
			options.whitespaceOnly = true
		case "--bunko-root":
			i++
			if i == len(args) || args[i] == "" {
				return options, false
			}
			options.bunkoRoot = args[i]
		case "--stdin":
			options.stdin = true
		case "--stdin-filepath":
			i++
			if i == len(args) || args[i] == "" {
				return options, false
			}
			options.filename = args[i]
		default:
			if strings.HasPrefix(argument, "-") {
				return options, false
			}
			options.paths = append(options.paths, argument)
		}
	}
	if options.stdin {
		return options, len(options.paths) == 0
	}
	return options, len(options.paths) > 0 && options.filename == ""
}

// runFmt implements `mio fmt`.
func runFmt(args []string, stdin io.Reader, stdout, stderr io.Writer, language i18n.Language) int {
	options, ok := parseFmtArgs(args)
	if options.help {
		printFmtHelp(stdout, language)
		return 0
	}
	if !ok {
		printFmtHelp(stderr, language)
		return 2
	}
	project, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "mio fmt: %v\n", err)
		return 2
	}
	catalogs := make(map[string]*bunko.ImportOrganizer)
	formatSource := func(source []byte, name string) ([]byte, error) {
		formatted, err := format.Format(source)
		if err != nil || options.whitespaceOnly {
			return formatted, err
		}
		location := name
		if location == "" {
			location = filepath.Join(project, ".mio-fmt-stdin.mio")
		} else if abs, err := filepath.Abs(name); err == nil {
			name, location = abs, abs
		}
		root := options.bunkoRoot
		if root == "" {
			root = bunkoRoot(location)
		}
		if catalogs[root] == nil {
			catalogs[root] = bunko.NewImportOrganizer(root, project)
		}
		organized := catalogs[root].Organize(source, name)
		if bytes.Equal(organized, source) {
			return formatted, nil
		}
		return format.Format(organized)
	}
	if options.stdin {
		name := options.filename
		if name == "" {
			name = "<stdin>"
		}
		source, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return 2
		}
		formatted, err := formatSource(source, options.filename)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return 2
		}
		changed := !bytes.Equal(source, formatted)
		if options.diff && changed {
			if err := writeFmtDiff(stdout, name, source, formatted); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", name, err)
				return 2
			}
		}
		if options.check && changed {
			return 1
		}
		if !options.check && !options.diff {
			if _, err := stdout.Write(formatted); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", name, err)
				return 2
			}
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
		formatted, err := formatSource(source, path)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			status = 2
			continue
		}
		if bytes.Equal(source, formatted) {
			continue
		}
		changed++
		if options.diff {
			if err := writeFmtDiff(stdout, path, source, formatted); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				status = 2
			}
			continue
		}
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

// writeFmtDiff emits one unified hunk covering the changed file.
func writeFmtDiff(out io.Writer, name string, source, formatted []byte) error {
	rangeOf := func(data []byte) string {
		if len(data) == 0 {
			return "0,0"
		}
		lines := bytes.Count(data, []byte("\n"))
		if data[len(data)-1] != '\n' {
			lines++
		}
		return fmt.Sprintf("1,%d", lines)
	}
	var patch bytes.Buffer
	fmt.Fprintf(&patch, "--- %s\n+++ %s\n@@ -%s +%s @@\n", name, name, rangeOf(source), rangeOf(formatted))
	for _, side := range []struct {
		prefix byte
		data   []byte
	}{{'-', source}, {'+', formatted}} {
		for len(side.data) > 0 {
			end := bytes.IndexByte(side.data, '\n') + 1
			if end == 0 {
				end = len(side.data)
			}
			line := side.data[:end]
			side.data = side.data[end:]
			patch.WriteByte(side.prefix)
			patch.Write(line)
			if line[len(line)-1] != '\n' {
				patch.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	_, err := out.Write(patch.Bytes())
	return err
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
