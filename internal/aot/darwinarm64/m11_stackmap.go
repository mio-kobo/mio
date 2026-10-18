package darwinarm64

import (
	"encoding/binary"
	"fmt"
)

const m11StackMapMagic = uint32(0x424D3131) // "11MB"

// M11StackMap is the scalable, compiler-produced description retained in the
// Mach-O constant section for diagnostics and native-PC correlation. Runtime
// marking uses the equivalent root vector spilled into the shadow frame.
type M11StackMap struct {
	SafepointID uint32
	NativeWord  uint32
	FrameID     uint32
	FrameSlots  []int
	GlobalSlots []int
}

// M11EncodeStackMaps encodes a variable-length table without the M9 fixed
// limits. Each entry is: id, native word, frame id, frame count, global count,
// followed by uint32 frame slots then uint32 global slots.
func M11EncodeStackMaps(maps []M11StackMap) ([]byte, error) {
	total := 8
	for _, m := range maps {
		if len(m.FrameSlots) > MaxM5OperandSlots+MaxM4Locals || len(m.GlobalSlots) > MaxGlobalSlots {
			return nil, fmt.Errorf("M11b stack map %d exceeds slot limits", m.SafepointID)
		}
		total += 20 + 4*(len(m.FrameSlots)+len(m.GlobalSlots))
	}
	out := make([]byte, total)
	binary.LittleEndian.PutUint32(out, m11StackMapMagic)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(maps)))
	off := 8
	for _, m := range maps {
		for _, v := range []uint32{m.SafepointID, m.NativeWord, m.FrameID, uint32(len(m.FrameSlots)), uint32(len(m.GlobalSlots))} {
			binary.LittleEndian.PutUint32(out[off:], v)
			off += 4
		}
		seen := map[uint32]bool{}
		for _, slots := range [][]int{m.FrameSlots, m.GlobalSlots} {
			for _, slot := range slots {
				if slot < 0 || uint64(slot) > uint64(^uint32(0)) || seen[uint32(slot)] {
					return nil, fmt.Errorf("M11b stack map %d has invalid or duplicate slot %d", m.SafepointID, slot)
				}
				seen[uint32(slot)] = true
				binary.LittleEndian.PutUint32(out[off:], uint32(slot))
				off += 4
			}
			seen = map[uint32]bool{}
		}
	}
	return out, nil
}
