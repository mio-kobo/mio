import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { resolveMioExecutable } from '../../cli/cliResolver';

const executableName = process.platform === 'win32' ? 'mio.exe' : 'mio';

function createExecutable(directory: string, name = executableName): string {
  fs.mkdirSync(directory, { recursive: true });
  const file = path.join(directory, name);
  fs.writeFileSync(file, '', 'utf8');
  if (process.platform !== 'win32') {
    fs.chmodSync(file, 0o755);
  }
  return file;
}

suite('Mio CLI resolver', () => {
  let temporaryRoot: string;

  setup(() => {
    temporaryRoot = fs.mkdtempSync(
      path.join(os.tmpdir(), 'mio cli 中文 日本語-'),
    );
  });

  teardown(() => {
    fs.rmSync(temporaryRoot, { recursive: true, force: true });
  });

  test('uses a configured path with spaces and non-ASCII characters', () => {
    const directory = path.join(temporaryRoot, 'Mio CLI 中文 日本語');
    const file = createExecutable(directory);
    const resolution = resolveMioExecutable({ configuredPath: file });
    assert.equal(resolution.kind, 'found');
    if (resolution.kind === 'found') {
      assert.equal(resolution.source, 'configured');
      assert.equal(resolution.executable, path.resolve(file));
    }
  });

  test('strips surrounding quotes from the configured path', () => {
    const file = createExecutable(path.join(temporaryRoot, 'bin'));
    const resolution = resolveMioExecutable({
      configuredPath: `"${file}"`,
    });
    assert.equal(resolution.kind, 'found');
  });

  test('reports a missing configured path', () => {
    const resolution = resolveMioExecutable({
      configuredPath: path.join(temporaryRoot, 'missing', executableName),
    });
    assert.equal(resolution.kind, 'invalid');
    if (resolution.kind === 'invalid') {
      assert.match(resolution.message, /does not exist/u);
    }
  });

  test('discovers the CLI from PATH', () => {
    const bin = path.join(temporaryRoot, 'bin with spaces');
    const file = createExecutable(bin);
    const resolution = resolveMioExecutable({
      envPath: [path.join(temporaryRoot, 'empty'), bin].join(path.delimiter),
    });
    assert.equal(resolution.kind, 'found');
    if (resolution.kind === 'found') {
      assert.equal(resolution.source, 'path');
      assert.equal(resolution.executable, path.resolve(file));
    }
  });

  test('returns notFound when PATH has no Mio CLI', () => {
    const resolution = resolveMioExecutable({
      envPath: path.join(temporaryRoot, 'empty'),
    });
    assert.equal(resolution.kind, 'notFound');
  });

  test('reports a non-executable CLI on Unix', () => {
    if (process.platform === 'win32') {
      return;
    }
    const bin = path.join(temporaryRoot, 'bin');
    fs.mkdirSync(bin, { recursive: true });
    const file = path.join(bin, executableName);
    fs.writeFileSync(file, '', 'utf8');
    fs.chmodSync(file, 0o644);
    const resolution = resolveMioExecutable({ envPath: bin });
    assert.equal(resolution.kind, 'invalid');
    if (resolution.kind === 'invalid') {
      assert.match(resolution.message, /not executable/u);
    }
  });

  test('expands a leading home directory', () => {
    const home = path.join(temporaryRoot, 'home');
    createExecutable(home);
    const resolution = resolveMioExecutable({
      configuredPath: `~/${executableName}`,
      homeDir: home,
    });
    assert.equal(resolution.kind, 'found');
  });

  test('rejects a directory configured as the CLI', () => {
    const directory = path.join(temporaryRoot, 'bin');
    fs.mkdirSync(directory, { recursive: true });
    const resolution = resolveMioExecutable({
      configuredPath: directory,
    });
    assert.equal(resolution.kind, 'invalid');
  });

  test('rejects Windows batch launchers', () => {
    if (process.platform !== 'win32') {
      return;
    }
    const directory = path.join(temporaryRoot, 'bin');
    fs.mkdirSync(directory, { recursive: true });
    const file = path.join(directory, 'mio.cmd');
    fs.writeFileSync(file, '@echo off\r\n', 'utf8');
    const resolution = resolveMioExecutable({ configuredPath: file });
    assert.equal(resolution.kind, 'invalid');
    if (resolution.kind === 'invalid') {
      assert.match(resolution.message, /native executables/u);
    }
  });
});
