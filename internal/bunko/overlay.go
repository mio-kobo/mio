package bunko

import (
	"path/filepath"
)

// Overlay 保存编辑器内存中的源文件内容，键为规范化后的绝对路径。
// 它只在读取文件时覆盖磁盘内容，不会改变目录枚举结果。
type Overlay struct {
	files map[string][]byte
}

// NewOverlay 创建空的内存覆盖层
func NewOverlay() Overlay {
	return Overlay{files: make(map[string][]byte)}
}

// Set 写入一份内存源文件
func (o *Overlay) Set(path string, content []byte) {
	if o == nil {
		return
	}
	if o.files == nil {
		o.files = make(map[string][]byte)
	}
	copied := append([]byte(nil), content...)
	o.files[overlayKey(path)] = copied
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		o.files[filepath.Clean(resolved)] = copied
	}
}

// Len 返回覆盖层中的文件数量
func (o Overlay) Len() int {
	return len(o.files)
}

// Has 判断路径是否被覆盖
func (o Overlay) Has(path string) bool {
	_, ok := o.Lookup(path)
	return ok
}

// Lookup 读取覆盖内容；未命中时返回 false
func (o Overlay) Lookup(path string) ([]byte, bool) {
	if len(o.files) == 0 {
		return nil, false
	}
	if content, ok := o.files[overlayKey(path)]; ok {
		return content, true
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, false
	}
	content, ok := o.files[filepath.Clean(resolved)]
	return content, ok
}

func (o Overlay) readFile(path string) ([]byte, bool) {
	content, ok := o.Lookup(path)
	if !ok {
		return nil, false
	}
	return append([]byte(nil), content...), true
}

func overlayKey(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}
