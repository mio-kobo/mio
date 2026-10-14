package resolver

import (
	"fmt"

	"Mio/internal/token"
)

// 符号种类
type SymbolKind string

const (
	SymbolVariable  SymbolKind = "variable"
	SymbolFunction  SymbolKind = "function"
	SymbolParameter SymbolKind = "parameter"
	SymbolBuiltin   SymbolKind = "builtin"
	SymbolType      SymbolKind = "type"
)

// 符号信息
type Symbol struct {
	ID   uint64
	Name string
	Kind SymbolKind
	Decl token.Position
}

// 作用域
type Scope struct {
	parent  *Scope
	symbols map[string]Symbol
}

// 创建全局作用域
func NewScope() *Scope {
	return &Scope{
		symbols: make(map[string]Symbol),
	}
}

// 创建子作用域
func NewEnclosedScope(parent *Scope) *Scope {
	return &Scope{
		parent:  parent,
		symbols: make(map[string]Symbol),
	}
}

// 定义普通变量符号
func (s *Scope) Define(name string) error {
	return s.DefineSymbol(Symbol{
		Name: name,
		Kind: SymbolVariable,
	})
}

// 定义包含元数据的符号
func (s *Scope) DefineSymbol(symbol Symbol) error {
	if _, exists := s.symbols[symbol.Name]; exists {
		return fmt.Errorf(
			"symbol %q already defined",
			symbol.Name,
		)
	}

	s.symbols[symbol.Name] = symbol
	return nil
}

// ResolveLocal 查找当前作用域中的符号
func (s *Scope) ResolveLocal(name string) (Symbol, bool) {
	symbol, ok := s.symbols[name]
	return symbol, ok
}

// 查找当前及父作用域中的符号
func (s *Scope) Resolve(name string) (Symbol, bool) {
	if symbol, ok := s.symbols[name]; ok {
		return symbol, true
	}

	if s.parent != nil {
		return s.parent.Resolve(name)
	}

	return Symbol{}, false
}
