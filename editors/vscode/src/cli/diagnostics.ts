import * as path from 'node:path';

export interface MioSourceLocation {
  offset?: number;
  line?: number;
  column?: number;
}

export interface MioSourceSpan {
  start?: MioSourceLocation;
  end?: MioSourceLocation;
}

export interface MioDiagnosticLabel {
  span?: MioSourceSpan;
  message?: string;
  primary?: boolean;
}

export interface MioDiagnosticPayload {
  code?: string;
  severity?: string;
  message?: string;
  path?: string;
  location?: MioSourceLocation;
  labels?: MioDiagnosticLabel[];
  notes?: Array<{ message?: string }>;
  helps?: Array<{ message?: string }>;
  expected?: string;
  actual?: string;
}

export interface DiagnosticRange {
  start: number;
  end: number;
}

export type MioSeverity = 'error' | 'warning';

export function isMioDiagnosticPayload(value: unknown): value is MioDiagnosticPayload {
  if (typeof value !== 'object' || value === null) {
    return false;
  }
  const record = value as Record<string, unknown>;
  return (
    typeof record.message === 'string' ||
    typeof record.code === 'string' ||
    typeof record.severity === 'string'
  );
}

// 从混合输出中提取 JSON 对象，兼容 CLI 将来输出多条诊断或附加文本。
export function extractJsonObjects(text: string): unknown[] {
  const objects: unknown[] = [];
  let depth = 0;
  let start = -1;
  let inString = false;
  let escaped = false;

  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (character === undefined) {
      break;
    }
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (character === '\\') {
        escaped = true;
      } else if (character === '"') {
        inString = false;
      }
      continue;
    }
    if (character === '"') {
      inString = true;
      continue;
    }
    if (character === '{') {
      if (depth === 0) {
        start = index;
      }
      depth += 1;
      continue;
    }
    if (character === '}') {
      if (depth === 0) {
        continue;
      }
      depth -= 1;
      if (depth === 0 && start >= 0) {
        const candidate = text.slice(start, index + 1);
        try {
          objects.push(JSON.parse(candidate));
        } catch {
          // 忽略不完整的 JSON，CLI 的普通输出继续保留。
        }
        start = -1;
      }
    }
  }
  return objects;
}

export function parseMioDiagnostics(text: string): MioDiagnosticPayload[] {
  const diagnostics: MioDiagnosticPayload[] = [];
  for (const value of extractJsonObjects(text)) {
    if (Array.isArray(value)) {
      for (const entry of value) {
        if (isMioDiagnosticPayload(entry)) {
          diagnostics.push(entry);
        }
      }
      continue;
    }
    if (isMioDiagnosticPayload(value)) {
      diagnostics.push(value);
    }
  }
  return diagnostics;
}

export function severityFromPayload(payload: MioDiagnosticPayload): MioSeverity {
  return payload.severity?.toLowerCase() === 'warning' ? 'warning' : 'error';
}

function utf8Width(codePoint: number): number {
  if (codePoint < 0x80) {
    return 1;
  }
  if (codePoint < 0x800) {
    return 2;
  }
  if (codePoint < 0x10000) {
    return 3;
  }
  return 4;
}

// CLI 的 offset 是 UTF-8 字节偏移；VS Code 使用 UTF-16 字符串索引。
export function byteOffsetToUtf16Offset(text: string, byteOffset: number): number {
  if (!Number.isFinite(byteOffset) || byteOffset <= 0) {
    return 0;
  }
  let bytes = 0;
  let index = 0;
  while (index < text.length) {
    const codePoint = text.codePointAt(index);
    if (codePoint === undefined) {
      break;
    }
    const width = utf8Width(codePoint);
    if (bytes + width > byteOffset) {
      break;
    }
    bytes += width;
    index += codePoint > 0xffff ? 2 : 1;
  }
  return index;
}

// CLI 的 column 以 Unicode 标量值（显示宽度）计数；此回退仅用于缺少 offset 的旧输出。
export function lineColumnToUtf16Offset(
  text: string,
  line: number,
  column: number,
): number | undefined {
  if (!Number.isFinite(line) || !Number.isFinite(column) || line < 1 || column < 1) {
    return undefined;
  }
  const lines = text.split('\n');
  if (line > lines.length) {
    return undefined;
  }
  let base = 0;
  for (let index = 0; index < line - 1; index += 1) {
    base += (lines[index]?.length ?? 0) + 1;
  }
  const lineText = (lines[line - 1] ?? '').replace(/\r$/, '');
  const target = column - 1;
  let characters = 0;
  let codeUnits = 0;
  while (codeUnits < lineText.length && characters < target) {
    const codePoint = lineText.codePointAt(codeUnits);
    if (codePoint === undefined) {
      break;
    }
    codeUnits += codePoint > 0xffff ? 2 : 1;
    characters += 1;
  }
  return base + codeUnits;
}

function labelOffset(
  location: MioSourceLocation | undefined,
): number | undefined {
  const offset = location?.offset;
  if (typeof offset === 'number' && Number.isFinite(offset) && offset >= 0) {
    return offset;
  }
  return undefined;
}

// 只使用 CLI 真实提供的源码范围；无法确认位置时返回 undefined，不猜测行号。
export function toDiagnosticRange(
  payload: MioDiagnosticPayload,
  text: string,
): DiagnosticRange | undefined {
  const label =
    payload.labels?.find((candidate) => candidate.primary === true) ??
    payload.labels?.[0];
  const startByte = labelOffset(label?.span?.start);
  const endByte = labelOffset(label?.span?.end);

  if (startByte !== undefined) {
    const start = byteOffsetToUtf16Offset(text, startByte);
    const end =
      endByte !== undefined
        ? Math.max(start, byteOffsetToUtf16Offset(text, endByte))
        : start;
    return { start, end };
  }

  const location = payload.location;
  if (location === undefined) {
    return undefined;
  }
  if (typeof location.line === 'number' && location.line <= 0) {
    return undefined;
  }
  const locationOffset = labelOffset(location);
  if (locationOffset !== undefined) {
    const start = byteOffsetToUtf16Offset(text, locationOffset);
    return { start, end: start };
  }
  if (
    typeof location.line === 'number' &&
    typeof location.column === 'number'
  ) {
    const start = lineColumnToUtf16Offset(
      text,
      location.line,
      location.column,
    );
    if (start !== undefined) {
      return { start, end: start };
    }
  }
  return undefined;
}

export function formatDiagnosticMessage(payload: MioDiagnosticPayload): string {
  const lines: string[] = [payload.message?.trim() || 'Mio compiler error'];
  const seen = new Set<string>(lines);

  const append = (label: string, value: string | undefined): void => {
    const text = value?.trim();
    if (text === undefined || text === '') {
      return;
    }
    const line = `${label}: ${text}`;
    if (!seen.has(line)) {
      seen.add(line);
      lines.push(line);
    }
  };

  if (payload.expected !== undefined || payload.actual !== undefined) {
    append(
      'expected/actual',
      [payload.expected, payload.actual].filter(Boolean).join(' / '),
    );
  }
  for (const note of payload.notes ?? []) {
    append('note', note.message);
  }
  for (const help of payload.helps ?? []) {
    append('help', help.message);
  }
  for (const label of payload.labels ?? []) {
    if (label.message !== undefined && label.message !== '') {
      append('label', label.message);
    }
  }
  return lines.join('\n');
}

export function mapDiagnosticPath(
  payloadPath: string | undefined,
  pathMap: ReadonlyMap<string, string>,
  checkedPath: string,
): string {
  if (payloadPath === undefined || payloadPath === '') {
    return checkedPath;
  }
  const direct = pathMap.get(payloadPath);
  if (direct !== undefined) {
    return direct;
  }
  const base = path.basename(payloadPath);
  for (const [temporary, original] of pathMap) {
    if (path.basename(temporary) === base) {
      return original;
    }
  }
  return payloadPath;
}

export function isSameDocumentPath(left: string, right: string): boolean {
  const a = path.resolve(left);
  const b = path.resolve(right);
  return process.platform === 'win32'
    ? a.toLowerCase() === b.toLowerCase()
    : a === b;
}
