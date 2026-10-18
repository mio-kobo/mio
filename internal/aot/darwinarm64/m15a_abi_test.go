package darwinarm64

import "testing"

func TestM15aOutgoingStackAreaIsAlignedAndRestored(t *testing.T) {
	e, err := NewStackEmitter(0, 0, 0, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if err := e.PushInt64(int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	before := len(e.words)
	if err := e.CallNative(0, 16, false); err != nil {
		t.Fatal(err)
	}
	wantSub := subImmX(31, 31, 64)
	wantAdd := addImmX(31, 31, 64)
	foundSub, foundAdd := false, false
	for _, word := range e.words[before:] {
		foundSub = foundSub || word == wantSub
		foundAdd = foundAdd || word == wantAdd
	}
	if !foundSub || !foundAdd {
		t.Fatalf("16-argument call did not allocate/restore aligned 64-byte argument area: sub=%v add=%v", foundSub, foundAdd)
	}
}
