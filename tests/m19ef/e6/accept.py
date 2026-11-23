#!/usr/bin/env python3
"""Frozen current-source regression, then NEW native ABCD; no automatic G6 waiver.

Usage: python3 tests/m19ef/e6/accept.py NEW_OUTPUT COMPLETE_SAME_SOURCE_E3
All native heavy work is serial. A failed candidate ends; repair and use a NEW
output. A previous compiler is a build tool only, never a new bootstrap PASS.
"""
from pathlib import Path
import argparse,fcntl,hashlib,json,os,re,shutil,signal,struct,subprocess,sys,time,traceback
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('output',type=Path);parser.add_argument('same_source_e3',type=Path)
parser.add_argument('--compiler',type=Path,required=True);parser.add_argument('--compiler-sha256',required=True)
parser.add_argument('--prior-e2',type=Path)
parser.add_argument('--prior-e4-builder',type=Path)
parser.add_argument('--bulk-builder-proof',type=Path)
parser.add_argument('--historical-e3',type=Path,required=True)
parser.add_argument('--completed-e4-prefix',type=Path)
parser.add_argument('--los-before-builder',type=Path,required=True)
args=parser.parse_args()
r=Path.cwd();out=(r/args.output).resolve();prior=(r/args.same_source_e3).resolve()
assert out.is_relative_to(r/'build/m19ef-evidence');out.mkdir()
lock=os.fdopen(os.dup(int(os.environ['MIO_E5_TEST_LOCK_FD'])),'a+') if 'MIO_E5_TEST_LOCK_FD' in os.environ else (r/'build/.m19ef-native-test.lock').open('a+')
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
def save(name,data):(out/name).write_text(json.dumps(data,sort_keys=True,indent=2)+'\n')
compiler=args.compiler.resolve();pin=args.compiler_sha256;assert len(pin)==64 and sha(compiler)==pin
assert args.los_before_builder.resolve().is_file(),'missing preserved LOS before-builder'
snap=out/'source-snapshot';snap.mkdir();manifest={};rows=[];proofs={}
for folder in ('bunko','selfhost','tests','specs','tools'):
 for source in sorted((r/folder).rglob('*')):
  if source.is_file() and source.suffix in ('.mio','.py','.sh','.json','.md','.txt','.fixture'):
   rel=source.relative_to(r);dest=snap/rel;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(source,dest);manifest[str(rel)]=sha(source)
shutil.copyfile(r/'VERSION',snap/'VERSION');manifest['VERSION']=sha(r/'VERSION')
save('source-manifest.json',manifest)
save('plan.json',dict(status='FROZEN_RUNNING',manifest_sha256=sha(out/'source-manifest.json'),regression_compiler=str(compiler),regression_compiler_sha256=pin,predeclared_work=['complete_E3','E2_actual_atomic_TLS_join_16_and_timeout1','complete_E4','SATB_disabled_negative8','snapshot_known4000_unknown4000','E5_fixed32','random_seed_actor_replay8','M19c_policy_28_checks_x8','native_socket_ABI_actual_loopback8','LOS_false_OOM_live_survival_true_OOM_and_failed_unmap','registry_completed_park_soft_deadline_and_hard_ceiling','all_Bunko_selfhost_native_tests','real_CLI_LSP','CLI_check_run_emit_disasm_exec','portable_core_native_image_audit','NEW_seed_A_B_C_D'],retries=0,E6='IN_PROGRESS',M19e='NOT_ACCEPTED'))
save('git.json',dict(head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),branch=subprocess.check_output(['git','branch','--show-current'],text=True).strip(),dirty=subprocess.check_output(['git','status','--porcelain=v1','--untracked-files=all'],text=True)))
def unchanged():assert all(sha(r/k)==v and sha(snap/k)==v for k,v in manifest.items()),'frozen source changed'
def run(label,args,timeout=1800,expected=0):
 args=list(map(str,args));print(label,'START',flush=True);start=time.monotonic()
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  child=subprocess.Popen(args,cwd=r,stdout=so,stderr=se,start_new_session=True,env=dict(os.environ,MIO_E5_TEST_LOCK_FD=str(lock.fileno())),pass_fds=(lock.fileno(),));timed=False
  while True:
   got,status,u=os.wait4(child.pid,os.WNOHANG)
   if got:break
   if time.monotonic()-start>timeout:
    timed=True;os.killpg(child.pid,signal.SIGKILL);_,status,u=os.wait4(child.pid,0);break
   time.sleep(.1)
 row=dict(label=label,argv=args,cwd=str(r),exit=os.waitstatus_to_exitcode(status),expected_exit=expected,timed_out=timed,timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=u.ru_utime+u.ru_stime,peak_rss_bytes_darwin=u.ru_maxrss,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')))
 rows.append(row);save('commands.json',rows);print(label,row['exit'],flush=True);assert row['exit']==expected and not timed,row
 unchanged();return (out/(label+'.stdout')).read_bytes()
tools=out/'controllers';tools.mkdir()
def controller(original,name):
 source=(snap/original).read_text().replace('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio',str(compiler)).replace('be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79',pin)
 target=tools/name;target.write_text(source);return target
try:
 run('compiler-signature',['codesign','--verify','--strict',compiler])
 mio={k:v for k,v in manifest.items() if k.endswith('.mio') and k.startswith(('bunko/','selfhost/','tests/m19ef/'))}
 previous=json.loads((prior/'source-manifest.json').read_text());summary=json.loads((prior/'summary.json').read_text())
 assert previous==mio and summary['status']=='PASS_SCOPED_USER_WORKER_INTEGRATION' and summary['source_unchanged']
 prior_commands=json.loads((prior/'commands.json').read_text())
 assert json.loads((prior/'provenance.json').read_text())['VERSION_fixture_sha256']==manifest['VERSION']
 assert all(x['exit']==x['expected_exit'] and not x['timed_out'] for x in prior_commands)
 assert all(x['seed_sha256']==pin for x in prior_commands)
 # Recheck actual global packets through the unchanged shared strict oracle.
 sys.path.insert(0,str(snap/'tests/m19ef/user-workers'))
 from worker_trace import verify_worker_trace
 for i in range(64):
  assert (prior/('global-snapshot-'+str(i)+'.stdout')).read_bytes()==b'hai\n1\n1\n0\n'
  verify_worker_trace('global-snapshot',(prior/('global-snapshot-'+str(i)+'.stderr')).read_bytes())
 save('same-source-E3.json',dict(status='PASS_EXACT_MIO_INPUTS',path=str(prior),source_manifest_sha256=sha(prior/'source-manifest.json'),summary_sha256=sha(prior/'summary.json'),commands_sha256=sha(prior/'commands.json'),scope='all compiled Mio inputs exactly match final freeze; full original workload already executed after repair'))
 proofs['E3']=str(prior)
 if args.prior_e2:
  old_e2=args.prior_e2.resolve();old_manifest=json.loads((old_e2/'source-manifest.json').read_text())
  assert {k:v for k,v in old_manifest.items() if k in mio}==mio and old_manifest['VERSION']==manifest['VERSION']
  old_plan=json.loads((old_e2/'plan.json').read_text());assert old_plan['regression_compiler_sha256']==pin
  old_rows=[x for x in json.loads((old_e2/'commands.json').read_text()) if x['label'].startswith('E2-')]
  labels={'E2-'+s for s in ('builder','builder-signature','generate','signature','reproduce','timeout-generate','timeout-signature','timeout-native')}|{'E2-native-'+str(i) for i in range(16)}
  assert {x['label'] for x in old_rows}==labels
  for row in old_rows:
   assert row['exit']==row['expected_exit']==(71 if row['label']=='E2-timeout-native' else 0) and not row['timed_out']
   for stream in ('stdout','stderr'):assert sha(old_e2/(row['label']+'.'+stream))==row[stream+'_sha256']
  for i in range(16):assert (old_e2/('E2-native-'+str(i)+'.stdout')).read_bytes()==b'thread-smoke-pass\n'
  assert (old_e2/'thread-primitive-native').read_bytes()==(old_e2/'thread-primitive-reproduced-native').read_bytes()
  run('E2-retained-image-signature',['codesign','--verify','--strict',old_e2/'thread-primitive-native'])
  save('same-source-E2.json',dict(status='PASS_RETAINED_EXACT_COMPILED_INPUTS_AND_LOGS',path=str(old_e2),source_manifest_sha256=sha(old_e2/'source-manifest.json'),commands_sha256=sha(old_e2/'commands.json'),scope='16 real positives and native timeout71 already executed; no rerun or whole failed-candidate acceptance'))
  proofs['E2_current_native_primitives']=str(old_e2/'commands.json')
 else:
  primitive_builder=out/'thread-primitive-builder'
  run('E2-builder',[compiler,'build',snap/'tests/m19ef/thread-smoke/main.mio','--bunko-root',snap/'bunko','-o',primitive_builder])
  run('E2-builder-signature',['codesign','--verify','--strict',primitive_builder])
  primitive=out/'thread-primitive-native';run('E2-generate',[primitive_builder,primitive]);run('E2-signature',['codesign','--verify','--strict',primitive])
  primitive_copy=out/'thread-primitive-reproduced-native';run('E2-reproduce',[primitive_builder,primitive_copy]);assert primitive.read_bytes()==primitive_copy.read_bytes()
  for repeat in range(16):assert run('E2-native-'+str(repeat),[primitive],30)==b'thread-smoke-pass\n'
  timeout_image=out/'thread-primitive-timeout-native';run('E2-timeout-generate',[primitive_builder,timeout_image,'timeout']);run('E2-timeout-signature',['codesign','--verify','--strict',timeout_image]);run('E2-timeout-native',[timeout_image],10,expected=71)
  proofs['E2_current_native_primitives']=str(out/'commands.json')
 e4=r/'build/m19ef-evidence'/('E4-faults-'+out.name)
 for helper in ('bulk_overlay.py','bulk_trace.py'):shutil.copyfile(snap/'tests/m19ef/old-mark-faults'/helper,tools/helper)
 e4_command=[sys.executable,controller('tests/m19ef/old-mark-faults/run.py','E4.py'),e4]
 e4_command += [args.prior_e4_builder.resolve() if args.prior_e4_builder else '-',args.bulk_builder_proof.resolve() if args.bulk_builder_proof else '-',prior,args.historical_e3.resolve()]
 if args.completed_e4_prefix:e4_command.append(args.completed_e4_prefix.resolve())
 run('E4-full',e4_command,7200);proofs['E4']=str(e4)
 negative=r/'build/m19ef-evidence'/('E4-negative-'+out.name)
 run('E4-SATB-negative',[sys.executable,controller('tests/m19ef/old-mark-faults/satb_counterexample.py','SATB-negative.py'),negative,e4],1800);proofs['SATB_negative']=str(negative)
 snapshot_tools=tools/'snapshot';snapshot_tools.mkdir();shutil.copyfile(snap/'tests/m19ef/e6/snapshot/overlay.py',snapshot_tools/'overlay.py')
 source=(snap/'tests/m19ef/e6/snapshot/run.py').read_text().replace('build/m19ef-evidence/E6-final-20261031-01/mio-B',str(compiler)).replace('f09e8d7188784000491d54471f6339f57e6e55b2d6b67d8d5d86430c06aa4973',pin)
 source=source.replace('preliminary B compiler is actually source-bootstrapped from GF4, not an E6 final fixed-point claim','explicit native regression compiler; final NEW ABCD equality is separately required')
 (snapshot_tools/'run.py').write_text(source)
 for mode in ('known','unknown'):
  dest=r/'build/m19ef-evidence'/('E6-snapshot-'+out.name+'-'+mode)
  command=[sys.executable,snapshot_tools/'run.py',dest]+(['unknown'] if mode=='unknown' else [])
  run('snapshot-'+mode,command,1800);proofs['snapshot_'+mode]=str(dest)
 sweep=tools/'sweep';sweep.mkdir()
 for original in (snap/'tests/m19ef/old-sweep-v2').glob('*.py'):
  source=original.read_text().replace('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio',str(compiler)).replace('be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79',pin)
  for child in ('run.py','ungated_matrix.py','cancel_matrix.py','test_cancel_oracle.py'):source=source.replace("'tests/m19ef/old-sweep-v2/"+child+"'",repr(str(sweep/child)))
  (sweep/original.name).write_text(source)
 for pattern in ('*.fixture','old-unscheduled-epochs.json'):
  for original in (snap/'tests/m19ef/old-sweep-v2').glob(pattern):shutil.copyfile(original,sweep/original.name)
 e5=r/'build/m19ef-evidence'/('E5-'+out.name)
 run('E5-fixed32',[sys.executable,sweep/'matrix.py',e5,'-'],7200);proofs['E5']=str(e5)
 seeded=r/'build/m19ef-evidence'/('E6-seeded-'+out.name)
 run('fixed8-seeded-replay',[sys.executable,snap/'tests/m19ef/e6/seeded.py',seeded,e5/'ungated'],1800);proofs['seeded_replay']=str(seeded)
 memory=r/'build/m19ef-evidence'/('E6-memory-'+out.name)
 run('M19c-policy',[sys.executable,snap/'tests/m19ef/e6/memory/run.py',memory,compiler,pin],1800);proofs['M19c_policy']=str(memory)
 socket_proof=r/'build/m19ef-evidence'/('E6-socket-'+out.name)
 run('native-socket-ABI',[sys.executable,snap/'tests/m19ef/e6/socket/run.py',socket_proof,'--compiler',compiler,'--compiler-sha256',pin],1800);proofs['native_socket_ABI']=str(socket_proof)
 registry_proof=r/'build/m19ef-evidence'/('E6-registry-park-'+out.name)
 run('registry-completed-park-deadline',[sys.executable,snap/'tests/m19ef/e6/registry-park/run.py',registry_proof,'--compiler',compiler,'--compiler-sha256',pin],1200);proofs['registry_completed_park_deadline']=str(registry_proof)
 los_proof=r/'build/m19ef-evidence'/('E6-los-pressure-'+out.name)
 run('LOS-allocation-pressure',[sys.executable,snap/'tests/m19ef/e6/los-pressure/run.py',los_proof,'--compiler',compiler,'--compiler-sha256',pin,'--before-builder',args.los_before_builder.resolve()],1800);proofs['LOS_allocation_pressure']=str(los_proof)
 for package in ('bunko/...','selfhost/...'):
  data=json.loads(run('native-tests-'+package.replace('/','-'),[compiler,'test',package,'--bunko-root',snap/'bunko','--format=json'],7200));assert data['summary']['failed']==0 and data['summary']['passed']>0
 run('real-CLI-LSP',[sys.executable,snap/'tests/m19ef/e6/lsp_session.py',compiler,out/'real-LSP',snap/'bunko'],180)
 smoke=snap/'tests/m19ef/e6/smoke.mio';expected=b'2096128\n499500\n3.25\n'
 run('CLI-check',[compiler,'check',smoke,'--bunko-root',snap/'bunko'])
 assert run('CLI-run',[compiler,'run',smoke,'--bunko-root',snap/'bunko'])==expected
 mochi=out/'smoke.mochi';run('CLI-emit-mochi',[compiler,'build',smoke,'--emit=mochi','--bunko-root',snap/'bunko','-o',mochi]);assert mochi.stat().st_size>0
 assert len(run('CLI-disasm',[compiler,'disasm',mochi]))>20
 assert run('CLI-exec',[compiler,'exec',mochi])==expected
 core_audit={}
 for path in sorted((snap/'bunko/kiso/runtime/gc').glob('*.mio')):
  if path.name.endswith('_test.mio'):continue
  assert not re.search(r'yobu\s+"kiso/(?:aot/|runtime/(?:cpu|os)/)',path.read_text()),path
  core_audit[str(path.relative_to(snap))]=sha(path)
 native=compiler.read_bytes();assert native[:4]==bytes.fromhex('cffaedfe') and struct.unpack_from('<I',native,4)[0]==0x100000c
 assert all(marker not in native for marker in (b'__gopclntab',b'runtime.goexit',b'runtime.morestack'))
 save('portability-audit.json',dict(status='PASS_STATIC_GC_CORE_AND_ACTUAL_NATIVE_IMAGE',core_sources=core_audit,native_target='Darwin ARM64 Mach-O',other_targets='NOT_IMPLEMENTED; target native tests verify fail-closed descriptors',production_compiler='Mio only',default_STW_compatibility='retained; concurrent Old API is explicit internal native bridge'))
 # A fresh, independently captured ABCD on THIS frozen canonical tree. Earlier
 # ABCD results are never substituted. The tested regression compiler must be
 # byte-identical to this actual new D, or the candidate fails closed.
 bootstrap=r/'build/m19ef-evidence'/('ABCD-'+out.name)
 run('NEW-ABCD',[r/'tests/m19ef/bootstrap-abcd.sh','build','--out',bootstrap],18000)
 assert sha(bootstrap/'mio-D')==pin,'new D differs: rerun regressions with new compiler'
 assert json.loads((bootstrap/'source-manifest.json').read_text())==manifest
 proofs['ABCD']=str(bootstrap)
 unchanged();assert sha(prior/'summary.json')==json.loads((out/'same-source-E3.json').read_text())['summary_sha256']
 save('summary.json',dict(status='PASS_CURRENT_SOURCE_NATIVE_SUITES_PENDING_G6_REVIEW',evidence=proofs,manifest_sha256=sha(out/'source-manifest.json'),new_D_sha256=pin,source_unchanged=True,E6='IN_PROGRESS_PENDING_GATE_REVIEW',M19e='NOT_ACCEPTED'))
 save('artifact-manifest.json',{str(p.relative_to(out)):sha(p) for p in sorted(out.rglob('*')) if p.is_file()})
except BaseException as exc:
 save('FAIL.json',dict(status='FAIL_REPAIR_REQUIRED',error=repr(exc),traceback=traceback.format_exc(),completed_evidence=proofs));raise
