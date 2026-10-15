package bunko

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/token"
	"Mio/mma"
)

// Program 保存合并后的程序及包编译统计
type Program struct {
	AST              *ast.Program
	PackagesCompiled int
	SourcesParsed    int
	Tests            []TestFunction
}

// TestFunction 保存测试声明及其源码位置
type TestFunction struct {
	Name     string
	Path     string
	Source   string
	Function *ast.FunctionStatement
}

type packageUnit struct {
	path       string
	program    *ast.Program
	imports    map[string]string
	declared   map[string]string
	exported   map[string]string
	statements []ast.Statement
}

type programLoader struct {
	loader           *Loader
	registry         *mma.ModuleRegistry
	visiting         map[string]bool
	loaded           map[string]*packageUnit
	nativeLoaded     map[string]bool
	nativeStatements []ast.Statement
	ordered          []*packageUnit
	sources          int
}

// LoadProgram 读取单文件或目录程序，并按需合并 Bunko 包
func LoadProgram(inputPath, bunkoRoot string) (*Program, error) {
	return LoadProgramWithRegistry(inputPath, bunkoRoot, nil)
}

// LoadProgramWithRegistry 读取程序并解析显式注册的原生模块
func LoadProgramWithRegistry(inputPath, bunkoRoot string, registry *mma.ModuleRegistry) (*Program, error) {
	root, err := parseInput(inputPath)
	if err != nil {
		return nil, err
	}
	return loadRootProgram(root, nil, "", bunkoRoot, registry)
}

// LoadTestProgramWithRegistry 读取同包普通源码与测试源码
func LoadTestProgramWithRegistry(inputPath, bunkoRoot string, registry *mma.ModuleRegistry) (*Program, error) {
	root, tests, err := parseTestInput(inputPath)
	if err != nil {
		return nil, err
	}
	rootPath := packageImportPath(inputPath, bunkoRoot)
	return loadRootProgram(root, tests, rootPath, bunkoRoot, registry)
}

func loadRootProgram(root *ast.Program, tests []TestFunction, rootPath, bunkoRoot string, registry *mma.ModuleRegistry) (*Program, error) {
	state := &programLoader{
		loader: New(bunkoRoot), registry: registry, visiting: make(map[string]bool),
		loaded: make(map[string]*packageUnit), nativeLoaded: make(map[string]bool),
	}
	rootUnit := &packageUnit{path: rootPath, program: root, imports: make(map[string]string), declared: declarations(root, rootPath)}
	rootUnit.exported = exports(root, rootPath)
	for _, statement := range root.Statements {
		if imported, ok := statement.(*ast.ImportStatement); ok {
			if previous, exists := rootUnit.imports[imported.Alias]; exists && previous != imported.Path {
				return nil, fmt.Errorf("package alias %q is used by both %q and %q", imported.Alias, previous, imported.Path)
			}
			rootUnit.imports[imported.Alias] = imported.Path
			if err := state.loadDependency(imported.Path, nil); err != nil {
				return nil, err
			}
		}
	}
	if err := state.rewrite(rootUnit, rootPath != ""); err != nil {
		return nil, err
	}
	statements := make([]ast.Statement, 0, len(state.nativeStatements))
	statements = append(statements, state.nativeStatements...)
	for _, unit := range state.ordered {
		statements = append(statements, unit.statements...)
	}
	statements = append(statements, rootUnit.statements...)
	merged := &ast.Program{Statements: statements, Range: root.Range}
	return &Program{AST: merged, PackagesCompiled: len(state.ordered), SourcesParsed: state.sources, Tests: tests}, nil
}

func packageImportPath(inputPath, bunkoRoot string) string {
	root, rootErr := filepath.Abs(bunkoRoot)
	directory, directoryErr := filepath.Abs(inputPath)
	if rootErr != nil || directoryErr != nil {
		return ""
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(relative)
}

func (s *programLoader) loadDependency(importPath string, chain []string) error {
	if functions, ok := s.registry.Module(importPath); ok {
		if !s.nativeLoaded[importPath] {
			s.nativeLoaded[importPath] = true
			for _, function := range functions {
				if function.Public {
					s.nativeStatements = append(s.nativeStatements, nativeFunctionStatement(function))
				}
			}
		}
		return nil
	}
	_, err := s.load(importPath, chain)
	if err != nil {
		return fmt.Errorf("module %q not found: %w", importPath, err)
	}
	return nil
}

func nativeFunctionStatement(function mma.RegisteredFunction) *ast.FunctionStatement {
	parameters := make([]*ast.Identifier, len(function.Signature.Parameters))
	for index, parameter := range function.Signature.Parameters {
		parameters[index] = &ast.Identifier{Name: fmt.Sprintf("arg%d", index), DeclaredType: nativeTypeRef(parameter)}
	}
	return &ast.FunctionStatement{
		Name: &ast.Identifier{Name: function.BindingKey}, Parameters: parameters,
		ReturnType: nativeTypeRef(function.Signature.Return), Body: &ast.BlockStatement{},
		Exported: true, Native: true,
	}
}

func nativeTypeRef(kind mma.Type) *ast.TypeRef {
	if kind == mma.TypeBytes {
		return &ast.TypeRef{Name: "[]", Element: &ast.TypeRef{Name: "uint8"}}
	}
	return &ast.TypeRef{Name: string(kind)}
}

func parseInput(inputPath string) (*ast.Program, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", inputPath, err)
	}
	var paths []string
	if info.IsDir() {
		entries, err := os.ReadDir(inputPath)
		if err != nil {
			return nil, fmt.Errorf("read source directory %q: %w", inputPath, err)
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && isRegularSource(entry.Name()) {
				paths = append(paths, filepath.Join(inputPath, entry.Name()))
			}
		}
		sort.Strings(paths)
		if len(paths) == 0 {
			return nil, fmt.Errorf("source directory %q has no %s files", inputPath, sourceExtension)
		}
	} else {
		if isTestSource(filepath.Base(inputPath)) {
			return nil, fmt.Errorf("test source %q can only be executed with 'mio test'", inputPath)
		}
		paths = []string{inputPath}
	}
	return parsePaths(paths)
}

func parseTestInput(inputPath string) (*ast.Program, []TestFunction, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %q: %w", inputPath, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("test target %q is not a source directory", inputPath)
	}
	entries, err := os.ReadDir(inputPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read source directory %q: %w", inputPath, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && filepath.Ext(entry.Name()) == sourceExtension {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	combined := &ast.Program{}
	var tests []TestFunction
	for _, name := range names {
		filePath := filepath.Join(inputPath, name)
		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil, nil, fmt.Errorf("read %q: %w", filePath, readErr)
		}
		parsed, parseErr := parseSources([]Source{{Name: name, Path: filePath, Content: content}})
		if parseErr != nil {
			return nil, nil, fmt.Errorf("%s: %w", filePath, parseErr)
		}
		combined.Statements = append(combined.Statements, parsed.Statements...)
		if len(combined.Statements) == len(parsed.Statements) {
			combined.Range.Start = parsed.Range.Start
		}
		combined.Range.End = parsed.Range.End
		if !isTestSource(name) {
			continue
		}
		for _, statement := range parsed.Statements {
			function, ok := statement.(*ast.FunctionStatement)
			if ok && strings.HasPrefix(function.Name.Name, "test_") {
				function.Test = true
				tests = append(tests, TestFunction{Name: function.Name.Name, Path: filePath, Source: string(content), Function: function})
			}
		}
	}
	return combined, tests, nil
}

func parsePaths(paths []string) (*ast.Program, error) {
	sources := make([]Source, 0, len(paths))
	for _, sourcePath := range paths {
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", sourcePath, err)
		}
		sources = append(sources, Source{Name: filepath.Base(sourcePath), Path: sourcePath, Content: content})
	}
	return parseSources(sources)
}

func parseSources(sources []Source) (*ast.Program, error) {
	combined := &ast.Program{}
	for _, source := range sources {
		lex, err := lexer.NewChecked(string(source.Content), token.V05)
		if err != nil {
			return nil, err
		}
		program, err := parser.New(lex).ParseProgram()
		if err != nil {
			return nil, err
		}
		if err := lex.Err(); err != nil {
			return nil, err
		}
		combined.Statements = append(combined.Statements, program.Statements...)
		if len(combined.Statements) == len(program.Statements) {
			combined.Range.Start = program.Range.Start
		}
		combined.Range.End = program.Range.End
	}
	return combined, nil
}

func (s *programLoader) load(importPath string, chain []string) (*packageUnit, error) {
	if unit, ok := s.loaded[importPath]; ok {
		return unit, nil
	}
	if s.visiting[importPath] {
		return nil, fmt.Errorf("cyclic package import: %s", strings.Join(append(chain, importPath), " -> "))
	}
	s.visiting[importPath] = true
	defer delete(s.visiting, importPath)
	loaded, err := s.loader.Load(importPath)
	if err != nil {
		return nil, err
	}
	program, err := parseSources(loaded.Sources)
	if err != nil {
		return nil, fmt.Errorf("package %q: %w", importPath, err)
	}
	s.sources += len(loaded.Sources)
	unit := &packageUnit{path: importPath, program: program, imports: make(map[string]string)}
	unit.declared = declarations(program, importPath)
	unit.exported = exports(program, importPath)
	for _, statement := range program.Statements {
		if imported, ok := statement.(*ast.ImportStatement); ok {
			if previous, exists := unit.imports[imported.Alias]; exists && previous != imported.Path {
				return nil, fmt.Errorf("package %q uses alias %q for multiple imports", importPath, imported.Alias)
			}
			unit.imports[imported.Alias] = imported.Path
			if err := s.loadDependency(imported.Path, append(chain, importPath)); err != nil {
				return nil, err
			}
		}
	}
	if err := s.rewrite(unit, true); err != nil {
		return nil, fmt.Errorf("package %q: %w", importPath, err)
	}
	s.loaded[importPath] = unit
	s.ordered = append(s.ordered, unit)
	return unit, nil
}

func stableName(importPath, name string) string {
	if importPath == "" {
		return name
	}
	return importPath + "::" + name
}

func declarations(program *ast.Program, importPath string) map[string]string {
	result := make(map[string]string)
	for _, statement := range program.Statements {
		if name, _, ok := declarationInfo(statement); ok {
			result[name] = stableName(importPath, name)
		}
	}
	return result
}

func exports(program *ast.Program, importPath string) map[string]string {
	result := make(map[string]string)
	for _, statement := range program.Statements {
		if name, exported, ok := declarationInfo(statement); ok && exported {
			result[name] = stableName(importPath, name)
		}
	}
	return result
}

func declarationInfo(statement ast.Statement) (string, bool, bool) {
	switch declaration := statement.(type) {
	case *ast.FunctionStatement:
		return declaration.Name.Name, declaration.Exported, true
	case *ast.StructStatement:
		return declaration.Name.Name, declaration.Exported, true
	case *ast.EnumStatement:
		return declaration.Name.Name, declaration.Exported, true
	case *ast.ConstStatement:
		return declaration.Name.Name, declaration.Exported, true
	default:
		return "", false, false
	}
}

func (s *programLoader) rewrite(unit *packageUnit, library bool) error {
	aliases := make(map[string]map[string]string)
	for alias, importPath := range unit.imports {
		if functions, ok := s.registry.Module(importPath); ok {
			exports := make(map[string]string)
			for _, function := range functions {
				if function.Public {
					exports[function.ID.Name] = function.BindingKey
				}
			}
			aliases[alias] = exports
			continue
		}
		dependency, ok := s.loaded[importPath]
		if !ok {
			return fmt.Errorf("package %q was not loaded", importPath)
		}
		aliases[alias] = dependency.exported
	}
	r := &rewriter{
		top:             unit.declared,
		aliases:         aliases,
		internals:       library,
		arrayInternals:  unit.path == "kiso/array",
		stringInternals: unit.path == "kiso/string",
	}
	for _, statement := range unit.program.Statements {
		if _, imported := statement.(*ast.ImportStatement); imported {
			continue
		}
		if library {
			if _, _, declaration := declarationInfo(statement); !declaration {
				return fmt.Errorf("top-level executable statement %T is not allowed", statement)
			}
		}
		if err := r.statement(statement, make(map[string]bool)); err != nil {
			return err
		}
		unit.statements = append(unit.statements, statement)
	}
	return nil
}

type rewriter struct {
	top             map[string]string
	aliases         map[string]map[string]string
	internals       bool
	arrayInternals  bool
	stringInternals bool
}

var mathInternals = map[string]string{
	"__sqrt": "$math.sqrt", "__floor": "$math.floor", "__ceil": "$math.ceil",
	"__round_even": "$math.round_even", "__trunc": "$math.trunc", "__is_nan": "$math.is_nan",
	"__is_inf": "$math.is_inf", "__is_finite": "$math.is_finite", "__is_negative_zero": "$math.is_negative_zero",
	"__add_overflows": "$math.add_overflows", "__is_negative": "$math.is_negative", "__neg_overflows": "$math.neg_overflows",
	"__math_error": "$math.error",
}

var arrayInternals = map[string]string{
	"__array_make": "$array.make", "__array_with_capacity": "$array.with_capacity",
	"__array_push": "$array.push", "__array_capacity": "$array.capacity",
	"__array_reserve": "$array.reserve", "__array_clone": "$array.clone",
	"__array_truncate": "$array.truncate", "__array_append_all": "$array.append_all",
}

var stringInternals = map[string]string{
	"__string_to_bytes":  "$data.string_to_bytes",
	"__string_from_utf8": "$data.bytes_to_string",
	"__string_byte_len":  "$data.string_byte_len",
}

func cloneScope(scope map[string]bool) map[string]bool {
	copy := make(map[string]bool, len(scope))
	for name := range scope {
		copy[name] = true
	}
	return copy
}

func (r *rewriter) typeRef(ref *ast.TypeRef) error {
	if ref == nil {
		return nil
	}
	if dot := strings.IndexByte(ref.Name, '.'); dot >= 0 {
		alias, member := ref.Name[:dot], ref.Name[dot+1:]
		exports, ok := r.aliases[alias]
		if !ok {
			return fmt.Errorf("unknown package alias %q", alias)
		}
		resolved, ok := exports[member]
		if !ok {
			return fmt.Errorf("package %q does not export %q", alias, member)
		}
		ref.Name = resolved
	} else if resolved, ok := r.top[ref.Name]; ok {
		ref.Name = resolved
	}
	for _, arg := range ref.Args {
		if err := r.typeRef(arg); err != nil {
			return err
		}
	}
	for _, param := range ref.Params {
		if err := r.typeRef(param); err != nil {
			return err
		}
	}
	if err := r.typeRef(ref.Element); err != nil {
		return err
	}
	return r.typeRef(ref.Result)
}

func (r *rewriter) statement(statement ast.Statement, scope map[string]bool) error {
	switch n := statement.(type) {
	case *ast.ConstStatement:
		original := n.Name.Name
		n.Name.Name = r.top[original]
		if err := r.typeRef(n.Name.DeclaredType); err != nil {
			return err
		}
		return r.expression(&n.Value, scope)
	case *ast.FunctionStatement:
		original := n.Name.Name
		n.Name.Name = r.top[original]
		local := cloneScope(scope)
		for _, parameter := range n.TypeParams {
			local[parameter] = true
		}
		for _, parameter := range n.Parameters {
			local[parameter.Name] = true
			if err := r.typeRef(parameter.DeclaredType); err != nil {
				return err
			}
		}
		if err := r.typeRef(n.ReturnType); err != nil {
			return err
		}
		if err := r.block(n.Body, local); err != nil {
			return err
		}
		n.Intrinsic = functionIntrinsic(n.Body)
		return nil
	case *ast.StructStatement:
		original := n.Name.Name
		n.Name.Name = r.top[original]
		for _, field := range n.Fields {
			if err := r.typeRef(field.DeclaredType); err != nil {
				return err
			}
		}
		return nil
	case *ast.EnumStatement:
		original := n.Name.Name
		n.Name.Name = r.top[original]
		for i := range n.Variants {
			for _, ref := range n.Variants[i].Fields {
				if err := r.typeRef(ref); err != nil {
					return err
				}
			}
		}
		return nil
	case *ast.LetStatement:
		if err := r.typeRef(n.Name.DeclaredType); err != nil {
			return err
		}
		if err := r.expression(&n.Value, scope); err != nil {
			return err
		}
		scope[n.Name.Name] = true
		return nil
	case *ast.AssignStatement:
		if !scope[n.Name.Name] {
			if resolved, ok := r.top[n.Name.Name]; ok {
				n.Name.Name = resolved
			}
		}
		return r.expression(&n.Value, scope)
	case *ast.ExpressionStatement:
		return r.expression(&n.Expr, scope)
	case *ast.ReturnStatement:
		if n.Value != nil {
			return r.expression(&n.Value, scope)
		}
		return nil
	case *ast.IfStatement:
		if err := r.expression(&n.Condition, scope); err != nil {
			return err
		}
		if err := r.block(n.Consequence, cloneScope(scope)); err != nil {
			return err
		}
		if n.Alternative != nil {
			return r.block(n.Alternative, cloneScope(scope))
		}
		return nil
	case *ast.WhileStatement:
		if err := r.expression(&n.Condition, scope); err != nil {
			return err
		}
		return r.block(n.Body, cloneScope(scope))
	case *ast.IndexAssignStatement:
		var target ast.Expression = n.Target
		if err := r.expression(&target, scope); err != nil {
			return err
		}
		return r.expression(&n.Value, scope)
	case *ast.MatchStatement:
		if err := r.expression(&n.Value, scope); err != nil {
			return err
		}
		for i := range n.Arms {
			local := cloneScope(scope)
			for _, binding := range n.Arms[i].Bindings {
				local[binding.Name] = true
			}
			if err := r.block(n.Arms[i].Body, local); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *rewriter) block(block *ast.BlockStatement, scope map[string]bool) error {
	for _, statement := range block.Statements {
		if err := r.statement(statement, scope); err != nil {
			return err
		}
	}
	return nil
}

func (r *rewriter) expression(expr *ast.Expression, scope map[string]bool) error {
	switch n := (*expr).(type) {
	case *ast.Identifier:
		if r.internals && !scope[n.Name] {
			if internal, ok := mathInternals[n.Name]; ok {
				n.Name = internal
				return nil
			}
			if r.arrayInternals {
				if internal, ok := arrayInternals[n.Name]; ok {
					n.Name = internal
					return nil
				}
			}
			if r.stringInternals {
				if internal, ok := stringInternals[n.Name]; ok {
					n.Name = internal
					return nil
				}
			}
		}
		if !scope[n.Name] {
			if resolved, ok := r.top[n.Name]; ok {
				n.Name = resolved
			}
		}
	case *ast.FieldExpression:
		if identifier, ok := n.Left.(*ast.Identifier); ok {
			if exports, imported := r.aliases[identifier.Name]; imported {
				resolved, exported := exports[n.Name]
				if !exported {
					return fmt.Errorf("package %q does not export %q", identifier.Name, n.Name)
				}
				*expr = &ast.Identifier{Name: resolved, Range: n.Range}
				return nil
			}
		}
		left := n.Left
		if err := r.expression(&left, scope); err != nil {
			return err
		}
		n.Left = left
	case *ast.GroupedExpression:
		return r.expression(&n.Expr, scope)
	case *ast.PrefixExpression:
		return r.expression(&n.Right, scope)
	case *ast.InfixExpression:
		if err := r.expression(&n.Left, scope); err != nil {
			return err
		}
		return r.expression(&n.Right, scope)
	case *ast.CallExpression:
		if err := r.expression(&n.Function, scope); err != nil {
			return err
		}
		for i := range n.Arguments {
			if err := r.expression(&n.Arguments[i], scope); err != nil {
				return err
			}
		}
	case *ast.TypeApplyExpression:
		if err := r.expression(&n.Function, scope); err != nil {
			return err
		}
		for _, ref := range n.Args {
			if err := r.typeRef(ref); err != nil {
				return err
			}
		}
	case *ast.ConversionExpression:
		for i := range n.Arguments {
			if err := r.expression(&n.Arguments[i], scope); err != nil {
				return err
			}
		}
	case *ast.ArrayLiteral:
		for i := range n.Elements {
			if err := r.expression(&n.Elements[i], scope); err != nil {
				return err
			}
		}
	case *ast.IndexExpression:
		if err := r.expression(&n.Left, scope); err != nil {
			return err
		}
		return r.expression(&n.Index, scope)
	case *ast.StructLiteral:
		if dot := strings.IndexByte(n.TypeName, '.'); dot >= 0 {
			exports := r.aliases[n.TypeName[:dot]]
			resolved, ok := exports[n.TypeName[dot+1:]]
			if !ok {
				return fmt.Errorf("package %q does not export %q", n.TypeName[:dot], n.TypeName[dot+1:])
			}
			n.TypeName = resolved
		} else if resolved, ok := r.top[n.TypeName]; ok {
			n.TypeName = resolved
		}
		for _, ref := range n.TypeArgs {
			if err := r.typeRef(ref); err != nil {
				return err
			}
		}
		for i := range n.Fields {
			if err := r.expression(&n.Fields[i].Value, scope); err != nil {
				return err
			}
		}
	case *ast.FunctionLiteral:
		local := cloneScope(scope)
		for _, parameter := range n.Parameters {
			local[parameter.Name] = true
			if err := r.typeRef(parameter.DeclaredType); err != nil {
				return err
			}
		}
		if err := r.typeRef(n.ReturnType); err != nil {
			return err
		}
		return r.block(n.Body, local)
	}
	return nil
}

func functionIntrinsic(body *ast.BlockStatement) string {
	if body == nil || len(body.Statements) != 1 {
		return ""
	}
	var expression ast.Expression
	switch statement := body.Statements[0].(type) {
	case *ast.ReturnStatement:
		expression = statement.Value
	case *ast.ExpressionStatement:
		expression = statement.Expr
	default:
		return ""
	}
	call, ok := expression.(*ast.CallExpression)
	if !ok {
		return ""
	}
	identifier, ok := call.Function.(*ast.Identifier)
	if !ok || !(strings.HasPrefix(identifier.Name, "$array.") || strings.HasPrefix(identifier.Name, "$data.")) {
		return ""
	}
	return identifier.Name
}
