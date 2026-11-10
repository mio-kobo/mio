package format

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGolden(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"spacing and indentation",
			"waza add(a:int64,b:int64)->int64{kaku s=a+b;kaesu s;}\n",
			"waza add(a: int64, b: int64) -> int64 { kaku s = a + b; kaesu s; }\n",
		},
		{
			"multi-line body",
			"waza f() -> int64 {\nkaku x=1;\n  moshi (x==1) {\nkaesu 2;\n      }\nkaesu x;\n}\n",
			"waza f() -> int64 {\n    kaku x = 1;\n    moshi (x == 1) {\n        kaesu 2;\n    }\n    kaesu x;\n}\n",
		},
		{
			"comments are preserved and re-indented",
			"// header\n\n\n\nwaza f() -> unit {\n// inner\nkaku x = 1;   // trailing\n/* block */\n}\n",
			"// header\n\nwaza f() -> unit {\n    // inner\n    kaku x = 1;   // trailing\n    /* block */\n}\n",
		},
		{
			"unicode strings and comments are untouched",
			"// 日本語のコメント\nkaku s = \"こんにちは  世界\";\nkaku r = 'あ';\n",
			"// 日本語のコメント\nkaku s = \"こんにちは  世界\";\nkaku r = 'あ';\n",
		},
		{
			"unary minus and generics keep their meaning",
			"kata Pair<T> { first: T; second: T; }\nkaku a = -1;\nkaku b = 3-1;\nkaku c: []int64 = [];\nkaku d = first[int64]([42,99]);\n",
			"kata Pair<T> { first: T; second: T; }\nkaku a = -1;\nkaku b = 3 - 1;\nkaku c: []int64 = [];\nkaku d = first[int64]([42, 99]);\n",
		},
		{
			"missing final newline and trailing blank lines",
			"kaku x = 1;",
			"kaku x = 1;\n",
		},
		{
			"CRLF is kept",
			"kaku x=1;\r\nkaku y=2;\r\n",
			"kaku x = 1;\r\nkaku y = 2;\r\n",
		},
		{
			"BOM is kept",
			"\xef\xbb\xbfkaku x=1;\n",
			"\xef\xbb\xbfkaku x = 1;\n",
		},
		{"empty file", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Format([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tc.want)
			}
			again, err := Format(got)
			if err != nil || string(again) != string(got) {
				t.Fatalf("not idempotent: %q (%v)", again, err)
			}
		})
	}
}

func TestRefusesInvalidSource(t *testing.T) {
	for name, src := range map[string]string{
		"syntax error":      "kaku x = ;\n",
		"unterminated":      "kaku s = \"abc;\n",
		"invalid utf8":      "kaku s = \"\xff\";\n",
		"block comment":     "/* never closed\nkaku x = 1;\n",
		"unbalanced brace":  "waza f() -> unit {\n",
		"bare carriage ret": "kaku x = 1;\rkaku y = 2;\n",
	} {
		if out, err := Format([]byte(src)); err == nil {
			t.Errorf("%s: expected an error, got %q", name, out)
		}
	}
}

// Every Mio source shipped in this repository must either be refused because
// it is intentionally invalid, or format idempotently with identical tokens and
// comments (verified inside Format).
func TestRepositorySources(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	formatted, refused := 0, 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".mio") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		out, formatErr := Format(src)
		if formatErr != nil {
			if strings.Contains(formatErr.Error(), "internal formatter error") {
				t.Errorf("%s: %v", path, formatErr)
			}
			refused++
			return nil
		}
		formatted++
		if again, _ := Format(out); string(again) != string(out) {
			t.Errorf("%s: not idempotent", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if formatted < 100 {
		t.Fatalf("only %d sources formatted (refused %d); sweep looks broken", formatted, refused)
	}
	t.Logf("formatted=%d refused(invalid)=%d", formatted, refused)
}
