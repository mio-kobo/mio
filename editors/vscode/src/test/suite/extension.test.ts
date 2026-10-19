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

async function setLanguageServerEnabled(value: boolean): Promise<void> {
  await vscode.workspace
    .getConfiguration('mio')
    .update(
      'languageServer.enabled',
      value,
      vscode.ConfigurationTarget.Global,
    );
  await delay(200);
}

async function waitFor<T>(
  description: string,
  check: () => T | undefined | false | Promise<T | undefined | false>,
  timeoutMs = 30_000,
): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await check();
    if (value !== undefined && value !== false) {
      return value;
    }
    if (Date.now() > deadline) {
      throw new Error(`timed out waiting for ${description}`);
    }
    await delay(100);
  }
}


function hoverText(hovers: vscode.Hover[] | undefined): string {
  const values: string[] = [];
  for (const hover of hovers ?? []) {
    for (const content of hover.contents) {
      values.push(typeof content === 'string' ? content : content.value);
    }
  }
  return values.join('\n');
}

function documentOffset(
  document: vscode.TextDocument,
  needle: string,
  delta = 0,
): vscode.Position {
  const index = document.getText().indexOf(needle);
  assert.ok(index >= 0, `missing ${needle}`);
  return document.positionAt(index + delta);
}

function completionLabels(
  list: vscode.CompletionList | vscode.CompletionItem[] | undefined,
): string[] {
  const items = Array.isArray(list) ? list : (list?.items ?? []);
  return items.map((item) =>
    typeof item.label === 'string' ? item.label : item.label.label,
  );
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

    await setLanguageServerEnabled(false);
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
      await setLanguageServerEnabled(true);
      fs.rmSync(temporaryRoot, { recursive: true, force: true });
    }
  });

  test('starts the MioCore language server and publishes live diagnostics', async function () {
    const cli = testCliPath();
    if (cli === undefined) {
      this.skip();
      return;
    }
    await setLanguageServerEnabled(true);
    await setExecutablePath(cli);
    await waitFor(
      'language server ready',
      () => api.getLanguageServerState() === 'ready',
    );
    assert.equal(api.getDiagnosticsOwner(), 'lsp');

    const temporaryRoot = fs.mkdtempSync(
      path.join(os.tmpdir(), 'mio lsp live-'),
    );
    const sourcePath = path.join(temporaryRoot, 'live.mio');
    fs.writeFileSync(
      sourcePath,
      [
        'waza broken() -> int64 {',
        '    kaesu missing_value;',
        '}',
        '',
      ].join('\n'),
      'utf8',
    );
    try {
      const document = await openDocument(sourcePath);
      const diagnostic = await waitFor('language server E0201', () =>
        vscode.languages
          .getDiagnostics(document.uri)
          .find(
            (item) => item.source === 'mio' && String(item.code) === 'E0201',
          ),
      );
      assert.equal(diagnostic.range.start.line, 1);
      assert.equal(diagnostic.range.start.character, 10);
      assert.match(diagnostic.message, /missing_value/u);

      const editor = vscode.window.activeTextEditor;
      assert.ok(editor, 'the test document should be active');
      const fixed = [
        'waza broken() -> int64 {',
        '    kaesu 1;',
        '}',
        '',
      ].join('\n');
      await editor.edit((builder) => {
        builder.replace(
          new vscode.Range(
            new vscode.Position(0, 0),
            document.positionAt(document.getText().length),
          ),
          fixed,
        );
      });
      await waitFor('language server diagnostics cleared', () => {
        const remaining = vscode.languages
          .getDiagnostics(document.uri)
          .filter((item) => item.source === 'mio');
        return remaining.length === 0;
      });
      await document.save();
    } finally {
      await closeAllEditors();
      fs.rmSync(temporaryRoot, { recursive: true, force: true });
    }
  });

  test('falls back to CLI diagnostics when the language server is disabled', async function () {
    const cli = testCliPath();
    if (cli === undefined) {
      this.skip();
      return;
    }
    const temporaryRoot = fs.mkdtempSync(
      path.join(os.tmpdir(), 'mio lsp fallback-'),
    );
    const sourcePath = path.join(temporaryRoot, 'fallback.mio');
    fs.writeFileSync(
      sourcePath,
      [
        'waza broken() -> int64 {',
        '    kaesu missing_value;',
        '}',
        '',
      ].join('\n'),
      'utf8',
    );

    try {
      await setExecutablePath(cli);
      const document = await openDocument(sourcePath);
      await setLanguageServerEnabled(false);
      await waitFor(
        'language server disabled',
        () => api.getLanguageServerState() === 'disabled',
      );
      assert.equal(api.getDiagnosticsOwner(), 'cli');

      const diagnostic = await waitFor('CLI fallback E0201', () =>
        vscode.languages
          .getDiagnostics(document.uri)
          .find(
            (item) => item.source === 'mio' && String(item.code) === 'E0201',
          ),
      );
      assert.ok(diagnostic);

      await vscode.commands.executeCommand('mio.checkCurrentFile');
      await delay(500);
      const ours = vscode.languages
        .getDiagnostics(document.uri)
        .filter((item) => item.source === 'mio');
      assert.equal(
        ours.length,
        1,
        `expected exactly one Mio diagnostic, got ${ours.length}`,
      );

      await setLanguageServerEnabled(true);
      await waitFor(
        'language server ready again',
        () => api.getLanguageServerState() === 'ready',
      );
    } finally {
      await closeAllEditors();
      fs.rmSync(temporaryRoot, { recursive: true, force: true });
    }
  });


  test('provides semantic navigation through the language server', async function () {
    const cli = testCliPath();
    if (cli === undefined) {
      this.skip();
      return;
    }
    await setLanguageServerEnabled(true);
    await setExecutablePath(cli);
    await waitFor(
      'language server ready',
      () => api.getLanguageServerState() === 'ready',
    );

    const temporaryRoot = fs.mkdtempSync(
      path.join(os.tmpdir(), 'mio lsp semantic-'),
    );
    const sourcePath = path.join(temporaryRoot, 'semantic.mio');
    const source = [
      'kata Point {',
      '    x: int64;',
      '    y: int64;',
      '}',
      '',
      'sentaku Color {',
      '    Red;',
      '    Green;',
      '}',
      '',
      '// 两个数相加。',
      'waza add(a: int64, b: int64) -> int64 {',
      '    kaku sum = a + b;',
      '    kaesu sum;',
      '}',
      '',
      'waza main() -> int64 {',
      '    kaku point = Point { x: 1, y: 2 };',
      '    kaku color: Color = Color.Red();',
      '    kaesu add(point.x, point.y);',
      '}',
      '',
    ].join('\n');
    fs.writeFileSync(sourcePath, source, 'utf8');

    try {
      const document = await openDocument(sourcePath);
      const sumUse = documentOffset(document, 'kaesu sum;', 'kaesu '.length);

      const hover = await waitFor('semantic hover', async () => {
        const hovers = await vscode.commands.executeCommand<vscode.Hover[]>(
          'vscode.executeHoverProvider',
          document.uri,
          sumUse,
        );
        const text = hoverText(hovers);
        return text.includes('kaku sum') && text.includes('int64')
          ? text
          : undefined;
      });
      assert.ok(hover.length > 0);

      const definitions = await waitFor('semantic definition', async () => {
        const result = await vscode.commands.executeCommand<vscode.Location[]>(
          'vscode.executeDefinitionProvider',
          document.uri,
          sumUse,
        );
        return result !== undefined && result.length > 0 ? result : undefined;
      });
      assert.equal(definitions[0]?.range.start.line, 12);

      const references = await waitFor('semantic references', async () => {
        const result = await vscode.commands.executeCommand<vscode.Location[]>(
          'vscode.executeReferenceProvider',
          document.uri,
          sumUse,
        );
        return result !== undefined && result.length >= 2 ? result : undefined;
      });
      assert.ok(references.length >= 2);

      const symbols = await waitFor('semantic document symbols', async () => {
        const result = await vscode.commands.executeCommand<
          vscode.DocumentSymbol[]
        >('vscode.executeDocumentSymbolProvider', document.uri);
        if (result === undefined) {
          return undefined;
        }
        const names = new Set(result.map((symbol) => symbol.name));
        const complete = ['Point', 'Color', 'add', 'main'].every((name) =>
          names.has(name),
        );
        return complete ? result : undefined;
      });
      const point = symbols.find((symbol) => symbol.name === 'Point');
      assert.ok(point);
      assert.deepEqual(
        (point?.children ?? []).map((child) => child.name),
        ['x', 'y'],
      );

      const memberPosition = documentOffset(
        document,
        'point.x',
        'point.'.length,
      );
      const completions = await waitFor('semantic completion', async () => {
        const list = await vscode.commands.executeCommand<vscode.CompletionList>(
          'vscode.executeCompletionItemProvider',
          document.uri,
          memberPosition,
        );
        const labels = completionLabels(list);
        return labels.includes('x') && labels.includes('y')
          ? labels
          : undefined;
      });
      assert.ok(completions.includes('x'));

      const signaturePosition = documentOffset(
        document,
        'add(point.x',
        'add('.length,
      );
      const signature = await waitFor('semantic signature help', async () => {
        const help = await vscode.commands.executeCommand<vscode.SignatureHelp>(
          'vscode.executeSignatureHelpProvider',
          document.uri,
          signaturePosition,
          '(',
        );
        if (help === undefined || help.signatures.length === 0) {
          return undefined;
        }
        return help.signatures[0]?.label.includes('add') ? help : undefined;
      });
      assert.equal(signature.activeParameter, 0);

      // 文档修改后语义结果必须更新。
      const editor = vscode.window.activeTextEditor;
      assert.ok(editor);
      const declarationLine = documentOffset(document, 'kaku sum = a + b;');
      await editor.edit((builder) => {
        builder.replace(
          new vscode.Range(
            declarationLine.line,
            0,
            declarationLine.line,
            'kaku sum = a + b;'.length,
          ),
          '    kaku sum = "text";',
        );
      });
      const updated = await waitFor('updated semantic hover', async () => {
        const hovers = await vscode.commands.executeCommand<vscode.Hover[]>(
          'vscode.executeHoverProvider',
          document.uri,
          sumUse,
        );
        const text = hoverText(hovers);
        return text.includes('kaku sum') && text.includes('string')
          ? text
          : undefined;
      });
      assert.ok(updated.includes('string'));
    } finally {
      await closeAllEditors();
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
