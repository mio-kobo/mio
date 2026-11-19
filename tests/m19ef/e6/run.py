#!/usr/bin/env python3
"""E6 native source bootstrap and GC regression driver; never auto-grants G6."""
from pathlib import Path
import os,sys,json,hashlib,shutil,subprocess,signal,time,re,fcntl
r=Path.cwd();out=(r/sys.argv[1]).resolve();assert out.is_relative_to(r/'build/m19ef-evidence');out.mkdir()
lock=(r/'build/.m19ef-native-test.lock').open('a+');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
seed=Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
def save(p,x):p.write_text(json.dumps(x,ensure_ascii=False,sort_keys=True,indent=2)+'\n')
review=json.loads((r/'build/m19ef-evidence/E5-v21-fixed32-20261030-01/final-integrity-review.json').read_text());assert review['E5']=='PASS_SCOPED'
rows=[];snap=r

def run(label,args,expected=0,timeout=1200,cwd=None):
 args=list(map(str,args));start=time.monotonic();print(label,'START',flush=True)
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  child=subprocess.Popen(args,cwd=cwd or snap,stdout=so,stderr=se,start_new_session=True,env=dict(os.environ,MIO_E5_TEST_LOCK_FD=str(lock.fileno())),pass_fds=(lock.fileno(),));timed=False
  while True:
   got,status,u=os.wait4(child.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:
    timed=True;os.killpg(child.pid,signal.SIGKILL);got,status,u=os.wait4(child.pid,0);break
   time.sleep(.05)
 row=dict(label=label,argv=args,cwd=str(cwd or snap),exit=os.waitstatus_to_exitcode(status),expected_exit=expected,timed_out=timed,timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=u.ru_utime+u.ru_stime,peak_rss_bytes_darwin=u.ru_maxrss,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')));rows.append(row);save(out/'commands.json',rows);print(label,row['exit'],flush=True);assert row['exit']==expected and not timed,row
 return (out/(label+'.stdout')).read_bytes()

save(out/'provenance.json',dict(status='E6_IN_PROGRESS_NOT_ACCEPTED',E5_review_sha256=sha(r/'build/m19ef-evidence/E5-v21-fixed32-20261030-01/final-integrity-review.json'),seed_sha256=sha(seed),head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),dirty=subprocess.check_output(['git','status','--porcelain=v1','--untracked-files=all'],text=True),target='darwin-arm64',production_compilation='native Mio only'))
for evidence in ('E6-snapshot-known-20261031-01','E6-snapshot-unknown-20261031-01'):
 proof=r/'build/m19ef-evidence'/evidence
 result=json.loads((proof/'summary.json').read_text());assert result['status']=='PASS_DIRECTED_OWNED_SNAPSHOT_NOT_E6_ACCEPTANCE' and result['snapshot_reads']==4000
 inputs=json.loads((proof/'canonical-manifest.json').read_text());assert inputs['bunko/kiso/runtime/gc/observations.mio']==sha(r/'bunko/kiso/runtime/gc/observations.mio')
backup=out/'pre-resource-bunko_data.mio';shutil.copyfile(r/'selfhost/resources/bunko_data.mio',backup)
gen=out/'resource-generator';run('resource-generator-build',[seed,'build','selfhost/embed-bunko/main.mio','--bunko-root',r/'bunko','-o',gen]);run('resource-generator-signature',['codesign','--verify','--strict',gen])
config=out/'resource-config';config.mkdir();(config/'input.config').write_text(str(r/'bunko'));(config/'output.config').write_text(str(r/'selfhost/resources/bunko_data.mio'));run('resource-refresh',[gen],cwd=config)
text=(r/'selfhost/resources/bunko_data.mio').read_text();pattern=r'path: ("(?:[^"\\]|\\.)*"), version: "[^"\n]+", length: (\d+), sha256: "([0-9a-f]+)", source: ("(?:[^"\\]|\\.)*")'
entries={}
for path,n,h,body in re.findall(pattern,text):
 path=json.loads(path);payload=json.loads(body).encode();actual=(r/'bunko'/path).read_bytes();assert payload==actual and len(actual)==int(n) and hashlib.sha256(actual).hexdigest()==h;entries[path]=h
expected={str(p.relative_to(r/'bunko')):sha(p) for p in (r/'bunko/kiso').rglob('*.mio') if not p.name.endswith('_test.mio')};assert entries==expected,(len(entries),len(expected));save(out/'embedded-resource-verification.json',dict(status='PASS_ALL_BYTES',count=len(entries),entries=entries,previous_bundle_sha256=sha(backup),current_bundle_sha256=sha(r/'selfhost/resources/bunko_data.mio')))
snap=out/'source-snapshot';snap.mkdir();manifest={}
for folder in ('bunko','selfhost','tests','specs','tools'):
 for p in sorted((r/folder).rglob('*')):
  if p.is_file() and p.suffix in ('.mio','.py','.md','.json','.sh','.txt','.fixture'):
   rel=p.relative_to(r);dest=snap/rel;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,dest);manifest[str(rel)]=sha(p)
save(out/'source-manifest.json',manifest);save(out/'plan.json',dict(status='FROZEN_E6_CANDIDATE',source_manifest_sha256=sha(out/'source-manifest.json'),signed_unsigned_fixed_point='B=C=D_REQUIRED',E5='PASS_SCOPED',E6='IN_PROGRESS',M19e='NOT_ACCEPTED'))
compiler=seed;generations={}
for gen in ('A','B','C','D'):
 image=out/('mio-'+gen);run(gen+'-full-source-build',[compiler,'build','selfhost/cli','--bunko-root',snap/'bunko','-o',image],timeout=1800);run(gen+'-signature',['codesign','--verify','--strict',image]);run(gen+'-version',[image,'version'])
 smoke=out/(gen+'-smoke');run(gen+'-smoke-build',[image,'build','tests/m19ef/e6/smoke.mio','--bunko-root',snap/'bunko','-o',smoke]);assert run(gen+'-smoke',[smoke])==b'2096128\n499500\n3.25\n'
 generations[gen]=dict(sha256=sha(image),compiler_sha256=sha(compiler),source_manifest_sha256=sha(out/'source-manifest.json'));save(out/'generations.json',generations);compiler=image
assert (out/'mio-B').read_bytes()==(out/'mio-C').read_bytes()==(out/'mio-D').read_bytes(),'signed fixed point mismatch'
unsigned_builder=out/'unsigned-builder';run('unsigned-builder-build',[compiler,'build','tests/m19ef/e6/unsigned_builder.mio','--bunko-root',snap/'bunko','-o',unsigned_builder]);run('unsigned-builder-signature',['codesign','--verify','--strict',unsigned_builder])
for gen in ('B','C','D'):
 unsigned=out/('mio-'+gen+'.unsigned');signed=out/('mio-'+gen+'.resigned');run(gen+'-raw-source-rebuild',[unsigned_builder,snap,'selfhost/cli',snap/'bunko',unsigned,signed],timeout=1800);assert signed.read_bytes()==(out/('mio-'+gen)).read_bytes();generations[gen]['unsigned_sha256']=sha(unsigned)
assert (out/'mio-B.unsigned').read_bytes()==(out/'mio-C.unsigned').read_bytes()==(out/'mio-D.unsigned').read_bytes()
save(out/'bootstrap-result.json',dict(status='PASS_ACTUAL_FULL_SOURCE_FIXED_POINT',generations=generations,raw_unsigned='captured directly from production pipeline.build_source, independently rebuilt and signed bytes matched to actual CLI chain',E6='IN_PROGRESS',M19e='NOT_ACCEPTED'))
for package in ('bunko/...','selfhost/frontend','selfhost/pipeline','selfhost/lsp/rpc'):
 data=json.loads(run('native-tests-'+package.replace('/','-'),[compiler,'test',package,'--bunko-root',snap/'bunko','--format=json'],timeout=1800));assert data['summary']['failed']==0 and data['summary']['passed']>0
# Existing complete regression controllers, with only the explicit native seed
# path/pin replaced by the newly proven current D. Canonical tests stay intact.
shutil.copyfile(snap/'tests/m19ef/user-workers/worker_trace.py',out/'worker_trace.py')
for helper in ('bulk_overlay.py','bulk_trace.py'):shutil.copyfile(snap/'tests/m19ef/old-mark-faults'/helper,out/helper)
for phase,original,destination in [('E3','tests/m19ef/user-workers/run.py','E3-workers-'+out.name),('E4','tests/m19ef/old-mark-faults/run.py','E4-faults-'+out.name)]:
 source=(snap/original).read_text();source=source.replace('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio',str(compiler)).replace('be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79',sha(compiler));controller=out/(phase+'-controller-current-D.py');controller.write_text(source);run(phase+'-complete-regression',[sys.executable,controller,r/'build/m19ef-evidence'/destination],timeout=7200,cwd=r)
# Rebuild and run both observer inputs with the CURRENT actual D generation.
snapshot_tools=out/'snapshot-tools';snapshot_tools.mkdir()
shutil.copyfile(snap/'tests/m19ef/e6/snapshot/overlay.py',snapshot_tools/'overlay.py')
source=(snap/'tests/m19ef/e6/snapshot/run.py').read_text().replace('build/m19ef-evidence/E6-final-20261031-01/mio-B',str(compiler)).replace('f09e8d7188784000491d54471f6339f57e6e55b2d6b67d8d5d86430c06aa4973',sha(compiler))
controller=snapshot_tools/'run.py';controller.write_text(source)
for mode in ('known','unknown'):
 args=[sys.executable,controller,r/'build/m19ef-evidence'/('E6-snapshot-'+out.name+'-'+mode)]
 if mode=='unknown':args.append('unknown')
 run('snapshot-current-D-'+mode,args,timeout=3600,cwd=r)
# Full current-source E5 v2.1.1, using the actual D for every native compiler.
# Only the audit driver's explicit native seed path/pin and child script paths
# differ. Production GC/test case Mio sources remain the frozen final sources.
gc_tools=out/'sweep-tools';gc_tools.mkdir()
for original in (snap/'tests/m19ef/old-sweep-v2').glob('*.py'):
 source=original.read_text().replace('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio',str(compiler)).replace('be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79',sha(compiler))
 for child in ('run.py','ungated_matrix.py'):
  source=source.replace("'tests/m19ef/old-sweep-v2/"+child+"'",repr(str(gc_tools/child)))
 (gc_tools/original.name).write_text(source)
run('E5-complete-current-D-matrix',[sys.executable,gc_tools/'matrix.py',r/'build/m19ef-evidence'/('E5-'+out.name),'-'],timeout=7200,cwd=r)
assert all(sha(r/k)==v and sha(snap/k)==v for k,v in manifest.items());save(out/'summary.json',dict(status='PASS_BOOTSTRAP_NATIVE_UNITS_E3_E4_E5_SNAPSHOT_PENDING_G6_REVIEW',source_manifest_sha256=sha(out/'source-manifest.json'),E5='PASS_SCOPED',E6='IN_PROGRESS',M19e='NOT_ACCEPTED'))
