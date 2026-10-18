import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';

interface PackageManifest {
  name?: string;
  displayName?: string;
  version?: string;
  publisher?: string;
  engines?: { vscode?: string };
  main?: string;
  categories?: string[];
  keywords?: string[];
  repository?: { url?: string; directory?: string };
  license?: string;
  icon?: string;
  contributes?: {
    languages?: Array<{
      id?: string;
      extensions?: string[];
      configuration?: string;
      icon?: string;
    }>;
    grammars?: Array<{
      language?: string;
      scopeName?: string;
      path?: string;
    }>;
    snippets?: Array<{ language?: string; path?: string }>;
    commands?: Array<{ command?: string; title?: string }>;
    configuration?: {
      title?: string;
      properties?: Record<string, unknown>;
    };
  };
}

interface LanguageConfiguration {
  comments?: {
    lineComment?: string;
    blockComment?: [string, string];
  };
  brackets?: string[][];
  autoClosingPairs?: Array<{
    open?: string;
    close?: string;
    notIn?: string[];
  }>;
  surroundingPairs?: string[][];
  indentationRules?: {
    increaseIndentPattern?: string;
    decreaseIndentPattern?: string;
  };
  folding?: {
    offSide?: boolean;
    markers?: { start?: string; end?: string };
  };
  wordPattern?: string;
}

interface Snippet {
  prefix?: string | string[];
  body?: string[];
  description?: string;
}

interface Grammar {
  scopeName?: string;
  patterns?: Array<{ include?: string }>;
  repository?: Record<string, unknown>;
}

const pluginRoot = path.resolve(__dirname, '../../..');

function readText(relativePath: string): string {
  return fs.readFileSync(path.join(pluginRoot, relativePath), 'utf8');
}

function readJson<T>(relativePath: string): T {
  return JSON.parse(readText(relativePath)) as T;
}

function collectNlsKeys(value: unknown, keys: Set<string>): void {
  if (typeof value === 'string') {
    const match = /^%(.+)%$/u.exec(value);
    if (match?.[1] !== undefined) {
      keys.add(match[1]);
    }
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      collectNlsKeys(item, keys);
    }
    return;
  }
  if (value !== null && typeof value === 'object') {
    for (const item of Object.values(value)) {
      collectNlsKeys(item, keys);
    }
  }
}

suite('extension manifest', () => {
  const manifest = readJson<PackageManifest>('package.json');
  const languageConfiguration = readJson<LanguageConfiguration>(
    'language-configuration.json',
  );
  const snippets = readJson<Record<string, Snippet>>('snippets/mio.json');
  const grammar = readJson<Grammar>('syntaxes/mio.tmLanguage.json');

  test('declares the Marketplace metadata', () => {
    assert.equal(manifest.name, 'mio-language-support');
    assert.equal(manifest.displayName, 'Mio Language Support');
    assert.match(manifest.version ?? '', /^\d+\.\d+\.\d+$/u);
    assert.equal(typeof manifest.publisher, 'string');
    assert.notEqual(manifest.publisher, '');
    assert.match(manifest.engines?.vscode ?? '', /^\^?\d+\.\d+\.\d+$/u);
    assert.equal(manifest.main, './out/extension.js');
    assert.equal(manifest.license, 'MIT');
    assert.equal(manifest.icon, 'icon.png');
    assert.ok(manifest.categories?.includes('Programming Languages'));
    assert.ok(manifest.keywords?.includes('mio'));
    assert.match(
      manifest.repository?.url ?? '',
      /github\.com\/ExMikuPro\/Mio/u,
    );
  });

  test('contributes the Mio language, grammar, and snippets', () => {
    const language = manifest.contributes?.languages?.find(
      (entry) => entry.id === 'mio',
    );
    assert.ok(language);
    assert.deepEqual(language?.extensions, ['.mio']);
    assert.equal(language?.configuration, './language-configuration.json');
    assert.ok(
      fs.existsSync(
        path.join(pluginRoot, language?.configuration ?? ''),
      ),
    );

    const grammarEntry = manifest.contributes?.grammars?.find(
      (entry) => entry.language === 'mio',
    );
    assert.ok(grammarEntry);
    assert.equal(grammarEntry?.scopeName, 'source.mio');
    assert.ok(
      fs.existsSync(path.join(pluginRoot, grammarEntry?.path ?? '')),
    );

    const snippetEntry = manifest.contributes?.snippets?.find(
      (entry) => entry.language === 'mio',
    );
    assert.ok(snippetEntry);
    assert.ok(fs.existsSync(path.join(pluginRoot, snippetEntry?.path ?? '')));
  });

  test('contributes exactly the five requested commands', () => {
    const commands = new Set(
      (manifest.contributes?.commands ?? []).map((entry) => entry.command),
    );
    assert.deepEqual([...commands].sort(), [
      'mio.buildCurrentFile',
      'mio.checkCurrentFile',
      'mio.openOutput',
      'mio.runCurrentFile',
      'mio.showCompilerVersion',
    ]);
  });

  test('contributes the four supported settings and no fake version switch', () => {
    const properties =
      manifest.contributes?.configuration?.properties ?? {};
    assert.deepEqual(Object.keys(properties).sort(), [
      'mio.diagnostics.enabled',
      'mio.diagnostics.onSave',
      'mio.executablePath',
      'mio.outputDirectory',
    ]);
    assert.equal('mio.languageVersion' in properties, false);
  });

  test('resolves every NLS placeholder in both locale files', () => {
    const keys = new Set<string>();
    collectNlsKeys(manifest, keys);
    assert.ok(keys.size > 0);
    const english = readJson<Record<string, string>>('package.nls.json');
    const chinese = readJson<Record<string, string>>(
      'package.nls.zh-cn.json',
    );
    for (const key of keys) {
      assert.equal(typeof english[key], 'string', `missing en NLS key ${key}`);
      assert.equal(typeof chinese[key], 'string', `missing zh NLS key ${key}`);
    }
  });

  test('configures comments, brackets, quotes, and surrounding pairs', () => {
    assert.equal(languageConfiguration.comments?.lineComment, '//');
    assert.deepEqual(languageConfiguration.comments?.blockComment, [
      '/*',
      '*/',
    ]);
    const brackets = languageConfiguration.brackets ?? [];
    assert.deepEqual(brackets, [
      ['{', '}'],
      ['[', ']'],
      ['(', ')'],
    ]);

    const pairs = new Map(
      (languageConfiguration.autoClosingPairs ?? []).map((pair) => [
        pair.open,
        pair.close,
      ]),
    );
    assert.equal(pairs.get('('), ')');
    assert.equal(pairs.get('['), ']');
    assert.equal(pairs.get('{'), '}');
    assert.equal(pairs.get('"'), '"');
    assert.equal(pairs.get("'"), "'");
    for (const pair of languageConfiguration.autoClosingPairs ?? []) {
      assert.deepEqual(pair.notIn, ['string', 'comment']);
    }

    const surrounding = new Map(
      (languageConfiguration.surroundingPairs ?? []).map((pair) => [
        pair[0],
        pair[1],
      ]),
    );
    assert.equal(surrounding.get('('), ')');
    assert.equal(surrounding.get('"'), '"');
  });

  test('uses valid indentation rules for Mio blocks', () => {
    const increase = new RegExp(
      languageConfiguration.indentationRules?.increaseIndentPattern ?? '',
    );
    const decrease = new RegExp(
      languageConfiguration.indentationRules?.decreaseIndentPattern ?? '',
    );
    assert.equal(increase.test('waza add(a: int64) -> int64 {'), true);
    assert.equal(increase.test('moshi (a > b) {'), true);
    assert.equal(increase.test('kaku values = [1,'), true);
    assert.equal(increase.test('kaku x = 1;'), false);
    assert.equal(decrease.test('}'), true);
    assert.equal(decrease.test('sore {'), true);
    assert.equal(decrease.test('baai Ok(value) {'), false);
  });

  test('provides folding markers and a Mio word pattern', () => {
    assert.equal(languageConfiguration.folding?.offSide, false);
    assert.equal(
      new RegExp(
        languageConfiguration.folding?.markers?.start ?? '',
      ).test('// region helpers'),
      true,
    );
    assert.equal(
      new RegExp(
        languageConfiguration.folding?.markers?.end ?? '',
      ).test('// endregion'),
      true,
    );

    const word = new RegExp(
      `^(?:${languageConfiguration.wordPattern ?? ''})$`,
      'u',
    );
    assert.equal(word.test('name'), true);
    assert.equal(word.test('_name'), true);
    assert.equal(word.test('123'), true);
    assert.equal(word.test('你好'), false);
  });

  test('ships valid Mio snippets with the expected coverage', () => {
    const expected = [
      'Variable declaration',
      'Function',
      'Conditional',
      'Conditional with else',
      'While loop',
      'Return',
      'Import package',
      'Struct',
      'Enum',
      'Exhaustive match',
      'Array literal',
      'Map',
      'Generic function',
      'Generic struct',
      'Generic enum with data',
    ];
    for (const name of expected) {
      assert.ok(snippets[name], `missing snippet: ${name}`);
    }

    const forbidden =
      /\b(if|else|while|for|return|struct|enum|match|case|import|export|const|let|function|true|false|nil)\b/u;
    for (const [name, snippet] of Object.entries(snippets)) {
      const prefixList = Array.isArray(snippet.prefix)
        ? snippet.prefix
        : [snippet.prefix];
      assert.ok(
        prefixList.length > 0 && prefixList.every((prefix) => typeof prefix === 'string' && prefix !== ''),
        `invalid prefix in ${name}`,
      );
      assert.ok(
        Array.isArray(snippet.body) && snippet.body.length > 0,
        `invalid body in ${name}`,
      );
      assert.equal(typeof snippet.description, 'string', `missing description in ${name}`);
      const body = (snippet.body ?? []).join('\n');
      const match = forbidden.exec(body);
      assert.equal(
        match,
        null,
        `non-Mio keyword "${match?.[0] ?? ''}" in snippet ${name}`,
      );
    }
  });

  test('ships a TextMate grammar for source.mio', () => {
    assert.equal(grammar.scopeName, 'source.mio');
    const includes = new Set(
      (grammar.patterns ?? []).map((pattern) => pattern.include),
    );
    for (const include of ['#comments', '#keywords', '#strings', '#types']) {
      assert.ok(includes.has(include), `missing ${include}`);
    }

    const repository = grammar.repository ?? {};
    for (const key of [
      'comments',
      'import',
      'struct-declaration',
      'enum-declaration',
      'function-signature',
      'variable-declaration',
      'constant-declaration',
      'type-parameter-list',
      'generic-type-application',
      'generic-call-type-arguments',
      'generic-type-brackets',
      'strings',
      'runes',
      'keywords',
      'literals',
      'types',
      'function-call',
      'property-access',
      'numbers',
      'operators',
      'punctuation',
    ]) {
      assert.ok(key in repository, `missing grammar repository entry ${key}`);
    }

    const serialized = JSON.stringify(grammar);
    for (const scope of [
      'keyword.control.mio',
      'keyword.declaration.variable.mio',
      'keyword.declaration.function.mio',
      'keyword.declaration.struct.mio',
      'keyword.declaration.enum.mio',
      'keyword.declaration.const.mio',
      'keyword.control.import.mio',
      'keyword.control.case.mio',
      'constant.language.boolean.mio',
      'constant.language.null.mio',
      'storage.type.primitive.mio',
      'support.type.mio',
      'entity.name.function.mio',
      'entity.name.type.struct.mio',
      'entity.name.type.enum.mio',
      'entity.name.type.enummember.mio',
      'variable.parameter.mio',
      'variable.other.definition.mio',
      'variable.other.member.mio',
      'variable.other.property.mio',
      'string.quoted.double.mio',
      'string.quoted.double.import.mio',
      'constant.character.rune.mio',
      'comment.line.double-slash.mio',
      'comment.block.mio',
      'constant.numeric.float.mio',
      'constant.numeric.integer.mio',
      'keyword.operator.arrow.mio',
      'punctuation.definition.typearguments.begin.mio',
      'punctuation.definition.typeparameters.begin.mio',
    ]) {
      assert.ok(serialized.includes(scope), `missing scope ${scope}`);
    }
  });

  test('keeps non-Mio keywords out of the keyword rules', () => {
    const keywords = grammar.repository?.keywords as
      | { patterns?: Array<{ name?: string; match?: string }> }
      | undefined;
    const regexes = (keywords?.patterns ?? [])
      .map((pattern) => pattern.match ?? '')
      .join(' ');
    for (const word of [
      'if',
      'else',
      'while',
      'for',
      'return',
      'struct',
      'enum',
      'match',
      'case',
      'import',
      'export',
      'const',
      'let',
      'function',
    ]) {
      assert.equal(
        new RegExp(`\\b${word}\\b`, 'u').test(regexes),
        false,
        `non-Mio keyword ${word} found in grammar keywords`,
      );
    }
  });

  test('ships a 128x128 PNG icon', () => {
    const icon = fs.readFileSync(path.join(pluginRoot, 'icon.png'));
    assert.deepEqual(
      [...icon.subarray(0, 8)],
      [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a],
    );
    const width = icon.readUInt32BE(16);
    const height = icon.readUInt32BE(20);
    assert.ok(width >= 128, `icon width ${width}`);
    assert.ok(height >= 128, `icon height ${height}`);
  });

  test('ships bilingual README, license, notice, and changelog', () => {
    const english = readText('README.md');
    const chinese = readText('README.zh-CN.md');
    for (const readme of [english, chinese]) {
      assert.match(readme, /Mio Language Support/u);
      assert.match(readme, /github\.com\/ExMikuPro\/Mio/u);
    }
    assert.match(english, /Known limitations/u);
    assert.match(chinese, /已知限制/u);
    assert.match(readText('LICENSE'), /MIT License/u);
    assert.match(readText('NOTICE.md'), /character artwork/iu);
    assert.match(readText('NOTICE.md'), /not bundled/iu);
    assert.match(readText('CHANGELOG.md'), /0\.1\.0/u);
    assert.match(readText('docs/LSP_PLAN.md'), /vscode-languageclient/u);
  });
});
