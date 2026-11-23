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
# Reuse an exact native compiler image whose full compiler/GC/adapter source
# inputs match this new snapshot. Only the user Cell fixture changed.
prior=root/'build/m19ef-evidence/E4-satb-20261030-06';old_manifest=json.loads((prior/'source-manifest.json').read_text());assert all(old_manifest.get(p)==h for p,h in manifest.items() if p.startswith(('bunko/','selfhost/')))
builder=prior/'satb-builder';run('builder-signature',['codesign','--verify','--strict',builder]);(out/'builder-provenance.json').write_text(json.dumps(dict(path=str(builder),sha256=sha(builder),compiler_source_manifest=str(prior/'source-manifest.json'),compiler_inputs_identical=True),indent=2)+'\n')
image=out/'cell-native';args=[builder,snap,'tests/m19ef/old-mark-satb/cell/main.mio',snap/'bunko',image];run('cell-build',args);run('cell-signature',['codesign','--verify','--strict',image]);args[-1]=out/'cell-repro-native';run('cell-repro',args);assert image.read_bytes()==args[-1].read_bytes()
for n in range(32):
 label='cell-'+str(n);stdout=run(label,[image],timeout=60);assert stdout==b'0\n5\n4\n7\n1\n4\n0\n',stdout;data=(out/(label+'.stderr')).read_bytes();assert len(data)==552;old=struct.unpack('<24Q',data[:192]);assert old[1:6]==(4,0,7,7,1) and old[12:17]==(1,1,0,1,1) and old[23]==0
assert all(sha(root/p)==h and sha(snap/p)==h for p,h in manifest.items());(out/'summary.json').write_text(json.dumps(dict(status='PASS_SCOPED_CELL_PREWRITE',runs=32,E4='IN_PROGRESS',M19e='NOT_ACCEPTED',source_manifest_sha256=digest,compiler_inputs_identical_to=str(prior)),indent=2)+'\n')
