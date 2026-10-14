package incremental

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"sort"

	"Mio/internal/ast"
)

type fingerprinter struct {
	first  uint64
	second uint64
	module string
	known  map[string]struct{}
	calls  map[FunctionID]struct{}
}

func newFingerprinter() *fingerprinter {
	return &fingerprinter{
		first:  14695981039346656037,
		second: 7809847782465536322,
	}
}

func (f *fingerprinter) text(value string) {
	f.number(uint64(len(value)))
	for index := 0; index < len(value); index++ {
		f.mix(value[index])
	}
}

func (f *fingerprinter) number(value uint64) {
	for shift := 0; shift < 64; shift += 8 {
		f.mix(byte(value >> shift))
	}
}

func (f *fingerprinter) mix(value byte) {
	f.first ^= uint64(value)
	f.first *= 1099511628211
	f.second ^= uint64(value) + 0x9d
	f.second *= 14029467366897019727
}

func (f *fingerprinter) sum() string {
	var sum [16]byte
	binary.LittleEndian.PutUint64(sum[:8], f.first)
	binary.LittleEndian.PutUint64(sum[8:], f.second)
	return hex.EncodeToString(sum[:])
}

func signatureFingerprint(fn *ast.FunctionStatement) string {
	f := newFingerprinter()
	f.text("signature-v1")
	f.text(fn.Name.Name)
	f.number(uint64(len(fn.Parameters)))
	for _, parameter := range fn.TypeParams {
		f.text(parameter)
	}
	for _, parameter := range fn.Parameters {
		f.typeRef(parameter.DeclaredType)
	}
	f.typeRef(fn.ReturnType)
	return f.sum()
}

func functionBodyAnalysis(fn *ast.FunctionStatement, module string, known map[string]struct{}) (string, []FunctionID) {
	f := newFingerprinter()
	f.module = module
	f.known = known
	f.calls = make(map[FunctionID]struct{})
	f.text("body-v1")
	for _, parameter := range fn.Parameters {
		f.text(parameter.Name)
	}
	f.statement(fn.Body)
	return f.sum(), sortedCalls(f.calls)
}

func sortedCalls(calls map[FunctionID]struct{}) []FunctionID {
	dependencies := make([]FunctionID, 0, len(calls))
	for dependency := range calls {
		dependencies = append(dependencies, dependency)
	}
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].Module != dependencies[j].Module {
			return dependencies[i].Module < dependencies[j].Module
		}
		return dependencies[i].Name < dependencies[j].Name
	})
	return dependencies
}

func topLevelAnalysis(program *ast.Program, module string, known map[string]struct{}) (string, []FunctionID) {
	f := newFingerprinter()
	f.module = module
	f.known = known
	f.calls = make(map[FunctionID]struct{})
	f.text("top-level-v1")
	for _, statement := range program.Statements {
		if _, ok := statement.(*ast.FunctionStatement); ok {
			continue
		}
		f.statement(statement)
	}
	return f.sum(), sortedCalls(f.calls)
}

func combineFingerprint(parts ...string) string {
	f := newFingerprinter()
	for _, part := range parts {
		f.text(part)
	}
	return f.sum()
}

func (f *fingerprinter) typeRef(ref *ast.TypeRef) {
	if ref == nil {
		f.text("nil-type")
		return
	}
	f.text(ref.Name)
	f.typeRef(ref.Element)
	for _, arg := range ref.Args {
		f.typeRef(arg)
	}
	for _, param := range ref.Params {
		f.typeRef(param)
	}
	f.typeRef(ref.Result)
}

func (f *fingerprinter) statement(statement ast.Statement) {
	if statement == nil {
		f.text("nil-statement")
		return
	}
	switch node := statement.(type) {
	case *ast.ExpressionStatement:
		f.text("expression")
		f.expression(node.Expr)
	case *ast.LetStatement:
		f.text("let")
		f.text(node.Name.Name)
		f.typeRef(node.Name.DeclaredType)
		f.expression(node.Value)
	case *ast.AssignStatement:
		f.text("assign")
		f.text(node.Name.Name)
		f.expression(node.Value)
	case *ast.IndexAssignStatement:
		f.text("index-assign")
		f.expression(node.Target)
		f.expression(node.Value)
	case *ast.BlockStatement:
		f.text("block")
		for _, child := range node.Statements {
			f.statement(child)
		}
	case *ast.IfStatement:
		f.text("if")
		f.expression(node.Condition)
		f.statement(node.Consequence)
		f.statement(node.Alternative)
	case *ast.WhileStatement:
		f.text("while")
		f.expression(node.Condition)
		f.statement(node.Body)
	case *ast.BreakStatement:
		f.text("break")
	case *ast.ContinueStatement:
		f.text("continue")
	case *ast.ReturnStatement:
		f.text("return")
		f.expression(node.Value)
	case *ast.FunctionStatement:
		f.text("function")
		f.text(signatureFingerprint(node))
		for _, parameter := range node.Parameters {
			f.text(parameter.Name)
		}
		f.statement(node.Body)
	case *ast.StructStatement:
		f.text("struct")
		f.text(node.Name.Name)
		for _, parameter := range node.TypeParams {
			f.text(parameter)
		}
		for _, field := range node.Fields {
			f.text(field.Name)
			f.typeRef(field.DeclaredType)
		}
	case *ast.EnumStatement:
		f.text("enum")
		f.text(node.Name.Name)
		for _, parameter := range node.TypeParams {
			f.text(parameter)
		}
		for _, variant := range node.Variants {
			f.text(variant.Name)
			for _, field := range variant.Fields {
				f.typeRef(field)
			}
		}
	case *ast.MatchStatement:
		f.text("match")
		f.expression(node.Value)
		for _, arm := range node.Arms {
			f.text(arm.Variant)
			for _, binding := range arm.Bindings {
				f.text(binding.Name)
			}
			f.statement(arm.Body)
		}
	default:
		f.text("unknown-statement")
	}
}

func (f *fingerprinter) expression(expression ast.Expression) {
	if expression == nil {
		f.text("nil-expression")
		return
	}
	switch node := expression.(type) {
	case *ast.IntegerLiteral:
		f.text("int")
		f.number(uint64(node.Value))
	case *ast.FloatLiteral:
		f.text("float")
		f.number(math.Float64bits(node.Value))
	case *ast.BooleanLiteral:
		if node.Value {
			f.text("true")
		} else {
			f.text("false")
		}
	case *ast.StringLiteral:
		f.text("string")
		f.text(node.Value)
	case *ast.NilLiteral:
		f.text("nil")
	case *ast.Identifier:
		f.text("identifier")
		f.text(node.Name)
	case *ast.GroupedExpression:
		f.text("group")
		f.expression(node.Expr)
	case *ast.PrefixExpression:
		f.text("prefix")
		f.text(string(node.Operator))
		f.expression(node.Right)
	case *ast.InfixExpression:
		f.text("infix")
		f.text(string(node.Operator))
		f.expression(node.Left)
		f.expression(node.Right)
	case *ast.CallExpression:
		f.text("call")
		if identifier, ok := calledIdentifier(node.Function); ok && f.calls != nil {
			if _, exists := f.known[identifier.Name]; exists {
				f.calls[FunctionID{Module: f.module, Name: identifier.Name}] = struct{}{}
			}
		}
		f.expression(node.Function)
		for _, argument := range node.Arguments {
			f.expression(argument)
		}
	case *ast.TypeApplyExpression:
		f.text("type-apply")
		f.expression(node.Function)
		for _, arg := range node.Args {
			f.typeRef(arg)
		}
	case *ast.ConversionExpression:
		f.text("conversion")
		f.text(node.Target)
		for _, argument := range node.Arguments {
			f.expression(argument)
		}
	case *ast.ArrayLiteral:
		f.text("array")
		for _, element := range node.Elements {
			f.expression(element)
		}
	case *ast.IndexExpression:
		f.text("index")
		f.expression(node.Left)
		f.expression(node.Index)
	case *ast.FieldExpression:
		f.text("field")
		f.expression(node.Left)
		f.text(node.Name)
	case *ast.StructLiteral:
		f.text("struct-literal")
		f.text(node.TypeName)
		for _, arg := range node.TypeArgs {
			f.typeRef(arg)
		}
		for _, field := range node.Fields {
			f.text(field.Name)
			f.expression(field.Value)
		}
	case *ast.EnumLiteral:
		f.text("enum-literal")
		f.text(node.TypeName)
		f.text(node.Variant)
		for _, value := range node.Values {
			f.expression(value)
		}
	default:
		f.text("unknown-expression")
	}
}

func calledIdentifier(expression ast.Expression) (*ast.Identifier, bool) {
	if apply, ok := expression.(*ast.TypeApplyExpression); ok {
		expression = apply.Function
	}
	identifier, ok := expression.(*ast.Identifier)
	return identifier, ok
}
