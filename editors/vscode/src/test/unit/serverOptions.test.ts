import * as assert from 'node:assert/strict';
import { buildServerLaunch } from '../../lsp/serverOptions';

suite('language server launch options', () => {
  test('starts the mio lsp subcommand', () => {
    const launch = buildServerLaunch({
      executable: '/usr/local/bin/mio',
      baseEnv: {},
    });
    assert.equal(launch.command, '/usr/local/bin/mio');
    assert.deepEqual(launch.args, ['lsp']);
  });

  test('sets the workspace cwd and MIO_BUNKO', () => {
    const launch = buildServerLaunch({
      executable: '/opt/mio',
      workspaceFolder: '/work/project',
      bunkoRoot: '/work/project/bunko',
      baseEnv: { PATH: '/bin', MIO_BUNKO: '/old' },
    });
    assert.equal(launch.options.cwd, '/work/project');
    assert.equal(launch.options.env?.PATH, '/bin');
    assert.equal(launch.options.env?.MIO_BUNKO, '/work/project/bunko');
    assert.deepEqual(launch.args, ['lsp']);
  });

  test('inherits the environment when no bunko root is configured', () => {
    const launch = buildServerLaunch({
      executable: 'mio',
      baseEnv: { PATH: '/bin' },
    });
    assert.equal(launch.options.cwd, undefined);
    assert.equal(launch.options.env?.PATH, '/bin');
    assert.equal('MIO_BUNKO' in (launch.options.env ?? {}), false);
  });

  test('ignores empty workspace and bunko values', () => {
    const launch = buildServerLaunch({
      executable: 'mio',
      workspaceFolder: '',
      bunkoRoot: '',
      baseEnv: {},
    });
    assert.equal(launch.options.cwd, undefined);
    assert.equal('MIO_BUNKO' in (launch.options.env ?? {}), false);
  });
});
