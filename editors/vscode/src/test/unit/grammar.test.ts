import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as oniguruma from 'vscode-oniguruma';
import {
  INITIAL,
  Registry,
  type IGrammar,
  type IToken,
} from 'vscode-textmate';

let grammar: IGrammar;

interface Token {
  startIndex: number;
  endIndex: number;
  scopes: string[];
}

setup(async () => {
  const wasmPath = path.resolve(
    __dirname,
    '../../../node_modules/vscode-oniguruma/release/onig.wasm',
  );
  await oniguruma.loadWASM(fs.readFileSync(wasmPath));

  const grammarPath = path.resolve(
    __dirname,
    '../../../syntaxes/mio.tmLanguage.json',
  );
  const registry = new Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (patterns: string[]) =>
        new oniguruma.OnigScanner(patterns),
      createOnigString: (value: string) =>
        new oniguruma.OnigString(value),
    }),
    loadGrammar: async (scopeName) => {
      if (scopeName !== 'source.mio') {
        return null;
      }
      return JSON.parse(fs.readFileSync(grammarPath, 'utf8'));
    },
  });

  const loaded = await registry.loadGrammar('source.mio');
  assert.ok(loaded, 'failed to load source.mio grammar');
  grammar = loaded;
});

function toToken(token: IToken): Token {
  return {
    startIndex: token.startIndex,
    endIndex: token.endIndex,
    scopes: token.scopes,
  };
}

function tokenizeLine(line: string): Token[] {
  assert.ok(grammar, 'grammar not loaded');
  return grammar.tokenizeLine(line, INITIAL).tokens.map(toToken);
}

function tokenizeLines(lines: string[]): Token[][] {
  assert.ok(grammar, 'grammar not loaded');
  let state = INITIAL;
  const result: Token[][] = [];
  for (const line of lines) {
    const tokenized = grammar.tokenizeLine(line, state);
    result.push(tokenized.tokens.map(toToken));
    state = tokenized.ruleStack;
  }
  return result;
}

function nthIndexOf(text: string, needle: string, occurrence: number): number {
  let index = -1;
  for (let count = 0; count <= occurrence; count += 1) {
    index = text.indexOf(needle, index + 1);
    if (index < 0) {
      return -1;
    }
  }
  return index;
}

function scopesAt(
  line: string,
  needle: string,
  occurrence = 0,
): string[] {
  const index = nthIndexOf(line, needle, occurrence);
  assert.ok(index >= 0, `needle not found: ${needle} in ${line}`);
  const token = tokenizeLine(line).find(
    (candidate) =>
      candidate.startIndex <= index && index < candidate.endIndex,
  );
  return token?.scopes ?? [];
}

function hasScope(scopes: string[], scope: string): boolean {
  return scopes.some(
    (candidate) =>
      candidate === scope || candidate.startsWith(`${scope}.`),
  );
}

function assertScope(
  line: string,
  needle: string,
  scope: string,
  occurrence = 0,
): void {
  const scopes = scopesAt(line, needle, occurrence);
  assert.ok(
    hasScope(scopes, scope),
    `"${needle}" should have ${scope}; got ${scopes.join(', ')} in: ${line}`,
  );
}

function assertNoScope(
  line: string,
  needle: string,
  scope: string,
  occurrence = 0,
): void {
  const scopes = scopesAt(line, needle, occurrence);
  assert.equal(
    hasScope(scopes, scope),
    false,
    `"${needle}" must not have ${scope}; got ${scopes.join(', ')} in: ${line}`,
  );
}

suite('Mio TextMate grammar', () => {
  test('highlights declarations and built-in types', () => {
    assertScope('kaku name = 1;', 'kaku', 'keyword.declaration.variable.mio');
    assertScope(
      'kaku name = 1;',
      'name',
      'variable.other.definition.mio',
    );
    assertScope(
      'waza add(a: int64, b: int64) -> int64 {',
      'waza',
      'keyword.declaration.function.mio',
    );
    assertScope(
      'waza add(a: int64, b: int64) -> int64 {',
      'add',
      'entity.name.function.mio',
    );
    assertScope(
      'waza add(a: int64, b: int64) -> int64 {',
      'a:',
      'variable.parameter.mio',
    );
    assertScope(
      'waza add(a: int64, b: int64) -> int64 {',
      'int64',
      'storage.type.primitive.mio',
    );
    assertScope(
      'waza add(a: int64, b: int64) -> int64 {',
      '->',
      'keyword.operator.arrow.mio',
    );
    assertScope(
      'koukai waza max(left: int64) -> int64 {',
      'koukai',
      'keyword.declaration.mio',
    );
    assertScope(
      'teisu answer: int64 = 42;',
      'teisu',
      'keyword.declaration.const.mio',
    );
    assertScope(
      'teisu answer: int64 = 42;',
      'answer',
      'variable.other.constant.mio',
    );
    assertScope(
      'kaku value: resource = handle;',
      'resource',
      'storage.type.primitive.mio',
    );
    assertScope(
      'kaku value: float32 = 1.5;',
      'float32',
      'storage.type.primitive.mio',
    );
  });

  test('highlights control keywords, booleans, and null', () => {
    const cases: Array<[string, string, string]> = [
      ['moshi (hai) {', 'moshi', 'keyword.control.mio'],
      ['} sore {', 'sore', 'keyword.control.mio'],
      ['aida (condition) {', 'aida', 'keyword.control.mio'],
      ['nukeru;', 'nukeru', 'keyword.control.mio'],
      ['tsuzuku;', 'tsuzuku', 'keyword.control.mio'],
      ['kaesu value;', 'kaesu', 'keyword.control.mio'],
      ['awase (value) {', 'awase', 'keyword.control.mio'],
      ['baai Ok(value) {', 'baai', 'keyword.control.case.mio'],
      ['moshi (hai) {', 'hai', 'constant.language.boolean.mio'],
      ['moshi (iie) {', 'iie', 'constant.language.boolean.mio'],
      ['kaku value = mu;', 'mu', 'constant.language.null.mio'],
    ];
    for (const [line, needle, scope] of cases) {
      assertScope(line, needle, scope);
    }
  });

  test('does not treat English or case-variant keywords as Mio keywords', () => {
    assertNoScope('if (value) {', 'if', 'keyword.control.mio');
    assertNoScope('while (value) {', 'while', 'keyword.control.mio');
    assertNoScope('return value;', 'return', 'keyword.control.mio');
    assertNoScope('Kaku value = 1;', 'Kaku', 'keyword.declaration.variable.mio');
    assertNoScope('Moshi (hai) {', 'Moshi', 'keyword.control.mio');
  });

  test('highlights imports and function calls', () => {
    assertScope(
      'yobu "kiso/math";',
      'yobu',
      'keyword.control.import.mio',
    );
    assertScope(
      'yobu "kiso/math";',
      '"kiso/math"',
      'string.quoted.double.import.mio',
    );
    assertScope('add(20, 22);', 'add', 'variable.function.mio');
    assertScope('chant(42);', 'chant', 'support.function.builtin.mio');
    assertScope(
      'kaku count = len(values);',
      'len',
      'support.function.builtin.mio',
    );
    assertScope(
      'kaku root = math.sqrt(value);',
      'sqrt',
      'variable.function.mio',
    );
    assertScope(
      'kaku value = point.x;',
      'x',
      'variable.other.property.mio',
    );
    assertScope('kaku x = int64(3);', 'int64', 'storage.type.primitive.mio');
  });

  test('does not highlight keywords inside strings or comments', () => {
    const line = 'kaku message = "moshi kaku waza";';
    assertScope(line, 'moshi', 'string.quoted.double.mio');
    assertNoScope(line, 'moshi', 'keyword.control.mio');
    assertNoScope(
      line,
      'kaku',
      'keyword.declaration.variable.mio',
      1,
    );
    assertNoScope(line, 'waza', 'keyword.declaration.function.mio');

    const comment = '// kaku moshi waza kata sentaku awase';
    assertScope(comment, 'kaku', 'comment.line.double-slash.mio');
    assertNoScope(comment, 'kaku', 'keyword.declaration.variable.mio');
    assertNoScope(comment, 'moshi', 'keyword.control.mio');

    const block = '/* waza kata */ kaku x = 1;';
    assertScope(block, 'waza', 'comment.block.mio');
    assertNoScope(block, 'waza', 'keyword.declaration.function.mio');
    assertScope(block, 'kaku', 'keyword.declaration.variable.mio');
  });

  test('keeps strings and runes from crossing lines', () => {
    const lines = tokenizeLines([
      'kaku s = "unterminated',
      'kaku next = 1;',
    ]);
    const secondLineKaku = lines[1]?.find(
      (token) => token.startIndex === 0,
    );
    assert.ok(secondLineKaku);
    assert.equal(
      hasScope(secondLineKaku.scopes, 'keyword.declaration.variable.mio'),
      true,
    );
    assert.equal(
      hasScope(secondLineKaku.scopes, 'string.quoted.double.mio'),
      false,
    );

    const firstLine = tokenizeLine('kaku s = "unterminated');
    const stringToken = firstLine.find((token) =>
      'kaku s = "unterminated'
        .slice(token.startIndex, token.endIndex)
        .includes('unterminated'),
    );
    assert.ok(stringToken);
    assert.equal(
      stringToken.endIndex,
      'kaku s = "unterminated'.length,
    );
  });

  test('highlights escapes and runes', () => {
    const escaped = 'kaku message = "escaped \\" quote";';
    assertScope(escaped, '\\"', 'constant.character.escape.mio');
    const rune = "kaku symbol: rune = 'A';";
    assertScope(rune, "'A'", 'constant.character.rune.mio');
    const escapedRune = "kaku symbol: rune = '\\n';";
    assertScope(escapedRune, '\\n', 'constant.character.escape.mio');
  });

  test('highlights generic declarations and applications', () => {
    const genericFunction =
      'waza identity<T>(value: T) -> T {';
    assertScope(
      genericFunction,
      'T',
      'entity.name.type.parameter.mio',
      0,
    );
    assertScope(
      genericFunction,
      'value',
      'variable.parameter.mio',
    );
    assertScope(genericFunction, 'T', 'entity.name.type.mio', 2);

    const constrained =
      'waza abs<T: Signed>(value: T) -> T {';
    assertScope(
      constrained,
      'Signed',
      'support.type.constraint.mio',
    );

    const genericStruct = 'kata Pair<T> { first: T; }';
    assertScope(genericStruct, 'kata', 'keyword.declaration.struct.mio');
    assertScope(
      genericStruct,
      'Pair',
      'entity.name.type.struct.mio',
    );
    assertScope(
      genericStruct,
      'T',
      'entity.name.type.parameter.mio',
      0,
    );
    assertScope(
      genericStruct,
      'first',
      'variable.other.member.mio',
    );
    assertScope(genericStruct, 'T', 'entity.name.type.mio', 1);

    const genericEnum =
      'sentaku Result<T, E> { Ok(T); Err(E); }';
    assertScope(genericEnum, 'sentaku', 'keyword.declaration.enum.mio');
    assertScope(
      genericEnum,
      'Result',
      'entity.name.type.enum.mio',
    );
    assertScope(
      genericEnum,
      'Ok',
      'entity.name.type.enummember.mio',
    );
    assertScope(
      genericEnum,
      'Err',
      'entity.name.type.enummember.mio',
    );

    const genericApplication =
      'kaku pair = Pair[int64] { first: 1, second: 2 };';
    assertScope(
      genericApplication,
      'Pair',
      'entity.name.type.mio',
    );
    assertScope(
      genericApplication,
      'int64',
      'storage.type.primitive.mio',
    );
    assertScope(
      genericApplication,
      '[',
      'punctuation.definition.typearguments.begin.mio',
    );

    const genericCall = 'kaku value = first[int64]([7, 8]);';
    assertScope(genericCall, 'first', 'variable.function.mio');
    assertScope(
      genericCall,
      '[',
      'punctuation.definition.typearguments.begin.mio',
    );
    assertScope(
      genericCall,
      'int64',
      'storage.type.primitive.mio',
    );

    const mapType = 'kaku entries = Map<string, int64>();';
    assertScope(mapType, 'Map', 'entity.name.type.mio');
    assertScope(mapType, 'string', 'storage.type.primitive.mio');
    assertScope(mapType, '<', 'punctuation.definition.typearguments.begin.mio');
  });

  test('highlights structs, enums, and match arms', () => {
    const struct = 'kata Point { x: int64; y: int64; }';
    assertScope(struct, 'Point', 'entity.name.type.struct.mio');
    assertScope(struct, 'x', 'variable.other.member.mio');
    assertScope(struct, 'y', 'variable.other.member.mio');

    const match =
      'awase (result) { baai Ok(value) { kaesu value; } }';
    assertScope(match, 'awase', 'keyword.control.mio');
    assertScope(match, 'baai', 'keyword.control.case.mio');
    assertScope(match, 'Ok', 'entity.name.type.enummember.mio');

    const qualified = 'kaku token = TokenKind.Import();';
    assertScope(qualified, 'TokenKind', 'entity.name.type.mio');
    assertScope(
      qualified,
      'Import',
      'entity.name.type.enummember.mio',
    );
  });

  test('highlights numbers, nullable markers, and operators', () => {
    assertScope('kaku n = 42;', '42', 'constant.numeric.integer.mio');
    assertScope('kaku n = 1.5e-3;', '1.5e-3', 'constant.numeric.float.mio');
    assertScope('kaku n = 01;', '01', 'invalid.illegal.leading-zero.mio');
    assertScope(
      'kaku age: int64? = mu;',
      '?',
      'keyword.operator.nullable.mio',
    );
    assertScope(
      'moshi (a == b && c != d) {',
      '==',
      'keyword.operator.comparison.mio',
    );
    assertScope(
      'moshi (a == b && c != d) {',
      '&&',
      'keyword.operator.logical.mio',
    );
    assertScope(
      'moshi (a == b && c != d) {',
      '!=',
      'keyword.operator.comparison.mio',
    );
  });

  test('keeps block comments in state across lines', () => {
    const lines = tokenizeLines([
      '/* comment',
      'kaku moshi',
      '*/ kaku x = 1;',
    ]);
    const middle = lines[1]?.[0];
    assert.ok(middle);
    assert.equal(hasScope(middle.scopes, 'comment.block.mio'), true);

    const lastLine = lines[2] ?? [];
    const afterComment = lastLine.find((token) =>
      hasScope(
        token.scopes,
        'keyword.declaration.variable.mio',
      ),
    );
    assert.ok(afterComment);
    assert.equal(
      hasScope(
        afterComment.scopes,
        'keyword.declaration.variable.mio',
      ),
      true,
    );
  });
});
