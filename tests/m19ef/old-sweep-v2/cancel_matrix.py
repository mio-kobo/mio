#!/usr/bin/env python3
"""Fixed32 real partial-Sweep cancellation/reentry/shutdown/OOM Epoch handshakes."""
from pathlib import Path
import argparse,fcntl,hashlib,json,os,shutil,signal,struct,subprocess,time,traceback
from epoch_cancel_overlay import install
from workload_oracle import verify_workload,verify_cancel_gate
p=argparse.ArgumentParser();p.add_argument('output',type=Path);p.add_argument('--compiler',type=Path);p.add_argument('--compiler-sha256');p.add_argument('--directed',action='store_true');p.add_argument('--prior-builder-proof',type=Path);a=p.parse_args();r=Path.cwd();out=a.output.resolve();assert out.is_relative_to(r/'build/m19ef-evidence');out.mkdir();sha=lambda q:hashlib.sha256(q.read_bytes()).hexdigest();compiler=(a.compiler or Path(os.environ.get('MIO_NATIVE_COMPILER','/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio'))).resolve();pin=a.compiler_sha256 or os.environ.get('MIO_NATIVE_COMPILER_SHA256','be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79');assert sha(compiler)==pin
lock=os.fdopen(os.dup(int(os.environ['MIO_E5_TEST_LOCK_FD'])),'a+') if 'MIO_E5_TEST_LOCK_FD' in os.environ else (r/'build/.m19ef-native-test.lock').open('a+');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
snap=out/'source-snapshot';snap.mkdir();manifest={};rows=[];observations=[]
def save(n,x):(out/n).write_text(json.dumps(x,sort_keys=True,indent=2)+'\n')
for top in ('bunko','selfhost','tests/m19ef'):
 for q in (r/top).rglob('*.mio'):
  rel=q.relative_to(r);d=snap/rel;d.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(q,d);manifest[str(rel)]=sha(q)
save('canonical-manifest.json',manifest);install(snap);overlay={str(q.relative_to(snap)):sha(q) for q in snap.rglob('*.mio')};save('source-manifest.json',overlay)
repetitions=1 if a.directed else 32
save('plan.json',dict(contract='E5_EPOCH_CANCEL_V3',repetitions=repetitions,retries=0,compiler=str(compiler),compiler_sha256=pin,work=['epochs-cancel','reenter','shutdown','oom','oom-bound'],gate='same Major/Sweep Epoch; after real batch and actual unlock; actual partial clear+pending tasks required; real abort/join alone releases gate',concurrent_overlap_proof='separate ungated natural8-Epoch matrix'))
def run(label,argv,expected=0,timeout=300):
 start=time.monotonic();print(label,'START',flush=True);timed=False
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  c=subprocess.Popen(list(map(str,argv)),cwd=r,stdout=so,stderr=se,start_new_session=True)
  while True:
   got,status,u=os.wait4(c.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:timed=True;os.killpg(c.pid,signal.SIGKILL);_,status,u=os.wait4(c.pid,0);break
   time.sleep(.05)
 row=dict(label=label,argv=list(map(str,argv)),exit=os.waitstatus_to_exitcode(status),expected_exit=expected,timed_out=timed,timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=u.ru_utime+u.ru_stime,peak_rss_bytes_darwin=u.ru_maxrss,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')));rows.append(row);save('commands.json',rows);print(label,row['exit'],flush=True)
 if row['exit']!=expected or timed:
  b=(out/(label+'.stderr')).read_bytes();save('failed-packet.json',dict(label=label,stdout=(out/(label+'.stdout')).read_text(),bytes=len(b),words=list(struct.unpack('<'+str(len(b)//8)+'Q',b)) if len(b)%8==0 else []));raise RuntimeError(row)
 assert all(sha(r/k)==v for k,v in manifest.items()) and all(sha(snap/k)==v for k,v in overlay.items());return (out/(label+'.stdout')).read_bytes()
try:
 builder=out/'builder'
 if a.prior_builder_proof:
  prior=a.prior_builder_proof.resolve();assert json.loads((prior/'canonical-manifest.json').read_text())==manifest and json.loads((prior/'source-manifest.json').read_text())==overlay
  assert json.loads((prior/'plan.json').read_text())['compiler_sha256']==pin
  for row in json.loads((prior/'commands.json').read_text()):
   assert row['exit']==row['expected_exit'] and not row['timed_out']
   for stream in ('stdout','stderr'):assert sha(prior/(row['label']+'.'+stream))==row[stream+'_sha256']
  shutil.copy2(prior/'builder',builder);save('retained-builder.json',dict(path=str(prior),sha256=sha(builder),scope='successful exact-source native builder retained after host-only oracle NameError; no previous candidate accepted'))
 else:run('build',[compiler,'build',snap/'tests/m19ef/old-sweep/builder.mio','--bunko-root',snap/'bunko','-o',builder],timeout=1800)
 run('builder-signature',['codesign','--verify','--strict',builder])
 for case,source in [('epochs-cancel','tests/m19ef/old-sweep-v2/epochs-cancel.mio')]+[(c,'tests/m19ef/old-sweep/'+c+'/main.mio') for c in ('reenter','shutdown','oom','oom-bound')]:
  image=out/(case+'-native');common=[builder,snap,source,snap/'bunko'];run(case+'-generate',common+[image,67108864,32]);run(case+'-signature',['codesign','--verify','--strict',image]);copy=out/(case+'-repro');run(case+'-reproduce',common+[copy,67108864,32]);assert image.read_bytes()==copy.read_bytes()
  for i in range(repetitions):
   label=case+'-'+str(i);stdout=run(label,[image],2 if case.startswith('oom') else 0);data=(out/(label+'.stderr')).read_bytes();at=328 if case.startswith('oom') else 256;gate=struct.unpack('<16Q',data[at:at+128]);verify_cancel_gate(case,gate)
   original=data[:at]+data[at+128:];obs=verify_workload('epochs' if case=='epochs-cancel' else case,stdout,original,sha(out/'canonical-manifest.json'),repeat=i);
   if not case.startswith('oom'):assert gate[15]==obs['mutator_words'][42]
   obs.update(case=case,repeat=i,gate_words=list(gate),image_sha256=sha(image));observations.append(obs);save('observations.json',observations)
 save('summary.json',dict(status='PASS_DIRECTED_CANCEL_CASES_NOT_FULL_E5' if a.directed else 'PASS_FIXED32_EPOCH_CANCEL_RECOVERY',repetitions=repetitions,native_programs=len(observations),guaranteed_partial_epoch_cancels=7*repetitions,source_unchanged=True,scope='controlled cancellation only; independent ungated overlap required'))
except BaseException as e:save('FAIL.json',dict(error=repr(e),traceback=traceback.format_exc()));raise
