#!/usr/bin/env python3
"""Scoped typed SATB development: not full E4 or a concurrent allocation gate."""
import hashlib,json,os,pathlib,platform,shutil,signal,struct,subprocess,sys,time
root=pathlib.Path.cwd().resolve();out=(root/sys.argv[1]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence');out.mkdir();snap=out/'source-snapshot';snap.mkdir();sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();manifest={}
for folder in ['bunko','selfhost','tests/m19ef']:
 for p in sorted((root/folder).rglob('*.mio')):
  rel=p.relative_to(root);d=snap/rel;d.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,d);manifest[str(rel)]=sha(p)
(out/'source-manifest.json').write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');digest=sha(out/'source-manifest.json');shutil.copyfile(pathlib.Path(__file__),out/'controller.py');seed=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
(out/'provenance.json').write_text(json.dumps(dict(source_manifest_sha256=digest,seed_sha256=sha(seed),head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),branch=subprocess.check_output(['git','branch','--show-current'],text=True).strip(),host=platform.platform(),target='darwin-arm64',scope='typed global/Cell/array SATB development; no concurrent allocation/Minor/Sweep acceptance'),indent=2)+'\n');rows=[]
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
run('seed-signature',['codesign','--verify','--strict',seed]);builder=out/'satb-builder';run('build',[seed,'build','tests/m19ef/old-mark-proto/builder.mio','-o',builder,'--bunko-root',snap/'bunko']);run('builder-signature',['codesign','--verify','--strict',builder]);observations=[]
for case,source,repeats in [('cell','tests/m19ef/old-mark-satb/cell/main.mio',32),('array','tests/m19ef/old-mark-satb/array/main.mio',32),('immutable','tests/m19ef/old-mark-proto/immutable/main.mio',32),('abort-alloc','tests/m19ef/old-mark-proto/abort-alloc/main.mio',8)]:
 image=out/(case+'-native');run(case+'-build',[builder,snap,source,snap/'bunko',image]);run(case+'-signature',['codesign','--verify','--strict',image]);duplicate=out/(case+'-repro-native');run(case+'-repro',[builder,snap,source,snap/'bunko',duplicate]);assert image.read_bytes()==duplicate.read_bytes()
 for repeat in range(repeats):
  label=case+'-'+str(repeat);stdout=run(label,[image],timeout=60);data=(out/(label+'.stderr')).read_bytes();assert len(data)==552,(case,len(data));old=struct.unpack('<24Q',data[:192]);mut=struct.unpack('<45Q',data[192:]);assert old[0]==1330406477 and old[12:17]==(1,1,0,1,1);assert mut[26]==0
  if case=='array':
   nums=list(map(int,stdout.splitlines()));assert len(nums)==9,nums
   assert nums[:2]==[0,2] and nums[2:4]==[6001,6001],nums
   assert nums[3]>=nums[4]>0 and nums[5]==12009 and nums[6]==1 and nums[7:]==[4,0],nums
   assert old[1]==4 and old[2]==0 and old[3]==old[4] and old[3]==12009 and old[5]==1 and old[23]==0,old
   assert len(set([old[6],mut[42],mut[43],mut[44]]))==4
  elif case=='cell':
   assert stdout==b'0\n5\n4\n7\n1\n4\n0\n',stdout
   assert old[1:6]==(4,0,7,7,1) and old[23]==0
  elif case=='immutable':
   assert stdout==b'0\nhai\nhai\nhai\n4\n0\n';assert old[1:6]==(4,0,8001,8001,1) and old[23]==0
  else:assert stdout==b'7\n6\n0\n' and old[1]==6 and old[23]!=0
  if case in ('array','immutable'):assert old[9]>0 and old[7]<=old[10]<=old[11]<=old[8] and old[17:20]==(old[3],old[3],old[3]) and old[20:23]==(0,0,1)
  observations.append(dict(case=case,repeat=repeat,stdout=stdout.decode(),old_words=list(old),mutator_words=list(mut),image_sha256=sha(image),source_manifest_sha256=digest))
(out/'observations.json').write_text(json.dumps(observations,indent=2)+'\n')
scanner_builder=out/'scanner-builder';run('scanner-build',[seed,'build','tests/m19ef/trace-layout/main.mio','-o',scanner_builder,'--bunko-root',snap/'bunko']);scanner=out/'scanner-native';run('scanner-generate',[scanner_builder,scanner]);run('scanner-signature',['codesign','--verify','--strict',scanner]);run('scanner-repro',[scanner_builder,out/'scanner-repro-native']);assert scanner.read_bytes()==(out/'scanner-repro-native').read_bytes()
for repeat in range(32):assert run('scanner-'+str(repeat),[scanner],timeout=30)==struct.pack('<Q',20)

default=out/'default-builder';run('default-build',[seed,'build','tests/m19ef/user-workers/builder.mio','-o',default,'--bunko-root',snap/'bunko'])
for case,expected in [('ordinary',b'12000\n24000\n0\n'),('handoff',b'0\nhai\n0\n0\n'),('deep',b'91\n0\n'),('capture',b'101\n102\n0\n')]:
 image=out/('default-'+case+'-native');run('default-'+case+'-build',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);assert image.read_bytes()!=(root/'build/m19ef-evidence/E3-workers-20261030-16'/(case+'-native')).read_bytes();assert run('default-'+case,[image],timeout=30)==expected
 duplicate=out/('default-'+case+'-repro-native');run('default-'+case+'-repro',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',duplicate]);assert image.read_bytes()==duplicate.read_bytes()
for package in ['bunko/kiso/runtime/gc','bunko/kiso/aot/lower']:
 data=json.loads(run('unit-'+package.replace('/','-'),[seed,'test',package,'--bunko-root',snap/'bunko','--format=json'],timeout=1200));assert data['summary']['failed']==0
assert all(sha(root/p)==h and sha(snap/p)==h for p,h in manifest.items());(out/'source-unchanged.json').write_text(json.dumps(dict(unchanged=True,source_manifest_sha256=digest))+'\n');(out/'summary.json').write_text(json.dumps(dict(status='PASS_SCOPED_TYPED_SATB',E4='IN_PROGRESS',E5='NOT_RUN',E6='NOT_RUN',M19e='NOT_ACCEPTED',source_manifest_sha256=digest,array_runs=32,cell_runs=32,immutable_runs=32,abort_runs=8,E3_current_source='FULL_REVALIDATION_REQUIRED_AFTER_REGISTRY_PROGRESS_FIX',concurrent_allocation_minor='UNSUPPORTED_ABORT_AND_JOIN',bulk_writes='UNSUPPORTED_ABORT_AND_JOIN',sweep='NOT_IMPLEMENTED'),indent=2)+'\n')
