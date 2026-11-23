#!/usr/bin/env python3
"""Audit-only driver for actual production-heap concurrent work-queue component tests."""
import hashlib,json,os,pathlib,platform,shutil,signal,struct,subprocess,sys,time
root=pathlib.Path.cwd().resolve();out=(root/sys.argv[1]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence');out.mkdir();snap=out/'source-snapshot';snap.mkdir();sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();manifest={}
for folder in ['bunko','selfhost','tests/m19ef']:
 for p in sorted((root/folder).rglob('*.mio')):
  rel=p.relative_to(root);target=snap/rel;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,target);manifest[str(rel)]=sha(p)
(out/'source-manifest.json').write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');digest=sha(out/'source-manifest.json');shutil.copyfile(pathlib.Path(__file__),out/'controller.py');seed=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
(out/'provenance.json').write_text(json.dumps(dict(source_manifest_sha256=digest,seed_sha256=sha(seed),head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),branch=subprocess.check_output(['git','branch','--show-current'],text=True).strip(),host=platform.platform(),target='darwin-arm64',scope='two native producers + independent consumer of actual promoted Old objects; not SATB/Old Mark acceptance'),indent=2)+'\n');rows=[]
def run(label,args,expected=0,timeout=300):
 start=time.monotonic()
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  child=subprocess.Popen(list(map(str,args)),cwd=snap,stdout=so,stderr=se,start_new_session=True);timed=False
  while True:
   got,status,usage=os.wait4(child.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:
    timed=True;os.killpg(child.pid,signal.SIGKILL);got,status,usage=os.wait4(child.pid,0);break
   time.sleep(.03)
 row=dict(label=label,argv=list(map(str,args)),exit=os.waitstatus_to_exitcode(status),expected_exit=expected,timed_out=timed,timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=usage.ru_utime+usage.ru_stime,peak_rss_bytes_darwin=usage.ru_maxrss,source_manifest_sha256=digest,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')));rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,row['exit'],flush=True);assert row['exit']==expected and not timed
 return (out/(label+'.stdout')).read_bytes()
run('seed-signature',['codesign','--verify','--strict',seed]);builder=out/'builder';image=out/'queue-native'
run('build',[seed,'build','tests/m19ef/mark-work-queue/main.mio','-o',builder,'--bunko-root',snap/'bunko']);run('builder-signature',['codesign','--verify','--strict',builder]);run('generate',[builder,image]);run('image-signature',['codesign','--verify','--strict',image]);run('generate-repro',[builder,out/'queue-repro-native']);assert image.read_bytes()==(out/'queue-repro-native').read_bytes()
observations=[]
for repeat in range(64):
 data=run('queue-'+str(repeat),[image],timeout=30);assert len(data)==120
 v=struct.unpack('<15Q',data);assert v[:3]==(2002,2002,2002) and v[3]>0 and v[4:6]==(33033,2002)
 assert len(set(v[6:9]))==3 and all(v[6:9])
 for base in (9,11):assert max(v[base],v[13])<min(v[base+1],v[14])
 observations.append(dict(repeat=repeat,words=list(v),source_manifest_sha256=digest))
(out/'observations.json').write_text(json.dumps(observations,indent=2)+'\n')
# Both visitor mechanisms must enumerate identical actual production layouts.
trace_builder=out/'trace-builder';trace_image=out/'trace-native'
run('trace-build',[seed,'build','tests/m19ef/trace-layout/main.mio','-o',trace_builder,'--bunko-root',snap/'bunko']);run('trace-generate',[trace_builder,trace_image]);run('trace-signature',['codesign','--verify','--strict',trace_image])
run('trace-repro',[trace_builder,out/'trace-repro-native']);assert trace_image.read_bytes()==(out/'trace-repro-native').read_bytes()
for repeat in range(8):assert run('trace-'+str(repeat),[trace_image],timeout=30)==struct.pack('<Q',20)
# Test new core generation without claiming that it is concurrent collection.
data=json.loads(run('core-unit',[seed,'test','bunko/kiso/runtime/gc','--bunko-root',snap/'bunko','--format=json']));assert data['summary']['failed']==0
assert all(sha(root/p)==h and sha(snap/p)==h for p,h in manifest.items());(out/'source-unchanged.json').write_text(json.dumps(dict(unchanged=True,source_manifest_sha256=digest))+'\n');(out/'summary.json').write_text(json.dumps(dict(status='PASS_SCOPED_CONCURRENT_WORK_QUEUE',E3_accepted_source='E3-workers-20261030-16',E4='IN_PROGRESS',E5='NOT_RUN',E6='NOT_RUN',M19e='NOT_ACCEPTED',runs=64,items_per_run=2002,trace_cases_per_run=20,image_sha256=sha(image),source_manifest_sha256=digest,concurrent_mark='NOT_IMPLEMENTED',SATB='NOT_INTEGRATED',queue_native_workers=3),indent=2)+'\n')
