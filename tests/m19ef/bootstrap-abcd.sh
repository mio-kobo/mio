#!/usr/bin/env bash
# Shell entry point; Python only orchestrates/audits. All compilation is native Mio.
set -euo pipefail
MIO_ABCD_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)"
exec python3 - "$MIO_ABCD_ROOT" "${BASH_SOURCE[0]}" "$@" <<'PY'
from pathlib import Path
import argparse,fcntl,hashlib,json,os,platform,re,shutil,signal,subprocess,sys,time,traceback
root=Path(sys.argv[1]);script=Path(sys.argv[2]).resolve()
sys.path.insert(0,str(root/'scripts/distribution'))
sys.path.insert(0,str(root/'scripts/ci'))
import seed_evidence,seed_source
_active_seed=seed_evidence.active_seed_identity()
parser=argparse.ArgumentParser(description='Native Seed→A→B→C→D; exact signed/unsigned B=C=D, signatures and per-generation smoke. This is not full E6 acceptance.')
sub=parser.add_subparsers(dest='mode',required=True)
build=sub.add_parser('build',help='freeze sources, compile all generations, then verify')
build.add_argument('--out',required=True,type=Path,help='NEW output directory under build/m19ef-evidence')
build.add_argument('--seed',type=Path,default=seed_source.default_seed_path(_active_seed))
build.add_argument('--seed-sha256',default=_active_seed.sha256)
build.add_argument('--timeout',type=int,default=1800,help='seconds per native command; default 1800')
verify=sub.add_parser('verify',help='independently recheck saved hashes, frozen sources, signatures and complete fixed point')
verify.add_argument('--out',required=True,type=Path)
args=parser.parse_args(sys.argv[3:])
def sha(path):
 h=hashlib.sha256()
 with path.open('rb') as f:
  for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
 return h.hexdigest()
def save(path,value):path.write_text(json.dumps(value,ensure_ascii=False,sort_keys=True,indent=2)+'\n')
def require(ok,message):
 if not ok:raise RuntimeError(message)
out=args.out.expanduser().resolve()
require(out.is_relative_to(root/'build/m19ef-evidence'),'output must be under this worktree/build/m19ef-evidence')
def verify_result():
 result=json.loads((out/'result.json').read_text())
 require(result['status']=='PASS_NATIVE_ABCD_FIXED_POINT','result is not PASS')
 artifacts=json.loads((out/'artifact-manifest.json').read_text())
 required={'result.json','commands.json','provenance.json','source-manifest.json','controller.sh'}
 for generation in 'ABCD':required.add('mio-'+generation)
 for generation in 'BCD':required.update({'mio-'+generation+'.unsigned','mio-'+generation+'.resigned'})
 require(required<=set(artifacts),'incomplete artifact manifest')
 for name,digest in artifacts.items():
  path=(out/name).resolve();require(path.is_relative_to(out),'invalid artifact path')
  require(sha(path)==digest,'artifact hash mismatch: '+name)
 manifest=json.loads((out/'source-manifest.json').read_text())
 require(result['source_manifest_sha256']==sha(out/'source-manifest.json'),'result/source manifest hash differs')
 require(result['signed_sha256']==sha(out/'mio-D') and result['unsigned_sha256']==sha(out/'mio-D.unsigned'),'result/image hash differs')
 for name,digest in manifest.items():
  path=(out/'source-snapshot'/name).resolve()
  require(path.is_relative_to(out/'source-snapshot'),'invalid source path')
  require(sha(path)==digest,'frozen source hash mismatch: '+name)
 commands=json.loads((out/'commands.json').read_text())
 expected=set()
 for generation in 'ABCD':
  expected.update(generation+'-'+suffix for suffix in ('build','signature','version','smoke-build','smoke-signature','smoke'))
 for generation in 'BCD':
  expected.update(generation+'-'+suffix for suffix in ('unsigned-builder-build','unsigned-builder-signature','unsigned-build','resigned-signature'))
 require({row['label'] for row in commands}==expected,'missing or unexpected native commands')
 for row in commands:
  require(row['exit']==0 and not row['timed_out'],'failed command: '+row['label'])
  for stream in ('stdout','stderr'):
   require(sha(out/(row['label']+'.'+stream))==row[stream+'_sha256'],'log hash mismatch')
 for extension in ('','.unsigned','.resigned'):
  hashes=[sha(out/('mio-'+generation+extension)) for generation in 'BCD']
  require(len(set(hashes))==1,'B/C/D differ: '+extension)
 for generation in 'BCD':
  require(sha(out/('mio-'+generation))==sha(out/('mio-'+generation+'.resigned')),'raw-pipeline signed bytes do not match actual '+generation)
 for generation in 'ABCD':
  require((out/(generation+'-smoke.stdout')).read_bytes()==b'2096128\n499500\n3.25\n','smoke output differs: '+generation)
  for image in ('mio-'+generation,generation+'-smoke'):
   subprocess.run(['codesign','--verify','--strict',str(out/image)],check=True)
 print('PASS_NATIVE_ABCD_FIXED_POINT',flush=True)
 print('signed B=C=D SHA256:',sha(out/'mio-D'))
 print('unsigned B=C=D SHA256:',sha(out/'mio-D.unsigned'))
 print('evidence:',out)
if args.mode=='verify':
 verify_result();sys.exit(0)
require(sys.platform=='darwin' and platform.machine()=='arm64','this script requires native Darwin ARM64')
require(args.timeout>0,'timeout must be positive')
seed=args.seed.expanduser().resolve()
require(re.fullmatch('[0-9a-f]{64}',args.seed_sha256) is not None,'invalid seed SHA256')
require(sha(seed)==args.seed_sha256,'seed SHA256 mismatch')
if args.seed_sha256==_active_seed.sha256:
 seed_source.verify_seed_file(seed,_active_seed,check_arch=True,check_signature=True)
lock=os.fdopen(os.dup(int(os.environ['MIO_E5_TEST_LOCK_FD'])),'a+') if 'MIO_E5_TEST_LOCK_FD' in os.environ else (root/'build/.m19ef-native-test.lock').open('a+')
try:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
except BlockingIOError:raise SystemExit('Another native acceptance run holds the test lock. No build started.')
out.mkdir(parents=True,exist_ok=False)
shutil.copyfile(script,out/'controller.sh')
rows=[]
try:
 subprocess.run(['codesign','--verify','--strict',str(seed)],check=True)
 snapshot=out/'source-snapshot';snapshot.mkdir();manifest={}
 for folder in ('bunko','selfhost','tests','specs','tools'):
  for source in sorted((root/folder).rglob('*')):
   if source.is_file() and source.suffix in ('.mio','.py','.md','.json','.sh','.txt','.fixture'):
    name=source.relative_to(root);dest=snapshot/name;dest.parent.mkdir(parents=True,exist_ok=True)
    shutil.copyfile(source,dest);manifest[str(name)]=sha(source)
 shutil.copyfile(root/'VERSION',snapshot/'VERSION');manifest['VERSION']=sha(root/'VERSION')
 save(out/'source-manifest.json',manifest)
 save(out/'provenance.json',dict(seed=str(seed),seed_sha256=sha(seed),source_manifest_sha256=sha(out/'source-manifest.json'),head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),branch=subprocess.check_output(['git','branch','--show-current'],cwd=root,text=True).strip(),dirty=subprocess.check_output(['git','status','--porcelain=v1','--untracked-files=all'],cwd=root,text=True),host=platform.platform(),production_compilation='native Mio only',scope='compiler fixed point and smoke only; not E3/E4/E5/E6 acceptance',timeout_seconds=args.timeout))
 # Require exact embedded resources, without modifying the canonical source tree.
 bundle=(snapshot/'selfhost/resources/bunko_data.mio').read_text();entries={}
 pattern=r'path: ("(?:[^"\\]|\\.)*"), version: "[^"\n]+", length: (\d+), sha256: "([0-9a-f]+)", source: ("(?:[^"\\]|\\.)*")'
 for name,length,digest,body in re.findall(pattern,bundle):
  name=json.loads(name);actual=(snapshot/'bunko'/name).read_bytes()
  require(json.loads(body).encode()==actual and len(actual)==int(length) and hashlib.sha256(actual).hexdigest()==digest,'stale embedded module: '+name)
  require(name not in entries,'duplicate embedded module: '+name);entries[name]=digest
 expected={str(p.relative_to(snapshot/'bunko')):sha(p) for p in (snapshot/'bunko/kiso').rglob('*.mio') if not p.name.endswith('_test.mio')}
 require(entries==expected,'embedded Bunko module set differs; refresh resources before freezing')
 def run(label,argv):
  argv=list(map(str,argv));print(label+' START',flush=True);start=time.monotonic();timed=False
  with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
   child=subprocess.Popen(argv,cwd=snapshot,stdout=so,stderr=se,start_new_session=True)
   try:
    while True:
     got,status,usage=os.wait4(child.pid,os.WNOHANG)
     if got:break
     if time.monotonic()-start>args.timeout:
      timed=True;os.killpg(child.pid,signal.SIGKILL);_,status,usage=os.wait4(child.pid,0);break
     time.sleep(.1)
   except BaseException:
    try:os.killpg(child.pid,signal.SIGKILL)
    except ProcessLookupError:pass
    os.wait4(child.pid,0);raise
  row=dict(label=label,argv=argv,cwd=str(snapshot),exit=os.waitstatus_to_exitcode(status),timed_out=timed,wall_seconds=time.monotonic()-start,cpu_seconds=usage.ru_utime+usage.ru_stime,peak_rss_bytes_darwin=usage.ru_maxrss,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')))
  rows.append(row);save(out/'commands.json',rows);print(label+' EXIT '+str(row['exit']),flush=True)
  require(row['exit']==0 and not timed,'command failed: '+label)
 compiler=seed
 for generation in 'ABCD':
  image=out/('mio-'+generation)
  run(generation+'-build',[compiler,'build','selfhost/cli','--bunko-root',snapshot/'bunko','-o',image])
  run(generation+'-signature',['codesign','--verify','--strict',image]);run(generation+'-version',[image,'version'])
  smoke=out/(generation+'-smoke')
  run(generation+'-smoke-build',[image,'build','tests/m19ef/e6/smoke.mio','--bunko-root',snapshot/'bunko','-o',smoke])
  run(generation+'-smoke-signature',['codesign','--verify','--strict',smoke]);run(generation+'-smoke',[smoke])
  require((out/(generation+'-smoke.stdout')).read_bytes()==b'2096128\n499500\n3.25\n','smoke mismatch')
  compiler=image
 require(len({sha(out/('mio-'+g)) for g in 'BCD'})==1,'signed B/C/D fixed point mismatch')
 for producer,generation in zip('ABC','BCD'):
  helper=out/('unsigned-builder-'+generation)
  run(generation+'-unsigned-builder-build',[out/('mio-'+producer),'build','tests/m19ef/e6/unsigned_builder.mio','--bunko-root',snapshot/'bunko','-o',helper])
  run(generation+'-unsigned-builder-signature',['codesign','--verify','--strict',helper])
  run(generation+'-unsigned-build',[helper,snapshot,'selfhost/cli',snapshot/'bunko',out/('mio-'+generation+'.unsigned'),out/('mio-'+generation+'.resigned')])
  run(generation+'-resigned-signature',['codesign','--verify','--strict',out/('mio-'+generation+'.resigned')])
  require(sha(out/('mio-'+generation))==sha(out/('mio-'+generation+'.resigned')),'actual generation/raw-pipeline mismatch: '+generation)
 require(len({sha(out/('mio-'+g+'.unsigned')) for g in 'BCD'})==1,'unsigned B/C/D fixed point mismatch')
 require(all(sha(root/name)==digest and sha(snapshot/name)==digest for name,digest in manifest.items()),'source changed during acceptance')
 save(out/'result.json',dict(status='PASS_NATIVE_ABCD_FIXED_POINT',signed_sha256=sha(out/'mio-D'),unsigned_sha256=sha(out/'mio-D.unsigned'),source_manifest_sha256=sha(out/'source-manifest.json'),embedded_modules=len(entries),scope='full compiler source bootstrap and smoke; not full E6/GC acceptance'))
 save(out/'artifact-manifest.json',{str(p.relative_to(out)):sha(p) for p in sorted(out.rglob('*')) if p.is_file()})
 verify_result()
except BaseException as exc:
 save(out/'FAIL.json',dict(status='FAIL',error=repr(exc),traceback=traceback.format_exc()))
 raise
PY
