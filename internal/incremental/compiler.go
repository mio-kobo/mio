package incremental

import (
	"fmt"
	"sync"

	"Mio/internal/ast"
	bytecodecompiler "Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
)

const cacheFormatVersion = "mio-incremental-v2"

// FunctionID 标识模块中的函数
type FunctionID struct {
	Module string
	Name   string
}

// DependencyGraph 保存函数间依赖
type DependencyGraph struct {
	Calls map[FunctionID][]FunctionID
}

// PhaseStats 记录单个阶段的函数级统计
type PhaseStats struct {
	Hits        int
	Invalidated int
	Recomputed  int
}

// Stats 记录一次增量编译的可观测数据
type Stats struct {
	Parsed              int
	Resolved            int
	TypeCheck           PhaseStats
	Codegen             PhaseStats
	SignatureChanges    int
	BodyChanges         int
	CallerInvalidations int
	FullFallback        bool
}

// HitRate 返回函数级缓存命中率
func (s Stats) HitRate() float64 {
	hits := s.TypeCheck.Hits + s.Codegen.Hits
	total := hits + s.TypeCheck.Recomputed + s.Codegen.Recomputed
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

type functionRecord struct {
	id           FunctionID
	signature    string
	body         string
	semanticKey  string
	codegenKey   string
	dependencies []FunctionID
}

type snapshot struct {
	topLevel  string
	functions map[FunctionID]functionRecord
}

// Compiler 管理进程内函数级增量缓存
type Compiler struct {
	mu             sync.Mutex
	checks         map[string]struct{}
	artifacts      map[string]bytecodecompiler.FunctionArtifact
	modules        map[string]snapshot
	lastGraph      DependencyGraph
	allowCacheRead bool
}

// New 创建增量编译器
func New() *Compiler {
	return &Compiler{
		checks:    make(map[string]struct{}),
		artifacts: make(map[string]bytecodecompiler.FunctionArtifact),
		modules:   make(map[string]snapshot),
		lastGraph: DependencyGraph{Calls: make(map[FunctionID][]FunctionID)},
	}
}

// Compile 编译模块源码并复用未变化的函数结果
func (c *Compiler) Compile(module, source string) (*bytecodecompiler.Bytecode, Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	program, r, err := parseAndResolve(source)
	stats := Stats{Parsed: 1}
	if err != nil {
		return nil, stats, err
	}
	stats.Resolved = 1

	records, graph, topLevel, err := buildRecords(module, program)
	if err != nil {
		return c.compileFull(program, r, stats)
	}
	previous, hasPrevious := c.modules[module]
	forceFull := (hasPrevious && previous.topLevel != topLevel) || requiresCoreTypeFallback(program, r)
	stats.FullFallback = forceFull
	calculateInvalidations(previous, hasPrevious, records, &stats)

	checkKeys := make(map[*ast.FunctionStatement]string)
	codegenKeys := make(map[string]string)
	for _, statement := range program.Statements {
		fn, ok := statement.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		record := records[FunctionID{Module: module, Name: fn.Name.Name}]
		checkKeys[fn] = record.semanticKey
		codegenKeys[fn.Name.Name] = record.codegenKey
	}

	c.allowCacheRead = !forceFull
	checker := typecheck.NewWithResolver(r)
	typeStats, err := checker.CheckProgramIncremental(program, checkKeys, c)
	stats.TypeCheck.Hits = typeStats.Hits
	stats.TypeCheck.Recomputed = typeStats.Recomputed
	if err != nil {
		return nil, stats, err
	}

	compiled := bytecodecompiler.NewWithResolver(r)
	compiled.ConfigureFunctionCache(codegenKeys, c)
	if err := compiled.Compile(program); err != nil {
		return nil, stats, err
	}
	codeStats := compiled.IncrementalStats()
	stats.Codegen.Hits = codeStats.Hits
	stats.Codegen.Recomputed = codeStats.Recomputed

	c.modules[module] = snapshot{topLevel: topLevel, functions: records}
	c.lastGraph = graph
	return compiled.Bytecode(), stats, nil
}

func requiresCoreTypeFallback(program *ast.Program, r *resolver.Resolver) bool {
	var expressionNeedsFallback func(ast.Expression) bool
	var statementNeedsFallback func(ast.Statement) bool
	expressionNeedsFallback = func(expression ast.Expression) bool {
		switch node := expression.(type) {
		case *ast.FunctionLiteral:
			return true
		case *ast.CallExpression:
			if identifier, ok := node.Function.(*ast.Identifier); ok {
				if symbol, resolved := r.BindingOf(identifier); resolved && (symbol.Kind == resolver.SymbolVariable || symbol.Kind == resolver.SymbolParameter) {
					return true
				}
			} else {
				return true
			}
			for _, argument := range node.Arguments {
				if expressionNeedsFallback(argument) {
					return true
				}
			}
		case *ast.TypeApplyExpression, *ast.StructLiteral, *ast.EnumLiteral, *ast.FieldExpression:
			return true
		case *ast.IndexExpression:
			return expressionNeedsFallback(node.Left) || expressionNeedsFallback(node.Index)
		case *ast.ArrayLiteral:
			for _, element := range node.Elements {
				if expressionNeedsFallback(element) {
					return true
				}
			}
		case *ast.GroupedExpression:
			return expressionNeedsFallback(node.Expr)
		case *ast.PrefixExpression:
			return expressionNeedsFallback(node.Right)
		case *ast.InfixExpression:
			return expressionNeedsFallback(node.Left) || expressionNeedsFallback(node.Right)
		case *ast.ConversionExpression:
			for _, argument := range node.Arguments {
				if expressionNeedsFallback(argument) {
					return true
				}
			}
		}
		return false
	}
	statementNeedsFallback = func(statement ast.Statement) bool {
		switch node := statement.(type) {
		case *ast.StructStatement, *ast.EnumStatement, *ast.MatchStatement:
			return true
		case *ast.FunctionStatement:
			if len(node.TypeParams) != 0 || coreTypeRef(node.ReturnType) {
				return true
			}
			for _, parameter := range node.Parameters {
				if coreTypeRef(parameter.DeclaredType) {
					return true
				}
			}
			return statementNeedsFallback(node.Body)
		case *ast.BlockStatement:
			for _, child := range node.Statements {
				if statementNeedsFallback(child) {
					return true
				}
			}
		case *ast.LetStatement:
			return coreTypeRef(node.Name.DeclaredType) || expressionNeedsFallback(node.Value)
		case *ast.AssignStatement:
			return expressionNeedsFallback(node.Value)
		case *ast.IndexAssignStatement:
			return expressionNeedsFallback(node.Target) || expressionNeedsFallback(node.Value)
		case *ast.ExpressionStatement:
			return expressionNeedsFallback(node.Expr)
		case *ast.ReturnStatement:
			return expressionNeedsFallback(node.Value)
		case *ast.IfStatement:
			return expressionNeedsFallback(node.Condition) || statementNeedsFallback(node.Consequence) || statementNeedsFallback(node.Alternative)
		case *ast.WhileStatement:
			return expressionNeedsFallback(node.Condition) || statementNeedsFallback(node.Body)
		}
		return false
	}
	for _, statement := range program.Statements {
		if statementNeedsFallback(statement) {
			return true
		}
	}
	return false
}

func coreTypeRef(ref *ast.TypeRef) bool {
	if ref == nil {
		return false
	}
	if ref.Name == "[]" || ref.Name == "?" {
		return coreTypeRef(ref.Element)
	}
	if ref.Name == "int8" || ref.Name == "int16" || ref.Name == "int32" || ref.Name == "int64" || ref.Name == "rune" || ref.Name == "uint8" || ref.Name == "uint16" || ref.Name == "uint32" || ref.Name == "float32" || ref.Name == "float64" || ref.Name == "bool" || ref.Name == "string" || ref.Name == "unit" {
		return false
	}
	return true
}

func (c *Compiler) compileFull(program *ast.Program, r *resolver.Resolver, stats Stats) (*bytecodecompiler.Bytecode, Stats, error) {
	stats.FullFallback = true
	checker := typecheck.NewWithResolver(r)
	if err := checker.CheckProgram(program); err != nil {
		return nil, stats, err
	}
	compiled := bytecodecompiler.NewWithResolver(r)
	if err := compiled.Compile(program); err != nil {
		return nil, stats, err
	}
	return compiled.Bytecode(), stats, nil
}

// Graph 返回最近一次成功编译的依赖图副本
func (c *Compiler) Graph() DependencyGraph {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := DependencyGraph{Calls: make(map[FunctionID][]FunctionID, len(c.lastGraph.Calls))}
	for id, dependencies := range c.lastGraph.Calls {
		result.Calls[id] = append([]FunctionID(nil), dependencies...)
	}
	return result
}

// HasFunctionCheck 查询函数体检查缓存
func (c *Compiler) HasFunctionCheck(key string) bool {
	if !c.allowCacheRead {
		return false
	}
	_, ok := c.checks[key]
	return ok
}

// StoreFunctionCheck 保存函数体检查缓存
func (c *Compiler) StoreFunctionCheck(key string) {
	c.checks[key] = struct{}{}
}

// LoadFunction 查询函数编译缓存
func (c *Compiler) LoadFunction(key string) (bytecodecompiler.FunctionArtifact, bool) {
	if !c.allowCacheRead {
		return bytecodecompiler.FunctionArtifact{}, false
	}
	artifact, ok := c.artifacts[key]
	return artifact, ok
}

// StoreFunction 保存函数编译缓存
func (c *Compiler) StoreFunction(key string, artifact bytecodecompiler.FunctionArtifact) {
	c.artifacts[key] = artifact
}

func parseAndResolve(source string) (*ast.Program, *resolver.Resolver, error) {
	l, err := lexer.NewChecked(source, token.V05)
	if err != nil {
		return nil, nil, err
	}
	program, err := parser.New(l).ParseProgram()
	if err != nil {
		return nil, nil, err
	}
	if err := l.Err(); err != nil {
		return nil, nil, err
	}
	if err := constant.Fold(program); err != nil {
		return nil, nil, err
	}
	r := resolver.New()
	if err := r.ResolveProgram(program); err != nil {
		return nil, nil, err
	}
	return program, r, nil
}

func buildRecords(module string, program *ast.Program) (map[FunctionID]functionRecord, DependencyGraph, string, error) {
	if module == "" {
		return nil, DependencyGraph{}, "", fmt.Errorf("module identity is empty")
	}
	known := make(map[string]struct{})
	functions := make(map[string]*ast.FunctionStatement)
	order := make([]string, 0)
	for _, statement := range program.Statements {
		fn, ok := statement.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		if _, exists := known[fn.Name.Name]; exists {
			return nil, DependencyGraph{}, "", fmt.Errorf("duplicate function identity %q", fn.Name.Name)
		}
		known[fn.Name.Name] = struct{}{}
		functions[fn.Name.Name] = fn
		order = append(order, fn.Name.Name)
	}

	topLevel, topDependencies := topLevelAnalysis(program, module, known)
	layout := combineFingerprint(append([]string{cacheFormatVersion, "layout"}, order...)...)
	signatures := make(map[FunctionID]string, len(functions))
	for name, fn := range functions {
		id := FunctionID{Module: module, Name: name}
		signatures[id] = signatureFingerprint(fn)
	}
	topEnvironmentParts := []string{topLevel}
	for _, dependency := range topDependencies {
		topEnvironmentParts = append(topEnvironmentParts, dependency.Module, dependency.Name, signatures[dependency])
	}
	topEnvironment := combineFingerprint(topEnvironmentParts...)

	records := make(map[FunctionID]functionRecord, len(functions))
	graph := DependencyGraph{Calls: make(map[FunctionID][]FunctionID, len(functions))}
	for name, fn := range functions {
		id := FunctionID{Module: module, Name: name}
		body, dependencies := functionBodyAnalysis(fn, module, known)
		parts := []string{cacheFormatVersion, module, name, signatures[id], body, topEnvironment}
		for _, dependency := range dependencies {
			parts = append(parts, dependency.Module, dependency.Name, signatures[dependency])
		}
		semanticKey := combineFingerprint(parts...)
		records[id] = functionRecord{
			id:           id,
			signature:    signatures[id],
			body:         body,
			semanticKey:  semanticKey,
			codegenKey:   combineFingerprint(cacheFormatVersion, "codegen", semanticKey, layout),
			dependencies: dependencies,
		}
		graph.Calls[id] = append([]FunctionID(nil), dependencies...)
	}
	return records, graph, topLevel, nil
}

func calculateInvalidations(previous snapshot, hasPrevious bool, current map[FunctionID]functionRecord, stats *Stats) {
	if !hasPrevious {
		return
	}
	changedSignatures := make(map[FunctionID]struct{})
	for id, oldRecord := range previous.functions {
		newRecord, ok := current[id]
		if !ok || oldRecord.signature != newRecord.signature {
			changedSignatures[id] = struct{}{}
			stats.SignatureChanges++
		}
	}
	for id, newRecord := range current {
		oldRecord, ok := previous.functions[id]
		if !ok {
			stats.TypeCheck.Invalidated++
			stats.Codegen.Invalidated++
			continue
		}
		if oldRecord.signature == newRecord.signature && oldRecord.body != newRecord.body {
			stats.BodyChanges++
		}
		if oldRecord.semanticKey != newRecord.semanticKey {
			stats.TypeCheck.Invalidated++
		}
		if oldRecord.codegenKey != newRecord.codegenKey {
			stats.Codegen.Invalidated++
		}
		for _, dependency := range newRecord.dependencies {
			if _, changed := changedSignatures[dependency]; changed {
				stats.CallerInvalidations++
				break
			}
		}
	}
}
