#!/usr/bin/env python3
"""Resume the exact failed harness04 after all actual native checks passed so far.
No failed executable is accepted, no source may differ. Original failure stays.
"""
import hashlib,json,os,pathlib,shutil,signal,struct,subprocess,sys,time
root=pathlib.Path.cwd().resolve();prior=(root/sys.argv[1]).resolve();out=(root/sys.argv[2]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence');sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
manifest=json.loads((prior/'source-manifest.json').read_text());digest=sha(prior/'source-manifest.json');rows=json.loads((prior/'commands.json').read_text())
assert all(sha(root/p)==h and sha(prior/'source-snapshot'/p)==h for p,h in manifest.items())
assert all(r['exit']==r['expected_exit'] and not r['timed_out'] and r['source_manifest_sha256']==digest and sha(prior/(r['label']+'.stdout'))==r['stdout_sha256'] and sha(prior/(r['label']+'.stderr'))==r['stderr_sha256'] for r in rows)
assert rows[-1]['label']=='default-build'
for case,count in [('bulk',32),('abort-alloc',8),('ordinary',32),('cell',32),('array',32),('immutable',32),('bulk-component',32),('scanner',32)]:
 assert sum(r['label']==case+'-'+str(i) for r in rows for i in range(count))==count
shutil.copytree(prior,out);snap=out/'source-snapshot';shutil.copyfile(pathlib.Path(__file__),out/'continuation-controller.py');(out/'commands-original.json').write_text(json.dumps(rows,indent=2)+'\n');(out/'continuation-provenance.json').write_text(json.dumps(dict(original=str(prior),source_manifest_sha256=digest,all_original_commands_verified=True,original_harness_error='ValueError: too many values to unpack in default regression tuple; all actual commands so far successful',reuse_scope='same-source native executions only; no bootstrap/fixed-point inference',copied_native_builder_sha256=sha(out/'default-builder')),indent=2)+'\n')
def run(label,args,timeout=600):
 start=time.monotonic()
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  p=subprocess.Popen(list(map(str,args)),cwd=snap,stdout=so,stderr=se,start_new_session=True);timed=False
  while True:
   got,status,u=os.wait4(p.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:timed=True;os.killpg(p.pid,signal.SIGKILL);got,status,u=os.wait4(p.pid,0);break
   time.sleep(.03)
 row=dict(label=label,argv=list(map(str,args)),cwd=str(snap),exit=os.waitstatus_to_exitcode(status),expected_exit=0,timed_out=timed,timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=u.ru_utime+u.ru_stime,peak_rss_bytes_darwin=u.ru_maxrss,source_manifest_sha256=digest,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')));rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,row['exit'],flush=True);assert row['exit']==0 and not timed
 return (out/(label+'.stdout')).read_bytes()
default=out/'default-builder'
run('continued-default-builder-signature',['codesign','--verify','--strict',default])
for case,expected in [('ordinary',b'12000\n24000\n0\n'),('handoff',b'0\nhai\n0\n0\n'),('deep',b'91\n0\n'),('capture',b'101\n102\n0\n')]:
 image=out/('default-'+case+'-native');run('default-'+case+'-build',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',image]);assert image.read_bytes()==(root/'build/m19ef-evidence/E3-workers-20261030-17'/(case+'-native')).read_bytes();assert run('default-'+case,[image],30)==expected
 duplicate=out/('default-'+case+'-repro-native');run('default-'+case+'-repro',[default,snap,'tests/m19ef/user-workers/'+case+'/main.mio',snap/'bunko',duplicate]);assert image.read_bytes()==duplicate.read_bytes()
seed=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
for package in ['bunko/kiso/runtime/gc','bunko/kiso/aot/lower']:
 data=json.loads(run('unit-'+package.replace('/','-'),[seed,'test',package,'--bunko-root',snap/'bunko','--format=json'],1200));assert data['summary']['failed']==0
assert all(sha(root/p)==h and sha(snap/p)==h for p,h in manifest.items());(out/'source-unchanged.json').write_text(json.dumps(dict(unchanged=True,source_manifest_sha256=digest))+'\n')
(out/'summary.json').write_text(json.dumps(dict(status='PASS_SCOPED_BULK_MAP_MINOR',E4='IN_PROGRESS',E5='NOT_RUN',E6='NOT_RUN',M19e='NOT_ACCEPTED',source_manifest_sha256=digest,ordinary_programs=168,bulk_map_runs=32,allocation_minor_runs=32,array_runs=32,cell_runs=32,immutable_runs=32,legacy_major_cancel_runs=8,reference_bulk_component_runs=32,reference_bulk_checks_per_run=15,scanner_cases_per_run=20,scanner_runs=32,E3_default_images='IDENTICAL_TO_ACCEPTED_CANDIDATE17',overflow_recovery='NOT_IMPLEMENTED',repeated_epochs='NOT_IMPLEMENTED',sweep='NOT_IMPLEMENTED',original_same_source_executions=str(prior),wall_seconds=sum(r['wall_seconds'] for r in rows),cpu_seconds=sum(r['cpu_seconds'] for r in rows),peak_rss_bytes_darwin=max(r['peak_rss_bytes_darwin'] for r in rows)),indent=2)+'\n')
print('PASS_SCOPED_BULK_MAP_MINOR',flush=True)
