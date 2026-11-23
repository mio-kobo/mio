#!/usr/bin/env python3
"""Real full-queue deferred-task recovery and repeated epochs; E4 still incomplete."""
import hashlib,json,os,pathlib,platform,shutil,signal,struct,subprocess,sys,time
root=pathlib.Path.cwd().resolve();out=(root/sys.argv[1]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence');out.mkdir();snap=out/'source-snapshot';snap.mkdir();sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();manifest={}
for folder in ['bunko','selfhost','tests/m19ef']:
 for p in sorted((root/folder).rglob('*.mio')):
  rel=p.relative_to(root);d=snap/rel;d.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,d);manifest[str(rel)]=sha(p)
(out/'source-manifest.json').write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');digest=sha(out/'source-manifest.json');shutil.copyfile(pathlib.Path(__file__),out/'controller.py');seed=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
(out/'provenance.json').write_text(json.dumps(dict(source_manifest_sha256=digest,seed_sha256=sha(seed),head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),branch=subprocess.check_output(['git','branch','--show-current'],text=True).strip(),host=platform.platform(),target='darwin-arm64',scope='real ordinary allocation/Minor/LOS cooperation and SATB; bulk/epoch/overflow/Sweep still incomplete'),indent=2)+'\n');rows=[]
def run(label,args,expected=0,timeout=600):
 start=time.monotonic()
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  p=subprocess.Popen(list(map(str,args)),cwd=snap,stdout=so,stderr=se,start_new_session=True);timed=False
  while True:
   got,status,u=os.wait4(p.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:
    timed=True
    with (out/(label+'.processes')).open('xb') as diag:subprocess.run(['ps','-Ao','pid,ppid,%cpu,rss,etime,command'],stdout=diag)
    os.killpg(p.pid,signal.SIGKILL);got,status,u=os.wait4(p.pid,0);break
   time.sleep(.03)
 row=dict(label=label,argv=list(map(str,args)),cwd=str(snap),exit=os.waitstatus_to_exitcode(status),expected_exit=expected,timed_out=timed,timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=u.ru_utime+u.ru_stime,peak_rss_bytes_darwin=u.ru_maxrss,source_manifest_sha256=digest,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')));rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,row['exit'],flush=True);assert row['exit']==expected and not timed
 return (out/(label+'.stdout')).read_bytes()
run('seed-signature',['codesign','--verify','--strict',seed]);builder=out/'satb-builder'
if len(sys.argv)==3:
 prior=(root/sys.argv[2]).resolve();prior_manifest=json.loads((prior/'source-manifest.json').read_text())
 compiler=lambda m:{p:h for p,h in m.items() if p.startswith(('bunko/','selfhost/')) or p=='tests/m19ef/old-mark-proto/builder.mio'}
 assert compiler(manifest)==compiler(prior_manifest),'cannot reuse builder across compiler source changes'
 built=next(r for r in json.loads((prior/'commands.json').read_text()) if r['label']=='build');assert built['exit']==0 and not built['timed_out']
 shutil.copyfile(prior/'satb-builder',builder);builder.chmod(0o755)
 (out/'builder-reuse-audit.json').write_text(json.dumps(dict(prior=str(prior),compiler_sources_identical=True,original_build=built,builder_sha256=sha(builder),prior_builder_sha256=sha(prior/'satb-builder'),scope='native builder reuse only; every user image rebuilt; no compiler bootstrap acceptance'),indent=2)+'\n')
else:run('build',[seed,'build','tests/m19ef/old-mark-proto/builder.mio','-o',builder,'--bunko-root',snap/'bunko'])
run('builder-signature',['codesign','--verify','--strict',builder]);observations=[]
clock_builder=out/'clock-builder';run('clock-build',[seed,'build','tests/m19ef/old-mark-clock/main.mio','-o',clock_builder,'--bunko-root',snap/'bunko']);clock_image=out/'clock-native';run('clock-generate',[clock_builder,clock_image]);run('clock-signature',['codesign','--verify','--strict',clock_image]);run('clock-repro',[clock_builder,out/'clock-repro-native']);assert clock_image.read_bytes()==(out/'clock-repro-native').read_bytes()
for repeat in range(32):assert run('clock-'+str(repeat),[clock_image],timeout=30)==struct.pack('<4Q',4,6,1,0)

bounds_builder=out/'bounds-builder';run('bounds-build',[seed,'build','tests/m19ef/old-mark-bounds/main.mio','-o',bounds_builder,'--bunko-root',snap/'bunko']);bounds_image=out/'bounds-native';run('bounds-generate',[bounds_builder,bounds_image]);run('bounds-signature',['codesign','--verify','--strict',bounds_image]);run('bounds-repro',[bounds_builder,out/'bounds-repro-native']);assert bounds_image.read_bytes()==(out/'bounds-repro-native').read_bytes()
for repeat in range(32):assert run('bounds-'+str(repeat),[bounds_image],timeout=30)==struct.pack('<Q',9)

component_builder=out/'bulk-component-builder';run('bulk-component-build',[seed,'build','tests/m19ef/reference-bulk/main.mio','-o',component_builder,'--bunko-root',snap/'bunko']);component=out/'bulk-component-native';run('bulk-component-generate',[component_builder,component]);run('bulk-component-signature',['codesign','--verify','--strict',component]);run('bulk-component-repro',[component_builder,out/'bulk-component-repro-native']);assert component.read_bytes()==(out/'bulk-component-repro-native').read_bytes()
for repeat in range(32):assert run('bulk-component-'+str(repeat),[component],timeout=30)==struct.pack('<Q',15)

for case,source,repeats in [('epochs','tests/m19ef/old-mark-epochs/ordinary/main.mio',32),('bulk','tests/m19ef/old-mark-bulk/ordinary/main.mio',32),('abort-alloc','tests/m19ef/old-mark-proto/abort-alloc/main.mio',8),('ordinary','tests/m19ef/old-mark-minor/ordinary/main.mio',32),('cell','tests/m19ef/old-mark-satb/cell/main.mio',32),('array','tests/m19ef/old-mark-satb/array/main.mio',32),('immutable','tests/m19ef/old-mark-proto/immutable/main.mio',32)]:
 image=out/(case+'-native');capacity_args=['1048576','32'] if case=='epochs' else (['16777216'] if case in ('bulk','array') else []);run(case+'-build',[builder,snap,source,snap/'bunko',image]+capacity_args);run(case+'-signature',['codesign','--verify','--strict',image]);duplicate=out/(case+'-repro-native');run(case+'-repro',[builder,snap,source,snap/'bunko',duplicate]+capacity_args);assert image.read_bytes()==duplicate.read_bytes()
 for repeat in range(repeats):
  label=case+'-'+str(repeat);stdout=run(label,[image],timeout=60);data=(out/(label+'.stderr')).read_bytes();assert len(data)==552,(case,len(data));old=struct.unpack('<24Q',data[:192]);mut=struct.unpack('<45Q',data[192:]);assert old[0]==1330406477 and old[12:17]==((8,8,0,1,1) if case=='epochs' else ((4,4,0,1,1) if case=='array' else (1,1,0,1,1)));assert mut[26]==0
  if case=='epochs':
   assert stdout==b'0\n9\n4\n8\n8\n8\n0\n0\n',stdout
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=130 and old[5]>=1 and old[23]==0,old
   assert old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,8)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4
   for off in (7,14):assert mut[off:off+6]==(8,8,8,8,24,24)
  elif case=='bulk':
   lines=stdout.splitlines();assert len(lines)==9,stdout
   assert lines[:2]==[b'0',b'2'] and lines[5:]==[b'hai',b'hai',b'4',b'0'],stdout
   calls,nonnull,prewhite=map(int,lines[2:5]);assert calls>=16000 and nonnull>=10000 and prewhite>0,(calls,nonnull,prewhite)
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=8000 and old[5]>=1 and old[23]==0,old
   assert old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4
  elif case=='ordinary':
   lines=stdout.splitlines();assert len(lines)==11,stdout
   n=list(map(int,lines[:7]));assert n[:2]==[0,2] and n[2]>=200 and n[3]>=200 and n[4]>=5 and n[5]==0 and n[6]==8012,n
   assert lines[7:]==[b'hai',b'hai',b'4',b'0'],stdout
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=8012 and old[5]>=1 and old[23]==0,old
   assert old[9]>0 and old[7]<=old[10]<=old[11]<=old[8] and old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4 and mut[22]>=208 and mut[24:27]==(0,655456,0)
  elif case=='array':
   nums=list(map(int,stdout.splitlines()));assert len(nums)==40,nums
   cycles=[nums[i:i+10] for i in range(0,40,10)]
   for v in cycles:assert v[:4]==[0,2,17999,11999] and 0<=v[4]<=11999 and v[5:8]==[12021,1,4] and v[9]==0,v
   assert any(v[4]>2 and v[8]>0 for v in cycles),'no actual adversarial in-scan witness across four fully validated epochs'
   assert old[1]==4 and old[2]==0 and old[3]==old[4]==12021 and old[5]==1 and old[23]==0,old
   assert old[17:20]==(12021,12021,12021) and old[20:23]==(0,0,4)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4
   for off in (7,14):assert mut[off:off+6]==(4,4,4,4,12,12)
  elif case=='cell':
   assert stdout==b'0\n5\n4\n7\n1\n4\n0\n',stdout
   assert old[1:6]==(4,0,7,7,1) and old[23]==0
  elif case=='immutable':
   assert stdout==b'0\nhai\nhai\nhai\n4\n0\n';assert old[1:6]==(4,0,8001,8001,1) and old[23]==0
  else:assert stdout==b'7\n2\n0\n' and old[1]==6 and old[23]!=0
  if case=='immutable':assert old[9]>0 and old[7]<=old[10]<=old[11]<=old[8] and old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
  observations.append(dict(case=case,repeat=repeat,stdout=stdout.decode(),old_words=list(old),mutator_words=list(mut),image_sha256=sha(image),source_manifest_sha256=digest))
(out/'observations.json').write_text(json.dumps(observations,indent=2)+'\n')
queue_builder=out/'queue-builder';run('queue-build',[seed,'build','tests/m19ef/mark-work-queue/main.mio','-o',queue_builder,'--bunko-root',snap/'bunko']);queue_image=out/'queue-native';run('queue-generate',[queue_builder,queue_image]);run('queue-signature',['codesign','--verify','--strict',queue_image]);run('queue-repro',[queue_builder,out/'queue-repro-native']);assert queue_image.read_bytes()==(out/'queue-repro-native').read_bytes()
for repeat in range(64):
 v=struct.unpack('<15Q',run('queue-'+str(repeat),[queue_image],timeout=30));assert v[:3]==(2002,2002,2002) and v[3]>0 and v[4:6]==(33033,2002)
 assert len(set(v[6:9]))==3 and all(v[6:9])
 for base in (9,11):assert max(v[base],v[13])<min(v[base+1],v[14])

scanner_builder=out/'scanner-builder';run('scanner-build',[seed,'build','tests/m19ef/trace-layout/main.mio','-o',scanner_builder,'--bunko-root',snap/'bunko']);scanner=out/'scanner-native';run('scanner-generate',[scanner_builder,scanner]);run('scanner-signature',['codesign','--verify','--strict',scanner]);run('scanner-repro',[scanner_builder,out/'scanner-repro-native']);assert scanner.read_bytes()==(out/'scanner-repro-native').read_bytes()
for repeat in range(32):assert run('scanner-'+str(repeat),[scanner],timeout=30)==struct.pack('<Q',20)

default=out/'default-builder';run('default-build',[seed,'build','tests/m19ef/user-workers/builder.mio','-o',default,'--bunko-root',snap/'bunko'])
for case,expected in [('ordinary',b'12000\n24000\n0\n'),('handoff',b'0\nhai\n0\n0\n'),('deep',b'91\n0\n'),('capture',b'101\n102\n0\n')]:
 image=out/('default-'+case+'-native');run('default-'+case+'-build',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);assert image.read_bytes()==(root/'build/m19ef-evidence/E3-workers-20261030-17'/(case+'-native')).read_bytes();assert run('default-'+case,[image],timeout=30)==expected
 duplicate=out/('default-'+case+'-repro-native');run('default-'+case+'-repro',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',duplicate]);assert image.read_bytes()==duplicate.read_bytes()
for package in ['bunko/kiso/runtime/gc','bunko/kiso/aot/lower']:
 data=json.loads(run('unit-'+package.replace('/','-'),[seed,'test',package,'--bunko-root',snap/'bunko','--format=json'],timeout=1200));assert data['summary']['failed']==0
assert all(sha(root/p)==h and sha(snap/p)==h for p,h in manifest.items());(out/'source-unchanged.json').write_text(json.dumps(dict(unchanged=True,source_manifest_sha256=digest))+'\n');(out/'summary.json').write_text(json.dumps(dict(status='PASS_SCOPED_OVERFLOW_REPEAT_BULK_MINOR',E4='IN_PROGRESS',E5='NOT_RUN',E6='NOT_RUN',M19e='NOT_ACCEPTED',source_manifest_sha256=digest,epoch_program_runs=32,epochs_per_program=8,explicit_abort_recovery_epochs_per_program=3,queue_component_runs=64,bounds_component_runs=32,bulk_map_runs=32,allocation_minor_runs=32,array_runs=32,cell_runs=32,immutable_runs=32,legacy_major_cancel_runs=8,E3_default_images='IDENTICAL_TO_ACCEPTED_CANDIDATE17',concurrent_allocation_minor='VALIDATED_SCOPED',bulk_writes='PASS_SCOPED_REFERENCE_ARRAY_AND_MAP',sweep='NOT_IMPLEMENTED',complete_failure_audit='IN_PROGRESS'),indent=2)+'\n')
