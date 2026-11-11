package editors

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"Mio/internal/token"
)

// The compiler's lexer is the single source of truth for reserved words.
// Every editor keyword list must equal token.Keywords(token.CurrentVersion);
// this test fails when a keyword is added to the compiler and an editor list is not updated
// (or the other way round).

func compilerKeywords() []string {
	return token.Keywords(token.CurrentVersion)
}

func read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func wordSet(words []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		if w != "" && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Strings(out)
	return out
}

func compareExact(t *testing.T, name string, got []string) {
	t.Helper()
	want := compilerKeywords()
	got = wordSet(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s keyword list differs from the compiler\n  editor:   %v\n  compiler: %v", name, got, want)
	}
}

func TestVimKeywordsMatchCompiler(t *testing.T) {
	re := regexp.MustCompile(`(?m)^syn keyword mio(?:Keyword|Control|Import|Constant)\s+(.+)$`)
	var words []string
	for _, m := range re.FindAllStringSubmatch(read(t, "vim/syntax/mio.vim"), -1) {
		words = append(words, strings.Fields(m[1])...)
	}
	compareExact(t, "vim/syntax/mio.vim", words)
}

func TestEmacsKeywordsMatchCompiler(t *testing.T) {
	text := read(t, "emacs/mio-mode.el")
	var words []string
	for _, m := range regexp.MustCompile(`(?s)regexp-opt '\(([^)]*)\)`).FindAllStringSubmatch(text, -1) {
		if strings.Contains(m[1], `"int64"`) { // the builtin type list
			continue
		}
		for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
			words = append(words, q[1])
		}
	}
	compareExact(t, "emacs/mio-mode.el", words)
}

func TestSublimeKeywordsMatchCompiler(t *testing.T) {
	text := read(t, "sublime/Mio.sublime-syntax")
	re := regexp.MustCompile(`match: '\\b\(([^)]*)\)\\b'\n\s+scope: (keyword|constant\.language)[\w.]*`)
	var words []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		words = append(words, strings.Split(m[1], "|")...)
	}
	compareExact(t, "sublime/Mio.sublime-syntax", words)
}

func TestTreeSitterKeywordsMatchCompiler(t *testing.T) {
	grammar := read(t, "../tools/tree-sitter-mio/grammar.js")
	highlights := read(t, "../tools/tree-sitter-mio/queries/highlights.scm")
	// Literal keywords are highlighted through node captures rather than string tokens.
	literalNodes := map[string]string{"hai": "(boolean_literal)", "iie": "(boolean_literal)", "mu": "(nil_literal)"}
	quoted := regexp.MustCompile(`"([a-z]+)"`)
	inHighlights := map[string]bool{}
	for _, m := range quoted.FindAllStringSubmatch(highlights, -1) {
		inHighlights[m[1]] = true
	}
	for _, kw := range compilerKeywords() {
		if !strings.Contains(grammar, "'"+kw+"'") {
			t.Errorf("tree-sitter grammar.js is missing keyword %q", kw)
		}
		if node, ok := literalNodes[kw]; ok {
			if !strings.Contains(highlights, node) {
				t.Errorf("tree-sitter highlights.scm does not capture %s for %q", node, kw)
			}
		} else if !inHighlights[kw] {
			t.Errorf("tree-sitter highlights.scm is missing keyword %q", kw)
		}
	}
}
