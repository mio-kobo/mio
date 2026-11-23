#!/usr/bin/env python3
"""Sweep development controller; never grants E5 PASS."""
import hashlib,json,os,pathlib,platform,shutil,signal,struct,subprocess,sys,time
repetitions=32;development=False
if len(sys.argv)==4:
 assert sys.argv[3] in ('dev','dev-tail','dev-late');repetitions=1;development=True
assert 2<=len(sys.argv)<=4
root=pathlib.Path.cwd().resolve();out=(root/sys.argv[1]).resolve();assert out.is_relative_to(root/'build/m19ef-evidence');out.mkdir();snap=out/'source-snapshot';snap.mkdir();sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();manifest={}
for folder in ['bunko','selfhost','tests/m19ef']:
 for p in sorted((root/folder).rglob('*.mio')):
  rel=p.relative_to(root);d=snap/rel;d.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(p,d);manifest[str(rel)]=sha(p)
(out/'source-manifest.json').write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');digest=sha(out/'source-manifest.json');shutil.copyfile(pathlib.Path(__file__),out/'controller.py');seed=pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio');assert sha(seed)=='be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
(out/'provenance.json').write_text(json.dumps(dict(source_manifest_sha256=digest,seed_sha256=sha(seed),head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),branch=subprocess.check_output(['git','branch','--show-current'],text=True).strip(),host=platform.platform(),target='darwin-arm64',scope='initial full concurrent Sweep development only; not E5 gate acceptance'),indent=2)+'\n');rows=[]
stop_signal=0
def request_stop(number,frame):
 global stop_signal
 stop_signal=number
signal.signal(signal.SIGINT,request_stop)
signal.signal(signal.SIGTERM,request_stop)
def run(label,args,expected=0,timeout=600):
 if stop_signal:raise RuntimeError('interrupted before next command; evidence retained')
 cwd=root if len(args)==1 and pathlib.Path(args[0]).parent==out else snap
 start=time.monotonic()
 with (out/(label+'.stdout')).open('xb') as so,(out/(label+'.stderr')).open('xb') as se:
  p=subprocess.Popen(list(map(str,args)),cwd=cwd,stdout=so,stderr=se,start_new_session=True);timed=False
  while True:
   got,status,u=os.wait4(p.pid,os.WNOHANG)
   if got:break
   if stop_signal:
    os.killpg(p.pid,signal.SIGTERM)
    time.sleep(.2)
    try:os.killpg(p.pid,signal.SIGKILL)
    except ProcessLookupError:pass
    got,status,u=os.wait4(p.pid,0);break
   if time.monotonic()-start>timeout:
    timed=True
    with (out/(label+'.processes')).open('xb') as diag:subprocess.run(['ps','-Ao','pid,ppid,%cpu,rss,etime,command'],stdout=diag)
    os.killpg(p.pid,signal.SIGKILL);got,status,u=os.wait4(p.pid,0);break
   time.sleep(.03)
 row=dict(label=label,argv=list(map(str,args)),cwd=str(cwd),exit=os.waitstatus_to_exitcode(status),expected_exit=expected,timed_out=timed,interrupted=bool(stop_signal),timeout_seconds=timeout,wall_seconds=time.monotonic()-start,cpu_seconds=u.ru_utime+u.ru_stime,peak_rss_bytes_darwin=u.ru_maxrss,source_manifest_sha256=digest,stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')));rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,row['exit'],flush=True);assert row['exit']==expected and not timed and not stop_signal
 return (out/(label+'.stdout')).read_bytes()
run('seed-signature',['codesign','--verify','--strict',seed])
# Resource-negative image uses byte-identical private production factories.
# No fake Roots, live heap or reclaim; positive GC proof is ordinary programs.
resource=out/'resource-source';resource.mkdir();resource_inputs={}
for p in sorted((snap/'bunko/kiso/runtime/gc').glob('*.mio')):
 if p.name.endswith('_test.mio'):continue
 shutil.copyfile(p,resource/p.name);assert sha(p)==sha(resource/p.name);resource_inputs[p.name]=dict(original=str(p.relative_to(snap)),sha256=sha(p))
audit=snap/'tests/m19ef/old-sweep-faults/audit.mio';shutil.copyfile(audit,resource/'audit.mio');resource_inputs['audit.mio']=dict(original=str(audit.relative_to(snap)),sha256=sha(audit));(out/'resource-source-manifest.json').write_text(json.dumps(resource_inputs,indent=2)+'\n')
resource_builder=out/'resource-builder';run('resource-build',[seed,'build',resource,'-o',resource_builder,'--bunko-root',snap/'bunko']);run('resource-builder-signature',['codesign','--verify','--strict',resource_builder])
for mode in ['stack','join','clock','cancel-lock','version','epoch','duplicate']:
 image=out/('resource-'+mode+'-native');run('resource-'+mode+'-generate',[resource_builder,image,mode]);run('resource-'+mode+'-signature',['codesign','--verify','--strict',image]);run('resource-'+mode+'-repro',[resource_builder,out/('resource-'+mode+'-repro-native'),mode]);assert image.read_bytes()==(out/('resource-'+mode+'-repro-native')).read_bytes()
 for repeat in range(repetitions):
  v=struct.unpack('<15Q',run('resource-'+mode+'-'+str(repeat),[image],timeout=30))
  if mode=='stack':assert v==(4,22,0,0,0,0,0,0,0,0,1,0,0,0,0),v
  elif mode=='join':assert v==(4,11,1,1,1,1,1,1,2,0,1,0,0,0,0),v
  elif mode=='clock':assert v==(4,4,1,0,1,0,1,1,0,0,1,0,0,0,1),v
  elif mode=='cancel-lock':assert v[:13]==(4,0,1,1,1,1,1,1,0,0,1,0,2) and v[13]>0 and v[14]==0,v
  elif mode in ('version','epoch'):assert v==(4,4,0,0,0,0,0,0,0,0,0,0,0,0,0),v
  else:assert v==(4,4,1,1,1,1,1,1,0,0,1,0,0,0,0),v
builder=out/'sweep-builder' 
if len(sys.argv)>=3 and sys.argv[2]!='-':
 prior=(root/sys.argv[2]).resolve();previous=json.loads((prior/'source-manifest.json').read_text());compiler=lambda m:{p:h for p,h in m.items() if p.startswith(('bunko/','selfhost/')) or p=='tests/m19ef/old-sweep/builder.mio'};assert compiler(previous)==compiler(manifest);original=next(x for x in json.loads((prior/'commands.json').read_text()) if x['label']=='build');assert original['exit']==0 and not original['timed_out'];shutil.copyfile(prior/'sweep-builder',builder);builder.chmod(0o755);(out/'builder-reuse.json').write_text(json.dumps(dict(scope='identical compiler/builder sources only; user image freshly compiled, not a bootstrap',prior=str(prior),builder_sha256=sha(builder),original_build=original),indent=2)+'\n')
else:run('build',[seed,'build','tests/m19ef/old-sweep/builder.mio','-o',builder,'--bunko-root',snap/'bunko'])
run('builder-signature',['codesign','--verify','--strict',builder])
observations=[]
for case,source,args in [('ordinary','tests/m19ef/old-sweep/ordinary/main.mio',['16777216','32']),('minor','tests/m19ef/old-sweep/minor/main.mio',['67108864','32']),('epochs','tests/m19ef/old-sweep/epochs/main.mio',['67108864','32']),('los','tests/m19ef/old-sweep/los/main.mio',['16777216','32']),('los-fault','tests/m19ef/old-sweep/los/main.mio',['16777216','32','normal','fault-unmap']),('late','tests/m19ef/old-sweep/late/main.mio',['268435456','32']),('reenter','tests/m19ef/old-sweep/reenter/main.mio',['67108864','32']),('shutdown','tests/m19ef/old-sweep/shutdown/main.mio',['67108864','32']),('oom','tests/m19ef/old-sweep/oom/main.mio',['67108864','32']),('oom-bound','tests/m19ef/old-sweep/oom-bound/main.mio',['67108864','32']),('fatal','tests/m19ef/old-sweep/fatal/main.mio',['67108864','32'])]:
 if development and sys.argv[3]=='dev-tail' and case not in ('reenter','shutdown','oom','oom-bound','fatal'):continue
 if development and sys.argv[3]=='dev-late' and case!='late':continue
 image=out/(case+'-native');run(case+'-build',[builder,snap,source,snap/'bunko',image]+args);run(case+'-signature',['codesign','--verify','--strict',image]);run(case+'-repro',[builder,snap,source,snap/'bunko',out/(case+'-repro-native')]+args);assert image.read_bytes()==(out/(case+'-repro-native')).read_bytes()
 for repeat in range(repetitions):
  label=case+'-'+str(repeat);fifo=root/'build/m19ef-sweep-reader-fifo'
  if case.startswith('los'):assert not fifo.exists();os.mkfifo(fifo,0o600)
  try:stdout=run(label,[image],expected=(1 if case=='fatal' else (2 if case in ('oom','oom-bound') else 0)),timeout=300 if case=='epochs' else 60)
  finally:
   if case.startswith('los'):fifo.unlink()
  data=(out/(label+'.stderr')).read_bytes();(out/(label+'-native-output.json')).write_text(json.dumps(dict(stdout=stdout.decode(errors='replace'),stderr_bytes=len(data),words=list(struct.unpack('<'+str(len(data)//8)+'Q',data)) if len(data)%8==0 else []),indent=2)+'\n')
  if case in ('fatal','oom','oom-bound'):
   n=list(map(int,stdout.splitlines()));assert n[:3]==[0,69,2],n
   if case=='fatal':
    assert len(data)==456 and n==[0,69,2],(len(data),n)
    original=struct.unpack('<16Q',data[:128]);assert original[0]==1330398546 and original[1]==3 and original[-1]==1
    fatal=struct.unpack('<9Q',data[128:200]);sweep=struct.unpack('<32Q',data[200:]);assert fatal==(1330401613,1,4,0,1,1,1,1,85),fatal
    assert sweep[:4]==(1330406979,3,4,0) and sweep[4]>=400003 and sweep[6]>0 and sweep[7]==0 and sweep[12:14]==(1,0) and sweep[14] in (0,1) and sweep[15]==0 and sweep[25]==0,sweep
   else:
    assert len(data)==328 and n==[0,69,2,2],(len(data),n)
    fatal=struct.unpack('<9Q',data[:72]);sweep=struct.unpack('<32Q',data[72:]);assert fatal==(1330401613,1,4,0,1,1,1,1,0),fatal
    assert sweep[:4]==(1330406979,3,4,0) and sweep[4]>=400003 and sweep[6:8]==(0,0) and sweep[12:16]==(1,1,1,1) and sweep[25]>0 and sweep[4]==sweep[5]+sweep[25],sweep
   observations.append(dict(case=case,repeat=repeat,stdout=stdout.decode(),fatal_words=list(fatal),sweep_words=list(sweep),source_manifest_sha256=digest));continue
  assert len(data)==808,(len(data),stdout,data[:256]);sweep=struct.unpack('<32Q',data[:256]);old=struct.unpack('<24Q',data[256:448]);mut=struct.unpack('<45Q',data[448:]);n=list(map(int,stdout.splitlines()))
  if case in ('ordinary','minor'):
   assert len(n)==(14 if case=='ordinary' else 18),n
   assert n[0]==0 and n[1]>=65 and n[2]>=(100003 if case=='ordinary' else 400003) and n[3:6]==[3,0,0] and n[6]>=3200000 and n[7]>0 and n[8]>=10000 and n[9]>=10000 and n[10]+n[11]>0 and n[12]==0 and n[-1]==0,n
   if case=='minor':assert n[13]>0 and n[14]>0 and n[15]>0 and n[16]>=n[15] and sweep[26:29]==tuple(n[13:16]) and sweep[29]>=n[16] and sweep[30]>0 and sweep[31]==1,(n,sweep)
   assert sweep[:4]==(1330406979,3,3,0) and sweep[4]==sweep[5]>=100003 and sweep[6:8]==(0,0) and sweep[8]>=3200000,sweep
   assert sweep[12:16]==(1,1,1,1) and sweep[16]>0 and sweep[16]%2==0 and sweep[17]>0 and sweep[10]<=sweep[18]<=sweep[19]<=sweep[11],sweep
   assert sweep[21]==0 and sweep[22]==sweep[23]==sweep[24]>=1 and sweep[25]==0,sweep
   assert len(set([sweep[9],mut[42],mut[43],mut[44]]))==4 and mut[24]==0 and mut[26]==0
   assert old[0]==1330406477 and old[1:3]==(4,0) and old[5]>=100003 and old[12:17]==(1,1,0,1,1)
  elif case=='epochs':
   assert len(n)==121 and n[-1]==0,n
   for cycle in range(8):
    v=n[cycle*15:cycle*15+15];cancel=cycle%3==0
    assert v[0]==0 and v[1]>=65 and v[3:6]==([4,0,0] if cancel else [3,0,0]) and v[8]>=1000 and v[9]>=1000 and v[12]==0,v
    if cancel:assert v[13]==2 and v[14]>0 and 0<=v[2]<400003,v
    else:assert v[2]>=400003 and v[7]>0 and v[10]+v[11]>0 and v[13] in (2,3) and v[14]==0,v
   assert sweep[:4]==(1330406979,3,3,0) and sweep[4]==sweep[5]>=100003 and sweep[6:8]==(0,0) and sweep[12:16]==(8,8,1,1) and sweep[20]==8 and sweep[25]==0,sweep
   assert old[12:17]==(8,8,0,1,1) and old[22]==8 and mut[24]==0 and mut[26]==0
   for off in (7,14):assert mut[off:off+6]==(8,8,8,8,24,24),mut
  elif case=='reenter':
   assert len(n)==17 and n[:4]==[0,69,2,0] and n[4]>=400003 and 0<=n[5]<n[4] and n[6:9]==[4,1,1] and n[9]>=65 and n[10]+n[5]>=n[4] and n[11:14]==[3,0,0] and n[14]>0 and n[15:]==[0,0],n
   assert sweep[:4]==(1330406979,3,3,0) and sweep[4]==sweep[5] and sweep[6:8]==(0,0) and sweep[12:16]==(2,2,1,1) and sweep[20]==2 and sweep[25]==0 and mut[24]==0 and mut[26]==0,sweep
   assert old[12:17]==(2,2,0,1,1) and old[22]==2
  elif case=='late':
   assert len(n)==17 and n[0]==0 and n[1]>=67 and n[2:5]==[2,2,2400003] and n[5]>=8 and n[6]>=2 and n[7]>0 and n[8]==1 and n[9]==0 and n[10]>0 and n[11]==0 and n[12]>=0 and n[13]>=2 and n[14:]==[0,0,0],n
   assert sweep[:4]==(1330406979,3,3,0) and sweep[4]==sweep[5] and sweep[6:8]==(0,0) and sweep[12:16]==(2,2,1,1) and sweep[20]==2 and sweep[25]==0 and mut[24]==0 and mut[26]==0,sweep
   for off in (7,14):assert mut[off:off+6]==(1,1,1,1,3,3),mut
  elif case=='shutdown':
   assert n==[0,69,2] and sweep[0:2]==(1330406979,3) and sweep[2] in (3,4) and sweep[3]==0 and sweep[4]>=400003 and sweep[4]==sweep[5]+sweep[25] and sweep[6:8]==(0,0) and sweep[12:16]==(1,1,1,1), (n,sweep)
   for off in (7,14):assert mut[off:off+6]==(1,1,1,1,3,3),mut
  else:
   assert len(n)==30,n
   fault=case=='los-fault';assert n[0]==0 and n[1]>=4 and n[2]>=34,n
   assert n[3:9]==([1,2,294912,22,15,15] if fault else [0,1,147456,0,16,16]),n
   assert n[11]==0 and n[12]>=35 and n[13:19]==[0,1,147456,22 if fault else 0,16,16],n
   assert n[9]-n[19]==(131120 if fault else 0) and n[10]-n[20]==(131120 if fault else 0) and n[21:24]==[1,1,0] and n[24]>=0 and n[25]>=2 and n[26:]==[1,0,0,0],n
   assert sweep[:4]==(1330406979,3,3,0) and sweep[4]==sweep[5] and sweep[6:8]==(0,0) and sweep[12:16]==((3,3,1,1) if fault else (2,2,1,1)) and sweep[20]==2 and sweep[21]==0 and sweep[22:25]==(1,1,1) and sweep[25]==0,sweep
   assert mut[24]==0 and mut[26]==0 and mut[30]>0 and mut[31]>0
  observations.append(dict(case=case,repeat=repeat,stdout=stdout.decode(),sweep_words=list(sweep),old_words=list(old),mutator_words=list(mut),source_manifest_sha256=digest))
(out/'observations.json').write_text(json.dumps(observations,indent=2)+'\n');assert all(sha(root/p)==h and sha(snap/p)==h for p,h in manifest.items());(out/'summary.json').write_text(json.dumps(dict(status='PASS_DEVELOPMENT_ONLY_NOT_G5' if development else 'PASS_FULL_SWEEP_MATRIX_PENDING_AFFECTED_REGRESSION',repetitions=repetitions,source_manifest_sha256=digest,E5='IN_PROGRESS',E6='NOT_RUN',M19e='NOT_ACCEPTED'),indent=2)+'\n')
