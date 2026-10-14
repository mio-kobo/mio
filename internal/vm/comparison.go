package vm

import (
	"fmt"

	"Mio/internal/code"
	"Mio/internal/value"
)

// executeComparison 执行二元比较运算
func (v *VM) executeComparison(
	op code.Opcode,
	name string,
) error {
	size := len(v.stack)

	// 比较运算需要两个操作数
	if size < 2 {
		return fmt.Errorf(
			"stack underflow for %s",
			name,
		)
	}

	// 栈顶是右操作数
	right := v.stack[size-1]
	left := v.stack[size-2]

	var result bool

	switch op {

	// 相等与不等比较
	case code.OpEqual, code.OpNotEqual:
		// 两侧必须是相同类型
		if left.Kind() != right.Kind() {
			return fmt.Errorf(
				"%s requires matching types, got %s and %s",
				name,
				left.Kind(),
				right.Kind(),
			)
		}

		switch left.Kind() {
		case value.KindInt:
			a, _ := left.Int64()
			b, _ := right.Int64()
			result = a == b

		case value.KindFloat:
			a, _ := left.Float64()
			b, _ := right.Float64()
			result = a == b

		case value.KindBool:
			a, _ := left.Bool()
			b, _ := right.Bool()
			result = a == b

		case value.KindString:
			a, _ := left.Text()
			b, _ := right.Text()
			result = a == b

		default:
			return fmt.Errorf(
				"%s does not support %s operands",
				name,
				left.Kind(),
			)
		}

		// 不等比较是相等比较的取反
		if op == code.OpNotEqual {
			result = !result
		}

	// 大小比较
	case code.OpLess,
		code.OpLessEqual,
		code.OpGreater,
		code.OpGreaterEqual:

		if left.Kind() != right.Kind() || (left.Kind() != value.KindInt && left.Kind() != value.KindFloat) {
			return fmt.Errorf(
				"%s requires int64 operands or matching float64 operands, got %s and %s",
				name,
				left.Kind(),
				right.Kind(),
			)
		}

		if left.Kind() == value.KindFloat {
			a, _ := left.Float64()
			b, _ := right.Float64()
			result = compareFloat(op, a, b)
		} else {
			a, _ := left.Int64()
			b, _ := right.Int64()
			switch op {
			case code.OpLess:
				result = a < b
			case code.OpLessEqual:
				result = a <= b
			case code.OpGreater:
				result = a > b
			case code.OpGreaterEqual:
				result = a >= b
			}
		}

	default:
		return fmt.Errorf(
			"unsupported comparison opcode %d",
			op,
		)
	}

	// 比较成功后弹出操作数并压入布尔结果
	v.stack = v.stack[:size-2]
	v.stack = append(
		v.stack,
		value.NewBool(result),
	)

	return nil
}

// compareFloat 执行浮点大小比较
func compareFloat(op code.Opcode, left, right float64) bool {
	switch op {
	case code.OpLess:
		return left < right
	case code.OpLessEqual:
		return left <= right
	case code.OpGreater:
		return left > right
	case code.OpGreaterEqual:
		return left >= right
	default:
		return false
	}
}
