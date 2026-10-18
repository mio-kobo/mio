import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import {
  ProcessRunError,
  ProcessRunner,
  type CancellationLike,
} from '../../cli/processRunner';

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, milliseconds);
  });
}

suite('process runner', () => {
  let runner: ProcessRunner;
  let temporaryRoot: string;

  setup(() => {
    runner = new ProcessRunner();
    temporaryRoot = fs.mkdtempSync(
      path.join(os.tmpdir(), 'mio runner 中文 日本語-'),
    );
  });

  teardown(() => {
    runner.dispose();
    fs.rmSync(temporaryRoot, { recursive: true, force: true });
  });

  test('passes arguments as an array without shell interpolation', async () => {
    const result = await runner.run(
      process.execPath,
      [
        '-e',
        'process.stdout.write(process.argv[1])',
        'hello world; echo injected',
      ],
      { timeoutMs: 15_000 },
    );
    assert.equal(result.code, 0);
    assert.equal(result.stdout, 'hello world; echo injected');
    assert.equal(result.stderr, '');
  });

  test('captures stderr and non-zero exit codes', async () => {
    const result = await runner.run(
      process.execPath,
      [
        '-e',
        'process.stderr.write("oops"); process.exit(3);',
      ],
      { timeoutMs: 15_000 },
    );
    assert.equal(result.code, 3);
    assert.equal(result.stderr, 'oops');
  });

  test('passes environment variables and working directory', async () => {
    const result = await runner.run(
      process.execPath,
      [
        '-e',
        'process.stdout.write(`${process.env.MIO_TEST_VALUE}|${process.cwd()}`)',
      ],
      {
        timeoutMs: 15_000,
        cwd: temporaryRoot,
        env: { MIO_TEST_VALUE: 'hello world' },
      },
    );
    assert.equal(result.code, 0);
    assert.equal(
      result.stdout,
      `hello world|${fs.realpathSync(temporaryRoot)}`,
    );
  });

  test('launches an executable whose path contains spaces and Unicode', async () => {
    const directory = path.join(
      temporaryRoot,
      'Mio CLI 中文 日本語',
    );
    fs.mkdirSync(directory, { recursive: true });
    const linked = path.join(directory, 'node');
    try {
      fs.symlinkSync(process.execPath, linked);
    } catch {
      fs.copyFileSync(process.execPath, linked);
      if (process.platform !== 'win32') {
        fs.chmodSync(linked, 0o755);
      }
    }

    const result = await runner.run(
      linked,
      ['-e', 'process.stdout.write("spaced path works")'],
      { timeoutMs: 15_000 },
    );
    assert.equal(result.code, 0);
    assert.equal(result.stdout, 'spaced path works');
  });

  test('terminates a process after the timeout', async () => {
    const started = Date.now();
    const result = await runner.run(
      process.execPath,
      ['-e', 'setTimeout(() => {}, 60_000)'],
      { timeoutMs: 250 },
    );
    assert.equal(result.timedOut, true);
    assert.ok(Date.now() - started < 10_000);
  });

  test('cancels a running process', async () => {
    let listener: (() => void) | undefined;
    const token: CancellationLike & { isCancellationRequested: boolean } = {
      isCancellationRequested: false,
      onCancellationRequested(callback: () => void) {
        listener = callback;
        return {
          dispose() {
            if (listener === callback) {
              listener = undefined;
            }
          },
        };
      },
    };

    const promise = runner.run(
      process.execPath,
      ['-e', 'setTimeout(() => {}, 60_000)'],
      { timeoutMs: 0, token },
    );
    await delay(150);
    token.isCancellationRequested = true;
    listener?.();
    const result = await promise;
    assert.equal(result.cancelled, true);
  });

  test('reports a missing executable', async () => {
    await assert.rejects(
      () =>
        runner.run(path.join(temporaryRoot, 'missing-mio'), [], {
          timeoutMs: 5_000,
        }),
      (error: unknown) =>
        error instanceof ProcessRunError &&
        error.code === 'ENOENT',
    );
  });

  test('truncates excessive output', async () => {
    const result = await runner.run(
      process.execPath,
      ['-e', 'process.stdout.write("x".repeat(10_000))'],
      { timeoutMs: 15_000, maxOutputBytes: 100 },
    );
    assert.equal(result.truncated, true);
    assert.equal(result.stdout.length, 100);
  });

  test('dispose terminates running processes', async () => {
    const promise = runner.run(
      process.execPath,
      ['-e', 'setTimeout(() => {}, 60_000)'],
      { timeoutMs: 0 },
    );
    await delay(150);
    runner.dispose();
    const result = await promise;
    assert.ok(result.signal !== null || result.code !== null);
  });
});
