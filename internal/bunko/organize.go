package bunko

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"Mio/internal/ast"
)

// ImportOrganizer keeps the package catalog and parsed export tables for a
// formatting invocation. Package contents are loaded only for queried aliases.
type ImportOrganizer struct {
	loader   *Loader
	project  string
	catalog  map[string][]string
	packages map[string]*ast.Program
}

func NewImportOrganizer(root, project string) *ImportOrganizer {
	return &ImportOrganizer{loader: New(root), project: project, packages: make(map[string]*ast.Program)}
}

func (o *ImportOrganizer) discover() {
	if o.catalog != nil {
		return
	}
	o.catalog = make(map[string][]string)
	seen := make(map[string]bool)
	for _, folder := range []struct{ physical, prefix string }{
		{filepath.Join(o.loader.root, "kiso"), "kiso"},
		{o.project, ""},
	} {
		_ = filepath.WalkDir(folder.physical, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if entry.IsDir() && current != folder.physical {
				switch entry.Name() {
				case "bunko", "mio", "build", "vendor", "node_modules", "out", "dist", "target":
					return filepath.SkipDir
				}
				if strings.HasPrefix(entry.Name(), ".") || (folder.prefix == "" && entry.Name() == "kiso" && filepath.Dir(current) == folder.physical) {
					return filepath.SkipDir
				}
			}
			if !entry.Type().IsRegular() || !isRegularSource(entry.Name()) {
				return nil
			}
			rel, err := filepath.Rel(folder.physical, filepath.Dir(current))
			if err != nil {
				return nil
			}
			name := path.Join(folder.prefix, filepath.ToSlash(rel))
			if name != "." && !seen[name] && validOrganizerPath(name) {
				seen[name] = true
				alias := path.Base(name)
				o.catalog[alias] = append(o.catalog[alias], name)
			}
			return nil
		})
	}
}

func validOrganizerPath(name string) bool {
	return validateImportPath(name) == nil && !strings.ContainsFunc(name, func(r rune) bool {
		return r < 32 || r == '"' || r == '\\'
	})
}

func (o *ImportOrganizer) packageAST(name string) *ast.Program {
	if program, ok := o.packages[name]; ok {
		return program
	}
	var program *ast.Program
	if pkg, err := o.loader.Load(name); err == nil {
		program, _, _ = parseSources(pkg.Sources)
	}
	o.packages[name] = program
	return program
}

func (o *ImportOrganizer) declarationsOnly(name string, visited map[string]bool) bool {
	if visited[name] {
		return true
	}
	program := o.packageAST(name)
	if program == nil {
		return false
	}
	visited[name] = true
	for _, statement := range program.Statements {
		if imp, ok := statement.(*ast.ImportStatement); ok {
			if !strings.HasPrefix(imp.Path, "mio/") && !o.declarationsOnly(imp.Path, visited) {
				return false
			}
		} else if _, _, ok := declarationInfo(statement); !ok {
			return false
		}
	}
	return true
}

func (o *ImportOrganizer) candidates(alias string, members map[string]bool, filename string) []string {
	o.discover()
	var matches []string
	for _, name := range o.catalog[alias] {
		program := o.packageAST(name)
		if program == nil || !o.declarationsOnly(name, map[string]bool{}) {
			continue
		}
		if filename != "" {
			root := o.project
			if strings.HasPrefix(name, "kiso/") {
				root = o.loader.root
			}
			if filepath.Clean(filepath.Join(root, filepath.FromSlash(name))) == filepath.Dir(filename) {
				continue
			}
		}
		table := exports(program, name)
		valid := true
		for member := range members {
			if _, ok := table[member]; !ok {
				valid = false
				break
			}
		}
		if valid {
			matches = append(matches, name)
		}
	}
	return matches
}

func sourceScope(program *ast.Program) map[string]string {
	names := declarations(program, "")
	for _, statement := range program.Statements {
		if let, ok := statement.(*ast.LetStatement); ok {
			names[let.Name.Name] = let.Name.Name
		}
	}
	return names
}

// Organize uses the package binder's scope walk and export table. A missing
// selector receives an import only if all its member uses agree on one package.
func (o *ImportOrganizer) Organize(source []byte, filename string) []byte {
	if bytes.HasPrefix(source, []byte("\xef\xbb\xbf")) {
		return source
	}
	entry, _, err := parseSources([]Source{{Name: "main.mio", Path: filename, Content: source}})
	if err != nil {
		return source
	}
	unit, statements := entry, entry.Statements
	complete := true
	if filename != "" {
		var sources []Source
		dir := filepath.Dir(filename)
		entries, err := os.ReadDir(dir)
		if err != nil {
			complete = false
		}
		for _, file := range entries {
			filePath := filepath.Join(dir, file.Name())
			if !file.Type().IsRegular() || !isRegularSource(file.Name()) || filePath == filename {
				continue
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				complete = false
				continue
			}
			sources = append(sources, Source{Name: file.Name(), Path: filePath, Content: data})
		}
		if len(sources) > 0 {
			siblings, _, err := parseSources(sources)
			if err != nil {
				complete = false
			} else {
				statements = append(append([]ast.Statement(nil), statements...), siblings.Statements...)
				unit = &ast.Program{Statements: statements}
			}
		}
	}
	imported := make(map[string]bool)
	for _, statement := range statements {
		if imp, ok := statement.(*ast.ImportStatement); ok {
			imported[imp.Alias] = true
		}
	}
	used := make(map[string]bool)
	missing := make(map[string]map[string]bool)
	inEntry := false
	r := rewriter{top: sourceScope(unit), observeQualifier: func(alias, member string) {
		if imported[alias] {
			used[alias] = true
		} else if inEntry {
			if missing[alias] == nil {
				missing[alias] = make(map[string]bool)
			}
			missing[alias][member] = true
		}
	}}
	for i, statement := range statements {
		inEntry = i < len(entry.Statements)
		_ = r.statement(statement, make(map[string]bool))
	}
	add := make(map[string]bool)
	for alias, members := range missing {
		matches := o.candidates(alias, members, filename)
		if len(matches) == 1 {
			add[matches[0]] = true
		}
	}

	start, end := 0, 0
	keep := make(map[string]bool)
	for i, statement := range entry.Statements {
		imp, ok := statement.(*ast.ImportStatement)
		if !ok {
			break
		}
		span := imp.Span()
		first, last := span.Start.Offset, span.End.Offset
		lineStart := bytes.LastIndexByte(source[:first], '\n') + 1
		lineEnd := len(source)
		if newline := bytes.IndexByte(source[last:], '\n'); newline >= 0 {
			lineEnd = last + newline + 1
		}
		raw := source[first:last]
		if (i > 0 && (lineStart < end || len(bytes.TrimSpace(source[end:lineStart])) > 0)) ||
			len(bytes.TrimSpace(source[lineStart:first])) > 0 || len(bytes.TrimSpace(source[last:lineEnd])) > 0 ||
			bytes.ContainsAny(raw, "\n\r") || bytes.Contains(raw, []byte("/*")) || bytes.Contains(raw, []byte("//")) ||
			!validOrganizerPath(imp.Path) || !bytes.Contains(raw, []byte("\""+imp.Path+"\"")) {
			start, end, keep = 0, 0, nil
			break
		}
		if i == 0 {
			start = lineStart
		}
		end = lineEnd
		if !complete || used[imp.Alias] || !o.declarationsOnly(imp.Path, make(map[string]bool)) {
			keep[imp.Path] = true
		}
	}
	for name := range keep {
		add[name] = true
	}
	if len(add) == 0 && start == end {
		return source
	}
	names := make([]string, 0, len(add))
	for name := range add {
		names = append(names, name)
	}
	sort.Strings(names)
	newline := "\n"
	if first := bytes.IndexByte(source, '\n'); first > 0 && source[first-1] == '\r' {
		newline = "\r\n"
	}
	var result bytes.Buffer
	result.Write(source[:start])
	for _, name := range names {
		result.WriteString("yobu \"" + name + "\";" + newline)
	}
	if start == end && len(names) > 0 {
		result.WriteString(newline)
	}
	result.Write(source[end:])
	return result.Bytes()
}
