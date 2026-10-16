package parser_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	miolang "Mio"
	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/lexer"
	goparser "Mio/internal/parser"
	"Mio/internal/token"
)

type wireNode struct {
	kind, integer, operator int
	text, extra             string
	flag                    bool
	span                    ast.Span
	children                []int
}

type wireError struct {
	kind     int
	position token.Position
}

type wireResult struct {
	root, tokenCount int
	nodes            []wireNode
	errors           []wireError
}

type canonicalNode struct {
	kind, integer, operator int
	text, extra             string
	flag                    bool
	span                    ast.Span
	children                []*canonicalNode
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

func repositoryRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func parserHarness(paths []string) string {
	var source strings.Builder
	source.WriteString(`
yobu "kiso/fs";
yobu "kiso/string";

waza emit_bytes(data: []uint8) -> unit {
    chant(len(data));
    kaku index = 0;
    aida (index < len(data)) { chant(data[index]); index = index + 1; }
}

waza emit_position(position: lexer.Position) -> unit {
    chant(position.offset); chant(position.line); chant(position.column);
}

waza emit(result: ParseResult) -> unit {
    chant(result.root);
    chant(len(result.tokens));
    chant(len(result.ast.nodes));
    chant(len(result.errors));
    kaku index = 0;
    aida (index < len(result.ast.nodes)) {
        kaku node = result.ast.nodes[index];
        chant(node_kind_code(node.kind));
        emit_bytes(string.to_bytes(node.text));
        emit_bytes(string.to_bytes(node.extra));
        chant(node.integer);
        moshi (node.flag) { chant(1); } sore { chant(0); }
        chant(node.operator);
        emit_position(node.span.start); emit_position(node.span.end);
        chant(len(node.children));
        kaku child = 0;
        aida (child < len(node.children)) { chant(node.children[child]); child = child + 1; }
        index = index + 1;
    }
    index = 0;
    aida (index < len(result.errors)) {
        kaku failure = result.errors[index];
        chant(parse_error_kind_code(failure.kind));
        emit_position(failure.position);
        index = index + 1;
    }
}

waza emit_path(path: string) -> unit {
    kaku loaded = fs.read_all(path);
    emit(parse(loaded.data));
}
`)
	for _, path := range paths {
		fmt.Fprintf(&source, "emit_path(%s);\n", strconv.Quote(path))
	}
	return source.String()
}

func compileParserProgram(t testing.TB, harness string) (*miolang.Environment, *miolang.Program) {
	t.Helper()
	root := repositoryRoot(t)
	directory := t.TempDir()
	for _, name := range []string{"ast.mio", "cursor.mio", "expression.mio", "statement.mio", "types.mio"} {
		content, err := os.ReadFile(filepath.Join(root, "selfhost", "parser", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "zz_harness.mio"), []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = environment.Close() })
	program, err := environment.CompileFile(directory, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	return environment, program
}

func runMioParser(t testing.TB, paths []string) ([]wireResult, int) {
	t.Helper()
	environment, program := compileParserProgram(t, parserHarness(paths))
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 20_000_000_000}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	readInt := func() int {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("读取 Mio Parser 输出: %v", err)
		}
		value, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			t.Fatalf("解析 Mio Parser 输出 %q: %v", line, err)
		}
		return value
	}
	readBytes := func() string {
		data := make([]byte, readInt())
		for index := range data {
			data[index] = byte(readInt())
		}
		return string(data)
	}
	readPosition := func() token.Position {
		return token.Position{Offset: readInt(), Line: readInt(), Column: readInt()}
	}
	results := make([]wireResult, len(paths))
	for resultIndex := range results {
		result := wireResult{root: readInt(), tokenCount: readInt()}
		nodeCount := readInt()
		errorCount := readInt()
		result.nodes = make([]wireNode, nodeCount)
		result.errors = make([]wireError, errorCount)
		for index := range result.nodes {
			node := wireNode{kind: readInt(), text: readBytes(), extra: readBytes(), integer: readInt(), flag: readInt() != 0, operator: readInt()}
			node.span = ast.Span{Start: readPosition(), End: readPosition()}
			node.children = make([]int, readInt())
			for child := range node.children {
				node.children[child] = readInt()
			}
			result.nodes[index] = node
		}
		for index := range result.errors {
			result.errors[index] = wireError{kind: readInt(), position: readPosition()}
		}
		results[resultIndex] = result
	}
	return results, artifact.Len()
}

func writeCorpus(t testing.TB, corpus [][]byte) []string {
	t.Helper()
	directory := t.TempDir()
	paths := make([]string, len(corpus))
	for index, source := range corpus {
		paths[index] = filepath.Join(directory, fmt.Sprintf("case-%03d.mio", index))
		if err := os.WriteFile(paths[index], source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func actualTree(result wireResult) (*canonicalNode, error) {
	visiting := make(map[int]bool)
	var visit func(int) (*canonicalNode, error)
	visit = func(id int) (*canonicalNode, error) {
		if id < 0 || id >= len(result.nodes) {
			return nil, fmt.Errorf("invalid NodeId %d", id)
		}
		if visiting[id] {
			return nil, fmt.Errorf("node cycle at %d", id)
		}
		visiting[id] = true
		wire := result.nodes[id]
		node := &canonicalNode{kind: wire.kind, text: wire.text, extra: wire.extra, integer: wire.integer, flag: wire.flag, operator: wire.operator, span: wire.span}
		for _, child := range wire.children {
			value, err := visit(child)
			if err != nil {
				return nil, err
			}
			node.children = append(node.children, value)
		}
		delete(visiting, id)
		return node, nil
	}
	return visit(result.root)
}

func node(kind int, span ast.Span, children ...*canonicalNode) *canonicalNode {
	return &canonicalNode{kind: kind, span: span, children: children}
}

func normalizeType(ref *ast.TypeRef) *canonicalNode {
	if ref == nil {
		return nil
	}
	switch ref.Name {
	case "[]":
		return node(40, ref.Range, normalizeType(ref.Element))
	case "?":
		return node(41, ref.Range, normalizeType(ref.Element))
	case "fn":
		children := make([]*canonicalNode, 0, len(ref.Params)+1)
		for _, parameter := range ref.Params {
			children = append(children, normalizeType(parameter))
		}
		children = append(children, normalizeType(ref.Result))
		result := node(42, ref.Range, children...)
		result.integer = len(ref.Params)
		return result
	default:
		children := make([]*canonicalNode, 0, len(ref.Args))
		for _, argument := range ref.Args {
			children = append(children, normalizeType(argument))
		}
		result := node(39, ref.Range, children...)
		result.text = ref.Name
		return result
	}
}

func normalizeExpression(expression ast.Expression, source string) *canonicalNode {
	switch value := expression.(type) {
	case *ast.Identifier:
		result := node(4, value.Range)
		result.text = value.Name
		return result
	case *ast.IntegerLiteral:
		result := node(5, value.Range)
		result.text = source[value.Range.Start.Offset:value.Range.End.Offset]
		if !value.Overflow && value.Magnitude <= uint64(^uint64(0)>>1) {
			result.integer = int(value.Value)
		}
		return result
	case *ast.FloatLiteral:
		result := node(6, value.Range)
		result.text = value.Raw
		return result
	case *ast.RuneLiteral:
		result := node(7, value.Range)
		result.text = string(rune(value.Value))
		result.integer = int(value.Value)
		return result
	case *ast.StringLiteral:
		result := node(8, value.Range)
		result.text = value.Value
		return result
	case *ast.BooleanLiteral:
		result := node(9, value.Range)
		result.flag = value.Value
		if value.Value {
			result.text = "hai"
		} else {
			result.text = "iie"
		}
		return result
	case *ast.NilLiteral:
		return node(10, value.Range)
	case *ast.InfixExpression:
		result := node(11, value.Range, normalizeExpression(value.Left, source), normalizeExpression(value.Right, source))
		result.operator = tokenCodes[value.Operator]
		return result
	case *ast.PrefixExpression:
		result := node(12, value.Range, normalizeExpression(value.Right, source))
		result.operator = tokenCodes[value.Operator]
		return result
	case *ast.GroupedExpression:
		return node(13, value.Ranges, normalizeExpression(value.Expr, source))
	case *ast.CallExpression:
		children := []*canonicalNode{normalizeExpression(value.Function, source)}
		for _, argument := range value.Arguments {
			children = append(children, normalizeExpression(argument, source))
		}
		result := node(22, value.Range, children...)
		result.integer = len(value.Arguments)
		return result
	case *ast.TypeApplyExpression:
		children := []*canonicalNode{normalizeExpression(value.Function, source)}
		for _, argument := range value.Args {
			children = append(children, normalizeType(argument))
		}
		result := node(23, value.Range, children...)
		result.integer = len(value.Args)
		return result
	case *ast.ConversionExpression:
		start := value.Range.Start
		end := start
		end.Offset += len(value.Target)
		end.Column += utf8.RuneCountInString(value.Target)
		identifier := node(4, ast.Span{Start: start, End: end})
		identifier.text = value.Target
		children := []*canonicalNode{identifier}
		for _, argument := range value.Arguments {
			children = append(children, normalizeExpression(argument, source))
		}
		result := node(24, value.Range, children...)
		result.text = value.Target
		result.integer = len(value.Arguments)
		return result
	case *ast.ArrayLiteral:
		children := make([]*canonicalNode, 0, len(value.Elements))
		for _, element := range value.Elements {
			children = append(children, normalizeExpression(element, source))
		}
		return node(25, value.Range, children...)
	case *ast.IndexExpression:
		return node(26, value.Range, normalizeExpression(value.Left, source), normalizeExpression(value.Index, source))
	case *ast.FieldExpression:
		result := node(27, value.Range, normalizeExpression(value.Left, source))
		result.text = value.Name
		return result
	case *ast.StructLiteral:
		children := make([]*canonicalNode, 0, len(value.TypeArgs)+len(value.Fields))
		for _, argument := range value.TypeArgs {
			children = append(children, normalizeType(argument))
		}
		for _, field := range value.Fields {
			fieldNode := node(29, field.Range, normalizeExpression(field.Value, source))
			fieldNode.text = field.Name
			children = append(children, fieldNode)
		}
		result := node(28, value.Range, children...)
		result.text = value.TypeName
		result.integer = len(value.TypeArgs)
		return result
	case *ast.FunctionLiteral:
		children := make([]*canonicalNode, 0, len(value.Parameters)+2)
		for _, parameter := range value.Parameters {
			children = append(children, normalizeParameter(parameter))
		}
		children = append(children, normalizeType(value.ReturnType), normalizeStatement(value.Body, source))
		result := node(33, value.Range, children...)
		result.integer = len(value.Parameters)
		return result
	default:
		panic(fmt.Sprintf("unsupported expression %T", expression))
	}
}

func normalizeParameter(identifier *ast.Identifier) *canonicalNode {
	result := node(43, identifier.Range)
	result.text = identifier.Name
	if identifier.DeclaredType != nil {
		result.children = append(result.children, normalizeType(identifier.DeclaredType))
		result.integer = 1
	}
	return result
}

func normalizeTypeParameters(names []string, constraints map[string]string) []*canonicalNode {
	result := make([]*canonicalNode, 0, len(names))
	for _, name := range names {
		parameter := node(44, ast.Span{})
		parameter.text = name
		parameter.extra = constraints[name]
		result = append(result, parameter)
	}
	return result
}

func normalizeStatement(statement ast.Statement, source string) *canonicalNode {
	switch value := statement.(type) {
	case *ast.ImportStatement:
		result := node(1, value.Range)
		result.text, result.extra = value.Path, value.Alias
		return result
	case *ast.ConstStatement:
		children := make([]*canonicalNode, 0, 2)
		if value.Name.DeclaredType != nil {
			children = append(children, normalizeType(value.Name.DeclaredType))
		}
		children = append(children, normalizeExpression(value.Value, source))
		result := node(2, value.Range, children...)
		result.text, result.integer, result.flag = value.Name.Name, len(children)-1, value.Exported
		return result
	case *ast.ExpressionStatement:
		return node(3, value.Range, normalizeExpression(value.Expr, source))
	case *ast.LetStatement:
		children := make([]*canonicalNode, 0, 2)
		if value.Name.DeclaredType != nil {
			children = append(children, normalizeType(value.Name.DeclaredType))
		}
		children = append(children, normalizeExpression(value.Value, source))
		result := node(14, value.Range, children...)
		result.text, result.integer = value.Name.Name, len(children)-1
		return result
	case *ast.AssignStatement:
		result := node(15, value.Range, normalizeExpression(value.Value, source))
		result.text = value.Name.Name
		return result
	case *ast.IndexAssignStatement:
		return node(16, value.Range, normalizeExpression(value.Target, source), normalizeExpression(value.Value, source))
	case *ast.BlockStatement:
		children := make([]*canonicalNode, 0, len(value.Statements))
		for _, child := range value.Statements {
			children = append(children, normalizeStatement(child, source))
		}
		return node(17, value.Range, children...)
	case *ast.IfStatement:
		children := []*canonicalNode{normalizeExpression(value.Condition, source), normalizeStatement(value.Consequence, source)}
		if value.Alternative != nil {
			children = append(children, normalizeStatement(value.Alternative, source))
		}
		return node(18, value.Range, children...)
	case *ast.WhileStatement:
		return node(19, value.Range, normalizeExpression(value.Condition, source), normalizeStatement(value.Body, source))
	case *ast.BreakStatement:
		return node(20, value.Range)
	case *ast.ContinueStatement:
		return node(21, value.Range)
	case *ast.ReturnStatement:
		if value.Value == nil {
			return node(31, value.Range)
		}
		return node(31, value.Range, normalizeExpression(value.Value, source))
	case *ast.FunctionStatement:
		children := normalizeTypeParameters(value.TypeParams, value.Constraints)
		for _, parameter := range value.Parameters {
			children = append(children, normalizeParameter(parameter))
		}
		if value.ReturnType != nil {
			children = append(children, normalizeType(value.ReturnType))
		}
		children = append(children, normalizeStatement(value.Body, source))
		result := node(32, value.Range, children...)
		result.text, result.integer, result.operator, result.flag = value.Name.Name, len(value.TypeParams), len(value.Parameters), value.Exported
		return result
	case *ast.StructStatement:
		children := normalizeTypeParameters(value.TypeParams, value.Constraints)
		for _, field := range value.Fields {
			fieldNode := node(45, field.Range, normalizeType(field.DeclaredType))
			fieldNode.text = field.Name
			children = append(children, fieldNode)
		}
		result := node(34, value.Range, children...)
		result.text, result.integer, result.flag = value.Name.Name, len(value.TypeParams), value.Exported
		return result
	case *ast.EnumStatement:
		children := normalizeTypeParameters(value.TypeParams, value.Constraints)
		for _, variant := range value.Variants {
			fields := make([]*canonicalNode, 0, len(variant.Fields))
			for _, field := range variant.Fields {
				fields = append(fields, normalizeType(field))
			}
			variantNode := node(35, variant.Range, fields...)
			variantNode.text = variant.Name
			children = append(children, variantNode)
		}
		result := node(36, value.Range, children...)
		result.text, result.integer, result.flag = value.Name.Name, len(value.TypeParams), value.Exported
		return result
	case *ast.MatchStatement:
		children := []*canonicalNode{normalizeExpression(value.Value, source)}
		for _, arm := range value.Arms {
			armChildren := make([]*canonicalNode, 0, len(arm.Bindings)+1)
			for _, binding := range arm.Bindings {
				bindingNode := node(4, binding.Range)
				bindingNode.text = binding.Name
				armChildren = append(armChildren, bindingNode)
			}
			armChildren = append(armChildren, normalizeStatement(arm.Body, source))
			armNode := node(37, arm.Range, armChildren...)
			armNode.text, armNode.integer = arm.Variant, len(arm.Bindings)
			children = append(children, armNode)
		}
		return node(38, value.Range, children...)
	default:
		panic(fmt.Sprintf("unsupported statement %T", statement))
	}
}

func normalizeProgram(program *ast.Program, source string) *canonicalNode {
	children := make([]*canonicalNode, 0, len(program.Statements))
	for _, statement := range program.Statements {
		children = append(children, normalizeStatement(statement, source))
	}
	return node(0, program.Range, children...)
}

func goParse(source []byte) (*canonicalNode, []wireError) {
	checked, err := lexer.NewChecked(string(source), token.V05)
	if err != nil {
		lexical := err.(*lexer.LexicalError)
		return nil, []wireError{{kind: 0, position: lexical.Position}}
	}
	program, err := goparser.New(checked).ParseProgram()
	if err == nil {
		return normalizeProgram(program, string(source)), nil
	}
	if lexical, ok := err.(*lexer.LexicalError); ok {
		return nil, []wireError{{kind: 0, position: lexical.Position}}
	}
	if detailed, ok := err.(*diagnostic.Diagnostic); ok {
		kind := 1
		if detailed.Code == "E0102" {
			kind = 2
		}
		label, _ := detailed.PrimaryLabel()
		return nil, []wireError{{kind: kind, position: token.Position{Offset: label.Span.Start.Offset, Line: label.Span.Start.Line, Column: label.Span.Start.Column}}}
	}
	return nil, []wireError{{kind: 6}}
}

func compareTree(path string, actual, expected *canonicalNode) error {
	if actual == nil || expected == nil {
		if actual == expected {
			return nil
		}
		return fmt.Errorf("%s: nil mismatch", path)
	}
	actualSpan, expectedSpan := actual.span, expected.span
	if actual.kind == 44 {
		actualSpan, expectedSpan = ast.Span{}, ast.Span{}
	}
	if actual.kind != expected.kind || actual.text != expected.text || actual.extra != expected.extra || actual.integer != expected.integer || actual.flag != expected.flag || actual.operator != expected.operator || actualSpan != expectedSpan {
		return fmt.Errorf("%s: actual=%#v expected=%#v", path, actual, expected)
	}
	if len(actual.children) != len(expected.children) {
		return fmt.Errorf("%s: children=%d expected=%d", path, len(actual.children), len(expected.children))
	}
	for index := range actual.children {
		if err := compareTree(fmt.Sprintf("%s/%d", path, index), actual.children[index], expected.children[index]); err != nil {
			return err
		}
	}
	return nil
}

func compareCorpus(t *testing.T, corpus [][]byte) (int, int) {
	t.Helper()
	actual, _ := runMioParser(t, writeCorpus(t, corpus))
	valid, invalid := 0, 0
	for index, source := range corpus {
		expectedTree, expectedErrors := goParse(source)
		if len(expectedErrors) != 0 {
			invalid++
			if !reflect.DeepEqual(actual[index].errors, expectedErrors) {
				t.Fatalf("语料 %d 错误不一致\nsource=%s\nMio=%#v\nGo=%#v", index, source, actual[index].errors, expectedErrors)
			}
			continue
		}
		valid++
		if len(actual[index].errors) != 0 {
			t.Fatalf("语料 %d Mio 解析失败: %s %#v", index, source, actual[index].errors)
		}
		actualTree, err := actualTree(actual[index])
		if err != nil {
			t.Fatal(err)
		}
		if err := compareTree("root", actualTree, expectedTree); err != nil {
			t.Fatalf("语料 %d AST 不一致: %v\nsource=%s", index, err, source)
		}
	}
	return valid, invalid
}

func comparePaths(t *testing.T, paths []string) (int, int) {
	t.Helper()
	actual, _ := runMioParser(t, paths)
	valid, invalid := 0, 0
	for index, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		expectedTree, expectedErrors := goParse(source)
		if len(expectedErrors) != 0 {
			invalid++
			if !reflect.DeepEqual(actual[index].errors, expectedErrors) {
				t.Fatalf("%s 错误不一致: Mio=%#v Go=%#v", path, actual[index].errors, expectedErrors)
			}
			continue
		}
		valid++
		if len(actual[index].errors) != 0 {
			t.Fatalf("%s Mio 解析失败: %#v", path, actual[index].errors)
		}
		actualTree, treeErr := actualTree(actual[index])
		if treeErr != nil {
			t.Fatal(treeErr)
		}
		if treeErr := compareTree("root", actualTree, expectedTree); treeErr != nil {
			t.Fatalf("%s AST 不一致: %v", path, treeErr)
		}
	}
	return valid, invalid
}

// 测试完整语法的 Go 与 Mio AST 差分
func TestMioParserDifferentialCorpus(t *testing.T) {
	corpus := [][]byte{
		{}, []byte("1 + 2 * 3;"), []byte("(1 + 2) * -3 == 9 || !iie && hai;"),
		[]byte(`kaku text = "Mio"; kaku rune_value = '🍡'; kaku empty: int64? = mu;`),
		[]byte(`teisu answer: int64 = 42; koukai teisu Name = "Mio"; value = answer; values[0] = 7;`),
		[]byte(`waza apply<T, U>(value: T, f: (T) -> U) -> U { kaesu f(value); }`),
		[]byte(`kaku closure = waza (value: int64) -> int64 { kaesu value + 1; };`),
		[]byte(`kata Box<T> { value: T; } kaku box = Box[int64] { value: 42 }; box.value;`),
		[]byte(`sentaku Result<T, E> { Ok(T); Err(E); } kaku item = Result[int64, string].Ok(42);`),
		[]byte(`awase (value) { baai Ok(item) { chant(item); } baai Err(message) { chant(message); } }`),
		[]byte(`moshi (value != mu) { value; } sore { kaesu; } aida (hai) { tsuzuku; nukeru; }`),
		[]byte(`kaku values: []int64? = [1, mu]; kaku callback: (int64, string) -> bool = check;`),
		[]byte(`kaku entries = Map[string, int64](); entries["answer"] = int64(42);`),
		[]byte(`kaku maximum = 9223372036854775807; kaku magnitude = 9223372036854775808; kaku minimum = -9223372036854775808; kaku below = -9223372036854775809;`),
		[]byte(`yobu "kiso/array"; koukai waza exported() -> unit { kaesu; }`),
		[]byte(`kaku value = (1 + 2;`), []byte(`waza missing(value: int64 -> unit {}`),
		[]byte(`moshi hai) {}`), []byte(`kaku = 1;`), []byte(`1 + ;`), []byte("\xff"),
	}
	valid, invalid := compareCorpus(t, corpus)
	t.Logf("定向语料：有效 %d，非法 %d", valid, invalid)
}

// 测试固定种子的表达式 AST 差分
func TestMioParserDifferentialRandomExpressions(t *testing.T) {
	random := rand.New(rand.NewSource(20261016))
	operators := []string{" + ", " - ", " * ", " / ", " == ", " != ", " < ", " <= ", " && ", " || "}
	corpus := make([][]byte, 48)
	for index := range corpus {
		terms := random.Intn(8) + 1
		var source strings.Builder
		for term := 0; term < terms; term++ {
			if term != 0 {
				source.WriteString(operators[random.Intn(len(operators))])
			}
			if random.Intn(4) == 0 {
				source.WriteByte('-')
			}
			fmt.Fprintf(&source, "%d", random.Intn(100))
		}
		source.WriteByte(';')
		corpus[index] = []byte(source.String())
	}
	valid, invalid := compareCorpus(t, corpus)
	if invalid != 0 {
		t.Fatalf("随机表达式出现 %d 个非法输入", invalid)
	}
	t.Logf("随机有效表达式：%d", valid)
}

// 测试固定种子的非法语法差分
func TestMioParserDifferentialRandomInvalid(t *testing.T) {
	random := rand.New(rand.NewSource(20261017))
	corpus := make([][]byte, 32)
	for index := range corpus {
		left := random.Intn(100)
		right := random.Intn(100)
		switch index % 4 {
		case 0:
			corpus[index] = []byte(fmt.Sprintf("kaku value%d = (%d + %d;", index, left, right))
		case 1:
			corpus[index] = []byte(fmt.Sprintf("kaku value%d = %d", index, left))
		case 2:
			corpus[index] = []byte(fmt.Sprintf("%d + ;", left))
		default:
			corpus[index] = []byte(fmt.Sprintf("moshi (hai) { kaku value%d = %d;", index, right))
		}
	}
	valid, invalid := compareCorpus(t, corpus)
	if valid != 0 || invalid != len(corpus) {
		t.Fatalf("随机非法语法：有效 %d，非法 %d", valid, invalid)
	}
	t.Logf("随机非法语法：%d", invalid)
}

// 测试 Mio Parser 解析自身源码
func TestMioParserParsesItself(t *testing.T) {
	root := repositoryRoot(t)
	paths := []string{
		filepath.Join(root, "selfhost", "parser", "ast.mio"),
		filepath.Join(root, "selfhost", "parser", "cursor.mio"),
		filepath.Join(root, "selfhost", "parser", "expression.mio"),
		filepath.Join(root, "selfhost", "parser", "statement.mio"),
		filepath.Join(root, "selfhost", "parser", "types.mio"),
	}
	valid, invalid := comparePaths(t, paths)
	if valid != len(paths) || invalid != 0 {
		t.Fatalf("自解析：有效 %d，非法 %d", valid, invalid)
	}
}

// 测试 Mio Parser 解析 Lexer 源码
func TestMioParserParsesLexer(t *testing.T) {
	root := repositoryRoot(t)
	paths := []string{
		filepath.Join(root, "selfhost", "lexer", "token.mio"),
		filepath.Join(root, "selfhost", "lexer", "lexer.mio"),
		filepath.Join(root, "selfhost", "lexer", "position.mio"),
	}
	valid, invalid := comparePaths(t, paths)
	if valid != len(paths) || invalid != 0 {
		t.Fatalf("Lexer 解析：有效 %d，非法 %d", valid, invalid)
	}
}

// 测试 Mio Parser 解析全部 Bunko 源码
func TestMioParserParsesBunko(t *testing.T) {
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
	valid, invalid := comparePaths(t, paths)
	if valid != len(paths) || invalid != 0 {
		t.Fatalf("Bunko 解析：有效 %d，非法 %d", valid, invalid)
	}
}

// 测试 Parser 的 Mochi 往返执行
func TestMioParserMochiRoundTrip(t *testing.T) {
	paths := writeCorpus(t, [][]byte{[]byte("kaku value = 1 + 2 * 3;")})
	environment, program := compileParserProgram(t, parserHarness(paths))
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		t.Fatal(err)
	}
	decoded, err := miolang.DecodeProgram(bytes.NewReader(artifact.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(decoded, &output, miolang.ExecutionOptions{InstructionLimit: 200_000_000}); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("Mochi 执行没有输出")
	}
}

// 测试嵌套深度限制返回结构化错误
func TestMioParserNestingLimit(t *testing.T) {
	source := strings.Repeat("(", 600) + "1" + strings.Repeat(")", 600) + ";"
	actual, _ := runMioParser(t, writeCorpus(t, [][]byte{[]byte(source)}))
	if len(actual[0].errors) != 1 || actual[0].errors[0].kind != 5 {
		t.Fatalf("嵌套限制结果=%#v", actual[0].errors)
	}
}

func benchmarkParserSource(size int) []byte {
	line := []byte("kaku value = 12345 + 6 * 7;\n")
	result := make([]byte, size)
	offset := 0
	for offset+len(line) <= len(result) {
		copy(result[offset:], line)
		offset += len(line)
	}
	for offset < len(result) {
		result[offset] = ' '
		offset++
	}
	return result
}

func countCanonicalNodes(root *canonicalNode) int {
	if root == nil {
		return 0
	}
	count := 1
	for _, child := range root.children {
		count += countCanonicalNodes(child)
	}
	return count
}

func countTokens(source []byte) int {
	checked, err := lexer.NewChecked(string(source), token.V05)
	if err != nil {
		return 0
	}
	count := 0
	for {
		count++
		if checked.NextToken().Type == token.EOF {
			return count
		}
	}
}

func benchmarkMioParser(b *testing.B, source []byte) {
	path := writeCorpus(b, [][]byte{source})[0]
	harness := `
yobu "kiso/fs";
kaku loaded = fs.read_all(` + strconv.Quote(path) + `);
kaku result = parse(loaded.data);
chant(len(result.ast.nodes));
`
	environment, program := compileParserProgram(b, harness)
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		b.Fatal(err)
	}
	expected, errors := goParse(source)
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(countTokens(source)), "tokens/op")
	b.ReportMetric(float64(countCanonicalNodes(expected)), "nodes/op")
	b.ReportMetric(float64(len(errors)), "errors/op")
	b.ReportMetric(float64(artifact.Len()), "Mochi-bytes")
	for iteration := 0; iteration < b.N; iteration++ {
		if err := environment.RunWithOptions(program, io.Discard, miolang.ExecutionOptions{InstructionLimit: 20_000_000_000}); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkGoParser(b *testing.B, source []byte) {
	expected, errors := goParse(source)
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(countTokens(source)), "tokens/op")
	b.ReportMetric(float64(countCanonicalNodes(expected)), "nodes/op")
	b.ReportMetric(float64(len(errors)), "errors/op")
	for iteration := 0; iteration < b.N; iteration++ {
		checked, err := lexer.NewChecked(string(source), token.V05)
		if err == nil {
			_, _ = goparser.New(checked).ParseProgram()
		}
	}
}

func parserBenchmarkCases() []struct {
	name   string
	source []byte
} {
	complex := []byte(`
kata Box<T: Number> { value: T; }
sentaku Result<T, E> { Ok(T); Err(E); }
waza apply<T, U>(value: T, transform: (T) -> U) -> U { kaesu transform(value); }
kaku closure = waza (value: int64) -> int64 { kaesu value + 1; };
kaku box = Box[int64] { value: 42 };
kaku result = Result[int64, string].Ok(apply[int64, int64](box.value, closure));
`)
	invalid := benchmarkParserSource(1024)
	invalid = append(invalid, []byte("kaku broken = (1 + 2;")...)
	return []struct {
		name   string
		source []byte
	}{{"1KiB", benchmarkParserSource(1024)}, {"64KiB", benchmarkParserSource(64 * 1024)}, {"Complex", complex}, {"Error", invalid}}
}

// BenchmarkMioParser 测量纯 Mio Lexer 与 Parser 的整体成本
func BenchmarkMioParser(b *testing.B) {
	for _, test := range parserBenchmarkCases() {
		b.Run(test.name, func(b *testing.B) { benchmarkMioParser(b, test.source) })
	}
}

// BenchmarkGoParser 测量 Go Lexer 与 Parser 的整体成本
func BenchmarkGoParser(b *testing.B) {
	for _, test := range parserBenchmarkCases() {
		b.Run(test.name, func(b *testing.B) { benchmarkGoParser(b, test.source) })
	}
}
