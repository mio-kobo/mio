package darwinarm64

import (
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"testing"
)

type emu6b struct {
	r      [32]uint64
	pc, sp uint64
	nzcv   uint8
	mem    []byte
	out    []byte
	steps  int
}

const emask uint64 = ^uint64(0)

func (e *emu6b) get(r int) uint64 {
	if r == 31 {
		return 0
	}
	return e.r[r]
}
func (e *emu6b) base(r int) uint64 {
	if r == 31 {
		return e.sp
	}
	return e.r[r]
}
func (e *emu6b) put(r int, v uint64, bits int) {
	if r == 31 {
		return
	}
	if bits == 32 {
		v = uint64(uint32(v))
	}
	e.r[r] = v
}
func (e *emu6b) read(addr uint64, bytes int) uint64 {
	if addr+uint64(bytes) > uint64(len(e.mem)) {
		panic(fmt.Sprintf("read oob %#x pc=%#x step=%d sp=%#x", addr, e.pc*4, e.steps, e.sp))
	}
	var v uint64
	for i := 0; i < bytes; i++ {
		v |= uint64(e.mem[int(addr)+i]) << uint(i*8)
	}
	return v
}
func (e *emu6b) store(addr uint64, v uint64, bytes int) {
	if addr+uint64(bytes) > uint64(len(e.mem)) {
		panic(fmt.Sprintf("write oob %#x pc=%#x step=%d sp=%#x", addr, e.pc*4, e.steps, e.sp))
	}
	for i := 0; i < bytes; i++ {
		e.mem[int(addr)+i] = byte(v >> uint(i*8))
	}
}
func sx(v uint64, n uint) int64 {
	if v&(uint64(1)<<(n-1)) != 0 {
		return int64(v | (^uint64(0) << n))
	}
	return int64(v)
}
func (e *emu6b) flags(a, b, res uint64, bits int) {
	mask := emask
	sign := uint64(1) << 63
	if bits == 32 {
		mask = 0xffffffff
		sign = 1 << 31
	}
	a &= mask
	b &= mask
	res &= mask
	var f uint8
	if res&sign != 0 {
		f |= 8
	}
	if res == 0 {
		f |= 4
	}
	if a >= b {
		f |= 2
	}
	if (a^b)&(a^res)&sign != 0 {
		f |= 1
	}
	e.nzcv = f
}
func (e *emu6b) cond(c uint32) bool {
	n := e.nzcv&8 != 0
	z := e.nzcv&4 != 0
	cy := e.nzcv&2 != 0
	v := e.nzcv&1 != 0
	switch c {
	case 0:
		return z
	case 1:
		return !z
	case 2:
		return cy
	case 3:
		return !cy
	case 4:
		return n
	case 5:
		return !n
	case 6:
		return v
	case 7:
		return !v
	case 8:
		return cy && !z
	case 9:
		return !cy || z
	case 10:
		return n == v
	case 11:
		return n != v
	case 12:
		return !z && n == v
	case 13:
		return z || n != v
	case 14:
		return true
	}
	panic(c)
}
func run6b(bits uint64, kind uint8) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("emu panic: %v", r)
		}
	}()
	e := &emu6b{sp: 0x180000, mem: make([]byte, 2<<20)}
	e.r[0] = bits
	e.r[1] = uint64(kind)
	for i := 19; i <= 29; i++ {
		e.r[i] = 0x08000000 + uint64(i)*19
	}
	e.r[18] = 0xfeedcafe
	e.r[30] = 0x400000
	initial := e.r
	origSp := e.sp
	const maxSteps = 12000000
	for ; e.steps < maxSteps; e.steps++ {
		if e.sp&15 != 0 {
			return "", fmt.Errorf("SP lost 16-byte alignment at instruction %#x", e.pc*4)
		}
		if e.pc == 0x400000/4 {
			if e.sp != origSp {
				return "", fmt.Errorf("SP not restored: %#x", e.sp)
			}
			for i := 18; i <= 29; i++ {
				if e.r[i] != initial[i] {
					return "", fmt.Errorf("callee-saved x%d changed: %#x -> %#x", i, initial[i], e.r[i])
				}
			}
			return string(e.out), nil
		}
		if e.pc >= uint64(len(floatRuntimeWords)) {
			return "", fmt.Errorf("pc out of range %d", e.pc)
		}
		w := floatRuntimeWords[e.pc]
		rd, rn, rm := int(w&31), int(w>>5&31), int(w>>16&31)
		next := e.pc + 1
		sf := int(32)
		if (w>>31)&1 != 0 {
			sf = 64
		}
		if w == 0xA9BF7BFD {
			e.sp -= 16
			e.store(e.sp, e.r[29], 8)
			e.store(e.sp+8, e.r[30], 8)
		} else if w == 0xA8C17BFD {
			e.r[29] = e.read(e.sp, 8)
			e.r[30] = e.read(e.sp+8, 8)
			e.sp += 16
		} else if w == 0xA8817C1F {
			addr := e.r[0]
			e.store(addr, 0, 8)
			e.store(addr+8, 0, 8)
			e.r[0] += 16
		} else if w&0xFFC00000 == 0xA9000000 || w&0xFFC00000 == 0xA9400000 {
			ld := (w>>22)&1 == 1
			off := sx(uint64((w>>15)&0x7f), 7) * 8
			addr := uint64(int64(e.base(rn)) + off)
			rt2 := int(w >> 10 & 31)
			if ld {
				e.put(rd, e.read(addr, 8), 64)
				e.put(rt2, e.read(addr+8, 8), 64)
			} else {
				e.store(addr, e.get(rd), 8)
				e.store(addr+8, e.get(rt2), 8)
			}
		} else if w&0x7F800000 == 0x52800000 {
			shift := (w >> 21 & 3) * 16
			e.put(rd, uint64(w>>5&0xffff)<<shift, sf)
		} else if w&0x7F800000 == 0x72800000 {
			shift := (w >> 21 & 3) * 16
			mask := uint64(0xffff) << shift
			e.put(rd, (e.get(rd)&^mask)|(uint64(w>>5&0xffff)<<shift), sf)
		} else if w&0x7f800000 == 0x53000000 {
			imr := uint(w >> 16 & 63)
			ims := uint(w >> 10 & 63)
			v := e.get(rn)
			if sf == 32 {
				v &= 0xffffffff
			}
			if ims >= imr {
				width := ims - imr + 1
				v >>= imr
				if width < 64 {
					v &= (uint64(1) << width) - 1
				}
			} else {
				v <<= uint(sf) - imr
			}
			e.put(rd, v, sf)
		} else if w == 0xb24c016b {
			e.put(rd, e.get(rn)|(uint64(1)<<52), 64)
		} else if w == 0xb269016b {
			e.put(rd, e.get(rn)|(uint64(1)<<23), 64)
		} else if w == 0x9240016f {
			e.put(rd, e.get(rn)&1, 64)
		} else if w == 0xd24001ef {
			e.put(rd, e.get(rn)^1, 64)
		} else if w&0x1F000000 == 0x0A000000 {
			opc := (w >> 29) & 3
			shift := uint(w >> 10 & 63)
			v := e.get(rm) << shift
			a := e.get(rn)
			if sf == 32 {
				v = uint64(uint32(v))
				a = uint64(uint32(a))
			}
			switch opc {
			case 0:
				e.put(rd, a&v, sf)
			case 1:
				e.put(rd, a|v, sf)
			case 2:
				e.put(rd, a^v, sf)
			default:
				panic(fmt.Sprintf("logical reg %#x", w))
			}
		} else if w&0x1F000000 == 0x11000000 {
			op := (w >> 30) & 1
			S := (w >> 29) & 1
			imm := uint64(w >> 10 & 0xfff)
			if (w>>22)&1 != 0 {
				imm <<= 12
			}
			a := e.base(rn)
			if S != 0 && rn == 31 {
				a = 0
			}
			if sf == 32 {
				a = uint64(uint32(a))
			}
			var res uint64
			if op == 0 {
				res = a + imm
			} else {
				res = a - imm
			}
			if S != 0 {
				e.put(rd, res, sf)
				if op == 1 {
					e.flags(a, imm, res, sf)
				} else { // add flags
					mask := uint64(0xffffffffffffffff)
					if sf == 32 {
						mask = 0xffffffff
					}
					v := imm & mask
					res &= mask
					var f uint8
					if sf == 64 && res>>63 != 0 || sf == 32 && (res&(1<<31)) != 0 {
						f |= 8
					}
					if res == 0 {
						f |= 4
					}
					if a+v > mask {
						f |= 2
					}
					if (^(a ^ v))&(a^res)&(uint64(1)<<uint(sf-1)) != 0 {
						f |= 1
					}
					e.nzcv = f
				}
			} else if rd == 31 {
				e.sp = res
			} else {
				e.put(rd, res, sf)
			}
		} else if w&0x1F200000 == 0x0B000000 {
			op := (w >> 30) & 1
			S := (w >> 29) & 1
			shift := uint(w >> 10 & 63)
			a := e.get(rn)
			b := e.get(rm) << shift
			var res uint64
			if op == 0 {
				res = a + b
			} else {
				res = a - b
			}
			if S != 0 {
				e.put(rd, res, sf)
				e.flags(a, b, res, sf)
			} else {
				e.put(rd, res, sf)
			}
		} else if w&0xFFC00000 == 0xF9000000 || w&0xFFC00000 == 0xF9400000 || w&0xFFC00000 == 0xB9000000 || w&0xFFC00000 == 0xB9400000 {
			size := 8
			if (w>>30)&1 == 0 {
				size = 4
			}
			addr := e.base(rn) + uint64((w>>10)&0xfff)*uint64(size)
			ld := (w>>22)&1 == 1
			if ld {
				e.put(rd, e.read(addr, size), size*8)
			} else {
				e.store(addr, e.get(rd), size)
			}
		} else if w&0x3fe00c00 == 0x38000400 { // STRB post-index 或 STR W post-index
			size := 1
			if w>>30&3 == 2 {
				size = 4
			}
			addr := e.base(rn)
			e.store(addr, e.get(rd), size)
			off := sx(uint64((w>>12)&0x1ff), 9)
			if rn == 31 {
				e.sp = uint64(int64(e.sp) + off)
			} else {
				e.r[rn] = uint64(int64(e.r[rn]) + off)
			}
		} else if w&0x3B200C00 == 0x38200800 { // STRB / LDRB register offset
			addr := e.base(rn) + e.get(rm)
			if (w>>22)&1 == 1 {
				e.put(rd, e.read(addr, 1), 32)
			} else {
				e.store(addr, e.get(rd), 1)
			}
		} else if w&0xFFC00000 == 0x39000000 {
			addr := e.base(rn) + uint64((w>>10)&0xfff)
			e.store(addr, e.get(rd), 1)
		} else if w&0xFFC00000 == 0x39400000 {
			addr := e.base(rn) + uint64((w>>10)&0xfff)
			e.put(rd, e.read(addr, 1), 32)
		} else if w&0xFFE0FC00 == 0x9AC00800 {
			den := e.get(rm)
			q := uint64(0)
			if den != 0 {
				q = e.get(rn) / den
			}
			e.put(rd, q, 64)
		} else if w&0xFFE08000 == 0x9B000000 || w&0xFFE08000 == 0x9B008000 {
			ra := int((w >> 10) & 31)
			v := e.get(rn) * e.get(rm)
			if (w>>15)&1 != 0 {
				v = e.get(ra) - v
			} else {
				v += e.get(ra)
			}
			e.put(rd, v, 64)
		} else if w&0xFF000010 == 0x54000000 {
			if e.cond(w & 0xf) {
				next = uint64(int64(e.pc) + sx(uint64(w>>5&0x7ffff), 19))
			}
		} else if w&0x7E000000 == 0x34000000 {
			zero := e.get(rd) == 0
			if (w >> 31) == 0 {
				zero = uint32(e.get(rd)) == 0
			}
			if (w>>24)&1 == 1 {
				zero = !zero
			}
			if zero {
				next = uint64(int64(e.pc) + sx(uint64(w>>5&0x7ffff), 19))
			}
		} else if w&0x7E000000 == 0x36000000 {
			bitN := (w >> 19) & 0x1f
			if (w>>31)&1 != 0 {
				bitN += 32
			}
			zero := ((e.get(rd) >> bitN) & 1) == 0
			if (w>>24)&1 == 1 {
				zero = !zero
			}
			if zero {
				next = uint64(int64(e.pc) + sx(uint64(w>>5&0x3fff), 14))
			}
		} else if w&0xFC000000 == 0x94000000 {
			e.r[30] = (e.pc + 1) * 4
			next = uint64(int64(e.pc) + sx(uint64(w&0x3ffffff), 26))
		} else if w&0xFC000000 == 0x14000000 {
			next = uint64(int64(e.pc) + sx(uint64(w&0x3ffffff), 26))
		} else if w == 0xd65f03c0 {
			next = e.r[30] / 4
		} else if w == 0xd4001001 {
			if e.r[16] == 4 {
				if e.r[0] != 1 {
					return "", fmt.Errorf("write fd=%d", e.r[0])
				}
				e.out = append(e.out, e.mem[e.r[1]:e.r[1]+e.r[2]]...)
				e.r[0] = e.r[2]
			} else {
				return "", fmt.Errorf("trap status=%d", e.r[0])
			}
		} else {
			return "", fmt.Errorf("unknown word at pc %#x %#08x", e.pc*4, w)
		}
		e.pc = next
	}
	return "", fmt.Errorf("instruction step limit %d", e.steps)
}
func TestM6bNativeFormatterEmulated(t *testing.T) {
	cases64 := []uint64{0, 0x8000000000000000, 0x3ff0000000000000, 0x3ff199999999999a, 0x400921fb54442d18, 0x3f847ae147ae147b, 0x3eb0c6f7a0b5ed8d, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000042, 0xfff8000000000042, 0x0000000000000001, 0x0010000000000000, 0x7fefffffffffffff, 0x3e70000000000000, 0x7fe0000000000000, 0x4340000000000000, 0x3ff0000000000001}
	for _, b := range cases64 {
		got, err := run6b(b, 4)
		if err != nil {
			t.Fatalf("64 bits=%#x: %v", b, err)
		}
		want := strconv.FormatFloat(math.Float64frombits(b), 'g', -1, 64) + "\n"
		if got != want {
			t.Fatalf("64 bits=%#x got=%q want=%q", b, got, want)
		}
	}
	cases32 := []uint32{0, 0x80000000, 0x3f800000, 0x3f99999a, 0x40490fdb, 0x7f800000, 0xff800000, 0x7fc00004, 1, 0x00800000, 0x7f7fffff, 0x3eaaaaab, 0x4b800000}
	for _, b := range cases32 {
		got, err := run6b(uint64(b), 3)
		if err != nil {
			t.Fatalf("32 bits=%#x: %v", b, err)
		}
		want := strconv.FormatFloat(float64(math.Float32frombits(b)), 'g', -1, 32) + "\n"
		if got != want {
			t.Fatalf("32 bits=%#x got=%q want=%q", b, got, want)
		}
	}
	rng := rand.New(rand.NewSource(1701))
	for i := 0; i < 120; i++ {
		kind := uint8(4)
		bits := rng.Uint64()
		want := strconv.FormatFloat(math.Float64frombits(bits), 'g', -1, 64) + "\n"
		if i%2 == 0 {
			kind = 3
			bits = uint64(uint32(bits))
			want = strconv.FormatFloat(float64(math.Float32frombits(uint32(bits))), 'g', -1, 32) + "\n"
		}
		got, err := run6b(bits, kind)
		if err != nil || got != want {
			t.Fatalf("random %d kind=%d bits=%#x got=%q want=%q err=%v", i, kind, bits, got, want, err)
		}
	}
}
