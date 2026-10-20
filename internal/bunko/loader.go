package bunko

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const sourceExtension = ".mio"

// Source 保存包内单个 Mio 源文件
type Source struct {
	Name    string
	Path    string
	Content []byte
}

// Package 保存已读取的 Bunko 包
type Package struct {
	ImportPath string
	Directory  string
	Sources    []Source
}

// Loader 按公开导入路径读取 Bunko 包
type Loader struct {
	root    string
	overlay Overlay
}

// New 创建 Bunko 包加载器
func New(root string) *Loader {
	return NewWithOverlay(root, Overlay{})
}

// NewWithOverlay 创建带内存覆盖层的 Bunko 包加载器
func NewWithOverlay(root string, overlay Overlay) *Loader {
	return &Loader{root: filepath.Clean(root), overlay: overlay}
}

// Load 读取导入路径对应目录中的 Mio 源文件
func (l *Loader) Load(importPath string) (*Package, error) {
	if err := validateImportPath(importPath); err != nil {
		return nil, err
	}

	root, err := filepath.EvalSymlinks(l.root)
	if err != nil {
		return nil, fmt.Errorf("resolve Bunko root %q: %w", l.root, err)
	}
	directory, packageRoot, err := resolvePackageDirectory(root, importPath)
	if err != nil {
		return nil, err
	}
	if err := requireWithinRoot(packageRoot, directory); err != nil {
		return nil, fmt.Errorf("resolve package %q: %w", importPath, err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read Bunko package %q: %w", importPath, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && isRegularSource(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("Bunko package %q has no %s source files", importPath, sourceExtension)
	}

	sources := make([]Source, 0, len(names))
	for _, name := range names {
		filePath := filepath.Join(directory, name)
		content, err := l.readSourceFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read Bunko source %q: %w", filePath, err)
		}
		sources = append(sources, Source{Name: name, Path: filePath, Content: content})
	}

	return &Package{ImportPath: importPath, Directory: directory, Sources: sources}, nil
}

// readSourceFile 优先读取内存覆盖层，其次读取磁盘
func (l *Loader) readSourceFile(path string) ([]byte, error) {
	if content, ok := l.overlay.readFile(path); ok {
		return content, nil
	}
	return os.ReadFile(path)
}

// resolvePackageDirectory 依次查找 Bunko 与同级工作区包
func resolvePackageDirectory(bunkoRoot, importPath string) (string, string, error) {
	relative := filepath.FromSlash(importPath)
	roots := []string{bunkoRoot}
	if filepath.Base(bunkoRoot) == "bunko" {
		roots = append(roots, filepath.Dir(bunkoRoot))
	}
	var lastErr error
	for _, root := range roots {
		directory, err := filepath.EvalSymlinks(filepath.Join(root, relative))
		if err == nil {
			if withinErr := requireWithinRoot(root, directory); withinErr != nil {
				return "", "", fmt.Errorf("resolve package %q: %w", importPath, withinErr)
			}
			return directory, root, nil
		}
		lastErr = err
	}
	return "", "", fmt.Errorf("resolve package %q: %w", importPath, lastErr)
}

func isTestSource(name string) bool {
	return strings.HasSuffix(name, "_test"+sourceExtension)
}

func isRegularSource(name string) bool {
	return filepath.Ext(name) == sourceExtension && !isTestSource(name)
}

// validateImportPath 检查公开导入路径
func validateImportPath(importPath string) error {
	if importPath == "" || strings.Contains(importPath, "\\") || strings.HasPrefix(importPath, "/") || path.Clean(importPath) != importPath {
		return fmt.Errorf("invalid Bunko import path %q", importPath)
	}
	parts := strings.Split(importPath, "/")
	if parts[0] == "bunko" {
		return fmt.Errorf("Bunko import path %q exposes the physical root", importPath)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid Bunko import path %q", importPath)
		}
	}
	return nil
}

// requireWithinRoot 防止包路径越过 Bunko 根目录
func requireWithinRoot(root, directory string) error {
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("package directory escapes Bunko root")
	}
	return nil
}
