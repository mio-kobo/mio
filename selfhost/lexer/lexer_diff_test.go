package lexer_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	miolang "Mio"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

type wireToken struct {
	kind       int
	literal    string
	raw        string
	start, end token.Position
}

type wireError struct {
	kind     int
	position token.Position
}

type wireResult struct {
	tokens []wireToken
	errors []wireError
}

var tokenCodes = map[token.Type]int{
	token.Illegal: 0, token.EOF: 1, token.Ident: 2, token.Integer: 3, token.Float: 4,
	token.String: 5, token.Rune: 6, token.Assign: 7, token.Plus: 8, token.Minus: 9,
	token.Asterisk: 10, token.Slash: 11, token.Bang: 12, token.Question: 13,
	token.Equal: 14, token.NotEqual: 15, token.Less: 16, token.LessEqual: 17,
	token.Greater: 18, token.GreaterEqual: 19, token.And: 20, token.Or: 21,
	token.Comma: 22, token.Semicolon: 23, token.LParen: 24, token.RParen: 25,
	token.LBrace: 26, token.RBrace: 27, token.LBracket: 28, token.RBracket: 29,
	token.Dot: 30, token.Let: 31, token.Function: 32, token.If: 33, token.Else: 34,
	token.Return: 35, token.True: 36, token.False: 37, token.Nil: 38, token.While: 39,
	token.Break: 40, token.Continue: 41, token.Struct: 42, token.Enum: 43,
	token.Match: 44, token.Case: 45, token.Import: 46, token.Export: 47,
	token.Const: 48, token.Colon: 49, token.Arrow: 50,
}

var errorCodes = map[lexer.ErrorKind]int{
	lexer.ErrInvalidUTF8: 0, lexer.ErrInvalidNumber: 1, lexer.ErrInvalidEscape: 2,
	lexer.ErrInvalidRune: 3, lexer.ErrUnterminatedRune: 4, lexer.ErrUnterminatedString: 5,
	lexer.ErrUnterminatedBlockComment: 6, lexer.ErrUnexpectedCharacter: 7,
}

func repositoryRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func referenceResult(source []byte) wireResult {
	checked, err := lexer.NewChecked(string(source), token.V05)
	if err != nil {
		lexical := err.(*lexer.LexicalError)
		return wireResult{tokens: []wireToken{}, errors: []wireError{{kind: errorCodes[lexical.Kind], position: lexical.Position}}}
	}
	result := wireResult{tokens: []wireToken{}, errors: []wireError{}}
	for steps := 0; steps <= len(source)+1; steps++ {
		current := checked.NextToken()
		result.tokens = append(result.tokens, wireToken{
			kind: tokenCodes[current.Type], literal: current.Literal,
			raw: string(source[current.Start.Offset:current.End.Offset]), start: current.Start, end: current.End,
		})
		if current.Type == token.EOF {
			break
		}
	}
	if err := checked.Err(); err != nil {
		lexical := err.(*lexer.LexicalError)
		result.errors = append(result.errors, wireError{kind: errorCodes[lexical.Kind], position: lexical.Position})
	}
	return result
}

func harnessSource(paths []string) string {
	var source strings.Builder
	source.WriteString(`
yobu "kiso/fs";
yobu "kiso/string";

waza emit_bytes(data: []uint8) -> unit {
    chant(len(data));
    kaku index = 0;
    aida (index < len(data)) { chant(data[index]); index = index + 1; }
}

waza emit(result: LexResult) -> unit {
    chant(len(result.tokens));
    chant(len(result.errors));
    kaku index = 0;
    aida (index < len(result.tokens)) {
        kaku token = result.tokens[index];
        chant(token_kind_code(token.kind));
        emit_bytes(string.to_bytes(token.literal));
        emit_bytes(token.raw);
        chant(token.start.offset); chant(token.start.line); chant(token.start.column);
        chant(token.end.offset); chant(token.end.line); chant(token.end.column);
        index = index + 1;
    }
    index = 0;
    aida (index < len(result.errors)) {
        kaku failure = result.errors[index];
        chant(error_kind_code(failure.kind));
        chant(failure.position.offset); chant(failure.position.line); chant(failure.position.column);
        index = index + 1;
    }
}

waza emit_path(path: string) -> unit {
    kaku loaded = fs.read_all(path);
    emit(lex(loaded.data));
}
`)
	for _, path := range paths {
		fmt.Fprintf(&source, "emit_path(%s);\n", strconv.Quote(path))
	}
	return source.String()
}

func compileMioProgram(t testing.TB, harness string) (*miolang.Environment, *miolang.Program) {
	t.Helper()
	root := repositoryRoot(t)
	packageDirectory := t.TempDir()
	for _, name := range []string{"position.mio", "token.mio", "lexer.mio"} {
		content, err := os.ReadFile(filepath.Join(root, "selfhost", "lexer", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packageDirectory, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(packageDirectory, "zz_harness.mio"), []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = environment.Close() })
	program, err := environment.CompileFile(packageDirectory, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	return environment, program
}

func runMioLexer(t testing.TB, paths []string) []wireResult {
	t.Helper()
	environment, program := compileMioProgram(t, harnessSource(paths))
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 1_000_000_000}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	results := make([]wireResult, 0, len(paths))
	readInt := func() int {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("读取 Mio Lexer 输出: %v", readErr)
		}
		value, parseErr := strconv.Atoi(strings.TrimSpace(line))
		if parseErr != nil {
			t.Fatalf("解析 Mio Lexer 输出 %q: %v", line, parseErr)
		}
		return value
	}
	readBytes := func() []byte {
		length := readInt()
		data := make([]byte, length)
		for index := range data {
			data[index] = byte(readInt())
		}
		return data
	}
	for range paths {
		tokenCount := readInt()
		errorCount := readInt()
		result := wireResult{tokens: make([]wireToken, tokenCount), errors: make([]wireError, errorCount)}
		for index := range result.tokens {
			result.tokens[index] = wireToken{
				kind: readInt(), literal: string(readBytes()), raw: string(readBytes()),
				start: token.Position{Offset: readInt(), Line: readInt(), Column: readInt()},
				end:   token.Position{Offset: readInt(), Line: readInt(), Column: readInt()},
			}
		}
		for index := range result.errors {
			result.errors[index] = wireError{
				kind: readInt(), position: token.Position{Offset: readInt(), Line: readInt(), Column: readInt()},
			}
		}
		results = append(results, result)
	}
	return results
}

func writeCorpus(t testing.TB, corpus [][]byte) []string {
	t.Helper()
	directory := t.TempDir()
	paths := make([]string, len(corpus))
	for index, source := range corpus {
		paths[index] = filepath.Join(directory, fmt.Sprintf("case-%03d.bin", index))
		if err := os.WriteFile(paths[index], source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func compareCorpus(t *testing.T, corpus [][]byte) (int, int) {
	t.Helper()
	actual := runMioLexer(t, writeCorpus(t, corpus))
	valid := 0
	invalid := 0
	for index, source := range corpus {
		expected := referenceResult(source)
		if len(expected.errors) == 0 {
			valid++
		} else {
			invalid++
		}
		if fmt.Sprintf("%#v", actual[index]) != fmt.Sprintf("%#v", expected) {
			t.Fatalf("语料 %d 不一致\nsource=%q\nMio=%#v\nGo=%#v", index, source, actual[index], expected)
		}
	}
	return valid, invalid
}

// 测试有效与非法词法语料的逐字段差分
func TestMioLexerDifferentialCorpus(t *testing.T) {
	corpus := [][]byte{
		{}, []byte(" \t\r\n"),
		[]byte("kaku waza moshi sore kaesu hai iie mu aida nukeru tsuzuku kata sentaku awase baai yobu koukai teisu"),
		[]byte("+-*/ = == ! != ? < <= > >= && || & | , ; : . -> () {} []"),
		[]byte("0 1 42 3.14 1e3 2.5e-4 01 1. 1e 1e+ 1.2.3 12abc"),
		[]byte("\"ASCII\" \"中文 日本語 🍡\" \"a\\n\\r\\t\\\"\\\\b\" \"bad\\q\""),
		[]byte("'A' '你' '🍡' '\\n' '\\t' '\\\\' '\\'' '\\x41' '\\u4F60' '\\U0001F361'"),
		[]byte(`'\"' '\a' '\b' '\f' '\v' '\101' '\377' '	'`),
		[]byte("'' 'AB' '🇯🇵' '\\uD800' '\\U00110000' '\\q' 'A"),
		[]byte("// Unicode 注释 🍡\r\nkaku\tvalue; /* 块 */ hai"),
		[]byte("/* 未闭合 🍡"), []byte("@\x00#"), []byte("\"unterminated"),
		{65, 10, 0xe4, 0xbd, 0xa0, 0xff}, {0xf0, 0x9f, 0x8d},
	}
	valid, invalid := compareCorpus(t, corpus)
	t.Logf("定向语料：有效 %d，非法 %d", valid, invalid)
}

// 测试固定种子的随机词法输入
func TestMioLexerDifferentialRandom(t *testing.T) {
	random := rand.New(rand.NewSource(20261016))
	alphabet := []byte("abcXYZ_019 .eE+-*/=!<>?&|,;:(){}[]'\"\\\n\r\t")
	corpus := make([][]byte, 64)
	for index := range corpus {
		length := random.Intn(96)
		corpus[index] = make([]byte, length)
		for offset := range corpus[index] {
			corpus[index][offset] = alphabet[random.Intn(len(alphabet))]
		}
	}
	valid, invalid := compareCorpus(t, corpus)
	t.Logf("随机语料：有效 %d，非法 %d", valid, invalid)
}

// 测试 Mio Lexer 扫描自身源码
func TestMioLexerScansItself(t *testing.T) {
	root := repositoryRoot(t)
	paths := []string{
		filepath.Join(root, "selfhost", "lexer", "token.mio"),
		filepath.Join(root, "selfhost", "lexer", "lexer.mio"),
		filepath.Join(root, "selfhost", "lexer", "position.mio"),
	}
	actual := runMioLexer(t, paths)
	for index, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		expected := referenceResult(source)
		if fmt.Sprintf("%#v", actual[index]) != fmt.Sprintf("%#v", expected) {
			t.Fatalf("自扫描不一致: %s", path)
		}
	}
}

// 测试 Mio Lexer 扫描 Bunko 源码
func TestMioLexerScansBunko(t *testing.T) {
	root := repositoryRoot(t)
	var paths []string
	err := filepath.WalkDir(filepath.Join(root, "bunko"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".mio" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	actual := runMioLexer(t, paths)
	for index, path := range paths {
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		expected := referenceResult(source)
		if fmt.Sprintf("%#v", actual[index]) != fmt.Sprintf("%#v", expected) {
			t.Fatalf("Bunko 扫描不一致: %s", path)
		}
	}
}

// 测试源码大小限制返回结构化错误
func TestMioLexerResourceLimit(t *testing.T) {
	source := make([]byte, 16*1024*1024+1)
	for index := range source {
		source[index] = ' '
	}
	result := runMioLexer(t, writeCorpus(t, [][]byte{source}))[0]
	if len(result.tokens) != 0 || len(result.errors) != 1 || result.errors[0].kind != 8 {
		t.Fatalf("资源限制结果=%#v", result)
	}
}

// 测试自举 Lexer 的 Mochi 往返执行
func TestMioLexerMochiRoundTrip(t *testing.T) {
	paths := writeCorpus(t, [][]byte{[]byte("kaku value = '🍡';")})
	environment, program := compileMioProgram(t, harnessSource(paths))
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		t.Fatal(err)
	}
	decoded, err := miolang.DecodeProgram(bytes.NewReader(artifact.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(decoded, &output, miolang.ExecutionOptions{InstructionLimit: 100_000_000}); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("Mochi 执行没有输出")
	}
}

func benchmarkSource(size int) []byte {
	line := []byte("kaku value = 12345;\n")
	result := make([]byte, size)
	for offset := 0; offset < len(result); offset += len(line) {
		copy(result[offset:], line)
	}
	return result
}

func benchmarkMioLexer(b *testing.B, size int) {
	source := benchmarkSource(size)
	path := writeCorpus(b, [][]byte{source})[0]
	harness := `
yobu "kiso/fs";
kaku loaded = fs.read_all(` + strconv.Quote(path) + `);
kaku result = lex(loaded.data);
chant(len(result.tokens));
`
	environment, program := compileMioProgram(b, harness)
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		b.Fatal(err)
	}
	tokens := len(referenceResult(source).tokens)
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(tokens), "tokens/op")
	b.ReportMetric(float64(artifact.Len()), "Mochi-bytes")
	for iteration := 0; iteration < b.N; iteration++ {
		if err := environment.RunWithOptions(program, io.Discard, miolang.ExecutionOptions{InstructionLimit: 20_000_000_000}); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkGoLexer(b *testing.B, size int) {
	source := benchmarkSource(size)
	tokens := len(referenceResult(source).tokens)
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(tokens), "tokens/op")
	for iteration := 0; iteration < b.N; iteration++ {
		current, err := lexer.NewChecked(string(source), token.V05)
		if err != nil {
			b.Fatal(err)
		}
		for current.NextToken().Type != token.EOF {
		}
	}
}

// BenchmarkMioLexer 测量 Mio Lexer 的执行成本
func BenchmarkMioLexer(b *testing.B) {
	for _, size := range []struct {
		name string
		byte int
	}{{"1KiB", 1024}, {"64KiB", 64 * 1024}, {"1MiB", 1024 * 1024}} {
		b.Run(size.name, func(b *testing.B) { benchmarkMioLexer(b, size.byte) })
	}
}

// BenchmarkGoLexer 测量参考 Lexer 的执行成本
func BenchmarkGoLexer(b *testing.B) {
	for _, size := range []struct {
		name string
		byte int
	}{{"1KiB", 1024}, {"64KiB", 64 * 1024}, {"1MiB", 1024 * 1024}} {
		b.Run(size.name, func(b *testing.B) { benchmarkGoLexer(b, size.byte) })
	}
}
