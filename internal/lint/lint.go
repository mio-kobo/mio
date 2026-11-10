// Package lint implements the optional Mio static-analysis rules behind `mio lint`.
//
// Lint findings are warnings, never compiler errors: a file must first pass the
// same pipeline as `mio check` (load, constant folding, name resolution, type
// checking); otherwise Run returns the compiler's error unchanged and no lint
// rules run. Every rule is deliberately conservative: it reports only patterns
// that are certain from the syntax or from the resolver's bindings, and prefers
// missing a problem over reporting a false one.
//
// Rules (stable identifiers):
//
//	unused-variable    A function-local `kaku` variable that is never referenced
//	                   again (assignments count as references). Names that start
//	                   with an underscore are exempt. Parameters, match bindings
//	                   and top-level variables are never reported.
//	unused-import      A `yobu` import whose package alias never appears as an
//	                   identifier in the file. A local name equal to the alias
//	                   counts as a use, so this rule can miss but never
//	                   misreport.
//	unreachable-code   The first statement after `kaesu`, `nukeru` or `tsuzuku`
//	                   in the same block.
//	constant-condition A `moshi` condition (optionally parenthesized) that is the
//	                   literal `hai` or `iie`, or an `aida` loop whose condition is
//	                   the literal `iie`. `aida (hai)` is the idiomatic infinite
//	                   loop and is not reported.
package lint

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"Mio/internal/ast"
	"Mio/internal/bunko"
	"Mio/internal/constant"
	"Mio/internal/i18n"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
	"Mio/mma"
)

// Finding is one lint warning.
type Finding struct {
	Rule        string `json:"rule"`
	Message     string `json:"message"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	EndLine     int    `json:"endLine"`
	EndColumn   int    `json:"endColumn"`
	messageKey  i18n.Key
	messageArgs []any
}

// LocalizedMessage renders a finding for a human reader.
func (f Finding) LocalizedMessage(language i18n.Language) string {
	if f.messageKey == "" {
		return f.Message
	}
	return i18n.Format(language, f.messageKey, f.messageArgs...)
}

// RuleNames lists every rule identifier in documentation order.
var RuleNames = []string{"unused-variable", "unused-import", "unreachable-code", "constant-condition"}

// Run lints the Mio source file at path. The returned error is a compiler error
// (the file does not pass `mio check`); findings are only produced for valid files.
func Run(path, bunkoRoot string, registry *mma.ModuleRegistry) ([]Finding, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	loaded, err := bunko.LoadProgramWithRegistry(path, bunkoRoot, registry)
	if err != nil {
		return nil, err
	}
	// Syntactic rules inspect the user's own tokens, so they use a fresh parse
	// that is not affected by constant folding or package rewriting.
	lex, err := lexer.NewChecked(string(text), token.CurrentVersion)
	if err != nil {
		return nil, err
	}
	rootSyntax, err := parser.New(lex).ParseProgram()
	if err != nil {
		return nil, err
	}

	program := loaded.AST
	if err := constant.Fold(program); err != nil {
		return nil, err
	}
	names := resolver.New()
	if err := names.ResolveProgram(program); err != nil {
		return nil, err
	}
	if err := typecheck.NewWithResolver(names).CheckProgram(program); err != nil {
		return nil, err
	}

	display := path
	var findings []Finding
	findings = append(findings, unreachableCode(display, rootSyntax)...)
	findings = append(findings, constantConditions(display, rootSyntax)...)
	findings = append(findings, unusedImports(display, rootSyntax, loaded.Imports)...)
	findings = append(findings, unusedVariables(display, path, program, names, loaded)...)
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Rule < b.Rule
	})
	return findings, nil
}

func finding(rule, file string, span ast.Span, key i18n.Key, args ...any) Finding {
	return Finding{
		Rule:        rule,
		Message:     i18n.Format(i18n.English, key, args...),
		messageKey:  key,
		messageArgs: args,
		File:        file,
		Line:        span.Start.Line,
		Column:      span.Start.Column,
		EndLine:     span.End.Line,
		EndColumn:   span.End.Column,
	}
}

var (
	spanType     = reflect.TypeOf(ast.Span{})
	positionType = reflect.TypeOf(token.Position{})
)

// walk visits every AST node reachable from root depth first. The callback
// returns false to skip a node's children.
func walk(root any, visit func(node any) bool) {
	var step func(v reflect.Value)
	step = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Interface, reflect.Pointer:
			if v.IsNil() {
				return
			}
			if v.Kind() == reflect.Pointer {
				if !visit(v.Interface()) {
					return
				}
			}
			step(v.Elem())
		case reflect.Struct:
			if v.Type() == spanType || v.Type() == positionType {
				return
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					step(v.Field(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				step(v.Index(i))
			}
		}
	}
	step(reflect.ValueOf(root))
}

// ---- unreachable-code ----

func unreachableCode(file string, program *ast.Program) []Finding {
	var out []Finding
	walk(program, func(node any) bool {
		var statements []ast.Statement
		switch n := node.(type) {
		case *ast.BlockStatement:
			statements = n.Statements
		default:
			return true
		}
		for i, statement := range statements {
			keyword := ""
			switch statement.(type) {
			case *ast.ReturnStatement:
				keyword = "kaesu"
			case *ast.BreakStatement:
				keyword = "nukeru"
			case *ast.ContinueStatement:
				keyword = "tsuzuku"
			}
			if keyword != "" && i+1 < len(statements) {
				out = append(out, finding("unreachable-code", file, statements[i+1].Span(),
					i18n.KeyLintFindingUnreachable, keyword))
				break
			}
		}
		return true
	})
	return out
}

// ---- constant-condition ----

func literalBool(expr ast.Expression) (value, ok bool) {
	for {
		switch n := expr.(type) {
		case *ast.GroupedExpression:
			expr = n.Expr
		case *ast.BooleanLiteral:
			return n.Value, true
		default:
			return false, false
		}
	}
}

func constantConditions(file string, program *ast.Program) []Finding {
	var out []Finding
	spell := func(v bool) string {
		if v {
			return "hai"
		}
		return "iie"
	}
	walk(program, func(node any) bool {
		switch n := node.(type) {
		case *ast.IfStatement:
			if v, ok := literalBool(n.Condition); ok {
				out = append(out, finding("constant-condition", file, n.Condition.Span(),
					i18n.KeyLintFindingIf, spell(v)))
			}
		case *ast.WhileStatement:
			if v, ok := literalBool(n.Condition); ok && !v {
				out = append(out, finding("constant-condition", file, n.Condition.Span(),
					i18n.KeyLintFindingLoop))
			}
		}
		return true
	})
	return out
}

// ---- unused-import ----

// referencedNames returns every name the file mentions, including package
// qualifiers that only occur as text in type positions and literals (for
// example `lexer.Position`). Every string field of every node other than
// import declarations contributes both the whole string and its qualifier
// prefix, so the rule can only err towards "used".
func referencedNames(program *ast.Program) map[string]struct{} {
	names := make(map[string]struct{})
	add := func(text string) {
		names[text] = struct{}{}
		for _, separator := range []string{".", "::"} {
			if index := strings.Index(text, separator); index > 0 {
				names[text[:index]] = struct{}{}
			}
		}
	}
	var step func(v reflect.Value)
	step = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Interface, reflect.Pointer:
			if v.IsNil() {
				return
			}
			if _, isImport := v.Interface().(*ast.ImportStatement); isImport {
				return
			}
			step(v.Elem())
		case reflect.String:
			add(v.String())
		case reflect.Struct:
			if v.Type() == spanType || v.Type() == positionType {
				return
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					step(v.Field(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				step(v.Index(i))
			}
		case reflect.Map:
			for _, key := range v.MapKeys() {
				step(key)
				step(v.MapIndex(key))
			}
		}
	}
	step(reflect.ValueOf(program))
	return names
}

func unusedImports(file string, program *ast.Program, imports []bunko.Import) []Finding {
	if len(imports) == 0 {
		return nil
	}
	used := referencedNames(program)
	var out []Finding
	for _, statement := range program.Statements {
		declaration, ok := statement.(*ast.ImportStatement)
		if !ok {
			continue
		}
		alias := ""
		for _, imported := range imports {
			if imported.Path == declaration.Path {
				alias = imported.Alias
				break
			}
		}
		if alias == "" {
			continue
		}
		if _, ok := used[alias]; !ok {
			out = append(out, finding("unused-import", file, declaration.Range,
				i18n.KeyLintFindingImport, strconv.Quote(declaration.Path), alias))
		}
	}
	return out
}

// ---- unused-variable ----

func unusedVariables(file, path string, program *ast.Program, names *resolver.Resolver, loaded *bunko.Program) []Finding {
	rootInfo, err := os.Stat(path)
	if err != nil {
		return nil
	}
	isRoot := func(statement ast.Statement) bool {
		source, ok := loaded.SourceOf(statement)
		if !ok {
			return false
		}
		info, err := os.Stat(filepath.Clean(source))
		return err == nil && os.SameFile(info, rootInfo)
	}

	counts := make(map[uint64]int)
	walk(program, func(node any) bool {
		if identifier, ok := node.(*ast.Identifier); ok {
			if symbol, ok := names.BindingOf(identifier); ok {
				counts[symbol.ID]++
			}
		}
		return true
	})

	var out []Finding
	for _, statement := range program.Statements {
		function, ok := statement.(*ast.FunctionStatement)
		if !ok || function.Body == nil || !isRoot(statement) {
			continue
		}
		walk(function.Body, func(node any) bool {
			let, ok := node.(*ast.LetStatement)
			if !ok || let.Name == nil || len(let.Name.Name) == 0 || let.Name.Name[0] == '_' {
				return true
			}
			symbol, bound := names.BindingOf(let.Name)
			if bound && symbol.Kind == resolver.SymbolVariable && counts[symbol.ID] <= 1 {
				out = append(out, finding("unused-variable", file, let.Name.Span(),
					i18n.KeyLintFindingVariable, strconv.Quote(let.Name.Name)))
			}
			return true
		})
	}
	return out
}
