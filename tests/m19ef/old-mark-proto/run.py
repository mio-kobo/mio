#!/usr/bin/env python3
"""Frozen experimental immutable Old mark; never full SATB/E4 acceptance."""
import hashlib,json,os,pathlib,platform,shutil,signal,struct,subprocess,sys,time
root=pathlib.Path.cwd().resolve();out=(root/sys.argv[1]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence');out.mkdir();snap=out/'source-snapshot';snap.mkdir();sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();manifest={}
for folder in ['bunko','selfhost','tests/m19ef']:
 for p in sorted((root/folder).rglob('*.mio')):
  rel=p.relative_to(root);d=snap/rel;d.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,d);manifest[str(rel)]=sha(p)
(out/'source-manifest.json').write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');digest=sha(out/'source-manifest.json');shutil.copyfile(pathlib.Path(__file__),out/'controller.py');seed=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
(out/'provenance.json').write_text(json.dumps(dict(source_manifest_sha256=digest,seed_sha256=sha(seed),head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),branch=subprocess.check_output(['git','branch','--show-current'],text=True).strip(),host=platform.platform(),target='darwin-arm64',scope='immutable Old marker prototype; SATB/concurrent allocation/Minor and Sweep not implemented'),indent=2)+'\n');rows=[]
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
run('seed-signature',['codesign','--verify','--strict',seed]);builder=out/'old-builder';run('build',[seed,'build','tests/m19ef/old-mark-proto/builder.mio','-o',builder,'--bunko-root',snap/'bunko']);run('builder-signature',['codesign','--verify','--strict',builder]);observations=[]
for case,expected,repeats in [('immutable',b'0\nhai\nhai\nhai\n4\n0\n',32),('abort-store',b'6\n0\n',8),('abort-alloc',b'7\n6\n0\n',8)]:
 image=out/(case+'-native');source='tests/m19ef/old-mark-proto/'+case+'/main.mio';run(case+'-build',[builder,snap,source,snap/'bunko',image]);run(case+'-signature',['codesign','--verify','--strict',image]);duplicate=out/(case+'-repro-native');run(case+'-repro',[builder,snap,source,snap/'bunko',duplicate]);assert image.read_bytes()==duplicate.read_bytes()
 for repeat in range(repeats):
  label=case+'-'+str(repeat);assert run(label,[image],timeout=60)==expected
  data=(out/(label+'.stderr')).read_bytes();assert len(data)==552,(case,len(data));old=struct.unpack('<24Q',data[:192]);mut=struct.unpack('<45Q',data[192:]);assert old[0]==1330406477 and old[12:17]==(1,1,0,1,1) and old[20:23]==(0,0,1) if case=='immutable' else old[0]==1330406477 and old[12:17]==(1,1,0,1,1)
  assert mut[:6]==(1,1,0,0,3,3) and mut[26]==0 and old[6]!=mut[42] and old[6]!=mut[43]
  if case=='immutable':
   assert old[1:6]==(4,0,8001,8001,1),old
   assert old[9]>0 and old[7]<=old[10]<=old[11]<=old[8] and old[17:20]==(8001,8001,8001) and old[23]==0
   assert mut[7:13]==(1,1,1,1,3,3) and mut[43] and mut[43]!=mut[42]
  else:assert old[1]==6 and old[23]!=0
  observations.append(dict(case=case,repeat=repeat,old_words=list(old),mutator_words=list(mut),image_sha256=sha(image),source_manifest_sha256=digest))
(out/'observations.json').write_text(json.dumps(observations,indent=2)+'\n')
# Default E3 runtime must stay byte-identical on valid ordinary source.
default=out/'default-builder';run('default-build',[seed,'build','tests/m19ef/user-workers/builder.mio','-o',default,'--bunko-root',snap/'bunko'])
for case,expected in [('ordinary',b'12000\n24000\n0\n'),('handoff',b'0\nhai\n0\n0\n'),('deep',b'91\n0\n'),('capture',b'101\n102\n0\n')]:
 image=out/('default-'+case+'-native');run('default-'+case+'-build',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);assert image.read_bytes()==(root/'build/m19ef-evidence/E3-workers-20261030-16'/(case+'-native')).read_bytes();assert run('default-'+case,[image],timeout=30)==expected
for package in ['bunko/kiso/runtime/gc','bunko/kiso/aot/lower']:
 data=json.loads(run('unit-'+package.replace('/','-'),[seed,'test',package,'--bunko-root',snap/'bunko','--format=json']));assert data['summary']['failed']==0
assert all(sha(root/p)==h and sha(snap/p)==h for p,h in manifest.items());(out/'source-unchanged.json').write_text(json.dumps(dict(unchanged=True,source_manifest_sha256=digest))+'\n');(out/'summary.json').write_text(json.dumps(dict(status='PASS_SCOPED_IMMUTABLE_OLD_MARK_PROTOTYPE',E3_accepted_source='E3-workers-20261030-16',E4='IN_PROGRESS',E5='NOT_RUN',E6='NOT_RUN',M19e='NOT_ACCEPTED',source_manifest_sha256=digest,immutable_runs=32,abort_runs=16,SATB='NOT_INTEGRATED',concurrent_allocation_minor='UNSUPPORTED_ABORT_AND_JOIN',sweep='NOT_IMPLEMENTED'),indent=2)+'\n')
