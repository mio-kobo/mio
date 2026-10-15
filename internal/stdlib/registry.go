// Package stdlib 注册 Mio 随附的原生标准库模块。
package stdlib

import (
	"Mio/internal/nativefs"
	"Mio/mma"
)

// NewRegistry 创建标准库原生模块注册表
func NewRegistry() (*mma.ModuleRegistry, error) {
	registry := mma.NewModuleRegistry()
	if err := nativefs.Register(registry); err != nil {
		return nil, err
	}
	return registry, nil
}
