#!/usr/bin/env python3
"""Audit-only controller. Compiler, runtime, image production and signing are Mio.
Usage: python3 tests/m19ef/automatic-roots/run.py build/m19ef-evidence/NEW-DIRECTORY
The output directory must not exist. Heavy processes run serially.
"""
import hashlib,json,os,pathlib,platform,shutil,signal,struct,subprocess,sys,time
root=pathlib.Path.cwd().resolve()
assert (root/'tests/m19ef/automatic-roots/builder.mio').is_file()
assert subprocess.check_output(['git','branch','--show-current'],cwd=root,text=True).strip()=='EXP/m19ef-portable-gc'
out=(root/sys.argv[1]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence')
out.mkdir();snap=out/'source-snapshot';snap.mkdir()
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
seed=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio')
assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
def source_files():
 return sorted(p for folder in ['bunko','selfhost','tests/m19ef'] for p in (root/folder).rglob('*.mio'))
manifest={}
for p in source_files():
 rel=p.relative_to(root);dest=snap/rel;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,dest);manifest[str(rel)]=sha(p)
(out/'source-manifest.json').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n');digest=sha(out/'source-manifest.json')
metadata=dict(head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),branch='EXP/m19ef-portable-gc',dirty=subprocess.check_output(['git','status','--porcelain=v1','--untracked-files=all'],cwd=root,text=True),host=platform.platform(),target='darwin-arm64',seed_path=str(seed),seed_sha256=sha(seed),controller_sha256=sha(pathlib.Path(__file__)),source_manifest_sha256=digest)
(out/'provenance.json').write_text(json.dumps(metadata,indent=2)+'\n')
rows=[]
def run(label,args,timeout=600,expected=0,cwd=snap):
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
builder=out/'automatic-builder'
run('automatic-build',[seed,'build','tests/m19ef/automatic-roots/builder.mio','-o',builder,'--bunko-root',snap/'bunko'])
run('automatic-builder-signature',['codesign','--verify','--strict',builder])
traces=[]
for case,expected,heap in [('ordinary',b'15400\n',True),('scalars',b'55000\n',False),('containers',b'4797\n',True)]:
 image=out/(case+'-native');source='tests/m19ef/automatic-roots/'+case+'/main.mio'
 run(case+'-build',[builder,snap,source,snap/'bunko',image])
 run(case+'-signature',['codesign','--verify','--strict',image])
 duplicate=out/(case+'-repro-native');run(case+'-repro-build',[builder,snap,source,snap/'bunko',duplicate])
 assert image.read_bytes()==duplicate.read_bytes(),('non-deterministic signed image',case)
 for repeat in range(16):
  label=case+'-'+str(repeat);assert run(label,[image],30)==expected
  data=(out/(label+'.stderr')).read_bytes();assert len(data)==64,(case,data)
  polls,pubs,depth,registrations,exits,acquired,released,minor=struct.unpack('<8Q',data)
  assert polls>0 and pubs>polls and depth>0
  assert (registrations,exits,acquired,released)==(1,1,3,3)
  assert (minor>0)==heap
  if case=='ordinary':assert depth>=5
  traces.append(dict(case=case,repeat=repeat,polls=polls,publications=pubs,max_shadow_frames=depth,registrations=registrations,exits=exits,acquisitions=acquired,releases=released,minor_collections=minor,image_sha256=sha(image)))
(out/'automatic-observations.json').write_text(json.dumps(traces,indent=2)+'\n')
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
for p in sorted(images.iterdir()):
 run('runtime-'+p.name,[p],30)
 old=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-evidence/GF4/bootstrap-r2/regressions-r2/D/runtime')/(p.name+'.native')
 comparisons.append(dict(name=p.name,sha256=sha(p),historical_sha256=sha(old),complete_signed_bytes_equal=p.read_bytes()==old.read_bytes()))
assert len(comparisons)==11 and all(x['complete_signed_bytes_equal'] for x in comparisons)
(out/'runtime-comparison.json').write_text(json.dumps(comparisons,indent=2)+'\n')
current={str(p.relative_to(root)):sha(p) for p in source_files()};assert current==manifest
assert all(sha(snap/p)==h for p,h in manifest.items())
(out/'final-source-unchanged.json').write_text(json.dumps(dict(unchanged=True,source_manifest_sha256=digest),indent=2)+'\n')
summary=dict(status='PASS_SCOPED_AUTOMATIC_MAIN_ROOTS_ONLY',E3='IN_PROGRESS',E4='NOT_RUN',E5='NOT_RUN',E6='NOT_RUN',M19e='NOT_ACCEPTED',M19f='OUT_OF_SCOPE',source_manifest_sha256=digest,ordinary_automatic_runs=len(traces),native_unit_tests=native_tests,managed_fixture_repeats=64,gc_metadata_witnesses=11,source_unchanged=True,ordinary_multi_mutator_path='NOT_INTEGRATED',blocking_call_protocol='NOT_INTEGRATED',concurrent_mark_sweep='NOT_IMPLEMENTED',new_source_fixed_point='NOT_RUN',artifacts={p.name:sha(p) for p in out.iterdir() if p.is_file() and (p.name.endswith('-native') or p.name.endswith('-builder'))})
(out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps(summary),flush=True)
