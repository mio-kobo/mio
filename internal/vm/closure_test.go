package vm

import (
	"bytes"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
)

// 测试闭包返回后共享并修改捕获变量
func TestVMClosureMutableCapture(t *testing.T) {
	output := runFunctionSource(t, `
waza make_counter() -> () -> int64 {
    kaku count = 0;
    kaesu waza () -> int64 {
        count = count + 1;
        kaesu count;
    };
}
kaku first = make_counter();
kaku second = make_counter();
chant(first());
chant(first());
chant(second());
chant(first());
`)
	if output != "1\n2\n1\n3\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试无捕获匿名函数作为普通函数值
func TestVMUncapturedFunctionValue(t *testing.T) {
	output := runFunctionSource(t, `
kaku double = waza (value: int64) -> int64 { kaesu value * 2; };
chant(double(21));
`)
	if output != "42\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试闭包存入结构体字段后调用
func TestVMClosureInStructField(t *testing.T) {
	output := runFunctionSource(t, `
kata Handler { transform: (int64) -> int64; }
kaku offset = 3;
kaku handler = Handler { transform: waza (value: int64) -> int64 { kaesu value + offset; } };
chant(handler.transform(39));
`)
	if output != "42\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试匿名闭包通过共享 Cell 递归调用自身
func TestVMRecursiveClosure(t *testing.T) {
	output := runFunctionSource(t, `
waza make_factorial(offset: int64) -> (int64) -> int64 {
    kaku factorial: (int64) -> int64 = waza (value: int64) -> int64 {
        moshi (value <= 1) { kaesu offset; }
        kaesu value * factorial(value - 1);
    };
    kaesu factorial;
}
kaku factorial = make_factorial(1);
chant(factorial(5));
`)
	if output != "120\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试多个闭包共享同一 Cell
func TestVMClosuresShareCapture(t *testing.T) {
	output := runFunctionSource(t, `
waza make_pair() -> [](() -> int64) {
    kaku value = 0;
    kaku increment = waza () -> int64 { value = value + 1; kaesu value; };
    kaku read = waza () -> int64 { kaesu value; };
    kaesu [increment, read];
}
kaku pair = make_pair();
chant(pair[0]());
chant(pair[1]());
chant(pair[0]());
chant(pair[1]());
`)
	if output != "1\n1\n2\n2\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试嵌套捕获、遮蔽和只捕获实际变量
func TestVMNestedClosureAndShadowing(t *testing.T) {
	output := runFunctionSource(t, `
waza outer() -> () -> int64 {
    kaku value = 4;
    kaku middle = waza () -> () -> int64 {
        kaku shadow = 10;
        kaesu waza () -> int64 { value = value + 1; kaesu value + shadow; };
    };
    kaesu middle();
}
kaku closure = outer();
chant(closure());
chant(closure());
`)
	if output != "15\n16\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试循环每轮声明创建独立捕获绑定
func TestVMLoopClosureBindingsAreIndependent(t *testing.T) {
	output := runFunctionSource(t, `
waza zero() -> int64 { kaesu 0; }
waza make_all() -> [](() -> int64) {
    kaku result = [zero, zero, zero];
    kaku index = 0;
    aida (index < 3) {
        kaku current = index;
        result[index] = waza () -> int64 { kaesu current; };
        index = index + 1;
    }
    kaesu result;
}
kaku all = make_all();
chant(all[0]());
chant(all[1]());
chant(all[2]());
`)
	if output != "0\n1\n2\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试高阶泛型函数、结构体与数组捕获
func TestVMGenericHigherOrderAndCompositeCapture(t *testing.T) {
	output := runFunctionSource(t, `
kata Box { value: int64; }
waza apply<T, U>(value: T, transform: (T) -> U) -> U { kaesu transform(value); }
waza build(threshold: int64) -> (int64) -> int64 {
    kaku box = Box { value: threshold };
    kaku values = [1, 2, 3];
    kaesu waza (input: int64) -> int64 { values[0] = values[0] + 1; kaesu input + box.value + values[0]; };
}
kaku transform = build(10);
chant(apply(5, transform));
chant(apply(5, transform));
`)
	if output != "17\n18\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试捕获阈值的闭包参与数组遍历
func TestVMClosureDuringArrayTraversal(t *testing.T) {
	output := runFunctionSource(t, `
waza count_matching(items: []int64, predicate: (int64) -> bool) -> int64 {
    kaku index = 0;
    kaku count = 0;
    aida (index < len(items)) {
        moshi (predicate(items[index])) { count = count + 1; }
        index = index + 1;
    }
    kaesu count;
}
waza count_above(items: []int64, threshold: int64) -> int64 {
    kaesu count_matching(items, waza (value: int64) -> bool { kaesu value > threshold; });
}
chant(count_above([1, 4, 2, 5], 2));
`)
	if output != "2\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试 unit 闭包保持 Value Stack 平衡
func TestVMUnitClosureStackBalance(t *testing.T) {
	bytecode := compileFunctionSource(t, `
waza make() -> () -> unit {
    kaku value = 7;
    kaesu waza () -> unit { chant(value); kaesu; };
}
kaku show = make();
show();
show();
`)
	var output bytes.Buffer
	machine := NewWithOutput(bytecode, &output)
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "7\n7\n" || len(machine.stack) != 0 {
		t.Fatalf("output=%q stack=%d", output.String(), len(machine.stack))
	}
}

// 测试闭包 Mochi 往返保持运行语义
func TestVMClosureMochiRoundTrip(t *testing.T) {
	bytecode := compileFunctionSource(t, `
waza make() -> () -> int64 { kaku n = 40; kaesu waza () -> int64 { n = n + 1; kaesu n; }; }
kaku next = make(); chant(next()); chant(next());
`)
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, bytecode); err != nil {
		t.Fatal(err)
	}
	decoded, err := mochi.Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := NewWithOutput(decoded, &output).Run(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "41\n42\n" {
		t.Fatalf("output=%q", output.String())
	}
}

// 测试 VM 拒绝非法闭包捕获索引并清理状态
func TestVMRejectsInvalidClosureBytecode(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Instructions: code.Instructions{byte(code.OpCall), 0, 0},
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "outer", Instructions: code.Instructions{byte(code.OpClosure), 0, 1, byte(code.OpPop), byte(code.OpReturn)}},
			{ID: 1, Name: "bad", Captures: []compiler.Capture{{Local: true, Index: 0}}, Instructions: code.Instructions{byte(code.OpReturn)}},
		},
	}
	machine := New(bytecode)
	err := machine.Run()
	if err == nil || !strings.Contains(err.Error(), "local capture index 0 out of range") {
		t.Fatalf("error=%v", err)
	}
	if len(machine.stack) != 0 || len(machine.frames) != 0 || len(machine.globals) != 0 {
		t.Fatal("closure error left runtime state")
	}
}
