import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import {
  expandHome,
  findBunkoRoot,
  resolveBuildOutput,
  samePath,
} from '../../util/paths';

suite('path helpers', () => {
  let temporaryRoot: string;

  setup(() => {
    temporaryRoot = fs.mkdtempSync(
      path.join(os.tmpdir(), 'mio paths 中文 日本語-'),
    );
  });

  teardown(() => {
    fs.rmSync(temporaryRoot, { recursive: true, force: true });
  });

  test('finds the nearest bunko directory above a source file', () => {
    const bunko = path.join(temporaryRoot, 'bunko');
    const source = path.join(temporaryRoot, 'src', 'deep', 'main.mio');
    fs.mkdirSync(bunko, { recursive: true });
    fs.mkdirSync(path.dirname(source), { recursive: true });
    fs.writeFileSync(source, 'chant("hello");\n', 'utf8');

    assert.equal(findBunkoRoot(source, {}), bunko);
  });

  test('prefers MIO_BUNKO when it is a directory', () => {
    const configured = path.join(temporaryRoot, 'custom bunko');
    const source = path.join(temporaryRoot, 'src', 'main.mio');
    fs.mkdirSync(configured, { recursive: true });
    fs.mkdirSync(path.dirname(source), { recursive: true });
    fs.writeFileSync(source, 'chant("hello");\n', 'utf8');

    assert.equal(
      findBunkoRoot(source, { MIO_BUNKO: configured }),
      configured,
    );
  });

  test('returns undefined when no bunko directory exists', () => {
    const source = path.join(temporaryRoot, 'src', 'main.mio');
    fs.mkdirSync(path.dirname(source), { recursive: true });
    fs.writeFileSync(source, 'chant("hello");\n', 'utf8');

    assert.equal(findBunkoRoot(source, {}), undefined);
  });

  test('keeps the default build output next to the source', () => {
    const source = path.join(temporaryRoot, '集合 テスト.mio');
    assert.deepEqual(resolveBuildOutput(source, '', undefined), {
      outputDirectory: temporaryRoot,
      outputPath: path.join(temporaryRoot, '集合 テスト.mochi'),
    });
  });

  test('expands workspaceFolder in the build output directory', () => {
    const workspace = path.join(temporaryRoot, 'workspace');
    const source = path.join(workspace, 'src', 'main.mio');
    assert.deepEqual(
      resolveBuildOutput(source, '${workspaceFolder}/out', workspace),
      {
        outputDirectory: path.join(workspace, 'out'),
        outputPath: path.join(workspace, 'out', 'main.mochi'),
      },
    );
  });

  test('resolves a relative build output directory against the workspace', () => {
    const workspace = path.join(temporaryRoot, 'workspace');
    const source = path.join(workspace, 'src', 'main.mio');
    assert.deepEqual(resolveBuildOutput(source, 'dist', workspace), {
      outputDirectory: path.join(workspace, 'dist'),
      outputPath: path.join(workspace, 'dist', 'main.mochi'),
    });
  });

  test('falls back to the source directory without a workspace', () => {
    const source = path.join(temporaryRoot, 'src', 'main.mio');
    assert.deepEqual(resolveBuildOutput(source, 'dist', undefined), {
      outputDirectory: path.join(temporaryRoot, 'src', 'dist'),
      outputPath: path.join(temporaryRoot, 'src', 'dist', 'main.mochi'),
    });
  });

  test('expands a leading home directory', () => {
    const home = path.join(temporaryRoot, 'home');
    assert.equal(expandHome('~/bin/mio', home), path.join(home, 'bin/mio'));
    assert.equal(expandHome('~', home), home);
    assert.equal(expandHome('/absolute/mio', home), '/absolute/mio');
  });

  test('compares normalized paths', () => {
    assert.equal(
      samePath('/project/a/../a/mio', '/project/a/mio'),
      true,
    );
    assert.equal(samePath('/project/a/mio', '/project/b/mio'), false);
  });
});
