#!/usr/bin/env python3
"""Audit-only controller. Compiler, runtime, image production and signing are Mio.
Usage: python3 tests/m19ef/user-workers/run.py build/m19ef-evidence/NEW-DIRECTORY
The output directory must not exist. Heavy processes run serially.
"""
import fcntl,hashlib,json,os,pathlib,platform,shutil,signal,struct,subprocess,sys,time
from worker_trace import verify_worker_trace
root=pathlib.Path.cwd().resolve()
lock=os.fdopen(os.dup(int(os.environ['MIO_E5_TEST_LOCK_FD'])),'a+') if 'MIO_E5_TEST_LOCK_FD' in os.environ else (root/'build/.m19ef-native-test.lock').open('a+')
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
assert (root/'tests/m19ef/user-workers/builder.mio').is_file()
branch=subprocess.check_output(['git','branch','--show-current'],cwd=root,text=True).strip()
assert branch in ('EXP/m19ef-portable-gc','Mio-2.1'),branch
out=(root/sys.argv[1]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence')
out.mkdir();snap=out/'source-snapshot';snap.mkdir()
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
seed=pathlib.Path(os.environ.get('MIO_NATIVE_COMPILER','/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio')).resolve()
assert sha(seed)==os.environ.get('MIO_NATIVE_COMPILER_SHA256','be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79')
def source_files():
 return sorted(p for folder in ['bunko','selfhost','tests/m19ef'] for p in (root/folder).rglob('*.mio'))
manifest={}
for p in source_files():
 rel=p.relative_to(root);dest=snap/rel;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,dest);manifest[str(rel)]=sha(p)
(out/'source-manifest.json').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n');digest=sha(out/'source-manifest.json')
metadata=dict(head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),branch=branch,dirty=subprocess.check_output(['git','status','--porcelain=v1','--untracked-files=all'],cwd=root,text=True),host=platform.platform(),target='darwin-arm64',seed_path=str(seed),seed_sha256=sha(seed),controller_sha256=sha(pathlib.Path(__file__)),source_manifest_sha256=digest)
metadata['VERSION_fixture_sha256']=sha(root/'VERSION')
(out/'provenance.json').write_text(json.dumps(metadata,indent=2)+'\n')
shutil.copyfile(pathlib.Path(__file__),out/'controller.py')
shutil.copyfile(pathlib.Path(__file__).with_name('worker_trace.py'),out/'worker_trace.py')
rows=[]
def run(label,args,timeout=600,expected=0,cwd=snap):
 if len(args)==1 and pathlib.Path(args[0]).parent==out:cwd=root
 start=time.monotonic()
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  p=subprocess.Popen(list(map(str,args)),cwd=cwd,stdout=so,stderr=se,start_new_session=True);timed=False
  while True:
   got,status,usage=os.wait4(p.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:
    timed=True
    with (out/(label+'.timeout-processes')).open('xb') as diagnostic:subprocess.run(['ps','-Ao','pid,ppid,%cpu,rss,etime,command'],stdout=diagnostic,check=False)
    os.killpg(p.pid,signal.SIGKILL);got,status,usage=os.wait4(p.pid,0);break
   time.sleep(.05)
  p.returncode=os.waitstatus_to_exitcode(status)
 row=dict(label=label,argv=list(map(str,args)),cwd=str(cwd),exit=p.returncode,expected_exit=expected,timed_out=timed,timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=usage.ru_utime+usage.ru_stime,peak_rss_bytes_darwin=usage.ru_maxrss,seed_sha256=sha(seed),source_manifest_sha256=digest,target='darwin-arm64',stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')))
 row['status']='PASS' if p.returncode==expected and not timed else 'FAIL';rows.append(row)
 (out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,row['status'],p.returncode,flush=True)
 assert row['status']=='PASS',(label,(out/(label+'.stdout')).read_bytes()[-1200:],(out/(label+'.stderr')).read_bytes()[-1200:])
 return (out/(label+'.stdout')).read_bytes()
run('seed-version',[seed,'version']);run('seed-signature',['codesign','--verify','--strict',seed])
builder=out/'worker-builder'
run('worker-build',[seed,'build','tests/m19ef/user-workers/builder.mio','-o',builder,'--bunko-root',snap/'bunko'])
run('worker-builder-signature',['codesign','--verify','--strict',builder])
traces=[]
fifo=root/'build/m19ef-worker-fifo';assert not fifo.exists()
for case,expected in [('ordinary',b'12000\n24000\n0\n'),('handoff',b'0\nhai\n0\n0\n'),('nativefs',b'0\n2\n0\n0\n'),('blocking',b'1\n1\n0\n0\n'),('global-snapshot',b'hai\n1\n1\n0\n')]:
 image=out/(case+'-native');source='tests/m19ef/user-workers/'+case+'/main.mio'
 run(case+'-build',[builder,snap,source,snap/'bunko',image]);run(case+'-signature',['codesign','--verify','--strict',image])
 duplicate=out/(case+'-repro-native');run(case+'-repro-build',[builder,snap,source,snap/'bunko',duplicate]);assert image.read_bytes()==duplicate.read_bytes()
 if case in ('blocking','global-snapshot'):os.mkfifo(fifo,0o600)
 try:
  for repeat in range(64):
   label=case+'-'+str(repeat);assert run(label,[image],30)==expected
   v=verify_worker_trace(case,(out/(label+'.stderr')).read_bytes())
   traces.append(dict(case=case,repeat=repeat,words=list(v),snapshot_max_roots=[v[36],v[38],v[40]],snapshot_epochs=[v[37],v[39],v[41]],source_manifest_sha256=digest,image_sha256=sha(image)))
 finally:
  if case in ('blocking','global-snapshot'):fifo.unlink()
(out/'worker-observations.json').write_text(json.dumps(traces,indent=2)+'\n')
# A real failed NativeFS readiness probe must release the blocked reader and
# join both actual native threads; it remains a rejected positive GC witness.
failed_probe=snap/'global-snapshot-failed-probe.mio'
original=(snap/'tests/m19ef/user-workers/global-snapshot/main.mio').read_text()
version_path='VERSION';missing_path='build/m19ef-readiness-probe-missing'
assert not pathlib.Path(missing_path).exists() and original.count(version_path)==1
failed_probe.write_text(original.replace(version_path,missing_path))
image=out/'global-snapshot-failed-probe-native';run('global-failed-probe-build',[builder,snap,failed_probe.name,snap/'bunko',image]);run('global-failed-probe-signature',['codesign','--verify','--strict',image])
os.mkfifo(fifo,0o600)
try:
 assert run('global-failed-probe',[image],30)==b'-2\nhai\n1\n1\n0\n'
 packet=(out/'global-failed-probe.stderr').read_bytes();v=struct.unpack('<45Q',packet)
 assert v[:6]==(1,1,0,0,3,3) and v[24]==0 and v[26]==0
 for base in (7,14):assert v[base:base+6]==(1,1,1,1,3,3)
 (out/'global-failed-probe.json').write_text(json.dumps(dict(status='EXPECTED_FIXTURE_FAILURE_WITH_REAL_JOIN_AND_CLEANUP',source_sha256=sha(failed_probe),trace=list(v),concurrent_positive=False),indent=2)+'\n')
finally:fifo.unlink()
for case,expected in [('oom',2),('overflow',1),('duplicate',1),('snapshot-limit',1)]:
 image=out/(case+'-native');run(case+'-build',[builder,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);run(case+'-signature',['codesign','--verify','--strict',image]);run(case+'-negative',[image],30,expected=expected)
# Longer user waits must complete naturally; the joining parent never cancels.
for case,expected,repeats in [('join-long',b'3\n0\n0\n',4),('numeric',b'1.5\n3.25\nhai\niie\n7\n0\n',16)]:
 image=out/(case+'-native');run(case+'-build',[builder,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);run(case+'-signature',['codesign','--verify','--strict',image])
 for repeat in range(repeats):
  label=case+'-'+str(repeat);assert run(label,[image],10)==expected
  v=struct.unpack('<45Q',(out/(label+'.stderr')).read_bytes());assert v[42] and v[43] and v[42]!=v[43] and v[44]==0
  assert v[24]==0 and v[26]==0 and v[7:13]==(1,1,1,1,3,3)
  if case=='join-long':assert v[33]-v[32]>=1400000000
  traces.append(dict(case=case,repeat=repeat,words=list(v),source_manifest_sha256=digest,image_sha256=sha(image)))
# Ordinary recursive stacks, typed callee roots and forwarded array mutations.
for case,expected,repeats in [('deep',b'91\n0\n',8),('shapes',b'84\n84\n0\n',16),('remembered',b'0\n0\n',32),('capture',b'101\n102\n0\n',16)]:
 image=out/(case+'-native');run(case+'-build',[builder,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);run(case+'-signature',['codesign','--verify','--strict',image])
 for repeat in range(repeats):
  label=case+'-'+str(repeat);assert run(label,[image],30)==expected
  v=struct.unpack('<45Q',(out/(label+'.stderr')).read_bytes());assert v[42] and v[43] and v[42]!=v[43]
  assert v[24]==0 and v[26]==0
  for base in ((7,) if case=='deep' else (7,14)):
   assert v[base:base+6]==(1,1,1,1,3,3)
  if case!='deep':assert len(set(v[42:45]))==3 and all(v[42:45])
  if case=='remembered':assert v[22]>=10
  traces.append(dict(case=case,repeat=repeat,words=list(v),source_manifest_sha256=digest,image_sha256=sha(image)))
# Automatic shutdown joins and public process waits must permit real GC.
for case,expected in [('autojoin',b'123\n'),('spawn',b'0\n0\n')]:
 image=out/(case+'-native');run(case+'-build',[builder,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);run(case+'-signature',['codesign','--verify','--strict',image])
 for repeat in range(8):
  label=case+'-'+str(repeat);assert run(label,[image],30)==expected
  v=struct.unpack('<45Q',(out/(label+'.stderr')).read_bytes());assert v[7:13]==(1,1,1,1,3,3) and v[42] and v[43] and v[42]!=v[43] and v[44]==0
  if case=='spawn':assert v[26]==0 and v[23]>0 and v[29]>0
  traces.append(dict(case=case,repeat=repeat,words=list(v),source_manifest_sha256=digest,image_sha256=sha(image)))
# Unsupported fork-based test setup must reject at lowering, before image/run.
image=out/'spawn-test-native';run('spawn-test-rejected',[builder,snap,'tests/m19ef/user-workers/spawn-test/main.mio',snap/'bunko',image],expected=67);assert not image.exists()
# Native bridge ABI cannot reinterpret objects or narrow integers as words.
for case in ['bad-return','bad-return-narrow','bad-parameter']:
 image=out/(case+'-native');run(case+'-rejected',[builder,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image],expected=67);assert not image.exists()
# Normal test linking: delayed user callback reports before completion packet.
image=out/'packet-native';records=out/'packet-records.bin'
run('packet-build',[builder,snap,'tests/m19ef/user-workers/packet/main.mio',snap/'bunko',image,'packet']);run('packet-signature',['codesign','--verify','--strict',image])
run('packet',['/bin/zsh','-c','exec "$1" 3>"$2"','zsh',image,records],expected=1)
assert records.read_bytes()==struct.pack('<Q',14)+b'worker-failure'+struct.pack('<Q',18446744073709551615)
# Real configured-stack adapter rollback, with explicit imported-op fault hooks.
pbuild=out/'worker-platform-builder';image=out/'worker-platform-native'
run('worker-platform-build',[seed,'build','tests/m19ef/worker-platform/main.mio','-o',pbuild,'--bunko-root',snap/'bunko'])
run('worker-platform-generate',[pbuild,image]);run('worker-platform-signature',['codesign','--verify','--strict',image]);run('worker-platform',[image],30)
# Hold each real output FIFO full until Mio completes100 Major GCs.
for output_case,channel,expected_exit in [('printing','stdout',0),('report','stderr',1)]:
 image=out/(output_case+'-native');run(output_case+'-build',[builder,snap,'tests/m19ef/user-workers/'+output_case+'/main.mio',snap/'bunko',image]);run(output_case+'-signature',['codesign','--verify','--strict',image])
 for repeat in range(4):
  label=output_case+'-'+str(repeat);stem='print' if output_case=='printing' else 'report';fifo=root/('build/m19ef-'+stem+'-fifo');marker=root/('build/m19ef-'+stem+'-ready');assert not fifo.exists() and not marker.exists();os.mkfifo(fifo,0o600)
  readfd=os.open(fifo,os.O_RDONLY|os.O_NONBLOCK);writefd=os.open(fifo,os.O_WRONLY);start=time.monotonic();ready=False;chunks=[];done=False;usage=None
  try:
   other_channel='stderr' if channel=='stdout' else 'stdout'
   with (out/(label+'.'+other_channel)).open('xb') as other:
    child=subprocess.Popen([str(image)],cwd=root,stdout=writefd if channel=='stdout' else other,stderr=other if channel=='stdout' else writefd,start_new_session=True);os.close(writefd)
    def check_child():
     got,status,measured=os.wait4(child.pid,os.WNOHANG)
     return (got,os.waitstatus_to_exitcode(status) if got else None,measured)
    while time.monotonic()-start<20:
     if marker.exists():ready=True;break
     got,rc,measured=check_child()
     if got:done=True;usage=measured;child.returncode=rc;break
     time.sleep(.01)
    while time.monotonic()-start<30:
     try:
      data=os.read(readfd,65536)
      if data:chunks.append(data)
      elif done:break
     except BlockingIOError:pass
     if not done:
      got,rc,measured=check_child()
      if got:done=True;usage=measured;child.returncode=rc
     time.sleep(.002)
    if not done:
     os.killpg(child.pid,signal.SIGKILL);got,status,usage=os.wait4(child.pid,0);child.returncode=os.waitstatus_to_exitcode(status)
   output=b''.join(chunks);(out/(label+'.'+channel)).write_bytes(output)
   row=dict(label=label,argv=[str(image)],cwd=str(root),exit=child.returncode,expected_exit=expected_exit,timed_out=not done,timeout_seconds=30,wall_seconds=time.monotonic()-start,cpu_seconds=usage.ru_utime+usage.ru_stime,peak_rss_bytes_darwin=usage.ru_maxrss,source_manifest_sha256=digest,seed_sha256=sha(seed),target='darwin-arm64',stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')),GC_ready_before_draining=ready)
   row['status']='PASS' if child.returncode==expected_exit and ready and done else 'FAIL';rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,row['status'],child.returncode,flush=True)
   assert row['status']=='PASS'
   if channel=='stdout':assert output==b'A'*200000+b'\n0\n'
   else:assert len(output)==200361 and output[:200001]==b'A'*200000+b'\n' and (out/(label+'.stdout')).read_bytes()==b'0\n'
   trace=(out/(label+'.stderr')).read_bytes();v=struct.unpack('<45Q',trace if channel=='stdout' else trace[200001:]);assert v[23]>=100 and v[24]==0 and v[26]==0 and v[42] and v[43] and v[42]!=v[43]
   traces.append(dict(case=output_case,repeat=repeat,words=list(v),GC_ready_before_draining=True,source_manifest_sha256=digest,image_sha256=sha(image)))
  finally:
   os.close(readfd);fifo.unlink()
   if marker.exists():marker.unlink()
(out/'worker-observations.json').write_text(json.dumps(traces,indent=2)+'\n')
# Preserve the previous main-thread ABI through the same changed source.
mainbuilder=out/'main-builder';run('main-build',[seed,'build','tests/m19ef/automatic-roots/builder.mio','-o',mainbuilder,'--bunko-root',snap/'bunko'])
for case,expected in [('ordinary',b'15400\n'),('scalars',b'55000\n'),('containers',b'4797\n')]:
 image=out/('main-'+case+'-native');run('main-'+case+'-build',[mainbuilder,snap,'tests/m19ef/automatic-roots/'+case+'/main.mio',snap/'bunko',image]);assert run('main-'+case,[image],30)==expected
native_tests=0
for package in ['bunko/kiso/runtime/gc','bunko/kiso/runtime/target','bunko/kiso/runtime/cpu/aarch64','bunko/kiso/runtime/os/darwin','bunko/kiso/aot/arm64','bunko/kiso/aot/macho','bunko/kiso/aot/lower','selfhost/pipeline']:
 data=json.loads(run('unit-'+package.replace('/','-'),[seed,'test',package,'--bunko-root',snap/'bunko','--format=json']))
 assert data['summary']['failed']==0 and data['summary']['passed']>0;native_tests+=data['summary']['passed']
for case in ['main-attach','managed-roots','safepoint-roots','thread-lifecycle']:
 b=out/(case+'-builder');image=out/(case+'-native')
 run(case+'-build',[seed,'build','tests/m19ef/'+case+'/main.mio','-o',b,'--bunko-root',snap/'bunko'])
 run(case+'-generate',[b,image]);run(case+'-signature',['codesign','--verify','--strict',image])
 for i in range(64 if case=='managed-roots' else 1):
  data=run(case+'-'+str(i),[image],30)
  if case=='main-attach':assert struct.unpack('<8Q',data)==(2,2,15,15,0,0,0,5)
  elif case=='managed-roots':assert struct.unpack('<4Q',data)==(1000,1002,4,131088)
  elif case=='safepoint-roots':assert len(data)==40 and struct.unpack('<5Q',data)[2]==1000
  else:assert len(data)==416
fixture=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-evidence/GF4/audit-fixtures/runtime_metadata.mio')
assert sha(fixture)=='93d0965dd5698a248d663964ee86da2951c6148a9c5a04d6fe7613c9ba351d9a'
runtime=out/'runtime-source';runtime.mkdir();images=out/'runtime-images';images.mkdir()
for p in (snap/'bunko/kiso/aot/lower').glob('*.mio'):
 if not p.name.endswith('_test.mio'):shutil.copyfile(p,runtime/p.name)
shutil.copyfile(fixture,runtime/'audit.mio');rtbuilder=out/'runtime-builder'
run('runtime-build',[seed,'build',runtime,'-o',rtbuilder,'--bunko-root',snap/'bunko']);run('runtime-generate',[rtbuilder,images]);comparisons=[]
# A confirmed LOS false-OOM repair changes emitted Runtime bytes. Preserve the
# historical byte check independently: restore ONLY the two before-repair
# emitter bodies, require exact GF4 signed images, and still execute every
# original assertion in the repaired current images plus exact reproduction.
fixtures=root/'tests/m19ef/e6/los-pressure'
fixture_pins={'before-runtime-alloc.mio.fixture': 'b1bbbdfeafacbe1b0381353858901498cde59639b3c7ff034ed70b7c98e96277', 'before-large_objects.mio.fixture': '04d1a5d83cc03d154de08df3dbbfdf57dd8eef178791d46ac306ce2c08923bcf'}
for name,digest_value in fixture_pins.items():assert sha(fixtures/name)==digest_value
compat=out/'runtime-before-emitter-source';shutil.copytree(runtime,compat)
text=(compat/'lower.mio').read_text();a=text.index('waza emit_runtime_alloc(');b=text.index('\nwaza ',a+5)
(compat/'lower.mio').write_text(text[:a]+(fixtures/'before-runtime-alloc.mio.fixture').read_text().rstrip()+'\n'+text[b:])
shutil.copyfile(fixtures/'before-large_objects.mio.fixture',compat/'large_objects.mio')
compat_builder=out/'runtime-before-emitter-builder';run('runtime-before-emitter-build',[seed,'build',compat,'-o',compat_builder,'--bunko-root',snap/'bunko'])
compat_images=out/'runtime-before-emitter-images';compat_images.mkdir();run('runtime-before-emitter-generate',[compat_builder,compat_images])
duplicate_images=out/'runtime-reproduced-images';duplicate_images.mkdir();run('runtime-reproduce',[rtbuilder,duplicate_images])
expected_names={'ages','remembered','promotion-edge','major','precision','adaptive','cross-graph','alternating','minor-scan','precision-storage','shadow-frame-pop'}
assert {p.name for p in images.iterdir()}==expected_names=={p.name for p in compat_images.iterdir()}=={p.name for p in duplicate_images.iterdir()}
for p in sorted(images.iterdir()):
 run('runtime-'+p.name,[p],30);run('runtime-signature-'+p.name,['codesign','--verify','--strict',p])
 old=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-evidence/GF4/bootstrap-r2/regressions-r2/D/runtime')/(p.name+'.native')
 compatible=compat_images/p.name;run('runtime-before-emitter-'+p.name,[compatible],30)
 assert p.read_bytes()==(duplicate_images/p.name).read_bytes()
 assert compatible.read_bytes()==old.read_bytes()
 comparisons.append(dict(name=p.name,sha256=sha(p),historical_sha256=sha(old),complete_signed_bytes_equal=p.read_bytes()==old.read_bytes(),current_full_signed_reproduction=True,restored_before_emitter_sha256=sha(compatible),restored_before_emitter_equals_historical=True,current_original_metadata_assertions='PASS_NATIVE_EXIT0',reason='LOS allocation-pressure drain after final Major; historical bytes independently reproduced by restoring only pinned before-emitter bodies'))
assert len(comparisons)==11 and all(x['current_full_signed_reproduction'] and x['restored_before_emitter_equals_historical'] for x in comparisons)
(out/'runtime-comparison.json').write_text(json.dumps(comparisons,indent=2)+'\n')
(out/'runtime-compatibility-contract.json').write_text(json.dumps(dict(version='GC_METADATA_RUNTIME_REPAIR_V2',before_fixture_pins=fixture_pins,historical_whole_image_equality='independent restored-before-emitter images',current_verification='all11 original executable assertions, signatures, full signed reproduction; no weakened metadata assertions',new_runtime_regression='tests/m19ef/e6/los-pressure'),indent=2)+'\n')
current={str(p.relative_to(root)):sha(p) for p in source_files()};assert current==manifest
assert sha(root/'VERSION')==metadata['VERSION_fixture_sha256']
assert all(sha(snap/p)==h for p,h in manifest.items())
(out/'final-source-unchanged.json').write_text(json.dumps(dict(unchanged=True,source_manifest_sha256=digest),indent=2)+'\n')
summary=dict(status='PASS_SCOPED_USER_WORKER_INTEGRATION',E3='IN_PROGRESS',E4='NOT_RUN',E5='NOT_RUN',E6='NOT_RUN',M19e='NOT_ACCEPTED',M19f='OUT_OF_SCOPE',source_manifest_sha256=digest,ordinary_worker_runs=len(traces),native_unit_tests=native_tests,managed_fixture_repeats=64,gc_metadata_witnesses=11,source_unchanged=True,ordinary_multi_mutator_path='INTERNAL_NATIVE_STARTUP_BRIDGE_VALIDATED',blocking_call_protocol='NATIVEFS_SYSCALL_AND_WAIT_VALIDATED_SCOPED',concurrent_mark_sweep='NOT_IMPLEMENTED',new_source_fixed_point='NOT_RUN',artifacts={p.name:sha(p) for p in out.iterdir() if p.is_file() and (p.name.endswith('-native') or p.name.endswith('-builder'))})
(out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps(summary),flush=True)
