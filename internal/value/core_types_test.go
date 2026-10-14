package value

import "testing"

// 测试结构体保持不可变字段值
func TestStructValue(t *testing.T) {
	fields := []Value{NewInt(42)}
	structure := NewStruct("Box", fields)
	fields[0] = NewInt(0)
	field, ok := structure.StructField(0)
	if !ok || field != NewInt(42) {
		t.Fatalf("field=%v ok=%v", field, ok)
	}
}

// 测试 Map 共享修改并限制键值种类
func TestMapValue(t *testing.T) {
	mapping := NewMap()
	alias := mapping
	if !mapping.MapSet(NewString("answer"), NewInt(42)) {
		t.Fatal("写入 Map 失败")
	}
	item, found, valid := alias.MapGet(NewString("answer"))
	if !valid || !found || item != NewInt(42) {
		t.Fatalf("item=%v found=%v valid=%v", item, found, valid)
	}
	if mapping.MapSet(NewArray(nil), NewInt(1)) {
		t.Fatal("不应接受不可哈希键")
	}
	if mapping.MapSet(NewString("bad"), NewString("value")) {
		t.Fatal("不应接受不同运行时值种类")
	}
}

// 测试枚举与函数值读取
func TestEnumAndFunctionValue(t *testing.T) {
	enum := NewEnum("Option", 1, []Value{NewString("Mio")})
	if tag, ok := enum.EnumTag(); !ok || tag != 1 {
		t.Fatalf("tag=%d ok=%v", tag, ok)
	}
	if item, ok := enum.EnumValue(0); !ok || item != NewString("Mio") {
		t.Fatalf("item=%v ok=%v", item, ok)
	}
	if id, ok := NewFunction(7).FunctionID(); !ok || id != 7 {
		t.Fatalf("id=%d ok=%v", id, ok)
	}
}
