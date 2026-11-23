#!/usr/bin/env python3
"""Audit/launch only. Every compiler/runtime/codegen/link/sign stage is native Mio."""
import argparse, fcntl, hashlib, json, os, pathlib, platform, shutil
import signal, struct, subprocess, sys, time, traceback, uuid

ROOT = pathlib.Path(__file__).resolve().parents[2]
SEED = pathlib.Path('/Volumes/Sector0/AppData/Mio/mio/build/gofree-native/mio')
SEED_SHA = 'be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'

def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def save(path, value): path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
def sources():
    return {str(p.relative_to(ROOT)): sha(p) for folder in ('bunko', 'selfhost', 'tests/m19ef')
            for p in sorted((ROOT/folder).rglob('*.mio'))}
def compiler_inputs(manifest):
    return {p:h for p,h in manifest.items() if p.startswith(('bunko/', 'selfhost/'))
            or p == 'tests/m19ef/old-sweep/builder.mio'}

def main():
    parser = argparse.ArgumentParser(description='Mio E5 原生测试；不授予 E5/M19e PASS。')
    parser.add_argument('mode', choices=('fatal', 'full'), nargs='?', default='fatal')
    parser.add_argument('--repeat', type=int, default=32, help='fatal 重复次数，默认32；full固定32')
    parser.add_argument('--fresh', action='store_true', help='重新使用可信原生Seed构建测试编译器')
    parser.add_argument('--output', type=pathlib.Path, help='build/m19ef-evidence 下尚不存在的目录')
    args = parser.parse_args()
    if not 1 <= args.repeat <= 32: parser.error('--repeat 必须在1～32之间')
    if args.mode == 'full' and args.repeat != 32: parser.error('full 门禁固定32次，不允许减少')
    assert pathlib.Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],cwd=ROOT,text=True).strip()).resolve() == ROOT, f'错误工作树：{ROOT}'
    os.chdir(ROOT)
    assert platform.system() == 'Darwin' and platform.machine() == 'arm64', '仅支持当前 Darwin ARM64 原生环境'
    branch = subprocess.check_output(['git', 'branch', '--show-current'], text=True).strip()
    assert branch in ('EXP/m19ef-portable-gc', 'Mio-2.1'), branch
    assert SEED.is_file() and sha(SEED) == SEED_SHA, '可信Native Seed不匹配'
    (ROOT/'build').mkdir(exist_ok=True)
    lock = (ROOT/'build/.m19ef-native-test.lock').open('a+')
    try: fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError: raise SystemExit('另一个通过此入口启动的测试正在运行。')
    out = (args.output or ROOT/'build/m19ef-evidence'/(
        'manual-E5-'+args.mode+'-'+time.strftime('%Y%m%d-%H%M%S')+'-'+uuid.uuid4().hex[:8])).resolve()
    assert out.is_relative_to(ROOT/'build/m19ef-evidence'), '输出必须位于当前工作树 build/m19ef-evidence 内'
    out.mkdir(parents=True, exist_ok=False)
    print(f'证据目录：{out}', flush=True)
    snapshot = out/'source-snapshot'; snapshot.mkdir()
    manifest = sources()
    for relative, expected in manifest.items():
        p = ROOT/relative; assert not p.is_symlink(), p
        dest = snapshot/relative; dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(p, dest); assert sha(dest) == expected
    save(out/'source-manifest.json', manifest)
    digest = sha(out/'source-manifest.json')
    shutil.copyfile(__file__, out/'launcher.py')
    save(out/'provenance.json', dict(mode=args.mode, repetitions=args.repeat, worktree=str(ROOT),
        branch=branch, head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),
        dirty=subprocess.check_output(['git','status','--porcelain=v1','--untracked-files=all'],text=True),
        host=platform.platform(), target='darwin-arm64', seed=str(SEED), seed_sha256=SEED_SHA,
        source_manifest_sha256=digest, launcher_sha256=sha(pathlib.Path(__file__)),
        production_stages='native Mio only; Python freezes, launches, measures and audits'))
    rows = []
    def run(label, argv, expected=0, timeout=600, cwd=snapshot):
        if len(argv) == 1 and pathlib.Path(argv[0]).parent == out: cwd = ROOT
        start = time.monotonic(); interrupted = False; timed = False
        with (out/(label+'.stdout')).open('xb') as stdout, (out/(label+'.stderr')).open('xb') as stderr:
            child = subprocess.Popen(list(map(str,argv)), cwd=cwd, stdout=stdout, stderr=stderr, start_new_session=True)
            try:
                while True:
                    got, status, usage = os.wait4(child.pid, os.WNOHANG)
                    if got: break
                    if time.monotonic()-start > timeout:
                        timed=True
                        (out/(label+'.processes')).write_text(subprocess.check_output(['ps','-Ao','pid,ppid,%cpu,rss,etime,command'],text=True))
                        os.killpg(child.pid,signal.SIGTERM)
                        time.sleep(.2)
                        try: os.killpg(child.pid,signal.SIGKILL)
                        except ProcessLookupError: pass
                        got,status,usage=os.wait4(child.pid,0); break
                    time.sleep(.05)
            except KeyboardInterrupt:
                interrupted=True
                # Full controller handles SIGTERM and records/kills its own active native child.
                os.killpg(child.pid,signal.SIGTERM)
                got,status,usage=os.wait4(child.pid,0)
            row=dict(label=label,argv=list(map(str,argv)),cwd=str(cwd),exit=os.waitstatus_to_exitcode(status),
                expected_exit=expected,timed_out=timed,interrupted=interrupted,timeout_seconds=timeout,
                wall_seconds=time.monotonic()-start,cpu_seconds=usage.ru_utime+usage.ru_stime,
                peak_rss_bytes_darwin=usage.ru_maxrss,source_manifest_sha256=digest,
                stdout_sha256=sha(out/(label+'.stdout')),stderr_sha256=sha(out/(label+'.stderr')))
            rows.append(row); save(out/'commands.json',rows)
            print(f'{label}: exit={row["exit"]}, expected={expected}',flush=True)
            if interrupted: raise KeyboardInterrupt
            assert row['exit']==expected and not timed, f'{label} 失败，查看 {out/(label+".stderr")}'
            return (out/(label+'.stdout')).read_bytes()
    result=dict(status='IN_PROGRESS',E5='IN_PROGRESS',E6='NOT_RUN',M19e='NOT_ACCEPTED',source_manifest_sha256=digest)
    try:
        run('seed-signature',['codesign','--verify','--strict',SEED])
        prior=None
        if not args.fresh:
            for candidate in sorted((ROOT/'build/m19ef-evidence').glob('E5-*'), key=lambda p:p.stat().st_mtime, reverse=True):
                try:
                    old=json.loads((candidate/'source-manifest.json').read_text())
                    built=next(r for r in json.loads((candidate/'commands.json').read_text()) if r['label']=='build')
                    meta=json.loads((candidate/'summary.json').read_text())
                    binary=candidate/'sweep-builder'
                    if compiler_inputs(old)==compiler_inputs(manifest) and built['exit']==0 and not built['timed_out'] and meta.get('builder_sha256')==sha(binary):
                        prior=candidate; break
                except (OSError,ValueError,KeyError,StopIteration): continue
        if args.mode=='full':
            argv=['python3',ROOT/'tests/m19ef/old-sweep-v2/matrix.py',out/'matrix',prior or '-']
            fcntl.flock(lock,fcntl.LOCK_UN) # v2 matrix owns the lock across both native image families.
            print('完整矩阵固定32次；进度日志：'+str(out/'matrix-controller.stdout'),flush=True)
            run('matrix-controller',argv,timeout=21600,cwd=ROOT)
            native=json.loads((out/'matrix/summary.json').read_text())
            assert native['repetitions']==32 and native['status']=='PASS_FIXED32_PENDING_AFFECTED_REGRESSION'
            result['status']='PASS_E5_MATRIX_ONLY_PENDING_AFFECTED_REGRESSION_AND_E6'
        else:
            builder=out/'sweep-builder'
            if prior is None:
                run('build',[SEED,'build','tests/m19ef/old-sweep/builder.mio','-o',builder,'--bunko-root',snapshot/'bunko'])
            else:
                shutil.copyfile(prior/'sweep-builder',builder); builder.chmod(0o755)
                save(out/'builder-reuse.json',dict(prior=str(prior),builder_sha256=sha(builder),compiler_inputs_identical=True,
                    scope='native builder reuse only; user images rebuilt; not a bootstrap fixed point'))
            run('builder-signature',['codesign','--verify','--strict',builder])
            image=out/'fatal-native'; duplicate=out/'fatal-repro-native'
            common=[builder,snapshot,'tests/m19ef/old-sweep/fatal/main.mio',snapshot/'bunko']
            run('generate',common+[image,'67108864','32'])
            run('image-signature',['codesign','--verify','--strict',image])
            run('reproduce',common+[duplicate,'67108864','32'])
            assert image.read_bytes()==duplicate.read_bytes(), '两次原生构建的完整签名字节不同'
            assert pathlib.Path(str(image)+'.unsigned').read_bytes()==pathlib.Path(str(duplicate)+'.unsigned').read_bytes()
            observations=[]
            for repeat in range(args.repeat):
                label=f'fatal-{repeat:02d}'
                stdout=run(label,[image],expected=1,timeout=60)
                packet=(out/(label+'.stderr')).read_bytes()
                assert stdout==b'0\n69\n2\n', (label,stdout)
                assert len(packet)==456, (label,len(packet))
                registers=struct.unpack('<16Q',packet[:128]); fatal=struct.unpack('<9Q',packet[128:200]); sweep=struct.unpack('<32Q',packet[200:])
                assert registers[0]==1330398546 and registers[1]==3 and registers[-1]==1
                assert fatal==(1330401613,1,4,0,1,1,1,1,85), (label,fatal)
                assert sweep[:4]==(1330406979,3,4,0) and sweep[4]>=400003 and sweep[6]>0 and sweep[7]==0
                assert sweep[12:14]==(1,0) and sweep[14] in (0,1) and sweep[15]==0 and sweep[25]==0, (label,sweep)
                observations.append(dict(repeat=repeat,runtime_registers=list(registers),fatal_words=list(fatal),sweep_words=list(sweep),
                    meaning='non-stopped caller: real cancel-only85, no Sweep join/discard, mappings retained through terminal process exit',
                    telemetry='terminal partial packet; not an atomic memory snapshot or positive reclaim proof'))
                save(out/'fatal-observations.json',observations)
            result.update(status='PASS_FATAL_OWNERSHIP_SUBSET_ONLY',repetitions=args.repeat,
                formal_32_repeat_subset=args.repeat==32,native_image_sha256=sha(image),builder_sha256=sha(builder))
    except KeyboardInterrupt:
        result['status']='INTERRUPTED_NOT_ACCEPTED'; print('测试已中断，证据保留。',flush=True); return_code=130
    except Exception as error:
        result.update(status='FAIL_NOT_ACCEPTED',error=str(error))
        (out/'failure.txt').write_text(traceback.format_exc()); print(str(error),file=sys.stderr,flush=True); return_code=1
    else: return_code=0
    finally:
        unchanged=sources()==manifest
        save(out/'final-source-unchanged.json',dict(unchanged=unchanged,source_manifest_sha256=digest))
        if not unchanged: result.update(status='FAIL_SOURCE_CHANGED_NOT_ACCEPTED'); return_code=1
        result['source_unchanged']=unchanged
        save(out/'summary.json',result)
        print(f'结果：{result["status"]}；M19e NOT_ACCEPTED\n报告：{out/"summary.json"}',flush=True)
    return return_code

if __name__=='__main__': sys.exit(main())
