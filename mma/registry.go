package mma

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const bindingPrefix = "$mma::"

var (
	modulePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)
	symbolPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// FunctionDefinition 描述模块中的一个原生函数
type FunctionDefinition struct {
	Name      string
	Signature FunctionSignature
	Public    bool
	Function  NativeFunction
}

// ModuleDefinition 描述一个显式注册的原生模块
type ModuleDefinition struct {
	Path      string
	Functions []FunctionDefinition
}

// FunctionID 是与 Go 对象地址无关的函数身份
type FunctionID struct {
	Module string
	Name   string
}

// RegisteredFunction 保存只读的已注册函数信息
type RegisteredFunction struct {
	ID         FunctionID
	Signature  FunctionSignature
	Public     bool
	Function   NativeFunction
	BindingKey string
}

// ModuleRegistry 保存单个编译或执行环境的原生模块
type ModuleRegistry struct {
	mu      sync.RWMutex
	modules map[string]map[string]RegisteredFunction
}

// NewModuleRegistry 创建空注册表
func NewModuleRegistry() *ModuleRegistry {
	return &ModuleRegistry{modules: make(map[string]map[string]RegisteredFunction)}
}

// RegisterModule 原子注册模块及其函数
func (r *ModuleRegistry) RegisterModule(definition ModuleDefinition) error {
	if r == nil {
		return fmt.Errorf("mma: nil module registry")
	}
	if !modulePattern.MatchString(definition.Path) {
		return fmt.Errorf("mma: invalid module path %q", definition.Path)
	}
	functions := make(map[string]RegisteredFunction, len(definition.Functions))
	for _, function := range definition.Functions {
		if !symbolPattern.MatchString(function.Name) {
			return fmt.Errorf("mma: invalid function name %q", function.Name)
		}
		if _, exists := functions[function.Name]; exists {
			return fmt.Errorf("mma: duplicate function %q in module %q", function.Name, definition.Path)
		}
		if function.Function == nil {
			return fmt.Errorf("mma: function %q in module %q has no implementation", function.Name, definition.Path)
		}
		if err := function.Signature.Validate(); err != nil {
			return fmt.Errorf("mma: function %q in module %q: %w", function.Name, definition.Path, err)
		}
		id := FunctionID{Module: definition.Path, Name: function.Name}
		functions[function.Name] = RegisteredFunction{
			ID: id, Signature: cloneSignature(function.Signature), Public: function.Public,
			Function: function.Function, BindingKey: bindingKey(id, function.Signature),
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.modules[definition.Path]; exists {
		return fmt.Errorf("mma: module %q is already registered", definition.Path)
	}
	r.modules[definition.Path] = functions
	return nil
}

// Module 返回模块的只读函数快照
func (r *ModuleRegistry) Module(path string) ([]RegisteredFunction, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	functions, ok := r.modules[path]
	if !ok {
		return nil, false
	}
	result := make([]RegisteredFunction, 0, len(functions))
	for _, function := range functions {
		function.Signature = cloneSignature(function.Signature)
		result = append(result, function)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.Name < result[j].ID.Name })
	return result, true
}

// Resolve 查找已注册函数
func (r *ModuleRegistry) Resolve(id FunctionID) (RegisteredFunction, bool) {
	if r == nil {
		return RegisteredFunction{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	functions, ok := r.modules[id.Module]
	if !ok {
		return RegisteredFunction{}, false
	}
	function, ok := functions[id.Name]
	function.Signature = cloneSignature(function.Signature)
	return function, ok
}

// ParseBindingKey 解析 Mochi 中的稳定原生函数标识
func ParseBindingKey(key string) (FunctionID, string, bool) {
	if !strings.HasPrefix(key, bindingPrefix) {
		return FunctionID{}, "", false
	}
	parts := strings.Split(strings.TrimPrefix(key, bindingPrefix), "::")
	if len(parts) != 3 || !modulePattern.MatchString(parts[0]) || !symbolPattern.MatchString(parts[1]) || len(parts[2]) != sha256.Size*2 {
		return FunctionID{}, "", false
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return FunctionID{}, "", false
	}
	return FunctionID{Module: parts[0], Name: parts[1]}, parts[2], true
}

// SignatureFingerprint 返回稳定的签名指纹
func SignatureFingerprint(signature FunctionSignature) string {
	sum := sha256.Sum256([]byte(signature.canonical()))
	return hex.EncodeToString(sum[:])
}

func bindingKey(id FunctionID, signature FunctionSignature) string {
	return bindingPrefix + id.Module + "::" + id.Name + "::" + SignatureFingerprint(signature)
}

func cloneSignature(signature FunctionSignature) FunctionSignature {
	signature.Parameters = append([]Type(nil), signature.Parameters...)
	return signature
}
