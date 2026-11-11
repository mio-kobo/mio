"""Independent scaffold CLI oracle; used by the existing six-platform native driver.
Python is a test dependency only. The generated package and native CLI never invoke it.
"""
import json
import os
from pathlib import Path

def check_scaffolding(report, cli, work, runner):
    # Native CI pins Python 3.12. Import lazily so older Python can still inspect
    # the shared native driver and run unrelated CI harness self-tests.
    import tomllib
    # Full and Core CLIs are checked in the same work directory. Each must own
    # independent fixtures and logs, including on a second call in one report.
    variant=Path(cli).stem
    def run(report, label, *args, **kwargs):
        return runner(report, variant+'-'+label, *args, **kwargs)
    def check(name, *args, **kwargs):
        return report.check(name+' / '+variant, *args, **kwargs)
    root=Path(work)/('bunko-scaffold-'+variant);root.mkdir(exist_ok=True)
    empty=root/'empty-path';empty.mkdir(exist_ok=True)
    env={'PATH':str(empty)}
    common=['--namespace','fixture','--name','native-package','--description','A "quoted" \\ description','--version','1.0.0','--license','MIT','--platform','linux/x86_64']
    argv=[cli,'--lang=en-US','--color=never','bunko','create','--non-interactive',*common]
    code,out,err,_=run(report,'scaffold-create',argv,cwd=root,env=env)
    package=root/'native-package';problem=''
    try:
        required=['package.mio','package_test.mio','README.md','LICENSE','.gitignore','bunko.toml','bunko.package.toml']
        assert all((package/name).is_file() for name in required)
        config=tomllib.loads((package/'bunko.package.toml').read_text())
        assert config['package']['name']=='fixture/native-package'
        assert config['package']['description']=='A "quoted" \\ description'
        assert config['platforms']=={'linux':['x86_64']},'undeclared host target was inferred'
        assert tomllib.loads((package/'bunko.toml').read_text())=={'dependencies':{}}
        assert not (package/'.git').exists(),'Git initialized without permission'
        assert not (package/'.github').exists(),'workflow enabled implicitly'
    except (OSError,ValueError,AssertionError) as exc:problem=str(exc)
    ok=code==0 and not problem
    check('bunko-create-native','PASS' if ok else 'FAIL','native',problem or (err if code else ''))
    if not ok:return
    before=(package/'package.mio').read_bytes()
    code,out,err,_=run(report,'scaffold-existing',argv,cwd=root,env=env)
    check('bunko-create-existing','PASS' if code!=0 and (package/'package.mio').read_bytes()==before else 'FAIL','native','')
    for option,value,diagnostic in [('--name','../escape','INVALID_NAME'),('--version','latest','INVALID_VERSION'),('--platform','windows/arm64','INVALID_PLATFORM'),('--directory','../escape','INVALID_PATH')]:
        args=common.copy()
        if option in args:args[args.index(option)+1]=value
        else:args += [option,value]
        code,out,err,_=run(report,'scaffold-invalid-'+diagnostic.lower(),[cli,'--lang=en-US','bunko','create','--non-interactive',*args],cwd=root,env=env)
        check('bunko-create-'+diagnostic.lower(),'PASS' if code!=0 and 'BUNKO_E_SCAFFOLD_'+diagnostic in out+err and not (work/'escape').exists() else 'FAIL','native','')
    code,out,err,_=run(report,'scaffold-non-tty',[cli,'--lang=en-US','bunko','create'],cwd=root,stdin=b'',env=env)
    check('bunko-create-non-tty','PASS' if code!=0 and 'BUNKO_E_SCAFFOLD_NON_TTY' in out+err else 'FAIL','native',err if code==0 else '')
    # The whole operation works with an empty PATH: no Go, Python, Git, gh or shell runtime fallback.
    code,out,err,_=run(report,'scaffold-compile',[cli,'--lang=en-US','check',package],cwd=root,env=env)
    check('bunko-generated-compile','PASS' if code==0 else 'FAIL','native',err if code else '')
    code,out,err,_=run(report,'scaffold-tests',[cli,'--lang=en-US','test',package,'--format=json','--color=never'],cwd=root,env=env)
    try:
        data=json.loads(out);ok=code==0 and data['summary']['passed']==1 and data['summary']['failed']==0
    except (ValueError,KeyError,TypeError):ok=False
    check('bunko-generated-test','PASS' if ok else 'FAIL','native',err if not ok else '')
    outside=root/'outside';outside.mkdir(exist_ok=True);link=root/'linked'
    try:
        os.symlink(outside,link,target_is_directory=True)
    except OSError as exc:
        check('bunko-symlink-fixture','NOT_RUN','independent',str(exc),required=False)
    else:
        code,out,err,_=run(report,'scaffold-symlink',[*argv,'--directory','linked'],cwd=root,env=env)
        check('bunko-symlink-escape','PASS' if code!=0 and not list(outside.iterdir()) else 'FAIL','native','')
