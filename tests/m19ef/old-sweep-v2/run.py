#!/usr/bin/env python3
"""Native Mio lifecycle v2 directed/full runner; preserves every attempt."""
from pathlib import Path
import hashlib,json,shutil,subprocess,os,signal,time,struct,sys,fcntl
from overlay import install
from verify import verify
from verify_fatal import verify_fatal
r=Path.cwd();out=(r/sys.argv[1]).resolve();assert out.is_relative_to(r/'build/m19ef-evidence');out.mkdir();snap=out/'source-snapshot';snap.mkdir()
lock=os.fdopen(os.dup(int(os.environ['MIO_E5_TEST_LOCK_FD'])),'a+') if 'MIO_E5_TEST_LOCK_FD' in os.environ else (r/'build/.m19ef-native-test.lock').open('a+')
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
repetitions=32 if sys.argv[2]=='full' else 1;assert sys.argv[2] in ('full','directed')
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
def save(p,x):p.write_text(json.dumps(x,ensure_ascii=False,sort_keys=True,indent=2)+'\n')
manifest={}
for folder in ('bunko','selfhost','tests/m19ef','specs/002-m19ef-portable-pure-mio'):
 for p in sorted((r/folder).rglob('*')):
  if p.is_file() and p.suffix in ('.mio','.py','.md','.json','.sh','.txt'):
   rel=p.relative_to(r);target=snap/rel;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,target);manifest[str(rel)]=sha(p)
save(out/'canonical-manifest.json',manifest);install(snap)
overlay={str(p.relative_to(snap)):sha(p) for p in sorted(snap.rglob('*')) if p.is_file()};save(out/'source-manifest.json',overlay);digest=sha(out/'source-manifest.json')
save(out/'plan.json',dict(contract='E5-lifecycle-v2.1.1',repetitions=repetitions,source_manifest_sha256=digest,canonical_manifest_sha256=sha(out/'canonical-manifest.json'),test_overlay_changes=[k for k in overlay if overlay[k]!=manifest[k]],full30='FAIL_PERMANENTLY_PRESERVED',E5='IN_PROGRESS',E6='NOT_RUN'))
seed=Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79';rows=[]
def run(label,args,timeout=600,expected=0):
 start=time.monotonic()
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  child=subprocess.Popen(list(map(str,args)),cwd=snap,stdout=so,stderr=se,start_new_session=True);timed=False
  while True:
   got,status,u=os.wait4(child.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:
    timed=True;os.killpg(child.pid,signal.SIGKILL);got,status,u=os.wait4(child.pid,0);break
   time.sleep(.03)
 row=dict(label=label,argv=list(map(str,args)),cwd=str(snap),exit=os.waitstatus_to_exitcode(status),expected_exit=expected,timed_out=timed,wall_seconds=time.monotonic()-start,cpu_seconds=u.ru_utime+u.ru_stime,peak_rss_bytes_darwin=u.ru_maxrss,source_manifest_sha256=digest,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')));rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,row['exit'],flush=True);assert row['exit']==expected and not timed
 return (out/(label+'.stdout')).read_bytes()

run('seed-signature',['codesign','--verify','--strict',seed])
builder=out/'event-builder'
if len(sys.argv)>3:
 prior=r/sys.argv[3];previous=json.loads((prior/'source-manifest.json').read_text());compiler=lambda m:{k:v for k,v in m.items() if k.startswith(('bunko/','selfhost/')) or k=='tests/m19ef/old-sweep/builder.mio'}
 assert compiler(previous)==compiler(overlay);shutil.copyfile(prior/'event-builder',builder);builder.chmod(0o755)
 save(out/'builder-reuse.json',dict(prior=str(prior),builder_sha256=sha(builder),prior_manifest_sha256=sha(prior/'source-manifest.json'),compiler_inputs_identical=True,scope='Native compiler only; user image generated anew and fully reproduced'))
else:run('build',[seed,'build','tests/m19ef/old-sweep/builder.mio','-o',builder,'--bunko-root',snap/'bunko'])
run('builder-signature',['codesign','--verify','--strict',builder]);image=out/'late-native';args=[builder,snap,'tests/m19ef/old-sweep-v2/late.mio',snap/'bunko']
run('generate',args+[image,'268435456','32']);run('signature',['codesign','--verify','--strict',image]);copy=out/'late-repro-native';run('reproduce',args+[copy,'268435456','32']);assert image.read_bytes()==copy.read_bytes() and Path(str(image)+'.unsigned').read_bytes()==Path(str(copy)+'.unsigned').read_bytes()
observations=[]
for repeat in range(repetitions):
 label='late-'+str(repeat);stdout=run(label,[image],90);data=(out/(label+'.stderr')).read_bytes()
 result=verify(stdout,data);save(out/(label+'.events.json'),result);observations.append(result)
fatal_image=out/'fatal-native';fatal_args=[builder,snap,'tests/m19ef/old-sweep-v2/fatal.mio',snap/'bunko']
run('fatal-generate',fatal_args+[fatal_image,'67108864','32']);run('fatal-signature',['codesign','--verify','--strict',fatal_image]);fatal_copy=out/'fatal-repro-native';run('fatal-reproduce',fatal_args+[fatal_copy,'67108864','32']);assert fatal_image.read_bytes()==fatal_copy.read_bytes() and Path(str(fatal_image)+'.unsigned').read_bytes()==Path(str(fatal_copy)+'.unsigned').read_bytes()
for repeat in range(repetitions):
 label='fatal-'+str(repeat);stdout=run(label,[fatal_image],90,expected=1);result=verify_fatal(stdout,(out/(label+'.stderr')).read_bytes());save(out/(label+'.events.json'),result)
run('oracle-negative',[sys.executable,snap/'tests/m19ef/old-sweep-v2/test_verify.py',out],30)
assert all(sha(r/k)==v for k,v in manifest.items()) and all(sha(snap/k)==v for k,v in overlay.items())
save(out/'summary.json',dict(status='PASS_DIRECTED' if repetitions==1 else 'PASS_LIFECYCLE32_REQUIRES_UNGATED_MATRIX',repetitions=repetitions,source_manifest_sha256=digest,canonical_unchanged=True,E5='IN_PROGRESS',E6='NOT_RUN'))
