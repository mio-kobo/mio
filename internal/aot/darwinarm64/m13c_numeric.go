package darwinarm64

import (
	"fmt"
	"math"
)

const (
	integerInt8 = iota + 1
	integerInt16
	integerInt32
	integerInt64
	integerUint8
	integerUint16
	integerUint32
)

func (e *StackEmitter) numericLiteral(reg uint32, bits uint64) {
	e.words = append(e.words, movzX(reg, uint16(bits)))
	for shift := uint32(1); shift < 4; shift++ {
		e.words = append(e.words, movkX(reg, uint16(bits>>(shift*16)), shift))
	}
}

// IntegerConvert implements the VM's checked integer conversion contract.
// Values are already canonical in their eight-byte slots: signed narrow
// integers are sign-extended and unsigned narrow integers are zero-extended.
func (e *StackEmitter) IntegerConvert(sourceUnsigned bool, target int) error {
	if err := e.ensurePop("OpIntegerConvert", 1); err != nil {
		return err
	}
	var maximum uint64
	var minimum int64
	targetUnsigned := false
	switch target {
	case integerInt8:
		minimum, maximum = math.MinInt8, math.MaxInt8
	case integerInt16:
		minimum, maximum = math.MinInt16, math.MaxInt16
	case integerInt32:
		minimum, maximum = math.MinInt32, math.MaxInt32
	case integerInt64:
		minimum, maximum = math.MinInt64, math.MaxInt64
	case integerUint8:
		targetUnsigned, maximum = true, math.MaxUint8
	case integerUint16:
		targetUnsigned, maximum = true, math.MaxUint16
	case integerUint32:
		targetUnsigned, maximum = true, math.MaxUint32
	default:
		return fmt.Errorf("unknown integer target %d", target)
	}

	e.loadOperand(e.depth-1, 9)
	if sourceUnsigned {
		// All currently defined unsigned sources are at most uint32, so int64
		// is always a widening conversion. Other targets need only an upper
		// bound check using unsigned condition HI.
		if target != integerInt64 {
			e.numericLiteral(10, maximum)
			e.words = append(e.words, cmpX(9, 10))
			e.m6TrapIf(8) // HI
		}
	} else if targetUnsigned {
		e.words = append(e.words, cmpZeroX(9))
		e.m6TrapIf(11) // LT
		e.numericLiteral(10, maximum)
		e.words = append(e.words, cmpX(9, 10))
		e.m6TrapIf(12) // GT (both operands are now non-negative)
	} else if target != integerInt64 {
		e.numericLiteral(10, uint64(minimum))
		e.words = append(e.words, cmpX(9, 10))
		e.m6TrapIf(11) // LT
		e.numericLiteral(10, maximum)
		e.words = append(e.words, cmpX(9, 10))
		e.m6TrapIf(12) // GT
	}
	e.storeOperand(e.depth-1, 9)
	return nil
}

// NormalizeInteger applies the fixed-width wrap rule used after integer
// arithmetic. SBFM provides sign extension; UBFM provides zero extension.
func (e *StackEmitter) NormalizeInteger(width uint32, signed bool) error {
	if width == 64 {
		return nil
	}
	if width != 8 && width != 16 && width != 32 {
		return fmt.Errorf("invalid integer width %d", width)
	}
	if err := e.ensurePop("integer normalization", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	base := uint32(0xD3400000) // UBFM X9,X9,#0,#(width-1)
	if signed {
		base = 0x93400000 // SBFM
	}
	e.words = append(e.words, base|(width-1)<<10|9<<5|9)
	e.storeOperand(e.depth-1, 9)
	return nil
}

func (e *StackEmitter) DivideInteger(unsigned bool) error {
	if err := e.ensurePop("OpDiv", 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 9)
	e.loadOperand(e.depth-1, 10)
	e.words = append(e.words, cmpZeroX(10))
	e.m6TrapIf(0)                                  // EQ: division by zero
	word := uint32(0x9AC00C00 | 10<<16 | 9<<5 | 9) // SDIV X9,X9,X10
	if unsigned {
		word = udivX(9, 9, 10)
	}
	e.words = append(e.words, word)
	e.storeOperand(e.depth-2, 9)
	e.depth--
	return nil
}

// MathIsInf follows the VM definition for both binary32 and binary64.  FABS
// deliberately makes +Inf and -Inf equal while NaN remains unordered.
func (e *StackEmitter) MathIsInf(kind uint8, anyFloat bool) error {
	if kind != fp32Kind && kind != fp64Kind && !anyFloat {
		return fmt.Errorf("is_inf requires float32 or float64")
	}
	if err := e.ensurePop("OpMath is_inf", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	if anyFloat {
		constants := []uint64{
			math.Float64bits(math.Inf(1)), math.Float64bits(math.Inf(-1)),
			uint64(math.Float32bits(float32(math.Inf(1)))), uint64(math.Float32bits(float32(math.Inf(-1)))),
		}
		e.words = append(e.words, movzX(11, 0))
		for _, bits := range constants {
			e.numericLiteral(10, bits)
			e.words = append(e.words, cmpX(9, 10), csetW(12, 0), m12Or(11, 11, 12))
		}
		e.storeOperand(e.depth-1, 11)
		return nil
	}
	e.words = append(e.words, floatMoveFromX(kind, 0, 9))
	if kind == fp32Kind {
		e.words = append(e.words, 0x1E20C000) // FABS S0,S0
		e.numericLiteral(10, uint64(math.Float32bits(float32(math.Inf(1)))))
		e.words = append(e.words, floatMoveFromX(kind, 1, 10), 0x1E212000) // FCMP S0,S1
	} else {
		e.words = append(e.words, 0x1E60C000) // FABS D0,D0
		e.numericLiteral(10, math.Float64bits(math.Inf(1)))
		e.words = append(e.words, floatMoveFromX(kind, 1, 10), 0x1E612000) // FCMP D0,D1
	}
	e.words = append(e.words, csetW(9, 0)) // EQ; unordered NaN produces false
	e.storeOperand(e.depth-1, 9)
	return nil
}

// MathError is non-returning in the VM.  The common checked-runtime failure
// exit preserves that control-flow behavior and exits with status 1.
func (e *StackEmitter) MathError() error {
	if err := e.ensurePop("OpMath error", 1); err != nil {
		return err
	}
	e.depth--
	e.words = append(e.words, cmpZeroX(31)) // XZR == 0
	e.m6TrapIf(0)                           // EQ, therefore unconditional
	return nil
}
