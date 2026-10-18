import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';
import type { MioExtensionApi } from '../../extension';

const extensionId = 'exmikupro.mio-language-support';

const expectedCommands = [
  'mio.checkCurrentFile',
  'mio.runCurrentFile',
  'mio.buildCurrentFile',
  'mio.showCompilerVersion',
  'mio.openOutput',
];

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, milliseconds);
  });
}

function workspaceRoot(): string {
  const folder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  if (folder === undefined) {
    throw new Error('test workspace is not open');
  }
  return folder;
}

async function setExecutablePath(value: string | undefined): Promise<void> {
  await vscode.workspace
    .getConfiguration('mio')
    .update(
      'executablePath',
      value,
      vscode.ConfigurationTarget.Global,
    );
  await delay(150);
}

async function openDocument(
  filePath: string,
): Promise<vscode.TextDocument> {
  const document = await vscode.workspace.openTextDocument(
    vscode.Uri.file(filePath),
  );
  await vscode.window.showTextDocument(document, { preview: false });
  return document;
}

async function closeAllEditors(): Promise<void> {
  await vscode.commands.executeCommand('workbench.action.closeAllEditors');
  await delay(150);
}

function testCliPath(): string | undefined {
  const fromEnvironment = process.env.MIO_TEST_CLI;
  const candidate =
    fromEnvironment ??
    path.resolve(
      __dirname,
      '../../../.test-tmp',
      process.platform === 'win32' ? 'mio.exe' : 'mio',
    );
  return fs.existsSync(candidate) ? candidate : undefined;
}

suite('Mio extension host', () => {
  let extension: vscode.Extension<unknown>;
  let api: MioExtensionApi;

  suiteSetup(async () => {
    const found = vscode.extensions.getExtension(extensionId);
    assert.ok(found, `extension ${extensionId} was not found`);
    extension = found;
    await extension.activate();
    assert.equal(extension.isActive, true);
    api = extension.exports as MioExtensionApi;
    assert.ok(api, 'extension API was not returned');
    await setExecutablePath(undefined);
  });

  suiteTeardown(async () => {
    await setExecutablePath(undefined);
    await closeAllEditors();
  });

  test('activates and registers all Mio commands', async () => {
    assert.equal(extension.isActive, true);
    const commands = await vscode.commands.getCommands(true);
    for (const command of expectedCommands) {
      assert.ok(
        commands.includes(command),
        `missing command ${command}`,
      );
    }
  });

  test('opens .mio files with the Mio language id', async () => {
    const document = await openDocument(
      path.join(workspaceRoot(), 'sample.mio'),
    );
    assert.equal(document.languageId, 'mio');
    assert.equal(document.uri.fsPath.endsWith('.mio'), true);
  });

  test('contributes usable Mio snippets', async () => {
    const document = await vscode.workspace.openTextDocument({
      language: 'mio',
      content: '',
    });
    await vscode.window.showTextDocument(document, { preview: false });
    const list = await vscode.commands.executeCommand<vscode.CompletionList>(
      'vscode.executeCompletionItemProvider',
      document.uri,
      new vscode.Position(0, 0),
    );
    const labels = new Set(
      (list?.items ?? []).map((item) =>
        typeof item.label === 'string' ? item.label : item.label.label,
      ),
    );
    for (const prefix of [
      'kaku',
      'kaku-typed',
      'waza',
      'moshi',
      'moshi-sore',
      'aida',
      'kaesu',
      'yobu',
      'kata',
      'sentaku',
      'awase',
      'array',
      'map',
    ]) {
      assert.ok(labels.has(prefix), `missing snippet prefix: ${prefix}`);
    }
  });

  test('provides syntax-based folding ranges', async () => {
    const document = await openDocument(
      path.join(workspaceRoot(), 'sample.mio'),
    );
    const ranges = await vscode.commands.executeCommand<
      vscode.FoldingRange[] | undefined
    >('vscode.executeFoldingRangeProvider', document.uri);
    assert.ok(ranges !== undefined && ranges.length > 0);
    assert.ok(
      ranges.some((range) => range.start === 0),
      `expected a fold starting at line 0, got ${JSON.stringify(ranges)}`,
    );
  });

  test('opens the Mio output channel', async () => {
    await vscode.commands.executeCommand('mio.openOutput');
  });

  test('does not crash when the Mio CLI is missing', async () => {
    const missing = path.join(
      os.tmpdir(),
      'mio missing 中文 日本語',
      process.platform === 'win32' ? 'mio.exe' : 'mio',
    );
    await setExecutablePath(missing);
    const document = await openDocument(
      path.join(workspaceRoot(), 'sample.mio'),
    );
    try {
      await vscode.commands.executeCommand('mio.checkCurrentFile');
      assert.equal(extension.isActive, true);
      assert.equal(document.languageId, 'mio');
      assert.equal(
        vscode.languages.getDiagnostics(document.uri).length,
        0,
      );
    } finally {
      await setExecutablePath(undefined);
    }
  });

  test('shows real CLI diagnostics for a path with spaces and Unicode', async function () {
    const cli = testCliPath();
    if (cli === undefined) {
      this.skip();
      return;
    }

    const temporaryRoot = fs.mkdtempSync(
      path.join(os.tmpdir(), 'mio diag 中文 日本語-'),
    );
    const copiedCli = path.join(
      temporaryRoot,
      process.platform === 'win32' ? 'mio.exe' : 'mio',
    );
    fs.copyFileSync(cli, copiedCli);
    if (process.platform !== 'win32') {
      fs.chmodSync(copiedCli, 0o755);
    }

    const sourcePath = path.join(temporaryRoot, '集合 テスト.mio');
    fs.writeFileSync(
      sourcePath,
      [
        'waza main() -> int64 {',
        '    kaku message = "こんにちは, Mio!";',
        '    kaesu undefined_name + 1;',
        '}',
        '',
      ].join('\n'),
      'utf8',
    );

    await setExecutablePath(copiedCli);
    const document = await openDocument(sourcePath);

    try {
      await vscode.commands.executeCommand('mio.checkCurrentFile');
      const diagnostics = vscode.languages.getDiagnostics(document.uri);
      assert.ok(diagnostics.length > 0, 'expected at least one diagnostic');

      const diagnostic = diagnostics.find(
        (item) => item.source === 'mio' && String(item.code) === 'E0201',
      );
      assert.ok(
        diagnostic,
        `expected E0201, got ${diagnostics
          .map((item) => `${item.source ?? ''}/${String(item.code)}/${item.message}`)
          .join('; ')}`,
      );
      assert.equal(diagnostic.range.start.line, 2);
      assert.equal(diagnostic.range.start.character, 10);
      assert.equal(diagnostic.range.end.line, 2);
      assert.equal(diagnostic.range.end.character, 24);
      assert.match(diagnostic.message, /undefined_name/u);
    } finally {
      // VS Code 测试宿主会保留 openTextDocument 打开的文档，因此直接调用
      // 与 onDidClose 相同的清理路径，验证诊断资源被释放。
      await closeAllEditors();
      await delay(200);
      api.clearDiagnostics(document.uri);
      assert.equal(
        vscode.languages.getDiagnostics(document.uri).length,
        0,
        'the cleanup path should clear diagnostics',
      );
      await setExecutablePath(undefined);
      fs.rmSync(temporaryRoot, { recursive: true, force: true });
    }
  });

  test('disposes diagnostics and process resources', async () => {
    api.dispose();
    await delay(100);
    const remaining = vscode.languages
      .getDiagnostics()
      .filter(([, diagnostics]) =>
        diagnostics.some((diagnostic) => diagnostic.source === 'mio'),
      );
    assert.equal(
      remaining.length,
      0,
      'dispose should clear all Mio diagnostics',
    );
  });
});
