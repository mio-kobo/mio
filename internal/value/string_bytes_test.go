package value

import "testing"

// 测试字符串与紧凑字节数组复制隔离
func TestStringByteArrayCopies(t *testing.T) {
	array, err := NewByteArrayFromString("A\x00你🍡")
	if err != nil {
		t.Fatal(err)
	}
	if !array.SetArrayElement(0, NewUint8('Z')) {
		t.Fatal("无法修改测试数组")
	}
	text, valid, err := array.UTF8StringCopy()
	if err != nil || !valid || text != "Z\x00你🍡" {
		t.Fatalf("text=%q valid=%v err=%v", text, valid, err)
	}
	if !array.SetArrayElement(0, NewUint8('Q')) || text != "Z\x00你🍡" {
		t.Fatal("字符串与数组未隔离")
	}
	copy, ok := array.ByteArrayCopy()
	if !ok || string(copy) != "Q\x00你🍡" {
		t.Fatalf("copy=%q ok=%v", copy, ok)
	}
	copy[0] = 'X'
	element, _ := array.ArrayElement(0)
	value, _ := element.Uint8()
	if value != 'Q' {
		t.Fatal("复制结果与数组共享存储")
	}
}

// 测试 UTF-8 转换拒绝非法字节并接受空数组
func TestUTF8StringCopyValidation(t *testing.T) {
	empty, err := NewByteArray(nil)
	if err != nil {
		t.Fatal(err)
	}
	text, valid, err := empty.UTF8StringCopy()
	if err != nil || !valid || text != "" {
		t.Fatalf("text=%q valid=%v err=%v", text, valid, err)
	}
	invalid, err := NewByteArray([]byte{0xc0, 0x80})
	if err != nil {
		t.Fatal(err)
	}
	if text, valid, err = invalid.UTF8StringCopy(); err != nil || valid || text != "" {
		t.Fatalf("text=%q valid=%v err=%v", text, valid, err)
	}
	if _, _, err := NewArray([]Value{NewInt(1)}).UTF8StringCopy(); err == nil {
		t.Fatal("非 uint8 数组转换未报错")
	}
}
