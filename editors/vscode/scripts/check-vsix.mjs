import AdmZip from 'adm-zip';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const pluginRoot = path.resolve(here, '..');

const requiredEntries = [
  'extension/package.json',
  'extension/readme.md',
  'extension/README.zh-CN.md',
  'extension/LICENSE.txt',
  'extension/NOTICE.md',
  'extension/icon.png',
  'extension/docs/images/syntax-preview-dark.png',
  'extension/docs/images/syntax-preview-light.png',
  'extension/language-configuration.json',
  'extension/syntaxes/mio.tmLanguage.json',
  'extension/snippets/mio.json',
  'extension/package.nls.json',
  'extension/package.nls.zh-cn.json',
  'extension/out/extension.js',
  'extension/changelog.md',
];

const forbiddenPrefixes = [
  'extension/node_modules/',
  'extension/src/',
  'extension/out/test/',
  'extension/test-fixtures/',
  'extension/scripts/',
  'extension/.vscode-test/',
  'extension/.test-tmp/',
];

function newestVsix() {
  const candidates = fs
    .readdirSync(pluginRoot)
    .filter((name) => name.endsWith('.vsix'))
    .map((name) => path.join(pluginRoot, name))
    .map((file) => ({ file, modified: fs.statSync(file).mtimeMs }))
    .sort((left, right) => right.modified - left.modified);
  return candidates[0]?.file;
}

function readZip(vsix) {
  try {
    return new AdmZip(vsix);
  } catch (error) {
    console.error(
      `[check-vsix] unable to read ${path.basename(vsix)}: ${
        error instanceof Error ? error.message : String(error)
      }`,
    );
    process.exit(1);
  }
}

const vsix = newestVsix();
if (vsix === undefined) {
  console.error(
    '[check-vsix] no .vsix file found. Run `npx vsce package` first.',
  );
  process.exit(1);
}

const zip = readZip(vsix);
const entries = zip
  .getEntries()
  .map((entry) => entry.entryName)
  .filter((entry) => entry.startsWith('extension/'));
const entrySet = new Set(entries);
const missing = requiredEntries.filter((entry) => !entrySet.has(entry));
const forbidden = entries.filter((entry) =>
  forbiddenPrefixes.some((prefix) => entry.startsWith(prefix)),
);

if (missing.length > 0) {
  console.error('[check-vsix] missing required entries:');
  for (const entry of missing) {
    console.error(`  - ${entry}`);
  }
}
if (forbidden.length > 0) {
  console.error('[check-vsix] forbidden entries were packaged:');
  for (const entry of forbidden) {
    console.error(`  - ${entry}`);
  }
}

let manifest;
try {
  manifest = JSON.parse(zip.readAsText('extension/package.json'));
} catch (error) {
  console.error(
    `[check-vsix] packaged package.json is invalid: ${
      error instanceof Error ? error.message : String(error)
    }`,
  );
  process.exit(1);
}

const manifestProblems = [];
if (manifest.name !== 'mio-language-support') {
  manifestProblems.push('name is not mio-language-support');
}
if (manifest.displayName !== 'Mio Language Support') {
  manifestProblems.push('displayName is not Mio Language Support');
}
if (typeof manifest.version !== 'string' || manifest.version === '') {
  manifestProblems.push('version is missing');
}
if (manifest.main !== './out/extension.js') {
  manifestProblems.push('main is not ./out/extension.js');
}
if (typeof manifest.engines?.vscode !== 'string') {
  manifestProblems.push('engines.vscode is missing');
}
if (manifest.icon !== 'icon.png') {
  manifestProblems.push('icon is not icon.png');
}
for (const key of [
  'languages',
  'grammars',
  'snippets',
  'commands',
  'configuration',
]) {
  if (manifest.contributes?.[key] === undefined) {
    manifestProblems.push(`contributes.${key} is missing`);
  }
}
if (manifest.repository === undefined) {
  manifestProblems.push('repository is missing');
}
if (manifest.license !== 'MIT') {
  manifestProblems.push('license is not MIT');
}

if (manifestProblems.length > 0) {
  console.error('[check-vsix] package.json problems:');
  for (const problem of manifestProblems) {
    console.error(`  - ${problem}`);
  }
}

if (
  missing.length > 0 ||
  forbidden.length > 0 ||
  manifestProblems.length > 0
) {
  process.exit(1);
}

console.log(
  `[check-vsix] OK: ${path.basename(vsix)} (${entries.length} entries)`,
);
console.log(
  `[check-vsix] ${manifest.displayName} ${manifest.version}, engines.vscode ${manifest.engines.vscode}`,
);
