import { spawnSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const pluginRoot = path.resolve(here, '..');
const repoRoot = path.resolve(pluginRoot, '..', '..');
const outputDirectory = path.join(pluginRoot, '.test-tmp');
const executableName = process.platform === 'win32' ? 'mio.exe' : 'mio';
const outputPath = path.join(outputDirectory, executableName);
const markerPath = path.join(outputDirectory, 'NO_CLI');

fs.mkdirSync(outputDirectory, { recursive: true });
fs.rmSync(markerPath, { force: true });

function findGo() {
  const candidates = [
    process.env.GO,
    'go',
    '/usr/local/go/bin/go',
    '/opt/homebrew/bin/go',
    'C:\\Program Files\\Go\\bin\\go.exe',
  ].filter((candidate) => typeof candidate === 'string' && candidate !== '');

  for (const candidate of candidates) {
    const probe = spawnSync(candidate, ['version'], {
      stdio: 'ignore',
      shell: false,
    });
    if (probe.status === 0) {
      return candidate;
    }
  }
  return undefined;
}

const go = findGo();
if (go === undefined) {
  fs.writeFileSync(
    markerPath,
    'Go was not found; real-CLI integration tests will be skipped.\n',
    'utf8',
  );
  console.warn(
    '[build-test-cli] go was not found; real-CLI integration tests will be skipped.',
  );
  process.exit(0);
}

console.log(`[build-test-cli] building ${outputPath}`);
const build = spawnSync(
  go,
  ['build', '-o', outputPath, './cmd/mio'],
  {
    cwd: repoRoot,
    stdio: 'inherit',
    shell: false,
    env: process.env,
  },
);

if (build.error !== undefined) {
  console.error(`[build-test-cli] ${build.error.message}`);
  process.exit(1);
}
if (build.status !== 0) {
  console.error(
    `[build-test-cli] go build exited with code ${build.status ?? 'unknown'}`,
  );
  process.exit(build.status ?? 1);
}

const version = spawnSync(outputPath, ['version'], {
  encoding: 'utf8',
  shell: false,
});
if (version.status !== 0) {
  console.error(
    `[build-test-cli] built CLI failed to run: ${version.stderr ?? ''}`,
  );
  process.exit(1);
}
console.log(`[build-test-cli] ${version.stdout.trim()}`);
