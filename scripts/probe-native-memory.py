#!/usr/bin/env python3
"""Generate/execute a pure-Mio native LOS + pressure probe, with fresh evidence."""
import argparse, hashlib, json, pathlib, subprocess, time
ROOT=pathlib.Path(__file__).resolve().parent.parent

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--evidence',type=pathlib.Path,required=True);args=p.parse_args();d=args.evidence.resolve();d.mkdir(parents=True,exist_ok=False)
 imports={'yobu "kiso/fs";'};body=[];sources={}
 for f in sorted((ROOT/'bunko/kiso/aot/lower').glob('*.mio')):
  if f.name.endswith('_test.mio'): continue
  data=f.read_bytes();sources[str(f.relative_to(ROOT))]=hashlib.sha256(data).hexdigest()
  for line in data.decode().splitlines():
   if line.startswith('yobu '): imports.add(line)
   else: body.append(line)
 fixture=['waza probe_builder() -> unit { kaku code=arm64.assembler(); emit_runtime_init(code,8388608,3072); emit_observation_init(code);']
 def emit(x): fixture.append('emit(code,arm64.'+x+');')
 def bl(label): fixture.append(f'arm64.assembler_bl(code,{json.dumps(label)});')
 def bits(r,n): fixture.append(f'move_bits(code,{r},uint32(0),uint32({n}));')
 def eq(r,n): bits(11,n);emit(f'cmp_x({r},11)');fixture.append('arm64.assembler_b_cond(code,1,"runtime_error");')
 def metric(offset,n): emit(f'ldr_x(10,23,{offset})');eq(10,n)
 def setfield(offset,n): bits(9,n);emit(f'str_x(9,23,{offset})')
 def alloc_los(): bits(0,131104);bits(1,102);bl('runtime_alloc');fixture.append('arm64.assembler_cbz_x(code,0,"runtime_error");')
 emit('sub_imm_x(31,31,128,iie)');emit('str_x(31,31,0)');setfield(2752,2);setfield(2672,1)
 emit('movz_x(9,2,0)');emit('str_x(9,31,8)');emit('str_x(31,31,16)');emit('str_x(31,31,24)');emit('add_imm_x(24,31,0,iie)')
 alloc_los();metric(2584,1);bl('gen_minor');metric(2584,0);metric(1088,0);metric(2576,131120)
 alloc_los();fixture.append('move_x(code,20,0);');emit('str_x(20,31,16)')
 # Exact pointer-free 8-byte Storage, initialized across a page boundary.
 bits(9,2051);emit('str_x(9,20,0)');bits(9,16384);emit('str_x(9,20,8)');bits(9,1);emit('str_x(9,20,16)');emit('str_x(31,20,24)')
 bits(9,131096);emit('add_x(22,20,9)');bits(9,713);emit('str_x(9,22,0)')
 for _ in range(4): bl('gen_minor')
 emit('sub_imm_x(9,20,16,iie)');emit('ldr_x(10,9,0)');emit('lsr_x(10,10,32)');eq(10,56)
 # An address-shaped scalar in LOS must not retain a young ordinary Cell.
 bits(0,16);bits(1,105);bl('runtime_alloc');fixture.append('move_x(code,21,0);');bits(9,6);emit('str_x(9,21,0)');bits(9,927);emit('str_x(9,21,8)');emit('str_x(21,20,32)');fixture.append('emit_gen_remember(code,20);');bl('gen_minor');metric(1112,0)
 emit('ldr_x(10,20,32)');emit('cmp_x(10,21)');fixture.append('arm64.assembler_b_cond(code,1,"runtime_error");');emit('sub_imm_x(9,21,16,iie)');emit('ldr_x(10,9,0)');eq(10,0)
 bl('gen_major');emit('ldr_x(10,22,0)');eq(10,713);emit('ldr_x(10,31,16)');emit('cmp_x(10,20)');fixture.append('arm64.assembler_b_cond(code,1,"runtime_error");')
 emit('str_x(31,31,16)');bl('gen_major');metric(2584,0);metric(304,0)
 # The same native policy template handles four levels and delayed recovery.
 setfield(2712,1000000);setfield(2608,3065536);setfield(2616,0);setfield(2752,2);setfield(2672,1);setfield(2632,0);setfield(2640,0)
 def pressure(used,fresh=False):
  if fresh: setfield(2680,0)
  else: fixture.append('observation_word(code,3577471049);');emit('str_x(9,23,2680)')
  fixture.append('move_x(code,0,23);');bits(1,used);bits(2,0);bits(3,1000000);fixture.append('observation_word(code,3577470980);');bits(5,0);bits(6,0);bl('memory.update')
 for level,used in enumerate((0,700000,850000,950000)): pressure(used);metric(2640,level)
 pressure(0);metric(2640,3)
 for i in range(3): pressure(0,True);metric(2640,0 if i==2 else 3)
 setfield(2664,1);fixture.append('move_x(code,0,23);');emit('add_imm_x(1,31,64,iie)');bl('memory.snapshot');emit('ldr_x(10,31,112)');eq(10,1)
 # Exit with the exact shadow chain cleared, then emit machine-readable metrics.
 setfield(2712,8388608);setfield(2608,25231360);setfield(2664,0);emit('movz_x(24,0,0)');bl('memory_sample');bl('observation_dump');emit('movz_x(0,0,0)');emit('movz_x(16,1,0)');emit('svc(128)')
 for label in ('runtime_error','runtime_oom','runtime_gc_bad_type','runtime_gc_bad_kind','runtime_gc_bad_count'):
  fixture.append(f'arm64.assembler_label(code,"{label}");')
 emit('movz_x(0,1,0)');emit('movz_x(16,1,0)');emit('svc(128)')
 fixture.append('emit_managed_runtime(code,iie,3072,hai); kaku assembled=arm64.assembler_assemble(code); moshi(assembled.error!=mu){chant(-1);} sore {kaku patches:[]macho.AddressPatch=[];kaku image=macho.executable(macho.ImageSpec {words:assembled.words,constants:[],globals:0,patches:patches});')
 image=d/'probe.native';fixture.append(f'kaku written=fs.write_all({json.dumps(str(image))},image.bytes);moshi(written.error!=mu){{chant(-2);}} }} }} probe_builder();')
 source=d/'builder.mio';source.write_text('\n'.join(sorted(imports)+body+fixture)+'\n')
 report={'status':'RUNNING','sources':sources,'commands':[],'checks':['LOS threshold active','young LOS reclaimed by Minor before any Major','LOS promoted in place','scalar address does not retain Cell','cross-page payload and alias survive Major','all four pressure levels','three-sample delayed recovery','snapshot copy and GCActive','empty shadow chain at exit']}
 def save(): (d/'result.json').write_text(json.dumps(report,indent=2)+'\n')
 def run(label,argv):
  start=time.monotonic()
  with (d/(label+'.stdout')).open('xb') as out,(d/(label+'.stderr')).open('xb') as err:
   r=subprocess.run(argv,cwd=ROOT,stdout=out,stderr=err,timeout=180)
  report['commands'].append({'label':label,'argv':[str(x) for x in argv],'exit_code':r.returncode,'wall_seconds':time.monotonic()-start});save()
  if r.returncode: raise RuntimeError(label+' failed')
 save()
 try:
  mochi=d/'builder.mochi';builder=d/'builder.native'
  run('mio-build-mochi',['go','run','./cmd/mio','build','-o',str(mochi),str(source)])
  # The emitter runs as Mio bytecode with the same explicit instruction budget
  # as the existing pure-Mio metadata tests. The artifact under test is native.
  runner=d/'run_mochi.go'
  runner.write_text('package main\nimport("os";"Mio/internal/mochi";"Mio/internal/vm";"Mio/internal/stdlib")\nfunc main(){f,e:=os.Open(os.Args[1]);if e!=nil{panic(e)};p,e:=mochi.Decode(f);if e!=nil{panic(e)};r,e:=stdlib.NewRegistry();if e!=nil{panic(e)};if e=vm.NewWithRegistryAndOutput(p,r,os.Stdout).RunWithOptions(vm.ExecutionOptions{InstructionLimit:500_000_000});e!=nil{panic(e)}}\n')
  run('mio-build-probe',['go','run',str(runner),str(mochi)])
  if (d/'mio-build-probe.stdout').read_bytes(): raise RuntimeError('Mio assembler reported an error')
  image.chmod(0o700);run('sign',['codesign','-s','-',str(image)]);run('verify',['codesign','--verify','--strict',str(image)]);run('execute',[str(image)])
  if (d/'execute.stdout').read_bytes(): raise RuntimeError('unexpected native stdout')
  report.update(status='PASS',image_sha256=hashlib.sha256(image.read_bytes()).hexdigest(),builder_sha256=hashlib.sha256(source.read_bytes()).hexdigest());save()
 except BaseException as e: report.update(status='FAIL',error=str(e));save();raise
if __name__=='__main__':main()
