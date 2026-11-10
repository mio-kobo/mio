package lsp

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"Mio/internal/ast"
)

const (
	// 工作区扫描的硬上限，保证大型目录下内存与响应时间有界。
	workspaceSymbolMaxFiles   = 5000
	workspaceSymbolMaxBytes   = 2 << 20
	workspaceSymbolMaxResults = 200
)

// SymbolInformation 表示 workspace/symbol 结果项。
type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

type workspaceSymbolParams struct {
	Query string `json:"query"`
}

// fileSymbols 缓存某个磁盘文件（按 mtime+size 校验）的顶层声明。
type fileSymbols struct {
	stamp   fileStamp
	symbols []SymbolInformation
}

type workspaceSymbolCache struct {
	mu      sync.Mutex
	entries map[string]fileSymbols
}

func (c *workspaceSymbolCache) get(path string, stamp fileStamp) ([]SymbolInformation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[path]
	if !ok || entry.stamp.size != stamp.size || !entry.stamp.modTime.Equal(stamp.modTime) {
		return nil, false
	}
	return entry.symbols, true
}

func (c *workspaceSymbolCache) put(path string, stamp fileStamp, symbols []SymbolInformation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil || len(c.entries) >= workspaceSymbolMaxFiles {
		c.entries = make(map[string]fileSymbols)
	}
	c.entries[path] = fileSymbols{stamp: stamp, symbols: symbols}
}

// skippedWorkspaceDir 判断扫描时应跳过的目录（隐藏目录与常见产物目录）。
func skippedWorkspaceDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "build", "dist", "out", "target":
		return true
	}
	return false
}

// requestWorkspaceSymbol 实现 workspace/symbol。
// 只做顶层声明的轻量容错解析（不构建完整语义索引）；打开的文档使用编辑器中的文本，
// 其余 .mio 文件读取磁盘，并按 mtime/size 缓存。不跟随符号链接目录。
func (s *server) requestWorkspaceSymbol(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var value workspaceSymbolParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	open := make(map[string]*document)
	for _, doc := range s.store.all() {
		if path, ok := pathFromURI(doc.URI); ok {
			open[canonicalPath(path)] = doc
		}
	}
	seen := make(map[string]struct{})
	var all []SymbolInformation
	scanned := 0
	addFile := func(path string, info fs.FileInfo) {
		canonical := canonicalPath(path)
		if _, dup := seen[canonical]; dup {
			return
		}
		seen[canonical] = struct{}{}
		if doc, ok := open[canonical]; ok {
			all = append(all, topLevelSymbols(doc.URI, doc.Text)...)
			return
		}
		if info == nil || info.Size() > workspaceSymbolMaxBytes {
			return
		}
		stamp := fileStamp{size: info.Size(), modTime: info.ModTime()}
		if cached, ok := s.workspaceSymbols.get(canonical, stamp); ok {
			all = append(all, cached...)
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		symbols := topLevelSymbols(uriFromPath(path), string(data))
		s.workspaceSymbols.put(canonical, stamp, symbols)
		all = append(all, symbols...)
	}
	for _, root := range s.workspaceRoots {
		if ctx.Err() != nil {
			return []SymbolInformation{}, nil
		}
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return filepath.SkipAll
			}
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				if path != root && skippedWorkspaceDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".mio") {
				return nil
			}
			if scanned >= workspaceSymbolMaxFiles {
				return filepath.SkipAll
			}
			scanned++
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			addFile(path, info)
			return nil
		})
	}
	// 位于工作区之外但已打开的文档同样可搜索。
	for canonical, doc := range open {
		if _, dup := seen[canonical]; !dup {
			seen[canonical] = struct{}{}
			all = append(all, topLevelSymbols(doc.URI, doc.Text)...)
		}
	}
	if ctx.Err() != nil {
		return []SymbolInformation{}, nil
	}
	return rankWorkspaceSymbols(all, value.Query), nil
}

// topLevelSymbols 对一份源码做容错解析并返回其顶层声明。
func topLevelSymbols(uri, text string) []SymbolInformation {
	program := parseRecovering(text)
	if program == nil {
		return nil
	}
	buffer := newTextBuffer(text)
	var symbols []SymbolInformation
	add := func(name *ast.Identifier, kind int) {
		if name == nil || name.Name == "" {
			return
		}
		symbols = append(symbols, SymbolInformation{
			Name: name.Name,
			Kind: kind,
			Location: Location{URI: uri, Range: Range{
				Start: buffer.positionAt(name.Range.Start.Offset),
				End:   buffer.positionAt(name.Range.End.Offset),
			}},
		})
	}
	for _, statement := range program.Statements {
		switch node := statement.(type) {
		case *ast.FunctionStatement:
			add(node.Name, symbolKindFunction)
		case *ast.StructStatement:
			add(node.Name, symbolKindStruct)
		case *ast.EnumStatement:
			add(node.Name, symbolKindEnum)
		case *ast.ConstStatement:
			add(node.Name, symbolKindConstant)
		case *ast.LetStatement:
			add(node.Name, symbolKindVariable)
		}
	}
	return symbols
}

// subsequenceScore 返回 query 在 name 中的大小写不敏感子序列匹配分数（越小越好）；
// 不匹配返回 -1。空 query 匹配全部。
func subsequenceScore(name, query string) int {
	if query == "" {
		return 0
	}
	lowerName := []rune(strings.ToLower(name))
	lowerQuery := []rune(strings.ToLower(query))
	if strings.HasPrefix(string(lowerName), string(lowerQuery)) {
		return 0
	}
	if strings.Contains(string(lowerName), string(lowerQuery)) {
		return 1
	}
	next := 0
	for _, r := range lowerName {
		if next < len(lowerQuery) && unicode.ToLower(r) == lowerQuery[next] {
			next++
		}
	}
	if next == len(lowerQuery) {
		return 2
	}
	return -1
}

func rankWorkspaceSymbols(all []SymbolInformation, query string) []SymbolInformation {
	type scored struct {
		symbol SymbolInformation
		score  int
	}
	matches := make([]scored, 0)
	for _, symbol := range all {
		if score := subsequenceScore(symbol.Name, strings.TrimSpace(query)); score >= 0 {
			matches = append(matches, scored{symbol, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.score != b.score {
			return a.score < b.score
		}
		if a.symbol.Name != b.symbol.Name {
			return a.symbol.Name < b.symbol.Name
		}
		return a.symbol.Location.URI < b.symbol.Location.URI
	})
	if len(matches) > workspaceSymbolMaxResults {
		matches = matches[:workspaceSymbolMaxResults]
	}
	result := make([]SymbolInformation, len(matches))
	for i, match := range matches {
		result[i] = match.symbol
	}
	return result
}
