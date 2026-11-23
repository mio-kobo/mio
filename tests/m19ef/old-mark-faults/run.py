#!/usr/bin/env python3
"""Real full-queue deferred-task recovery and repeated epochs; E4 still incomplete."""
import hashlib,json,os,pathlib,platform,shutil,signal,struct,subprocess,sys,time
from bulk_overlay import install as install_bulk
from bulk_trace import verify_bulk
root=pathlib.Path.cwd().resolve();out=(root/sys.argv[1]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence');out.mkdir();snap=out/'source-snapshot';snap.mkdir();sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();manifest={}
for folder in ['bunko','selfhost','tests/m19ef']:
 for p in sorted((root/folder).rglob('*.mio')):
  rel=p.relative_to(root);d=snap/rel;d.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,d);manifest[str(rel)]=sha(p)
(out/'source-manifest.json').write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');digest=sha(out/'source-manifest.json');shutil.copyfile(pathlib.Path(__file__),out/'controller.py');seed=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
(out/'provenance.json').write_text(json.dumps(dict(source_manifest_sha256=digest,seed_sha256=sha(seed),head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),branch=subprocess.check_output(['git','branch','--show-current'],text=True).strip(),host=platform.platform(),target='darwin-arm64',scope='real ordinary allocation/Minor/LOS cooperation and SATB; bulk/epoch/overflow/Sweep still incomplete'),indent=2)+'\n');rows=[]
# Optional positional inputs: builder, bulk proof, complete E3, historical17,
# completed prefix. Every retained native result is replayed through the same
# assertions after exact compiler/Mio/argv/log/artifact validation.
e3_candidates=[]
requested=[(root/sys.argv[4]).resolve()] if len(sys.argv)>4 else list((root/'build/m19ef-evidence').glob('E3-workers-*'))+list((root/'build/m19ef-evidence').glob('*/E3-*'))
for candidate in requested:
 try:
  sm=json.loads((candidate/'summary.json').read_text());mm=json.loads((candidate/'source-manifest.json').read_text());cr=json.loads((candidate/'commands.json').read_text());pv=json.loads((candidate/'provenance.json').read_text())
  if mm==manifest and sm['status']=='PASS_SCOPED_USER_WORKER_INTEGRATION' and sm['source_unchanged'] and all(x['exit']==x['expected_exit'] and not x['timed_out'] and x['seed_sha256']==sha(seed) for x in cr) and pv['VERSION_fixture_sha256']==sha(root/'VERSION'):e3_candidates.append(candidate)
 except (OSError,ValueError,KeyError):pass
assert e3_candidates,'complete exact-Mio-source/current-compiler E3 required; pass argument4 explicitly'
e3_baseline=max(e3_candidates,key=lambda p:p.stat().st_mtime)
historical17=(root/sys.argv[5]).resolve() if len(sys.argv)>5 else root/'build/m19ef-evidence/E3-workers-20261030-17'
for case in ('ordinary','handoff','deep','capture'):assert (historical17/(case+'-native')).is_file(),('historical reference missing; pass argument5',historical17)
(out/'evidence-inputs.json').write_text(json.dumps(dict(same_source_E3=str(e3_baseline),same_source_E3_manifest_sha256=sha(e3_baseline/'source-manifest.json'),same_source_E3_commands_sha256=sha(e3_baseline/'commands.json'),historical17=str(historical17),historical_reference_scope='hash comparison only, never current-source acceptance'),indent=2)+'\n')
retained=(root/sys.argv[6]).resolve() if len(sys.argv)>6 else None;retained_rows=[];retained_index=0
if retained:
 assert retained!=out and json.loads((retained/'source-manifest.json').read_text())==manifest
 assert json.loads((retained/'provenance.json').read_text())['seed_sha256']==sha(seed)
 retained_rows=json.loads((retained/'commands.json').read_text());assert len({x['label'] for x in retained_rows})==len(retained_rows)
 pins=json.loads((retained/'partial-artifact-manifest.json').read_text())
 assert all(sha(retained/name)==digest for name,digest in pins.items()),'retained artifacts changed'
 for x in retained_rows:
  assert x['exit']==x['expected_exit'] and not x['timed_out']
  for channel in ('stdout','stderr'):assert sha(retained/(x['label']+'.'+channel))==x[channel+'_sha256']
 for observation in json.loads((retained/'observations.json').read_text()):assert sha(retained/(observation['case']+'-native'))==observation['image_sha256']
 executable_pins={}
 for artifact in retained.iterdir():
  if artifact.is_file() and ((artifact.stat().st_mode&0o111) or artifact.name.endswith(('.mochi.txt','.unsigned'))):
   dest=out/artifact.name;shutil.copy2(artifact,dest)
   if artifact.stat().st_mode&0o111:
    subprocess.run(['codesign','--verify','--strict',str(dest)],check=True);executable_pins[artifact.name]=sha(dest)
 (out/'retained-prefix.json').write_text(json.dumps(dict(scope='completed native commands only; original full controller remains FAIL; all original assertions re-evaluated; remaining commands execute afresh',path=str(retained),commands=len(retained_rows),commands_sha256=sha(retained/'commands.json'),artifact_manifest_sha256=sha(retained/'partial-artifact-manifest.json'),executables=executable_pins),indent=2)+'\n')

def run(label,args,expected=0,timeout=600):
 global retained_index
 cwd=root if len(args)==1 and pathlib.Path(args[0]).parent==out else snap
 if retained_index<len(retained_rows):
  previous=retained_rows[retained_index];actual=list(map(str,args))
  normalize=lambda argv,base:[str(v).replace(str(base),'<E4_OUTPUT>') for v in argv]
  assert previous['label']==label and previous['expected_exit']==expected and previous['timeout_seconds']==timeout
  assert normalize(previous['argv'],retained)==normalize(actual,out),(label,'retained argv mismatch')
  assert str(previous['cwd']).replace(str(retained),'<E4_OUTPUT>')==str(cwd).replace(str(out),'<E4_OUTPUT>')
  for channel in ('stdout','stderr'):shutil.copy2(retained/(label+'.'+channel),out/(label+'.'+channel))
  row=dict(previous,retained_from=str(retained),requested_argv=actual,evidence='VERIFIED_RETAINED_IDENTICAL_MIO_INPUTS_NOT_A_NEW_NATIVE_EXECUTION');rows.append(row);retained_index+=1
  (out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,'RETAINED',row['exit'],flush=True)
  return (out/(label+'.stdout')).read_bytes()
 start=time.monotonic()
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  p=subprocess.Popen(list(map(str,args)),cwd=cwd,stdout=so,stderr=se,start_new_session=True);timed=False
  while True:
   got,status,u=os.wait4(p.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:
    timed=True
    with (out/(label+'.processes')).open('xb') as diag:subprocess.run(['ps','-Ao','pid,ppid,%cpu,rss,etime,command'],stdout=diag)
    os.killpg(p.pid,signal.SIGKILL);got,status,u=os.wait4(p.pid,0);break
   time.sleep(.03)
 row=dict(label=label,argv=list(map(str,args)),cwd=str(cwd),exit=os.waitstatus_to_exitcode(status),expected_exit=expected,timed_out=timed,timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=u.ru_utime+u.ru_stime,peak_rss_bytes_darwin=u.ru_maxrss,source_manifest_sha256=digest,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')));rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,row['exit'],flush=True);assert row['exit']==expected and not timed
 return (out/(label+'.stdout')).read_bytes()
run('seed-signature',['codesign','--verify','--strict',seed]);builder=out/'satb-builder'
if len(sys.argv)>=3 and sys.argv[2]!='-':
 prior=(root/sys.argv[2]).resolve();prior_manifest=json.loads((prior/'source-manifest.json').read_text())
 compiler=lambda m:{p:h for p,h in m.items() if p.startswith(('bunko/','selfhost/')) or p=='tests/m19ef/old-mark-proto/builder.mio'}
 assert compiler(manifest)==compiler(prior_manifest),'cannot reuse builder across compiler source changes'
 built=next((r for r in json.loads((prior/'commands.json').read_text()) if r['label']=='build'),None)
 if built is None:built=json.loads((prior/'builder-reuse-audit.json').read_text())['original_build']
 assert built['exit']==0 and not built['timed_out']
 shutil.copyfile(prior/'satb-builder',builder);builder.chmod(0o755)
 (out/'builder-reuse-audit.json').write_text(json.dumps(dict(prior=str(prior),compiler_sources_identical=True,original_build=built,builder_sha256=sha(builder),prior_builder_sha256=sha(prior/'satb-builder'),scope='native builder reuse only; every user image rebuilt; no compiler bootstrap acceptance'),indent=2)+'\n')
else:run('build',[seed,'build','tests/m19ef/old-mark-proto/builder.mio','-o',builder,'--bunko-root',snap/'bunko'])
run('builder-signature',['codesign','--verify','--strict',builder]);observations=[]
# Only BULK uses this controlled white-edge rendezvous. All other workloads
# keep their ungated scans and their existing actual in-scan overlap checks.
bulk_snap=out/'bulk-source-snapshot';shutil.copytree(snap,bulk_snap);install_bulk(bulk_snap)
bulk_manifest={str(p.relative_to(bulk_snap)):sha(p) for p in sorted(bulk_snap.rglob('*.mio'))}
assert [k for k,v in bulk_manifest.items() if v!=manifest[k]]==['bunko/kiso/runtime/gc/old_mark.mio']
(out/'bulk-overlay-manifest.json').write_text(json.dumps(bulk_manifest,sort_keys=True,indent=2)+'\n')
if retained:assert bulk_manifest==json.loads((retained/'bulk-overlay-manifest.json').read_text()),'retained bulk overlay differs'
for name in ('bulk_overlay.py','bulk_trace.py'):shutil.copyfile(pathlib.Path(__file__).with_name(name),out/name)
bulk_builder=out/'bulk-builder'
if len(sys.argv)>=4 and sys.argv[3]!='-':
 prior_bulk=(root/sys.argv[3]).resolve();prior_manifest=json.loads((prior_bulk/'overlay-manifest.json').read_text())
 relevant=lambda m:{k:v for k,v in m.items() if k.endswith('.mio') and (k.startswith(('bunko/','selfhost/')) or k=='tests/m19ef/old-mark-proto/builder.mio')}
 assert relevant(prior_manifest)==relevant(bulk_manifest)
 assert json.loads((prior_bulk/'summary.json').read_text())['status']=='PASS_DIRECTED_SAME_EPOCH_BULK_WHITE_HANDSHAKE'
 built=next(x for x in json.loads((prior_bulk/'commands.json').read_text()) if x['label']=='build');assert built['exit']==0 and not built['timed_out']
 shutil.copyfile(prior_bulk/'bulk-builder',bulk_builder);bulk_builder.chmod(0o755)
 (out/'bulk-builder-reuse.json').write_text(json.dumps(dict(prior=str(prior_bulk),builder_sha256=sha(bulk_builder),original_build=built,scope='identical controlled test factory inputs; user image freshly rebuilt'),indent=2)+'\n')
else:run('bulk-builder-build',[seed,'build',bulk_snap/'tests/m19ef/old-mark-proto/builder.mio','-o',bulk_builder,'--bunko-root',bulk_snap/'bunko'])
run('bulk-builder-signature',['codesign','--verify','--strict',bulk_builder])
platform_builder=out/'platform-fault-builder';run('platform-fault-build',[seed,'build','tests/m19ef/old-mark-platform-faults/main.mio','-o',platform_builder,'--bunko-root',snap/'bunko']);run('platform-fault-builder-signature',['codesign','--verify','--strict',platform_builder])
for mode,expected in [('stack',(6,22,0,0,0,0,1)),('join',(6,11,1,1,1,1,2,0,1))]:
 image=out/('platform-fault-'+mode+'-native');run('platform-fault-'+mode+'-generate',[platform_builder,image,mode]);run('platform-fault-'+mode+'-signature',['codesign','--verify','--strict',image]);run('platform-fault-'+mode+'-repro',[platform_builder,out/('platform-fault-'+mode+'-repro-native'),mode]);assert image.read_bytes()==(out/('platform-fault-'+mode+'-repro-native')).read_bytes()
 for repeat in range(32):assert run('platform-fault-'+mode+'-'+str(repeat),[image],timeout=30)==struct.pack('<'+str(len(expected))+'Q',*expected)

for case in ['oom','oom-bound']:
 image=out/(case+'-native');source='tests/m19ef/old-mark-faults/'+case+'/main.mio';run(case+'-build',[builder,snap,source,snap/'bunko',image]);run(case+'-signature',['codesign','--verify','--strict',image]);run(case+'-repro',[builder,snap,source,snap/'bunko',out/(case+'-repro-native')]);assert image.read_bytes()==(out/(case+'-repro-native')).read_bytes()
 for repeat in range(8):
  label=case+'-'+str(repeat);assert run(label,[image],expected=2,timeout=30)==b'2\n';data=(out/(label+'.stderr')).read_bytes();assert len(data)==72,(label,len(data));assert struct.unpack('<9Q',data)==(1330401613,1,6,0,1,1,1,1,0)

clock_builder=out/'clock-builder';run('clock-build',[seed,'build','tests/m19ef/old-mark-clock/main.mio','-o',clock_builder,'--bunko-root',snap/'bunko']);clock_image=out/'clock-native';run('clock-generate',[clock_builder,clock_image]);run('clock-signature',['codesign','--verify','--strict',clock_image]);run('clock-repro',[clock_builder,out/'clock-repro-native']);assert clock_image.read_bytes()==(out/'clock-repro-native').read_bytes()
for repeat in range(32):assert run('clock-'+str(repeat),[clock_image],timeout=30)==struct.pack('<4Q',4,6,1,0)

bounds_builder=out/'bounds-builder';run('bounds-build',[seed,'build','tests/m19ef/old-mark-bounds/main.mio','-o',bounds_builder,'--bunko-root',snap/'bunko']);bounds_image=out/'bounds-native';run('bounds-generate',[bounds_builder,bounds_image]);run('bounds-signature',['codesign','--verify','--strict',bounds_image]);run('bounds-repro',[bounds_builder,out/'bounds-repro-native']);assert bounds_image.read_bytes()==(out/'bounds-repro-native').read_bytes()
for repeat in range(32):assert run('bounds-'+str(repeat),[bounds_image],timeout=30)==struct.pack('<Q',9)

component_builder=out/'bulk-component-builder';run('bulk-component-build',[seed,'build','tests/m19ef/reference-bulk/main.mio','-o',component_builder,'--bunko-root',snap/'bunko']);component=out/'bulk-component-native';run('bulk-component-generate',[component_builder,component]);run('bulk-component-signature',['codesign','--verify','--strict',component]);run('bulk-component-repro',[component_builder,out/'bulk-component-repro-native']);assert component.read_bytes()==(out/'bulk-component-repro-native').read_bytes()
for repeat in range(32):assert run('bulk-component-'+str(repeat),[component],timeout=30)==struct.pack('<Q',15)

for case,source,repeats in [('stress','tests/m19ef/old-mark-stress/ordinary/main.mio',32),('lifecycle','tests/m19ef/old-mark-faults/lifecycle/main.mio',32),('shapes','tests/m19ef/old-mark-faults/shapes/main.mio',32),('blocking','tests/m19ef/old-mark-faults/blocking/main.mio',32),('epochs','tests/m19ef/old-mark-epochs/ordinary/main.mio',32),('bulk','tests/m19ef/old-mark-bulk/ordinary/main.mio',32),('abort-alloc','tests/m19ef/old-mark-proto/abort-alloc/main.mio',8),('ordinary','tests/m19ef/old-mark-minor/ordinary/main.mio',32),('cell','tests/m19ef/old-mark-satb/cell/main.mio',32),('array','tests/m19ef/old-mark-satb/array/main.mio',32),('immutable','tests/m19ef/old-mark-proto/immutable/main.mio',32)]:
 image=out/(case+'-native');capacity_args=['1048576','32','stress'] if case=='stress' else (['1048576','32'] if case=='epochs' else (['16777216'] if case in ('bulk','array') else []))
 selected_builder=bulk_builder if case=='bulk' else builder;selected_snap=bulk_snap if case=='bulk' else snap
 run(case+'-build',[selected_builder,selected_snap,source,selected_snap/'bunko',image]+capacity_args);run(case+'-signature',['codesign','--verify','--strict',image]);duplicate=out/(case+'-repro-native');run(case+'-repro',[selected_builder,selected_snap,source,selected_snap/'bunko',duplicate]+capacity_args);assert image.read_bytes()==duplicate.read_bytes()
 for repeat in range(repeats):
  label=case+'-'+str(repeat);fifo=root/'build/m19ef-old-blocking-fifo'
  if case=='blocking':assert not fifo.exists();os.mkfifo(fifo,0o600)
  try:stdout=run(label,[image],timeout=60)
  finally:
   if case=='blocking':fifo.unlink()
  data=(out/(label+'.stderr')).read_bytes();assert len(data)==552,(case,len(data));old=struct.unpack('<24Q',data[:192]);mut=struct.unpack('<45Q',data[192:]);assert old[0]==1330406477 and old[12:17]==((8,8,0,1,1) if case=='epochs' else ((4,4,0,1,1) if case=='array' else (1,1,0,1,1)));assert mut[26]==0
  if case=='stress':
   lines=stdout.splitlines();assert len(lines)==7,stdout
   assert lines[:3]==[b'0',b'2',b'2'] and int(lines[3])>=184 and lines[4:]==[b'hai',b'4',b'0'],stdout
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=71 and old[5]>=1 and old[23]==0,old
   assert old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4 and mut[24:27]==(0,262240,0)
  elif case=='lifecycle':
   assert stdout==b'0\n1\nhai\n4\n0\n',stdout
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=4001 and old[5]>=1 and old[23]==0,old
   assert old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4
   for off in (7,14):assert mut[off:off+6]==(1,1,1,1,3,3)
  elif case=='shapes':
   assert stdout==b'0\n2688\n2688\n2\nhai\nhai\n4\n0\n',stdout
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=196 and old[5]>=1 and old[23]==0,old
   assert old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4
  elif case=='blocking':
   assert stdout==b'hai\nhai\n4\n0\n1\n1\n0\n',stdout
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=4 and old[5]>=1 and old[23]==0,old
   assert old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
   assert mut[22]>=108 and mut[24:27]==(0,131120,0) and mut[30]>0 and mut[31]>0
   assert all(mut[i]>0 and mut[i+1]>0 for i in (36,38,40))
  elif case=='epochs':
   assert stdout==b'0\n9\n4\n8\n8\n8\n0\n0\n',stdout
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=130 and old[5]>=1 and old[23]==0,old
   assert old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,8)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4
   for off in (7,14):assert mut[off:off+6]==(8,8,8,8,24,24)
  elif case=='bulk':
   verify_bulk(stdout,data)
  elif case=='ordinary':
   lines=stdout.splitlines();assert len(lines)==11,stdout
   n=list(map(int,lines[:7]));assert n[:2]==[0,2] and n[2]>=200 and n[3]>=200 and n[4]>=5 and n[5]==0 and n[6]==8012,n
   assert lines[7:]==[b'hai',b'hai',b'4',b'0'],stdout
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]>=8012 and old[5]>=1 and old[23]==0,old
   assert old[9]>0 and old[7]<=old[10]<=old[11]<=old[8] and old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4 and mut[22]>=208 and mut[24:27]==(0,262240,0)
  elif case=='array':
   nums=list(map(int,stdout.splitlines()));assert len(nums)==40,nums
   cycles=[nums[i:i+10] for i in range(0,40,10)]
   for v in cycles:assert v[:4]==[0,2,17999,11999] and 0<=v[4]<=11999 and v[5:8]==[12021,1,4] and v[9]==0,v
   assert any(v[4]>2 and v[8]>0 for v in cycles),'no actual adversarial in-scan witness across four fully validated epochs'
   assert old[1]==4 and old[2]==0 and old[3]==old[4]==12021 and old[5]==1 and old[23]==0,old
   assert old[17:20]==(12021,12021,12021) and old[20:23]==(0,0,4)
   assert old[9]==cycles[-1][8]
   if old[9]>0:assert old[7]<=old[10]<=old[11]<=old[8]
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

# A necessary Shadow descriptor v2 fix intentionally changes Worker images.
# Preserve historical hashes, but require exact current-source E3 equivalence,
# full signed reproduction and all original functional/default profile checks.
default_comparison=[]
default=out/'default-builder';run('default-build',[seed,'build','tests/m19ef/user-workers/builder.mio','-o',default,'--bunko-root',snap/'bunko'])
for case,expected in [('ordinary',b'12000\n24000\n0\n'),('handoff',b'0\nhai\n0\n0\n'),('deep',b'91\n0\n'),('capture',b'101\n102\n0\n')]:
 image=out/('default-'+case+'-native');run('default-'+case+'-build',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);assert image.read_bytes()==(e3_baseline/(case+'-native')).read_bytes();default_comparison.append(dict(case=case,current_sha256=sha(image),same_source_E3=str(e3_baseline),same_source_signed_bytes_equal=True,historical17_sha256=sha(historical17/(case+'-native')),historical17_equal=image.read_bytes()==(historical17/(case+'-native')).read_bytes(),reason='compiler-generated indirect global slots remove stale roots across blocked ordinary calls'));assert run('default-'+case,[image],timeout=30)==expected
 duplicate=out/('default-'+case+'-repro-native');run('default-'+case+'-repro',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',duplicate]);assert image.read_bytes()==duplicate.read_bytes()
(out/'default-current-source-comparison.json').write_text(json.dumps(default_comparison,indent=2)+'\n')
for package in ['bunko/kiso/runtime/gc','bunko/kiso/aot/lower']:
 data=json.loads(run('unit-'+package.replace('/','-'),[seed,'test',package,'--bunko-root',snap/'bunko','--format=json'],timeout=1200));assert data['summary']['failed']==0
assert all(sha(bulk_snap/p)==h for p,h in bulk_manifest.items())
assert all(sha(root/p)==h and sha(snap/p)==h for p,h in manifest.items());(out/'source-unchanged.json').write_text(json.dumps(dict(unchanged=True,source_manifest_sha256=digest))+'\n');(out/'summary.json').write_text(json.dumps(dict(status='PASS_SCOPED_FAILURE_TYPE_BLOCKING_REPEAT',retained_native_commands=retained_index,new_native_commands=len(rows)-retained_index,E4='IN_PROGRESS',E5='NOT_RUN',E6='NOT_RUN',M19e='NOT_ACCEPTED',source_manifest_sha256=digest,GCStress_program_runs=32,actual_OS_stack_failure_runs=32,actual_OS_join_failure_retry_runs=32,late_registration_runs=32,type_program_runs=32,blocking_program_runs=32,fatal_OOM_cases=16,clock_failure_runs=32,epoch_program_runs=32,epochs_per_program=8,explicit_abort_recovery_epochs_per_program=3,queue_component_runs=64,bounds_component_runs=32,bulk_map_runs=32,bulk_contract='WHITE_BEFORE_SCAN_V1_TEST_OVERLAY_ONLY',allocation_minor_runs=32,array_runs=32,cell_runs=32,immutable_runs=32,legacy_major_cancel_runs=8,E3_default_images='IDENTICAL_TO_COMPLETE_SAME_SOURCE_E3_DESCRIPTOR_V2',concurrent_allocation_minor='VALIDATED_SCOPED',bulk_writes='PASS_SCOPED_REFERENCE_ARRAY_AND_MAP',sweep='NOT_IMPLEMENTED',complete_failure_audit='IN_PROGRESS'),indent=2)+'\n')
