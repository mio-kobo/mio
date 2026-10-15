package mma

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
)

// Resource 是只能由 ResourceManager 创建的不透明资源句柄
type Resource struct {
	manager    *ResourceManager
	slot       uint32
	generation uint64
}

type resourceSlot struct {
	generation uint64
	typeName   string
	value      any
	close      func(any) error
	active     bool
}

// ResourceManager 管理单个 Runtime 的原生资源
type ResourceManager struct {
	mu     sync.Mutex
	slots  []resourceSlot
	free   []uint32
	closed bool
}

// NewResourceManager 创建独立资源表
func NewResourceManager() *ResourceManager {
	manager := &ResourceManager{}
	runtime.SetFinalizer(manager, func(value *ResourceManager) { _ = value.Cleanup() })
	return manager
}

// Create 创建带类型标签的资源句柄
func (m *ResourceManager) Create(typeName string, value any, close func(any) error) (Resource, error) {
	if m == nil {
		return Resource{}, fmt.Errorf("mma: nil resource manager")
	}
	if typeName == "" || value == nil {
		return Resource{}, fmt.Errorf("mma: invalid resource")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Resource{}, fmt.Errorf("mma: resource manager is closed")
	}
	var index uint32
	if count := len(m.free); count != 0 {
		index = m.free[count-1]
		m.free = m.free[:count-1]
	} else {
		index = uint32(len(m.slots))
		m.slots = append(m.slots, resourceSlot{})
	}
	slot := &m.slots[index]
	slot.generation++
	if slot.generation == 0 {
		slot.generation++
	}
	slot.typeName = typeName
	slot.value = value
	slot.close = close
	slot.active = true
	return Resource{manager: m, slot: index, generation: slot.generation}, nil
}

// Use 校验句柄并读取指定类型的资源
func (m *ResourceManager) Use(handle Resource, typeName string) (any, error) {
	if m == nil || handle.manager != m {
		return nil, Errors.New("invalid_resource", "资源句柄不属于当前 Runtime", nil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if int(handle.slot) >= len(m.slots) {
		return nil, Errors.New("invalid_resource", "资源句柄无效", nil)
	}
	slot := &m.slots[handle.slot]
	if !slot.active || slot.generation != handle.generation {
		return nil, Errors.New("closed_resource", "资源已经关闭", nil)
	}
	if slot.typeName != typeName {
		return nil, Errors.New("resource_type", fmt.Sprintf("资源类型为 %s，不能作为 %s 使用", slot.typeName, typeName), nil)
	}
	return slot.value, nil
}

// Close 关闭资源并使所有别名同时失效
func (m *ResourceManager) Close(handle Resource, typeName string) error {
	if m == nil || handle.manager != m {
		return Errors.New("invalid_resource", "资源句柄不属于当前 Runtime", nil)
	}
	m.mu.Lock()
	if int(handle.slot) >= len(m.slots) {
		m.mu.Unlock()
		return Errors.New("invalid_resource", "资源句柄无效", nil)
	}
	slot := &m.slots[handle.slot]
	if !slot.active || slot.generation != handle.generation {
		m.mu.Unlock()
		return Errors.New("closed_resource", "资源已经关闭", nil)
	}
	if slot.typeName != typeName {
		actual := slot.typeName
		m.mu.Unlock()
		return Errors.New("resource_type", fmt.Sprintf("资源类型为 %s，不能作为 %s 使用", actual, typeName), nil)
	}
	closeResource := slot.close
	value := slot.value
	slot.active = false
	slot.typeName = ""
	slot.value = nil
	slot.close = nil
	m.free = append(m.free, handle.slot)
	m.mu.Unlock()
	if closeResource != nil {
		return closeResource(value)
	}
	return nil
}

// Cleanup 关闭当前仍然存活的全部资源
func (m *ResourceManager) Cleanup() error {
	return m.cleanup(false)
}

// Shutdown 关闭资源表并拒绝后续创建
func (m *ResourceManager) Shutdown() error {
	return m.cleanup(true)
}

func (m *ResourceManager) cleanup(shutdown bool) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if shutdown {
		m.closed = true
	}
	type pendingClose struct {
		value any
		close func(any) error
	}
	pending := make([]pendingClose, 0)
	m.free = m.free[:0]
	for index := range m.slots {
		slot := &m.slots[index]
		if slot.active {
			pending = append(pending, pendingClose{value: slot.value, close: slot.close})
			slot.active = false
			slot.typeName = ""
			slot.value = nil
			slot.close = nil
		}
		m.free = append(m.free, uint32(index))
	}
	m.mu.Unlock()
	var result error
	for _, item := range pending {
		if item.close != nil {
			result = errors.Join(result, item.close(item.value))
		}
	}
	return result
}
