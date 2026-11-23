package typecheck

import "testing"

func TestWindowsCPrototype(t *testing.T) {
	valid := []string{
		`waza pid() -> uint32 = c "kernel32.dll" "GetCurrentProcessId"; chant(pid());`,
		`waza p() -> cptr = c "x.dll"; waza f(p:cptr) -> uint64 = c "x.dll"; kaku x=f(p()); chant(x==uint64(-1));`,
		`waza f(a:int8,b:uint16,c:float32,d:float64) -> unit = c "x.dll"; f(-128,65535,float32(1.0),2.0);`,
	}
	for _, s := range valid {
		if err := checkMioSource(s); err != nil {
			t.Fatalf("valid %s: %v", s, err)
		}
	}
	invalid := []string{
		`waza f() -> unit = c "";`,
		`waza f() -> unit = c "../x.dll";`,
		`waza f(x:string) -> unit = c "x.dll";`,
		`waza f(x:[]uint8) -> unit = c "x.dll";`,
		`waza f() -> string = c "x.dll";`,
		`waza f(x:cptr) -> unit = c "x.dll"; f("managed");`,
		`waza f() -> unit = c "x.dll"; waza g() -> int32 = c "X.DLL" "f";`,
		`kaku p=cptr(1);`, `kaku p=cptr("managed");`,
		`waza f(x:bool) -> unit = c "x.dll";`,
	}
	for _, s := range invalid {
		if err := checkMioSource(s); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
