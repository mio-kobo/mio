import * as assert from 'node:assert/strict';
import {
  byteOffsetToUtf16Offset,
  extractJsonObjects,
  formatDiagnosticMessage,
  isSameDocumentPath,
  lineColumnToUtf16Offset,
  mapDiagnosticPath,
  parseMioDiagnostics,
  severityFromPayload,
  toDiagnosticRange,
  type MioDiagnosticPayload,
} from '../../cli/diagnostics';

const sampleJson =
  '{"code":"E0201","severity":"error","message":"未定义的变量 \\"undefined_name\\"","path":"/tmp/bad.mio","location":{"offset":62,"line":3,"column":11},"labels":[{"span":{"start":{"offset":62,"line":3,"column":11},"end":{"offset":76,"line":3,"column":25}},"primary":true}],"helps":[{"message":"请检查名称拼写或在当前作用域中声明它"}]}';

suite('diagnostics parser', () => {
  test('parses the CLI JSON object from mixed stderr text', () => {
    const parsed = parseMioDiagnostics(`warning: prefix\n${sampleJson}\n`);
    assert.equal(parsed.length, 1);
    assert.equal(parsed[0]?.code, 'E0201');
    assert.equal(parsed[0]?.severity, 'error');
    assert.equal(parsed[0]?.path, '/tmp/bad.mio');
    assert.equal(parsed[0]?.labels?.[0]?.primary, true);
    assert.equal(parsed[0]?.helps?.[0]?.message, '请检查名称拼写或在当前作用域中声明它');
  });

  test('parses multiple JSON diagnostics and ignores ordinary output', () => {
    const second =
      '{"code":"E0401","severity":"error","message":"编译失败","location":{"offset":0,"line":0,"column":0},"notes":[{"message":"expected parameter name"}]}';
    const parsed = parseMioDiagnostics(
      `check passed\n${sampleJson}\n${second}\nnot json`,
    );
    assert.equal(parsed.length, 2);
    assert.equal(parsed[1]?.code, 'E0401');
  });

  test('extracts only complete JSON objects', () => {
    const objects = extractJsonObjects(
      'prefix {"a":1} middle {"b":{"c":2}} broken {"d":3',
    );
    assert.equal(objects.length, 2);
    assert.deepEqual(objects[0], { a: 1 });
    assert.deepEqual(objects[1], { b: { c: 2 } });
  });

  test('maps UTF-8 byte offsets to UTF-16 string offsets', () => {
    assert.equal(byteOffsetToUtf16Offset('abc', 0), 0);
    assert.equal(byteOffsetToUtf16Offset('abc', 2), 2);
    assert.equal(byteOffsetToUtf16Offset('abc', 99), 3);

    assert.equal(byteOffsetToUtf16Offset('你a', 3), 1);
    assert.equal(byteOffsetToUtf16Offset('你a', 4), 2);

    assert.equal(byteOffsetToUtf16Offset('😀a', 4), 2);
    assert.equal(byteOffsetToUtf16Offset('😀a', 5), 3);

    assert.equal(byteOffsetToUtf16Offset('a\r\nb', 3), 3);
  });

  test('maps 1-based line and column as a fallback', () => {
    assert.equal(lineColumnToUtf16Offset('abc\ndef', 2, 2), 5);
    assert.equal(lineColumnToUtf16Offset('你a\nb', 1, 2), 1);
    assert.equal(lineColumnToUtf16Offset('abc', 0, 1), undefined);
    assert.equal(lineColumnToUtf16Offset('abc', 2, 1), undefined);
  });

  test('uses the primary label span when available', () => {
    const payload: MioDiagnosticPayload = {
      labels: [
        {
          span: {
            start: { offset: 5, line: 1, column: 6 },
            end: { offset: 6, line: 1, column: 7 },
          },
          primary: true,
        },
      ],
    };
    assert.deepEqual(toDiagnosticRange(payload, 'kaku x = 1;'), {
      start: 5,
      end: 6,
    });
  });

  test('uses location offset only when no label exists', () => {
    const payload: MioDiagnosticPayload = {
      location: { offset: 7, line: 1, column: 8 },
    };
    assert.deepEqual(toDiagnosticRange(payload, 'kaku x = 1;'), {
      start: 7,
      end: 7,
    });
  });

  test('does not guess a range for line 0 locations', () => {
    const payload: MioDiagnosticPayload = {
      location: { offset: 0, line: 0, column: 0 },
    };
    assert.equal(toDiagnosticRange(payload, 'kaku x = 1;'), undefined);
  });

  test('formats notes, helps, and expected/actual types', () => {
    const message = formatDiagnosticMessage({
      message: '类型不匹配',
      expected: 'int64',
      actual: 'string',
      notes: [{ message: '变量在此声明' }],
      helps: [{ message: '改用 int64(...)' }],
    });
    assert.match(message, /类型不匹配/u);
    assert.match(message, /expected\/actual: int64 \/ string/u);
    assert.match(message, /note: 变量在此声明/u);
    assert.match(message, /help: 改用 int64\(\.\.\.\)/u);
  });

  test('maps temporary package paths back to original files', () => {
    const pathMap = new Map<string, string>([
      ['/tmp/mio-check-1/source.mio', '/project/source.mio'],
      ['/tmp/mio-check-1/lib.mio', '/project/lib.mio'],
    ]);
    assert.equal(
      mapDiagnosticPath(
        '/tmp/mio-check-1/source.mio',
        pathMap,
        '/tmp/mio-check-1/source.mio',
      ),
      '/project/source.mio',
    );
    assert.equal(
      mapDiagnosticPath(
        '/tmp/mio-check-1/other/lib.mio',
        pathMap,
        '/project/source.mio',
      ),
      '/project/lib.mio',
    );
    assert.equal(
      mapDiagnosticPath('/tmp/unknown/other.mio', pathMap, '/project/source.mio'),
      '/tmp/unknown/other.mio',
    );
    assert.equal(
      mapDiagnosticPath(undefined, pathMap, '/project/source.mio'),
      '/project/source.mio',
    );
  });

  test('compares normalized paths', () => {
    assert.equal(
      isSameDocumentPath('/project/a/../a/source.mio', '/project/a/source.mio'),
      true,
    );
    assert.equal(
      isSameDocumentPath('/project/a/source.mio', '/project/b/source.mio'),
      false,
    );
  });

  test('normalizes warning severity', () => {
    assert.equal(severityFromPayload({ severity: 'warning' }), 'warning');
    assert.equal(severityFromPayload({ severity: 'error' }), 'error');
    assert.equal(severityFromPayload({}), 'error');
  });
});
