package token

import "testing"

func TestLookupIdent(t *testing.T) {
	tests := []struct {
		literal string
		version LanguageVersion
		want    Type
	}{
		{"kaku", V01, Let},
		{"waza", V01, Function},
		{"aida", V01, Ident},
		{"aida", V02, While},
		{"nukeru", V01, Ident},
		{"nukeru", V02, Break},
		{"tsuzuku", V01, Ident},
		{"tsuzuku", V02, Continue},
		{"yobu", V02, Ident},
		{"yobu", V03, Import},
		{"chant", V01, Ident},
	}

	for _, tt := range tests {
		got := LookupIdent(tt.literal, tt.version)

		if got != tt.want {
			t.Errorf(
				"LookupIdent(%q) = %q, want %q",
				tt.literal,
				got,
				tt.want,
			)
		}
	}
}

// 测试 v0.5 包导出和常量关键字
func TestLookupPackageKeywords(t *testing.T) {
	for word, want := range map[string]Type{"koukai": Export, "teisu": Const} {
		if got := LookupIdent(word, V04); got != Ident {
			t.Fatalf("v0.4 %q=%s", word, got)
		}
		if got := LookupIdent(word, V05); got != want {
			t.Fatalf("v0.5 %q=%s want=%s", word, got, want)
		}
	}
}

// 测试 v0.4 核心类型关键字
func TestLookupCoreTypeKeywords(t *testing.T) {
	tests := map[string]Type{"kata": Struct, "sentaku": Enum, "awase": Match, "baai": Case}
	for word, want := range tests {
		if got := LookupIdent(word, V03); got != Ident {
			t.Fatalf("v0.3 %q=%s", word, got)
		}
		if got := LookupIdent(word, V04); got != want {
			t.Fatalf("v0.4 %q=%s want=%s", word, got, want)
		}
	}
}
