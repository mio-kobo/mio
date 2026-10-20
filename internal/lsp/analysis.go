package lsp

import (
	"context"
	"hash/fnv"
	"os"
	"path/filepath"
	"sync"
	"time"

	"Mio/internal/ast"
	"Mio/internal/bunko"
	"Mio/internal/constant"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
)

const (
	defaultMaxAnalyses = 8
)

// fileStamp 记录依赖文件的轻量指纹，用于发现磁盘上的模块变更。
type fileStamp struct {
	size    int64
	modTime time.Time
}

// analysis 表示一个文档版本对应的编译与语义结果。
// 构建完成后不再修改，可以被多个请求并发读取。
type analysis struct {
	URI     string
	Path    string // file: URI 对应的绝对路径；其他 scheme 为空
	Version int
	Text    string
	Buffer  *textBuffer

	// Partial 表示源码未通过严格解析，语义结果来自容错解析。
	Partial bool

	Program  *ast.Program
	Resolver *resolver.Resolver
	Checker  *typecheck.Checker

	// Files 是参与本次合并的源文件（绝对路径，按加载顺序）。
	Files []string
	// FileText / FileBuffer 提供其它文件的位置换算文本。
	FileText   map[string]string
	FileBuffer map[string]*textBuffer
	// DocKey 是当前文档在语义索引中的键（通常是客户端 URI）。
	DocKey string
	// keyPaths 把语义索引键映射到规范化磁盘路径。
	keyPaths map[string]string
	// preferred 记录规范化路径 → 客户端 URI 的映射。
	preferred map[string]string

	Diagnostics []Diagnostic
	Index       *semanticIndex

	BuiltAt  time.Time
	Duration time.Duration
	Stamps   map[string]fileStamp
	// Sources 记录合并后语句到来源文件的映射（容错解析时为空）。
	Sources map[ast.Statement]string
	Imports []bunko.Import
}

// finish 记录构建耗时。
func (a *analysis) finish(started time.Time) {
	a.BuiltAt = started
	a.Duration = time.Since(started)
}

// analyze 运行与 mio check 相同的诊断流水线，并构建可供导航使用的语义索引。
// 上下文取消时返回 nil，调用方不得发布任何结果。
func (a *analyzer) analyze(ctx context.Context, doc *document, overlay bunko.Overlay, preferred map[string]string) *analysis {
	started := time.Now()
	result := &analysis{
		URI:        doc.URI,
		Version:    doc.Version,
		Text:       doc.Text,
		Buffer:     newTextBuffer(doc.Text),
		FileText:   make(map[string]string),
		FileBuffer: make(map[string]*textBuffer),
		Stamps:     make(map[string]fileStamp),
		keyPaths:   make(map[string]string),
	}
	result.preferred = preferred
	if result.preferred == nil {
		result.preferred = make(map[string]string)
	}
	path, isFile := pathFromURI(doc.URI)
	if isFile {
		result.Path = path
		canonical := canonicalPath(path)
		result.preferred[canonical] = doc.URI
		result.DocKey = result.fileKey(path)
		overlay.Set(path, []byte(doc.Text))
	} else {
		result.DocKey = doc.URI
	}

	var strictErr error
	var program *ast.Program
	if isFile {
		registry, err := a.modules()
		if err != nil {
			result.Diagnostics = []Diagnostic{internalFailure(doc.Text, err)}
			result.finish(started)
			return result
		}
		root := bunkoRootFor(path, a.bunkoRoot)
		loaded, err := bunko.LoadProgramWithOverlay(path, root, registry, overlay)
		if err != nil {
			strictErr = err
		} else {
			program = loaded.AST
			result.Files = loaded.Files
			result.Sources = loaded.StatementSources
			result.Imports = loaded.Imports
		}
	} else {
		parsed, err := parseSource(doc.Text)
		if err != nil {
			strictErr = err
		} else {
			program = parsed
		}
	}
	if ctx.Err() != nil {
		return nil
	}

	// 语义 AST：严格加载失败时，对根文件做逐语句容错解析。
	semanticProgram := program
	recovered := false
	if semanticProgram == nil {
		semanticProgram = parseRecovering(doc.Text)
		recovered = true
		result.Partial = true
	}
	result.FileText, result.FileBuffer = collectFileText(isFile, result.Path, doc, result.Files, overlay)
	if ctx.Err() != nil {
		return nil
	}

	// constant.Fold 会移除 teisu 声明并把常量引用替换为字面量，
	// 因此语义索引必须在折叠前捕获声明与标识符位置。
	captured := captureBeforeFold(semanticProgram, result)

	// 严格诊断流水线：与 mio check 完全一致，只取第一个错误。
	foldApplied := false
	if strictErr == nil {
		if err := constant.Fold(program); err != nil {
			strictErr = err
		} else {
			foldApplied = true
		}
	}
	if recovered && !foldApplied {
		// 容错 AST 也尝试折叠，以恢复尽可能多的类型信息；
		// 折叠失败不会改写 AST。
		if err := constant.Fold(semanticProgram); err == nil {
			foldApplied = true
		}
	}
	if ctx.Err() != nil {
		return nil
	}

	var names *resolver.Resolver
	if strictErr == nil {
		strict := resolver.New()
		if err := strict.ResolveProgram(semanticProgram); err != nil {
			strictErr = err
		} else {
			names = strict
		}
	}
	if ctx.Err() != nil {
		return nil
	}

	checker := (*typecheck.Checker)(nil)
	if strictErr == nil && names != nil {
		strictChecker := typecheck.NewWithResolver(names)
		if err := strictChecker.CheckProgram(semanticProgram); err != nil {
			strictErr = err
			lenientChecker := typecheck.NewWithResolver(names)
			lenientChecker.CheckProgramLenient(semanticProgram)
			checker = lenientChecker
		} else {
			checker = strictChecker
		}
	}
	if checker == nil {
		// 容错路径：收集全部可恢复错误，尽量保留有效绑定与类型。
		names = resolver.New()
		names.ResolveProgramLenient(semanticProgram)
		lenientChecker := typecheck.NewWithResolver(names)
		lenientChecker.CheckProgramLenient(semanticProgram)
		checker = lenientChecker
	}
	if ctx.Err() != nil {
		return nil
	}

	result.Program = semanticProgram
	result.Resolver = names
	result.Checker = checker
	if strictErr != nil {
		result.Diagnostics = diagnosticsFromError(strictErr, doc.Text, doc.URI)
	} else {
		result.Diagnostics = []Diagnostic{}
	}

	result.Index = buildSemanticIndex(result, captured)
	result.recordStamps()
	result.finish(started)
	return result
}

// collectFileText 读取参与分析的全部源文件文本。
// 内存覆盖层优先于磁盘，保证未保存缓冲区与 AST 位置一致。
func collectFileText(isFile bool, rootPath string, doc *document, files []string, overlay bunko.Overlay) (map[string]string, map[string]*textBuffer) {
	texts := make(map[string]string, len(files)+1)
	buffers := make(map[string]*textBuffer, len(files)+1)
	if !isFile {
		texts[doc.URI] = doc.Text
		buffers[doc.URI] = newTextBuffer(doc.Text)
		return texts, buffers
	}
	seen := make(map[string]struct{}, len(files)+1)
	add := func(path, text string) {
		canonical := canonicalPath(path)
		if _, exists := seen[canonical]; exists {
			return
		}
		seen[canonical] = struct{}{}
		texts[canonical] = text
		buffers[canonical] = newTextBuffer(text)
	}
	rootCanonical := canonicalPath(rootPath)
	for _, path := range files {
		if path == "" {
			continue
		}
		if path == rootPath || canonicalPath(path) == rootCanonical {
			add(path, doc.Text)
			continue
		}
		if content, ok := overlay.Lookup(path); ok {
			add(path, string(content))
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		add(path, string(content))
	}
	add(rootPath, doc.Text)
	return texts, buffers
}

// canonicalPath 解析符号链接并清理路径，用作符号身份的稳定键。
func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

// recordStamps 记录依赖文件的 mtime/size，供缓存失效判断使用。
func (a *analysis) recordStamps() {
	root := canonicalPath(a.Path)
	for _, path := range a.Files {
		if path == "" {
			continue
		}
		canonical := canonicalPath(path)
		if canonical == root {
			continue
		}
		info, err := os.Stat(canonical)
		if err != nil {
			continue
		}
		a.Stamps[canonical] = fileStamp{size: info.Size(), modTime: info.ModTime()}
	}
}

// dependenciesFresh 判断依赖文件的磁盘状态是否仍与构建时一致。
func (a *analysis) dependenciesFresh() bool {
	for path, stamp := range a.Stamps {
		info, err := os.Stat(path)
		if err != nil {
			return false
		}
		if info.Size() != stamp.size || !info.ModTime().Equal(stamp.modTime) {
			return false
		}
	}
	return true
}

// capturedSource 保存 constant.Fold 之前的声明与标识符信息。
type capturedSource struct {
	consts      []*ast.ConstStatement
	constFiles  []string
	identifiers map[string]map[spanKey]string
}

// spanKey 把 span 归一化为可比较的键。
type spanKey struct {
	start int
	end   int
}

func keyOfSpan(span ast.Span) spanKey {
	return spanKey{start: span.Start.Offset, end: span.End.Offset}
}

// captureBeforeFold 在常量折叠前记录 teisu 声明和所有标识符位置。
// 折叠会把常量引用替换成字面量，但字面量保留原始 span，
// 因此可以据此把"常量名位置"映射回声明。
func captureBeforeFold(program *ast.Program, result *analysis) *capturedSource {
	captured := &capturedSource{identifiers: make(map[string]map[spanKey]string)}
	if program == nil {
		return captured
	}
	for _, statement := range program.Statements {
		fileKey := result.statementFileKey(statement)
		if fileKey == "" {
			continue
		}
		declaration, ok := statement.(*ast.ConstStatement)
		if !ok {
			continue
		}
		captured.consts = append(captured.consts, declaration)
		captured.constFiles = append(captured.constFiles, fileKey)
	}
	var walk func(expr ast.Expression, fileKey string)
	walk = func(expr ast.Expression, fileKey string) {
		switch n := expr.(type) {
		case nil:
			return
		case *ast.Identifier:
			recordIdentifiers(captured, fileKey, n.Name, n.Range)
		case *ast.InfixExpression:
			walk(n.Left, fileKey)
			walk(n.Right, fileKey)
		case *ast.GroupedExpression:
			walk(n.Expr, fileKey)
		case *ast.PrefixExpression:
			walk(n.Right, fileKey)
		case *ast.CallExpression:
			walk(n.Function, fileKey)
			for _, argument := range n.Arguments {
				walk(argument, fileKey)
			}
		case *ast.TypeApplyExpression:
			walk(n.Function, fileKey)
		case *ast.ConversionExpression:
			for _, argument := range n.Arguments {
				walk(argument, fileKey)
			}
		case *ast.ArrayLiteral:
			for _, element := range n.Elements {
				walk(element, fileKey)
			}
		case *ast.IndexExpression:
			walk(n.Left, fileKey)
			walk(n.Index, fileKey)
		case *ast.FieldExpression:
			walk(n.Left, fileKey)
		case *ast.StructLiteral:
			for _, field := range n.Fields {
				walk(field.Value, fileKey)
			}
		case *ast.EnumLiteral:
			for _, value := range n.Values {
				walk(value, fileKey)
			}
		case *ast.FunctionLiteral:
			for _, statement := range n.Body.Statements {
				walkCapturedStatement(captured, statement, fileKey, walk)
			}
		}
	}
	for _, statement := range program.Statements {
		fileKey := result.statementFileKey(statement)
		if fileKey == "" {
			continue
		}
		walkCapturedStatement(captured, statement, fileKey, walk)
	}
	return captured
}

func walkCapturedStatement(captured *capturedSource, statement ast.Statement, fileKey string, walk func(ast.Expression, string)) {
	switch n := statement.(type) {
	case *ast.ConstStatement:
		walk(n.Value, fileKey)
	case *ast.LetStatement:
		walk(n.Value, fileKey)
	case *ast.AssignStatement:
		recordIdentifiers(captured, fileKey, n.Name.Name, n.Name.Range)
		walk(n.Value, fileKey)
	case *ast.IndexAssignStatement:
		walk(n.Target, fileKey)
		walk(n.Value, fileKey)
	case *ast.ExpressionStatement:
		walk(n.Expr, fileKey)
	case *ast.ReturnStatement:
		walk(n.Value, fileKey)
	case *ast.IfStatement:
		walk(n.Condition, fileKey)
		for _, inner := range n.Consequence.Statements {
			walkCapturedStatement(captured, inner, fileKey, walk)
		}
		if n.Alternative != nil {
			for _, inner := range n.Alternative.Statements {
				walkCapturedStatement(captured, inner, fileKey, walk)
			}
		}
	case *ast.WhileStatement:
		walk(n.Condition, fileKey)
		for _, inner := range n.Body.Statements {
			walkCapturedStatement(captured, inner, fileKey, walk)
		}
	case *ast.FunctionStatement:
		for _, inner := range n.Body.Statements {
			walkCapturedStatement(captured, inner, fileKey, walk)
		}
	case *ast.MatchStatement:
		walk(n.Value, fileKey)
		for _, arm := range n.Arms {
			for _, inner := range arm.Body.Statements {
				walkCapturedStatement(captured, inner, fileKey, walk)
			}
		}
	case *ast.BlockStatement:
		for _, inner := range n.Statements {
			walkCapturedStatement(captured, inner, fileKey, walk)
		}
	}
}

func recordIdentifiers(captured *capturedSource, fileKey, name string, span ast.Span) {
	spans := captured.identifiers[fileKey]
	if spans == nil {
		spans = make(map[spanKey]string)
		captured.identifiers[fileKey] = spans
	}
	spans[keyOfSpan(span)] = name
}

// statementFileKey 返回顶层语句的来源文件键；合成语句返回空串。
func (a *analysis) statementFileKey(statement ast.Statement) string {
	if a.Sources != nil {
		if path, ok := a.Sources[statement]; ok && path != "" {
			return a.fileKey(path)
		}
	}
	if a.Path == "" {
		return a.DocKey
	}
	// 容错解析（没有 Bunko 合并）时，所有语句都属于根文件；
	// 索引键必须与 DocKey 一致，否则悬停/引用会按不同键查找。
	if a.Partial {
		return a.DocKey
	}
	return ""
}

// fileKey 把磁盘路径映射为语义索引键（客户端 URI 优先）。
// 符号身份使用规范化路径，避免 /var 与 /private/var 之类的符号链接别名
// 把同一个文件拆成两个身份。
func (a *analysis) fileKey(path string) string {
	if path == "" {
		return ""
	}
	if a.Path == "" {
		return a.DocKey
	}
	canonical := canonicalPath(path)
	if key, ok := a.preferred[canonical]; ok && key != "" {
		a.keyPaths[key] = canonical
		return key
	}
	key := uriFromPath(canonical)
	a.keyPaths[key] = canonical
	return key
}

// keyPath 是 fileKey 的逆映射：返回规范化磁盘路径；非文件文档返回空串。
func (a *analysis) keyPath(key string) string {
	if key == "" {
		return ""
	}
	if a.Path == "" {
		return ""
	}
	if path, ok := a.keyPaths[key]; ok {
		return path
	}
	// 只读回退：keyPaths 在构建期填充，请求期绝不能修改快照。
	if path, ok := pathFromURI(key); ok {
		return canonicalPath(path)
	}
	return ""
}

// textFor 返回语义索引键对应的源码文本。
func (a *analysis) textFor(key string) string {
	path := a.keyPath(key)
	if path == "" {
		return a.Text
	}
	return a.FileText[path]
}

// bufferFor 返回语义索引键对应的位置换算缓冲区。
func (a *analysis) bufferFor(key string) *textBuffer {
	path := a.keyPath(key)
	if path == "" {
		return a.Buffer
	}
	if buffer, ok := a.FileBuffer[path]; ok {
		return buffer
	}
	return newTextBuffer(a.FileText[path])
}

// parseRecovering 以完整语句为粒度容错解析源码。
// 只有当整个文件无法严格解析时才使用：单条语句失败会被跳过，
// 其余声明仍然进入 AST，从而保留未受影响的语义信息。
func parseRecovering(text string) *ast.Program {
	if _, err := lexer.NewChecked(text, token.V05); err != nil {
		// 非法 UTF-8 不在本阶段的容错范围内。
		return &ast.Program{}
	}
	program, _ := parser.New(lexer.New(text, token.V05)).ParseProgramRecovering()
	return program
}

// analysisKey 唯一标识一份文档快照。
type analysisKey struct {
	version int
	hash    uint64
}

type analysisEntry struct {
	key      analysisKey
	ready    chan struct{}
	analysis *analysis
	building bool
	lastUsed int64
}

// analysisService 按文档缓存语义快照，并提供单飞构建、依赖失效与容量上限。
type analysisService struct {
	mu         sync.Mutex
	entries    map[string]*analysisEntry
	deps       map[string]map[string]struct{} // 文件路径 → 依赖它的文档 URI
	byURI      map[string][]string            // 文档 URI → 依赖文件路径
	slots      chan struct{}
	maxEntries int
	builds     int
	clock      int64
}

func newAnalysisService(maxConcurrent, maxEntries int) *analysisService {
	if maxEntries <= 0 {
		maxEntries = defaultMaxAnalyses
	}
	return &analysisService{
		entries:    make(map[string]*analysisEntry),
		deps:       make(map[string]map[string]struct{}),
		byURI:      make(map[string][]string),
		slots:      make(chan struct{}, maxConcurrent),
		maxEntries: maxEntries,
	}
}

// get 返回文档当前版本的语义快照；必要时在并发上限内构建。
func (s *analysisService) get(ctx context.Context, doc *document, build func(context.Context) *analysis) *analysis {
	key := analysisKey{version: doc.Version, hash: hashSource(doc.Text)}
	for {
		s.mu.Lock()
		entry := s.entries[doc.URI]
		if entry != nil && entry.key == key {
			if entry.building {
				ready := entry.ready
				s.mu.Unlock()
				select {
				case <-ready:
					if entry.analysis == nil && ctx.Err() == nil {
						// 共享构建被调用方取消，当前请求重新尝试。
						continue
					}
					return entry.analysis
				case <-ctx.Done():
					return nil
				}
			}
			entry.lastUsed = s.nextClockLocked()
			result := entry.analysis
			s.mu.Unlock()
			if result != nil && !result.dependenciesFresh() {
				s.invalidateEntry(doc.URI, entry)
				continue
			}
			return result
		}
		entry = &analysisEntry{key: key, ready: make(chan struct{}), building: true, lastUsed: s.nextClockLocked()}
		s.entries[doc.URI] = entry
		s.mu.Unlock()
		return s.build(ctx, doc, entry, build)
	}
}

func (s *analysisService) nextClockLocked() int64 {
	s.clock++
	return s.clock
}

func (s *analysisService) build(ctx context.Context, doc *document, entry *analysisEntry, build func(context.Context) *analysis) *analysis {
	s.mu.Lock()
	s.builds++
	s.mu.Unlock()

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		s.publish(entry, nil)
		return nil
	}
	result := build(ctx)
	s.publish(entry, result)
	if result != nil {
		s.registerDependencies(result)
	}
	return result
}

func (s *analysisService) publish(entry *analysisEntry, result *analysis) {
	s.mu.Lock()
	entry.analysis = result
	entry.building = false
	close(entry.ready)
	s.mu.Unlock()
	s.evict()
}

// registerDependencies 记录文档 → 依赖文件的反向索引。
func (s *analysisService) registerDependencies(result *analysis) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, path := range s.byURI[result.URI] {
		if dependents, ok := s.deps[path]; ok {
			delete(dependents, result.URI)
			if len(dependents) == 0 {
				delete(s.deps, path)
			}
		}
	}
	dependencies := make([]string, 0, len(result.Files))
	for _, path := range result.Files {
		if path == "" {
			continue
		}
		canonical := canonicalPath(path)
		if canonical == canonicalPath(result.Path) {
			continue
		}
		dependencies = append(dependencies, canonical)
		dependents := s.deps[canonical]
		if dependents == nil {
			dependents = make(map[string]struct{})
			s.deps[canonical] = dependents
		}
		dependents[result.URI] = struct{}{}
	}
	s.byURI[result.URI] = dependencies
}

// invalidate 丢弃文档缓存，并连带失效依赖该文档磁盘文件的其它文档。
func (s *analysisService) invalidate(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeLocked(uri)
	if path, ok := pathFromURI(uri); ok {
		canonical := canonicalPath(path)
		if dependents, exists := s.deps[canonical]; exists {
			for dependent := range dependents {
				s.removeLocked(dependent)
			}
			delete(s.deps, canonical)
		}
	}
}

// invalidateAll 清空全部缓存。
func (s *analysisService) invalidateAll() {
	s.mu.Lock()
	s.entries = make(map[string]*analysisEntry)
	s.deps = make(map[string]map[string]struct{})
	s.byURI = make(map[string][]string)
	s.mu.Unlock()
}

// invalidateEntry 仅在缓存项仍是同一个对象时删除。
func (s *analysisService) invalidateEntry(uri string, entry *analysisEntry) {
	s.mu.Lock()
	if s.entries[uri] == entry {
		s.removeLocked(uri)
	}
	s.mu.Unlock()
}

func (s *analysisService) removeLocked(uri string) {
	delete(s.entries, uri)
	for _, path := range s.byURI[uri] {
		if dependents, ok := s.deps[path]; ok {
			delete(dependents, uri)
			if len(dependents) == 0 {
				delete(s.deps, path)
			}
		}
	}
	delete(s.byURI, uri)
}

// evict 在缓存超过上限时淘汰最久未使用的已完成项。
func (s *analysisService) evict() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) <= s.maxEntries {
		return
	}
	var oldestURI string
	var oldest int64
	for uri, entry := range s.entries {
		if entry.building {
			continue
		}
		if oldestURI == "" || entry.lastUsed < oldest {
			oldestURI = uri
			oldest = entry.lastUsed
		}
	}
	if oldestURI != "" {
		s.removeLocked(oldestURI)
	}
}

// buildCount 返回累计构建次数，供性能测试验证缓存命中。
func (s *analysisService) buildCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.builds
}

func hashSource(text string) uint64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(text))
	return hasher.Sum64()
}
