package aot

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

func TestM19cMemoryDifferential(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Darwin ARM64")
	}
	root, _ := filepath.Abs("../..")
	const common = `yobu "kiso/array";
 kata Box { value: int64; }
 waza churn(n: int64) -> unit {kaku i=0; aida(i<n){kaku b=array.make[uint8](16384,uint8(17)); i=i+1;}}
 `
	cases := []struct{ name, src string }{
		{"storage-sizes", common + `waza run() -> unit { kaku a=array.make[uint8](16384,uint8(18)); kaku b=array.make[uint8](131072,uint8(128)); kaku c=array.make[int64](131072,1048576); kaku d=array.make[uint8](2097152,uint8(211)); churn(120); chant(a[16383]); chant(b[131071]); chant(c[131071]); chant(d[2097151]); } run();`},
		{"old-los-young", common + `waza run() -> unit { kaku a=array.make[Box?](20000,mu); kaku alias=a; churn(30); kaku i=0; aida(i<20){a[19999]=Box {value:i}; churn(20); kaku v=alias[19999]; moshi(v!=mu){chant(v.value);} a[19999]=mu; i=i+1;} } run();`},
		{"old-normal-young-los", common + `kata Owner { data: []uint8; } waza run() -> unit { kaku a=array.with_capacity[[]uint8](8); kaku base=array.make[uint8](16,uint8(5)); array.push(a,base); churn(30); kaku i=0; aida(i<20){ kaku data=array.make[uint8](131072,uint8(217)); a[0]=data; churn(20); chant(a[0][131071]); i=i+1;} } run();`},
		{"closure-los", common + `waza run() -> unit { kaku a=array.make[uint8](131072,uint8(31)); kaku get=waza()->int64 {kaesu int64(a[131071]);}; kaku set=waza(v:[]uint8)->unit {a=v;}; churn(40); kaku i=0; aida(i<10){set(array.make[uint8](1048576,uint8(79))); churn(40); chant(get()); i=i+1;} } run();`},
		{"arena-growth", common + `waza run() -> unit { kaku a=array.with_capacity[Box](0); kaku i=0; aida(i<22000){array.push(a,Box {value:i});i=i+1;} churn(300); chant(a[0].value); chant(a[21999].value); chant(len(a));} run();`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := m17CompileMio(t, root, c.src)
			want := m17RunVM(t, p, 100_000_000)
			for _, engine := range []string{"go", "mio"} {
				for _, adaptive := range []bool{false, true} {
					for _, stress := range []bool{false, true} {
						if c.name == "arena-growth" && stress {
							continue
						} // full GCStress of the 22k live graph is covered separately by the small graph fixtures
						t.Run(fmt.Sprintf("%s/auto=%t/stress=%t", engine, adaptive, stress), func(t *testing.T) {
							var img []byte
							var e error
							if engine == "go" {
								img, e = CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 8 << 20, GCTelemetry: true, GCStress: stress, AdaptiveHeap: adaptive})
								if e != nil {
									t.Fatal(e)
								}
							} else {
								img = m19MioImage(t, p, 8<<20, stress, true, false, adaptive)
							}
							out, diag, e := m19Run(t, img)
							if e != nil || !bytes.Equal(out, want) {
								t.Fatalf("execution %v want %s got %s\n%s", e, want, out, diag)
							}
							m := m19Metrics(t, diag)
							m19CheckMetrics(t, m)
							m19bCheckMetrics(t, m)
							if m["los_live_bytes"]+m["los_reclaimed_bytes"] == 0 {
								t.Fatal("no real LOS allocation")
							}
							if m["heap_committed"] > m["heap_reserved"] || m["heap_committed"] > m["heap_limit"] || m["heap_goal"] > m["heap_capacity"] {
								t.Fatalf("budget exceeded: %v", m)
							}
							if m["pressure_sequence"] == 0 || m["pressure_sequence"]%2 != 0 {
								t.Fatal("unpublished snapshot")
							}
						})
					}
				}
			}
		})
	}
}

func TestM19cNativeOOM(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Darwin ARM64")
	}
	root, _ := filepath.Abs("../..")
	p := m17CompileMio(t, root, `yobu "kiso/array"; kaku a=array.make[uint8](2097152,uint8(7)); chant(a[2097151]);`)
	for _, engine := range []string{"go", "mio"} {
		t.Run(engine, func(t *testing.T) {
			var img []byte
			var e error
			if engine == "go" {
				img, e = CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 8 << 20, HeapHardCap: (3 << 20) + 65536, GCTelemetry: true})
				if e != nil {
					t.Fatal(e)
				}
			} else {
				img = m19MioImage(t, p, 1<<20, false, true, false)
			}
			out, diag, e := m19Run(t, img)
			if e == nil || len(out) != 0 {
				t.Fatalf("missing true OOM %v %s %s", e, out, diag)
			}
			if at := bytes.Index(diag, []byte("mio-gc ")); at >= 0 {
				diag = diag[at:]
			}
			m := m19Metrics(t, diag)
			if m["major_gc_count"] == 0 || m["oom_request_bytes"] < 2<<20 {
				t.Fatalf("no Major retry / OOM request: %v", m)
			}
			if m["heap_limit"] > (3<<20)+65536 {
				t.Fatal("explicit hard cap exceeded")
			}
		})
	}
}
