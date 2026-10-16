package typecheck_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	miolang "Mio"
	"Mio/internal/ast"
	"Mio/internal/constant"
	"Mio/internal/diagnostic"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	gotypes "Mio/internal/typecheck"
)

type typecheckWireResult struct {
	errors              int
	firstKind           int
	firstOffset         int
	types               int
	nodeTypes           int
	symbolTypes         int
	signatures          int
	substitutions       int
	tokens              int
	nodes               int
	resolved            int
	nodeDetails         map[string]string
	errorDetails        []typecheckWireError
	constantCount       int
	constants           map[string]typecheckWireConstant
	symbolDetails       map[string]string
	signatureInfo       map[string]string
	substitutionDetails map[string]string
}

type typecheckWireConstant struct {
	kind, state int
	typeName    string
	value       string
}

type typecheckWireError struct {
	kind, start, end int
	code             string
	expected, actual string
	detail           string
}

func typecheckRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func typecheckHarness(paths []string) string {
	var source strings.Builder
	source.WriteString(`
yobu "kiso/fs";
yobu "kiso/string";
yobu "kiso/convert";
yobu "selfhost/parser";
yobu "selfhost/resolver";
yobu "selfhost/typecheck";

waza emit(path: string) -> unit {
    kaku loaded = fs.read_all(path);
    kaku parsed = parser.parse(loaded.data);
    kaku resolved = resolver.resolve_program("case", parsed);
    kaku result = typecheck.check_program(resolved);
    chant(len(result.errors));
    kaku kind = -1;
    kaku offset = -1;
    moshi (len(result.errors) > 0) { kind = typecheck.type_error_kind_code(result.errors[0].kind); offset = result.errors[0].span.start.offset; }
    chant(kind); chant(offset); chant(len(result.types)); chant(len(result.node_types)); chant(len(result.symbol_types)); chant(len(result.signatures)); chant(len(result.substitutions)); chant(len(parsed.tokens)); chant(len(parsed.ast.nodes)); chant(len(resolved.symbols));
    chant(len(result.node_types));
    kaku node_index = 0;
    aida (node_index < len(result.node_types)) {
        kaku item = result.node_types[node_index]; kaku node = resolved.files[item.node.file_id].ast.nodes[item.node.node_id];
        chant(parser.node_kind_code(node.kind)); chant(node.span.start.offset); chant(node.span.end.offset); chant(typecheck.type_name(result, item.type_id));
        node_index = node_index + 1;
    }
    chant(len(result.symbol_types));
    kaku symbol_index = 0;
    aida (symbol_index < len(result.symbol_types)) {
        kaku item = result.symbol_types[symbol_index]; kaku symbol = resolved.symbols[item.symbol_id];
        chant(symbol.name); chant(symbol.span.start.offset); chant(symbol.span.end.offset); chant(typecheck.type_name(result, item.type_id));
        symbol_index = symbol_index + 1;
    }
    chant(len(result.signatures));
    kaku signature_index = 0;
    aida (signature_index < len(result.signatures)) {
        kaku signature = result.signatures[signature_index]; chant(signature.name); chant(typecheck.type_name(result, signature.result)); chant(len(signature.parameters));
        kaku parameter_index = 0; aida (parameter_index < len(signature.parameters)) { chant(typecheck.type_name(result, signature.parameters[parameter_index])); parameter_index = parameter_index + 1; }
        chant(len(signature.type_parameters)); parameter_index = 0; aida (parameter_index < len(signature.type_parameters)) { chant(signature.type_parameters[parameter_index].name); parameter_index = parameter_index + 1; }
        signature_index = signature_index + 1;
    }
    chant(len(result.substitutions));
    kaku substitution_index = 0;
    aida (substitution_index < len(result.substitutions)) {
        kaku substitution = result.substitutions[substitution_index]; kaku node = resolved.files[substitution.node.file_id].ast.nodes[substitution.node.node_id];
        chant(node.span.start.offset); chant(node.span.end.offset); chant(len(substitution.parameters));
        kaku parameter_index = 0; aida (parameter_index < len(substitution.parameters)) { chant(substitution.parameters[parameter_index]); chant(typecheck.type_name(result, substitution.types[parameter_index])); parameter_index = parameter_index + 1; }
        substitution_index = substitution_index + 1;
    }
    chant(len(result.constants));
    kaku constant_index = 0;
    aida (constant_index < len(result.constants)) {
        kaku item = result.constants[constant_index]; kaku name = "";
        moshi (item.symbol_id >= 0 && item.symbol_id < len(resolved.symbols)) { name = resolved.symbols[item.symbol_id].name; }
        chant(name); moshi (item.declaration) { chant(1); } sore { chant(0); }
        chant(typecheck.constant_kind_code(item.value.kind)); chant(typecheck.constant_state_code(item.value.state)); chant(typecheck.type_name(result, item.value.type_id));
        kaku value = item.value.string_value;
        kaku value_kind = typecheck.constant_kind_code(item.value.kind);
        moshi (value_kind == 1) { value = convert.format_int(item.value.integer); }
        moshi (value_kind == 2) { value = convert.format_float32(item.value.float32_value); }
        moshi (value_kind == 3) { value = convert.format_float64(item.value.float64_value); }
        moshi (value_kind == 4) { value = convert.format_bool(item.value.boolean); }
        chant(value); constant_index = constant_index + 1;
    }
    chant(len(result.errors));
    kaku error_index = 0;
    aida (error_index < len(result.errors)) {
        kaku failure = result.errors[error_index];
        chant(typecheck.type_error_kind_code(failure.kind)); chant(failure.code); chant(failure.span.start.offset); chant(failure.span.end.offset);
        chant(typecheck.type_name(result, failure.expected)); chant(typecheck.type_name(result, failure.actual)); chant(failure.detail);
        error_index = error_index + 1;
    }
}
`)
	for _, path := range paths {
		fmt.Fprintf(&source, "emit(%s);\n", strconv.Quote(path))
	}
	return source.String()
}

func runMioTypecheck(t testing.TB, sources []string) ([]typecheckWireResult, int) {
	t.Helper()
	root := typecheckRoot(t)
	directory := t.TempDir()
	paths := make([]string, len(sources))
	for index, source := range sources {
		paths[index] = filepath.Join(directory, fmt.Sprintf("case_%03d.mio", index))
		if err := os.WriteFile(paths[index], []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	harness := filepath.Join(directory, "harness.mio")
	if err := os.WriteFile(harness, []byte(typecheckHarness(paths)), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = environment.Close() })
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(&output)
	readIndex := 0
	read := func() int {
		if !scanner.Scan() {
			t.Fatal("纯 Mio TypeChecker 输出提前结束")
		}
		value, convertErr := strconv.Atoi(scanner.Text())
		if convertErr != nil {
			t.Fatalf("读取第 %d 个整数 %q: %v\n输出：\n%s", readIndex, scanner.Text(), convertErr, output.String())
		}
		readIndex++
		return value
	}
	readString := func() string {
		if !scanner.Scan() {
			t.Fatal("纯 Mio TypeChecker 字符串输出提前结束")
		}
		return scanner.Text()
	}
	results := make([]typecheckWireResult, len(sources))
	for index := range results {
		result := typecheckWireResult{
			errors: read(), firstKind: read(), firstOffset: read(), types: read(), nodeTypes: read(),
			symbolTypes: read(), signatures: read(), substitutions: read(), tokens: read(), nodes: read(), resolved: read(),
		}
		result.nodeDetails = make(map[string]string)
		nodeDetailCount := read()
		for detail := 0; detail < nodeDetailCount; detail++ {
			kind, start, end := read(), read(), read()
			result.nodeDetails[fmt.Sprintf("%d:%d:%d", kind, start, end)] = readString()
		}
		result.symbolDetails = make(map[string]string)
		symbolDetailCount := read()
		for detail := 0; detail < symbolDetailCount; detail++ {
			name, start, end, typeName := readString(), read(), read(), readString()
			result.symbolDetails[fmt.Sprintf("%s:%d:%d", name, start, end)] = typeName
		}
		result.signatureInfo = make(map[string]string)
		signatureCount := read()
		for detail := 0; detail < signatureCount; detail++ {
			name, returnType, parameterCount := readString(), readString(), read()
			parts := make([]string, parameterCount)
			for parameter := range parts {
				parts[parameter] = readString()
			}
			typeParameterCount := read()
			typeParameters := make([]string, typeParameterCount)
			for parameter := range typeParameters {
				typeParameters[parameter] = readString()
			}
			result.signatureInfo[name] = strings.Join(typeParameters, ",") + "|" + strings.Join(parts, ",") + "->" + returnType
		}
		result.substitutionDetails = make(map[string]string)
		substitutionCount := read()
		for detail := 0; detail < substitutionCount; detail++ {
			start, end, parameterCount := read(), read(), read()
			parts := make([]string, parameterCount)
			for parameter := range parts {
				parts[parameter] = readString() + "=" + readString()
			}
			result.substitutionDetails[fmt.Sprintf("%d:%d", start, end)] = strings.Join(parts, ",")
		}
		result.constantCount = read()
		result.constants = make(map[string]typecheckWireConstant)
		for constantIndex := 0; constantIndex < result.constantCount; constantIndex++ {
			name, declaration := readString(), read()
			value := typecheckWireConstant{kind: read(), state: read(), typeName: readString(), value: readString()}
			if declaration != 0 {
				result.constants[name] = value
			}
		}
		result.errorDetails = make([]typecheckWireError, read())
		for detail := range result.errorDetails {
			result.errorDetails[detail] = typecheckWireError{kind: read(), code: readString(), start: read(), end: read(), expected: readString(), actual: readString(), detail: readString()}
		}
		results[index] = result
	}
	return results, artifact.Len()
}

func runGoTypecheck(source string) error {
	_, _, err := runGoTypecheckDetailed(source)
	return err
}

func runGoTypecheckDetailed(source string) (*ast.Program, *gotypes.Checker, error) {
	checked, err := lexer.NewChecked(source, token.V05)
	if err != nil {
		return nil, nil, err
	}
	program, err := parser.New(checked).ParseProgram()
	if err != nil {
		return nil, nil, err
	}
	resolved := resolver.New()
	if err := resolved.ResolveProgram(program); err != nil {
		return nil, nil, err
	}
	checker := gotypes.NewWithResolver(resolved)
	return program, checker, checker.CheckProgram(program)
}

func runGoConstants(source string) (map[string]constant.Value, error) {
	checked, err := lexer.NewChecked(source, token.V05)
	if err != nil {
		return nil, err
	}
	program, err := parser.New(checked).ParseProgram()
	if err != nil {
		return nil, err
	}
	return constant.Evaluate(program)
}

func expressionKind(expression ast.Expression) int {
	switch expression.(type) {
	case *ast.Identifier:
		return 4
	case *ast.IntegerLiteral:
		return 5
	case *ast.FloatLiteral:
		return 6
	case *ast.RuneLiteral:
		return 7
	case *ast.StringLiteral:
		return 8
	case *ast.BooleanLiteral:
		return 9
	case *ast.NilLiteral:
		return 10
	case *ast.InfixExpression:
		return 11
	case *ast.PrefixExpression:
		return 12
	case *ast.GroupedExpression:
		return 13
	case *ast.CallExpression:
		return 22
	case *ast.TypeApplyExpression:
		return 23
	case *ast.ConversionExpression:
		return 24
	case *ast.ArrayLiteral:
		return 25
	case *ast.IndexExpression:
		return 26
	case *ast.FieldExpression:
		return 27
	case *ast.StructLiteral:
		return 28
	case *ast.EnumLiteral:
		return 30
	case *ast.FunctionLiteral:
		return 33
	default:
		return -1
	}
}

func collectExpressions(value reflect.Value, found map[ast.Expression]struct{}) {
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return
		}
		collectExpressions(value.Elem(), found)
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		if expression, ok := value.Interface().(ast.Expression); ok {
			found[expression] = struct{}{}
		}
		collectExpressions(value.Elem(), found)
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.CanInterface() {
				collectExpressions(field, found)
			}
		}
	case reflect.Slice:
		for index := 0; index < value.Len(); index++ {
			collectExpressions(value.Index(index), found)
		}
	}
}

func goNodeTypes(program *ast.Program, checker *gotypes.Checker) map[string]string {
	expressions := make(map[ast.Expression]struct{})
	collectExpressions(reflect.ValueOf(program), expressions)
	result := make(map[string]string)
	for expression := range expressions {
		kind, ok := checker.TypeOf(expression)
		if !ok {
			continue
		}
		span := expression.Span()
		name := kind.String()
		if name == "nil" {
			name = "mu"
		}
		result[fmt.Sprintf("%d:%d:%d", expressionKind(expression), span.Start.Offset, span.End.Offset)] = name
	}
	return result
}

func goSymbolTypes(program *ast.Program, checker *gotypes.Checker) map[string]string {
	expressions := make(map[ast.Expression]struct{})
	collectExpressions(reflect.ValueOf(program), expressions)
	result := make(map[string]string)
	for expression := range expressions {
		identifier, ok := expression.(*ast.Identifier)
		if !ok {
			continue
		}
		kind, ok := checker.TypeOfIdentifier(identifier)
		if !ok {
			continue
		}
		span := identifier.Span()
		result[fmt.Sprintf("%s:%d:%d", identifier.Name, span.Start.Offset, span.End.Offset)] = kind.String()
	}
	var addStatements func([]ast.Statement)
	addIdentifier := func(identifier *ast.Identifier, span ast.Span) {
		if kind, ok := checker.TypeOfIdentifier(identifier); ok {
			result[fmt.Sprintf("%s:%d:%d", identifier.Name, span.Start.Offset, span.End.Offset)] = kind.String()
		}
	}
	addStatements = func(statements []ast.Statement) {
		for _, statement := range statements {
			switch value := statement.(type) {
			case *ast.LetStatement:
				addIdentifier(value.Name, value.Span())
			case *ast.FunctionStatement:
				addIdentifier(value.Name, value.Span())
				if signature, ok := checker.SignatureOf(value.Name); ok && len(signature.TypeParams) == 0 {
					parameters := make([]string, len(signature.ParameterTypes))
					for index, parameter := range signature.ParameterTypes {
						parameters[index] = parameter.String()
					}
					span := value.Span()
					result[fmt.Sprintf("%s:%d:%d", value.Name.Name, span.Start.Offset, span.End.Offset)] = "(" + strings.Join(parameters, ", ") + ") -> " + signature.ReturnType.String()
				}
				for _, parameter := range value.Parameters {
					addIdentifier(parameter, parameter.Span())
				}
				addStatements(value.Body.Statements)
			case *ast.IfStatement:
				addStatements(value.Consequence.Statements)
				if value.Alternative != nil {
					addStatements(value.Alternative.Statements)
				}
			case *ast.WhileStatement:
				addStatements(value.Body.Statements)
			}
		}
	}
	addStatements(program.Statements)
	return result
}

func signatureText(signature gotypes.FunctionSignature) string {
	parameters := make([]string, len(signature.ParameterTypes))
	for index, parameter := range signature.ParameterTypes {
		parameters[index] = parameter.String()
	}
	return strings.Join(signature.TypeParams, ",") + "|" + strings.Join(parameters, ",") + "->" + signature.ReturnType.String()
}

func goSignatures(program *ast.Program, checker *gotypes.Checker) map[string]string {
	result := make(map[string]string)
	for _, statement := range program.Statements {
		function, ok := statement.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		if signature, found := checker.SignatureOf(function.Name); found {
			result[function.Name.Name] = signatureText(signature)
		}
	}
	return result
}

func goSubstitutions(program *ast.Program, checker *gotypes.Checker) map[string]string {
	expressions := make(map[ast.Expression]struct{})
	collectExpressions(reflect.ValueOf(program), expressions)
	result := make(map[string]string)
	for expression := range expressions {
		call, ok := expression.(*ast.CallExpression)
		if !ok || len(call.TypeArgs) == 0 {
			continue
		}
		calleeExpression := call.Function
		if apply, explicit := calleeExpression.(*ast.TypeApplyExpression); explicit {
			calleeExpression = apply.Function
		}
		callee, ok := calleeExpression.(*ast.Identifier)
		if !ok {
			continue
		}
		signature, ok := checker.SignatureOf(callee)
		if !ok || len(signature.TypeParams) != len(call.TypeArgs) {
			continue
		}
		parts := make([]string, len(call.TypeArgs))
		for index := range parts {
			parts[index] = signature.TypeParams[index] + "=" + call.TypeArgs[index]
		}
		span := call.Span()
		result[fmt.Sprintf("%d:%d", span.Start.Offset, span.End.Offset)] = strings.Join(parts, ",")
	}
	return result
}

// 测试纯 Mio TypeChecker 与 Go TypeChecker 的成功或失败结果一致
func TestMioTypecheckerDifferential(t *testing.T) {
	sources := []string{
		"kaku value: int32 = 10; chant(value);",
		"kaku a: int32 = 10; kaku b: int64 = 20; chant(a < b);",
		"kaku a: int32 = 10; kaku b: int64 = 20; chant(int64(a) < b);",
		"waza add(a: float32, b: float32) -> float32 { kaesu a + b; }",
		"waza bad(a: float32, b: float64) -> float64 { kaesu a + b; }",
		"kaku values: []uint8 = [0, 255];",
		"kaku values: []uint8 = [256];",
		"waza use(value: int64?) -> int64 { moshi (value != mu) { kaesu value + 1; } kaesu 0; }",
		"waza use(value: int64?) -> int64 { kaesu value + 1; }",
		"kata Pair<T> { left: T; right: T; } kaku value = Pair[int64] { left: 1, right: 2 };",
		"sentaku Flag { On; Off; } waza code(value: Flag) -> int64 { awase (value) { baai On { kaesu 1; } baai Off { kaesu 0; } } }",
		"sentaku Flag { On; Off; } waza code(value: Flag) -> int64 { awase (value) { baai On { kaesu 1; } } }",
		"waza identity<T: Number>(value: T) -> T { kaesu value; } kaku value = identity(1);",
		"waza identity<T: Float>(value: T) -> T { kaesu value; } kaku value = identity(1);",
		"kaku values: []int64 = [];",
		"kaku values: []int64 = [1, 2, 3]; chant(values[0]);",
	}
	actual, _ := runMioTypecheck(t, sources)
	for index, source := range sources {
		goFailed := runGoTypecheck(source) != nil
		mioFailed := actual[index].errors != 0
		if goFailed != mioFailed {
			t.Fatalf("case %d success mismatch: Go failed=%v Mio=%+v\n%s", index, goFailed, actual[index], source)
		}
		if !mioFailed && (actual[index].types == 0 || actual[index].nodeTypes == 0 || actual[index].symbolTypes == 0) {
			t.Fatalf("case %d missing static type information: %+v", index, actual[index])
		}
	}
}

// 测试逐表达式静态类型与泛型替换结果一致
func TestMioTypecheckerNodeTypeDifferential(t *testing.T) {
	sources := []string{
		"kaku value: int32 = 10; chant(value + 1);",
		"waza identity<T: Number>(value: T) -> T { kaesu value; } kaku result = identity(1);",
		"waza use(value: int64?) -> int64 { moshi (value != mu) { kaesu value + 1; } kaesu 0; }",
		"kata Pair<T> { left: T; right: T; } kaku pair = Pair[int64] { left: 1, right: 2 }; chant(pair.left);",
		"sentaku Flag { On; Off; } waza code(value: Flag) -> int64 { awase (value) { baai On { kaesu 1; } baai Off { kaesu 0; } } }",
		"kaku entries: Map<string, int64> = Map[string, int64](); kaku found = entries[\"answer\"];",
	}
	mio, _ := runMioTypecheck(t, sources)
	for index, source := range sources {
		program, checker, err := runGoTypecheckDetailed(source)
		if err != nil || mio[index].errors != 0 {
			t.Fatalf("case %d failed: Go=%v Mio=%+v", index, err, mio[index])
		}
		for key, expected := range goNodeTypes(program, checker) {
			actual, ok := mio[index].nodeDetails[key]
			if !ok {
				t.Fatalf("case %d missing Mio node type %s=%s\n%s", index, key, expected, source)
			}
			actual = strings.ReplaceAll(actual, "case.", "")
			if actual != expected {
				t.Fatalf("case %d node %s type mismatch: Go=%s Mio=%s\n%s", index, key, expected, actual, source)
			}
		}
		goSymbols := goSymbolTypes(program, checker)
		for key, actual := range mio[index].symbolDetails {
			expected, ok := goSymbols[key]
			if !ok {
				name := strings.SplitN(key, ":", 2)[0]
				for goKey, goType := range goSymbols {
					if strings.HasPrefix(goKey, name+":") {
						expected, ok = goType, true
						break
					}
				}
			}
			if !ok {
				t.Fatalf("case %d missing Go declaration type %s=%s", index, key, actual)
			}
			actual = strings.ReplaceAll(actual, "case.", "")
			if actual != expected {
				t.Fatalf("case %d declaration %s type mismatch: Go=%s Mio=%s", index, key, expected, actual)
			}
		}
		goSignatureInfo := goSignatures(program, checker)
		for name, expected := range goSignatureInfo {
			actual, ok := mio[index].signatureInfo[name]
			if !ok || strings.ReplaceAll(actual, "case.", "") != expected {
				t.Fatalf("case %d signature %s mismatch: Go=%s Mio=%s", index, name, expected, actual)
			}
		}
		goSubstitutionInfo := goSubstitutions(program, checker)
		for key, expected := range goSubstitutionInfo {
			actual, ok := mio[index].substitutionDetails[key]
			if !ok || strings.ReplaceAll(actual, "case.", "") != expected {
				t.Fatalf("case %d substitution %s mismatch: Go=%s Mio=%s", index, key, expected, actual)
			}
		}
	}
}

// 测试结构化诊断代码、类型参数与源码位置一致
func TestMioTypecheckerDiagnosticDifferential(t *testing.T) {
	sources := []string{
		"kaku a: int32 = 1; kaku b: int64 = 2; chant(a + b);",
		"kaku value: int32 = 1.0;",
	}
	mio, _ := runMioTypecheck(t, sources)
	for index, source := range sources {
		_, _, err := runGoTypecheckDetailed(source)
		var goDiagnostic *diagnostic.Diagnostic
		if !errors.As(err, &goDiagnostic) {
			t.Fatalf("case %d Go error is not structured: %v", index, err)
		}
		if len(mio[index].errorDetails) == 0 {
			t.Fatalf("case %d Mio error is not structured", index)
		}
		actual := mio[index].errorDetails[0]
		label, ok := goDiagnostic.PrimaryLabel()
		if !ok {
			t.Fatalf("case %d Go diagnostic has no primary label", index)
		}
		if actual.code != goDiagnostic.Code || actual.start != label.Span.Start.Offset || actual.end != label.Span.End.Offset || actual.expected != goDiagnostic.Expected || actual.actual != goDiagnostic.Actual {
			t.Fatalf("case %d diagnostic mismatch: Go=%+v label=%+v Mio=%+v", index, goDiagnostic, label, actual)
		}
	}
}

func goConstantText(value constant.Value) string {
	switch value.Type {
	case "float32":
		return strconv.FormatFloat(float64(math.Float32frombits(value.Float32Bits)), 'g', -1, 32)
	case "float64":
		return strconv.FormatFloat(math.Float64frombits(value.Float64Bits), 'g', -1, 64)
	case "bool":
		if value.Boolean {
			return "hai"
		}
		return "iie"
	case "string":
		return value.String
	default:
		return strconv.FormatInt(value.Integer, 10)
	}
}

// 测试 Go 与纯 Mio 常量最终类型和值一致
func TestMioConstantEvaluatorDifferential(t *testing.T) {
	sources := []string{
		"teisu answer: int64 = next + 1; teisu next: int64 = 41;",
		"teisu minimum: int64 = -9223372036854775808; teisu zero: int64 = -0;",
		"teisu small: uint8 = uint8(int32(255)); teisu wrapped: int8 = 127 + 1;",
		"teisu precise: float32 = 1.000000059604644775390625; teisu ratio: float64 = 1.0 / 10.0;",
		"teisu truth = (1 < 2) && hai; teisu text = \"Mio\";",
	}
	mio, _ := runMioTypecheck(t, sources)
	for index, source := range sources {
		goValues, err := runGoConstants(source)
		if err != nil || mio[index].errors != 0 {
			t.Fatalf("case %d failed: Go=%v Mio=%+v", index, err, mio[index])
		}
		for name, expected := range goValues {
			actual, ok := mio[index].constants[name]
			if !ok {
				t.Fatalf("case %d missing constant %s", index, name)
			}
			if actual.state != 2 || actual.typeName != expected.Type || actual.value != goConstantText(expected) {
				t.Fatalf("case %d constant %s mismatch: Go=%+v Mio=%+v", index, name, expected, actual)
			}
		}
	}
}

// 测试固定种子的合法与非法数值程序差分
func TestMioTypecheckerRandomDifferential(t *testing.T) {
	random := rand.New(rand.NewSource(20261016))
	sources := make([]string, 0, 48)
	for index := 0; index < 32; index++ {
		left, right := random.Intn(127), random.Intn(127)
		sources = append(sources, fmt.Sprintf("kaku left: int8 = %d; kaku right: int8 = %d; chant(left + right);", left, right))
	}
	for index := 0; index < 16; index++ {
		sources = append(sources, fmt.Sprintf("kaku left: int32 = %d; kaku right: int64 = %d; chant(left + right);", random.Intn(1000), random.Intn(1000)))
	}
	actual, _ := runMioTypecheck(t, sources)
	for index, source := range sources {
		if (runGoTypecheck(source) != nil) != (actual[index].errors != 0) {
			t.Fatalf("case %d differential mismatch: %+v\n%s", index, actual[index], source)
		}
	}
}

// 测试数值边界的独立预期结果
func TestMioTypecheckerNumericBoundaries(t *testing.T) {
	sources := []string{
		"kaku value: int8 = 127;", "kaku value: int8 = 128;",
		"kaku value: uint8 = 255;", "kaku value: uint8 = 256;",
		"kaku value: int16 = 32767;", "kaku value: int16 = 32768;",
		"kaku value: uint16 = 65535;", "kaku value: uint16 = 65536;",
		"kaku value: int64 = 9223372036854775807;", "kaku value: int64 = 9223372036854775808;",
		"kaku value: int64 = -9223372036854775808;", "kaku value: int64 = -9223372036854775809;",
		"kaku value: int64 = -0;", "kaku value: int64 = -((9223372036854775808));",
	}
	actual, _ := runMioTypecheck(t, sources)
	wantFailure := []bool{false, true, false, true, false, true, false, true, false, true, false, true, false, false}
	for index := range actual {
		if (actual[index].errors != 0) != wantFailure[index] {
			t.Fatalf("boundary %d errors=%d want failure=%v", index, actual[index].errors, wantFailure[index])
		}
		if (runGoTypecheck(sources[index]) != nil) != wantFailure[index] {
			t.Fatalf("boundary %d Go result mismatch", index)
		}
	}
}

// 测试纯 Mio TypeChecker 的 Mochi 往返执行
func TestMioTypecheckerMochiRoundTrip(t *testing.T) {
	results, artifact := runMioTypecheck(t, []string{"waza identity(value: int64) -> int64 { kaesu value; }"})
	if len(results) != 1 || results[0].errors != 0 || artifact == 0 {
		t.Fatalf("round trip result=%+v artifact=%d", results, artifact)
	}
}

type checkedSourcePackage struct {
	path         string
	directory    string
	dependencies []string
}

func selfTypecheckHarness(t testing.TB, packages []checkedSourcePackage) string {
	t.Helper()
	root := typecheckRoot(t)
	var source strings.Builder
	source.WriteString("yobu \"kiso/array\";\nyobu \"kiso/fs\";\nyobu \"selfhost/parser\";\nyobu \"selfhost/resolver\";\nyobu \"selfhost/typecheck\";\n")
	indexes := make(map[string]int)
	for packageIndex, item := range packages {
		indexes[item.path] = packageIndex
		entries, err := os.ReadDir(filepath.Join(root, item.directory))
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".mio") && !strings.HasSuffix(entry.Name(), "_test.mio") {
				paths = append(paths, filepath.Join(root, item.directory, entry.Name()))
			}
		}
		fmt.Fprintf(&source, "kaku files%d: []resolver.SourceFile = [];\n", packageIndex)
		for fileIndex, path := range paths {
			fmt.Fprintf(&source, "kaku loaded%d_%d = fs.read_all(%s);\n", packageIndex, fileIndex, strconv.Quote(path))
			fmt.Fprintf(&source, "kaku parsed%d_%d = parser.parse(loaded%d_%d.data);\n", packageIndex, fileIndex, packageIndex, fileIndex)
			fmt.Fprintf(&source, "chant(len(parsed%d_%d.errors));\n", packageIndex, fileIndex)
			fmt.Fprintf(&source, "array.push(files%d, resolver.SourceFile { file_id: %d, path: %s, ast: parsed%d_%d.ast, root: parsed%d_%d.root });\n", packageIndex, fileIndex, strconv.Quote(path), packageIndex, fileIndex, packageIndex, fileIndex)
		}
		fmt.Fprintf(&source, "kaku resolved_dependencies%d: []resolver.ResolvedPackage = [];\n", packageIndex)
		fmt.Fprintf(&source, "kaku type_dependencies%d: []typecheck.TypeCheckResult = [];\n", packageIndex)
		for _, dependency := range item.dependencies {
			dependencyIndex, ok := indexes[dependency]
			if !ok {
				t.Fatalf("package %s dependency %s is not ordered", item.path, dependency)
			}
			fmt.Fprintf(&source, "array.push(resolved_dependencies%d, package%d); array.push(type_dependencies%d, checked%d);\n", packageIndex, dependencyIndex, packageIndex, dependencyIndex)
		}
		fmt.Fprintf(&source, "kaku package%d = resolver.resolve_package(%s, files%d, resolved_dependencies%d);\n", packageIndex, strconv.Quote(item.path), packageIndex, packageIndex)
		fmt.Fprintf(&source, "kaku checked%d = typecheck.check_package(package%d, type_dependencies%d);\n", packageIndex, packageIndex, packageIndex)
		fmt.Fprintf(&source, "chant(len(package%d.errors)); chant(len(checked%d.errors)); chant(len(checked%d.types)); chant(len(checked%d.node_types)); chant(len(checked%d.symbol_types));\n", packageIndex, packageIndex, packageIndex, packageIndex, packageIndex)
	}
	return source.String()
}

// 测试纯 Mio TypeChecker 检查自身、前三阶段及 Bunko 源码
func TestMioTypecheckerSelfAndBunkoSources(t *testing.T) {
	if testing.Short() {
		t.Skip("大型纯 Mio 自检由 Full 测试覆盖")
	}
	packages := []checkedSourcePackage{
		{path: "kiso/array", directory: "bunko/kiso/array"},
		{path: "kiso/bytes", directory: "bunko/kiso/bytes", dependencies: []string{"kiso/array"}},
		{path: "kiso/utf8", directory: "bunko/kiso/utf8", dependencies: []string{"kiso/array", "kiso/bytes"}},
		{path: "kiso/string", directory: "bunko/kiso/string", dependencies: []string{"kiso/bytes", "kiso/utf8"}},
		{path: "kiso/math", directory: "bunko/kiso/math"},
		{path: "kiso/convert", directory: "bunko/kiso/convert", dependencies: []string{"kiso/array", "kiso/bytes", "kiso/math", "kiso/string"}},
		{path: "kiso/io", directory: "bunko/kiso/io", dependencies: []string{"kiso/array", "kiso/bytes"}},
		{path: "kiso/fs", directory: "bunko/kiso/fs", dependencies: []string{"kiso/array", "kiso/io"}},
		{path: "kiso/path", directory: "bunko/kiso/path", dependencies: []string{"kiso/array", "kiso/string"}},
		{path: "kiso/encoding/binary", directory: "bunko/kiso/encoding/binary", dependencies: []string{"kiso/array"}},
		{path: "kiso/testing", directory: "bunko/kiso/testing"},
		{path: "selfhost/lexer", directory: "selfhost/lexer", dependencies: []string{"kiso/array", "kiso/string", "kiso/utf8"}},
		{path: "selfhost/parser", directory: "selfhost/parser", dependencies: []string{"kiso/array", "kiso/convert", "kiso/string", "kiso/utf8", "selfhost/lexer"}},
		{path: "selfhost/resolver", directory: "selfhost/resolver", dependencies: []string{"kiso/array", "kiso/string", "selfhost/parser"}},
		{path: "selfhost/typecheck", directory: "selfhost/typecheck", dependencies: []string{"kiso/array", "kiso/convert", "kiso/string", "selfhost/parser", "selfhost/resolver"}},
	}
	root := typecheckRoot(t)
	directory := t.TempDir()
	harness := filepath.Join(directory, "self_typecheck.mio")
	if err := os.WriteFile(harness, []byte(selfTypecheckHarness(t, packages)), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 500_000_000_000}); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(&output)
	read := func() int {
		if !scanner.Scan() {
			t.Fatal("纯 Mio TypeChecker 自检输出提前结束")
		}
		value, convertErr := strconv.Atoi(scanner.Text())
		if convertErr != nil {
			t.Fatal(convertErr)
		}
		return value
	}
	for _, item := range packages {
		entries, readErr := os.ReadDir(filepath.Join(root, item.directory))
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".mio") && !strings.HasSuffix(entry.Name(), "_test.mio") && read() != 0 {
				t.Fatalf("%s/%s 纯 Mio Parser 失败", item.directory, entry.Name())
			}
		}
		resolveErrors, typeErrors, types, nodeTypes, symbolTypes := read(), read(), read(), read(), read()
		if resolveErrors != 0 || typeErrors != 0 {
			t.Fatalf("package %s resolve errors=%d type errors=%d types=%d node types=%d symbol types=%d", item.path, resolveErrors, typeErrors, types, nodeTypes, symbolTypes)
		}
		if types == 0 || nodeTypes == 0 || symbolTypes == 0 {
			t.Fatalf("package %s incomplete type information", item.path)
		}
	}
}

func benchmarkMioTypechecker(b *testing.B, size int) {
	var source strings.Builder
	for index := 0; source.Len() < size; index++ {
		fmt.Fprintf(&source, "kaku value%d: int64 = %d;\n", index, index)
	}
	root := typecheckRoot(b)
	directory := b.TempDir()
	input := filepath.Join(directory, "input.mio")
	if err := os.WriteFile(input, []byte(source.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	harness := filepath.Join(directory, "harness.mio")
	if err := os.WriteFile(harness, []byte(typecheckHarness([]string{input})), 0o644); err != nil {
		b.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		b.Fatal(err)
	}
	defer environment.Close()
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		b.Fatal(err)
	}
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		b.Fatal(err)
	}
	var metricsOutput bytes.Buffer
	if err := environment.RunWithOptions(program, &metricsOutput, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
		b.Fatal(err)
	}
	metricScanner := bufio.NewScanner(&metricsOutput)
	metrics := make([]int, 0, 11)
	for metricScanner.Scan() {
		value, convertErr := strconv.Atoi(metricScanner.Text())
		if convertErr != nil {
			b.Fatal(convertErr)
		}
		metrics = append(metrics, value)
	}
	if len(metrics) != 11 {
		b.Fatalf("metrics=%v", metrics)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := environment.RunWithOptions(program, io.Discard, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(metrics[8]), "tokens/op")
	b.ReportMetric(float64(metrics[9]), "AST-nodes/op")
	b.ReportMetric(float64(metrics[10]), "symbols/op")
	b.ReportMetric(float64(metrics[3]), "types/op")
	b.ReportMetric(float64(artifact.Len()), "Mochi-bytes")
}

// BenchmarkMioTypechecker 测量纯 Mio 完整静态检查链路
func BenchmarkMioTypechecker(b *testing.B) {
	for _, item := range []struct {
		name string
		size int
	}{{"1KiB", 1024}, {"64KiB", 64 * 1024}} {
		b.Run(item.name, func(b *testing.B) { benchmarkMioTypechecker(b, item.size) })
	}
}
