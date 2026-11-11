#!/usr/bin/env python3
"""Actual-host native wizard regression tests. Python is only a test driver.

Uses a real PTY on POSIX; never substitutes Go/reference execution for native.
Artifacts contain the three language transcripts and a machine-readable report.
"""
import argparse
import errno
import json
import os
from pathlib import Path
import re
import select
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]
CLI = None
ARTIFACTS = None

class WizardTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='mio-native-wizard-')
        self.root = Path(self.tmp.name)
        self.env = dict(os.environ, PATH=str(self.root/'empty'), TERM='xterm-256color', NO_COLOR='1')
        (self.root/'empty').mkdir()
    def tearDown(self):
        self.tmp.cleanup()
    def run_cli(self, args, **kwargs):
        return subprocess.run([str(CLI), *args], cwd=kwargs.pop('cwd', self.root), env=self.env,
                              capture_output=True, timeout=45, **kwargs)
    def test_all_catalog_locales_generate_identical_english_readme(self):
        baseline = None
        for catalog in sorted((ROOT/'selfhost/i18n/locales').glob('*.json')):
            lang = catalog.stem
            target = self.root/lang
            target.mkdir()
            result = self.run_cli(['--lang',lang,'bunko','create','--non-interactive',
                '--namespace','alice','--name','hello','--description','My first Mio package',
                '--platform','macos/arm64','--platform','linux/x86_64'], cwd=target)
            self.assertEqual(result.returncode,0,result.stderr.decode())
            readme=(target/'hello/README.md').read_bytes()
            if baseline is None: baseline=readme
            self.assertEqual(readme,baseline,lang)
            self.assertIn(b'## Publish to the official Bunko Index',readme)
            self.assertIn(b'packages/alice/hello/0.1.0.toml',readme)
            self.assertIn(b'macos = ["arm64"]',readme)
            self.assertNotIn(b'{{',readme)
            self.assertEqual(list((target/'hello').glob('README*')), [target/'hello/README.md'])
        (ARTIFACTS/'README.md').write_bytes(baseline)
    def test_non_tty_never_waits(self):
        # Keep stdin open without sending data: the CLI must reject before reading.
        process=subprocess.Popen([str(CLI),'bunko','create'],cwd=self.root,env=self.env,
                                 stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            self.assertNotEqual(process.wait(timeout=5),0)
            out,err=process.communicate()
            self.assertIn(b'NON_TTY',out+err)
            self.assertFalse((self.root/'hello').exists())
        finally:
            if process.poll() is None:process.kill();process.communicate()
    @unittest.skipUnless(sys.platform=='darwin' and os.uname().machine=='arm64','native interrupt guard is Darwin ARM64')
    def test_native_interrupt_guard(self):
        import signal
        image=self.root/'interrupt-guard'
        result=self.run_cli(['--lang','en-US','build',str(ROOT/'scripts/bunko/fixtures/interrupt_guard.mio'),
            '--bunko-root',str(ROOT/'bunko'),'-o',str(image)])
        self.assertEqual(result.returncode,0,result.stderr.decode())
        process=subprocess.Popen([str(image)],cwd=self.root,env=self.env,
            stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            self.assertTrue(select.select([process.stdout],[],[],5)[0])
            self.assertEqual(process.stdout.readline(),b'guarded\n')
            process.send_signal(signal.SIGINT)
            process.stdin.write(b'\n');process.stdin.flush()
            out,err=process.communicate(timeout=5)
            self.assertEqual(process.returncode,-signal.SIGINT,err)
            self.assertEqual(out,b'pending\n','SIGINT must remain pending until publication leaves its guard')
        finally:
            if process.poll() is None:process.kill();process.communicate(timeout=3)
    @unittest.skipUnless(os.name=='posix','PTY requires POSIX')
    def test_theme_and_unicode_metadata(self):
        self.env.pop('NO_COLOR',None)
        description='日本語と中文の紹介 ♡'
        out=self.terminal_session(self.root,'en-US',[(b'namespace/name',b'Bad/Name\n'),
            (b'INVALID_NAME',b'alice/hello\n'),(b'Tell us',(description+'\n').encode()),(b'[ ]',b' \r')],
            extra=['--color=always','--theme=dark'])
        self.assertRegex(out,r'\x1b\[[0-9;]+m\? Package name')
        self.assertRegex(out,r'\x1b\[[0-9;]+mBUNKO_E_SCAFFOLD_INVALID_NAME')
        self.assertRegex(out,r'\x1b\[[0-9;]+m✓ Package created')
        self.assertIn(description,(self.root/'hello/README.md').read_text())
    def test_readme_metadata_and_archive_recipe(self):
        import tarfile
        import tomllib
        args=['--lang','ja-JP','bunko','create','--non-interactive','--namespace','alice',
            '--name','hello','--description','A "quoted" \\ package','--version','1.2.3',
            '--license','Apache-2.0','--platform','macos/arm64','--dependency','bob/text@1.0.0']
        result=self.run_cli(args)
        self.assertEqual(result.returncode,0,result.stderr.decode())
        package=self.root/'hello';readme=(package/'README.md').read_text()
        self.assertEqual(readme.count('```') % 2,0,'unbalanced Markdown fences')
        record=tomllib.loads(readme.split('```toml\n',1)[1].split('```',1)[0])
        self.assertEqual(record['description'],'A "quoted" \\ package')
        self.assertEqual(record['version'],'1.2.3')
        self.assertEqual(record['license'],'Apache-2.0')
        self.assertEqual(record['platforms'],{'macos':['arm64']})
        self.assertEqual(record['dependencies'],{'bob/text':'1.0.0'})
        self.assertEqual(record['release_id'],'REPLACE_WITH_REAL_POSITIVE_DECIMAL_RELEASE_ID')
        recipe=readme.split("python3 - <<'PY'\n",1)[1].split('\nPY',1)[0]
        packed=subprocess.run([sys.executable,'-c',recipe],cwd=package,env=self.env,capture_output=True,timeout=10)
        self.assertEqual(packed.returncode,0,packed.stderr.decode())
        archive=self.root/'alice-hello-1.2.3.tar.gz'
        with tarfile.open(archive) as stream:
            entries=stream.getmembers()
            self.assertEqual([e.name for e in entries],['package.mio','package_test.mio','README.md','LICENSE'])
            self.assertTrue(all(e.isfile() and e.mode==0o644 for e in entries))
            self.assertTrue(all(stream.extractfile(e).read().decode('utf-8') for e in entries))
        (ARTIFACTS/'source-archive.tar.gz').write_bytes(archive.read_bytes())
    @unittest.skipUnless(os.name=='posix','PTY requires POSIX')
    def test_real_terminal_locales_selection_retry_and_restore(self):
        for lang,welcome in [('en-US',"Let's create a Mio package!"),('ja-JP','Mio パッケージを作りましょう！'),('zh-CN','让我们创建一个 Mio 软件包吧！')]:
            target=self.root/lang;target.mkdir()
            out=self.terminal_session(target,lang,[(b'namespace/name',b'Bad/Name\n'),
                (b'INVALID_NAME',b'alice/hello\n'),
                ({'en-US':b'Tell us','ja-JP':'紹介'.encode(),'zh-CN':'介绍'.encode()}[lang],b'My first Mio package\n'),
                (b'[ ]',b'\r'), # Empty selection must not create a package.
                (b'[ ]',b' \x1b[B \r')])
            self.assertIn(welcome,out)
            self.assertIn('[x]',out)
            self.assertNotIn('\x1b[3',out) # NO_COLOR: no ANSI foreground color sequences.
            self.assertIn('README.md',out)
            import tomllib
            info=tomllib.loads((target/'hello/bunko.package.toml').read_text())
            self.assertEqual(info['platforms'],{'linux':['x86_64'],'macos':['arm64']})
            for command in ['check','test']:
                result=self.run_cli(['--lang','en-US',command,'.'],cwd=target/'hello')
                self.assertEqual(result.returncode,0,result.stderr.decode())
            (ARTIFACTS/(lang+'.txt')).write_text(out)
    @unittest.skipUnless(os.name=='posix','PTY requires POSIX')
    def test_escape_and_ctrl_c_restore_without_project(self):
        for key in [b'\x1b',b'\x03']:
            target=self.root/str(key.hex());target.mkdir()
            self.terminal_session(target,'en-US',[(b'namespace/name',b'alice/hello\n'),
                (b'Tell us',b'My first Mio package\n'),(b'[ ]',key)],expect_success=False)
            self.assertFalse((target/'hello').exists())
    @unittest.skipUnless(os.name=='posix','PTY requires POSIX')
    def test_numeric_fallback(self):
        self.env['TERM']='dumb'
        out=self.terminal_session(self.root,'en-US',[(b'namespace/name',b'alice/hello\n'),
            (b'Tell us',b'My first Mio package\n'),(b'1,2',b'0\n'),
            (b'INVALID_PLATFORM',b'1,2\n')])
        self.assertIn('created successfully',out)
        self.assertNotIn('\x1b',out)
    def terminal_session(self,target,lang,steps,expect_success=True,extra=()):
        import termios
        master,slave=os.openpty()
        before=termios.tcgetattr(slave)
        process=subprocess.Popen([str(CLI),'--lang',lang,*extra,'bunko','create'],cwd=target,
            env=self.env,stdin=slave,stdout=slave,stderr=slave,start_new_session=True)
        data=bytearray();offset=0
        def read_until(needle):
            nonlocal offset
            deadline=time.monotonic()+15
            while needle not in data[offset:]:
                if time.monotonic()>deadline:raise AssertionError('PTY timed out: '+data.decode(errors='replace'))
                if select.select([master],[],[],0.1)[0]:
                    try:chunk=os.read(master,65536)
                    except OSError as exc:
                        if exc.errno==errno.EIO:chunk=b''
                        else:raise
                    if not chunk:raise AssertionError('PTY closed: '+data.decode(errors='replace'))
                    data.extend(chunk)
                if process.poll() is not None:raise AssertionError('CLI exited: '+data.decode(errors='replace'))
            offset=len(data)
        try:
            for needle,keys in steps:
                read_until(needle);os.write(master,keys)
            deadline=time.monotonic()+15
            # Drain the master while waiting: Darwin may wait for terminal
            # output during controlling-session teardown.
            while process.poll() is None:
                if time.monotonic()>deadline:raise AssertionError('CLI did not exit: '+data.decode(errors='replace'))
                if select.select([master],[],[],0.05)[0]:
                    try:chunk=os.read(master,65536)
                    except OSError:chunk=b''
                    data.extend(chunk)
            while select.select([master],[],[],0.05)[0]:
                try:chunk=os.read(master,65536)
                except OSError:break
                if not chunk:break
                data.extend(chunk)
            after=termios.tcgetattr(slave)
            # Darwin sets the transient PENDIN bit when returning to canonical
            # input. Compare all user settings, excluding that kernel state bit.
            after[3] &= ~getattr(termios,'PENDIN',0)
            before[3] &= ~getattr(termios,'PENDIN',0)
            self.assertEqual(after,before,'terminal state was not restored')
            if expect_success:self.assertEqual(process.returncode,0,data.decode(errors='replace'))
            else:
                self.assertNotEqual(process.returncode,0)
                self.assertNotIn(b'created successfully',data)
            # Keep actual UTF-8 output, removing only cursor motion for readable evidence.
            return re.sub(r'\x1b\[[0-9;]*[A-Z]','',data.decode().replace('\r',''))
        finally:
            os.close(master);os.close(slave)
            if process.poll() is None:
                process.kill()
                try:process.wait(timeout=3)
                except subprocess.TimeoutExpired:pass

if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('cli',type=Path);parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args();CLI=args.cli.resolve();ARTIFACTS=args.output.resolve();ARTIFACTS.mkdir(parents=True,exist_ok=True)
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(WizardTests)
    result=unittest.TextTestRunner(verbosity=2).run(suite)
    (ARTIFACTS/'report.json').write_text(json.dumps({'tests':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped)},indent=2)+'\n')
    sys.exit(not result.wasSuccessful())
