import * as vscode from 'vscode';

interface Bracket {
  character: '{' | '[' | '(';
  line: number;
  column: number;
}

// 轻量扫描 Mio 源码的括号与块注释，提供语法级折叠范围。
export class MioFoldingRangeProvider implements vscode.FoldingRangeProvider {
  provideFoldingRanges(
    document: vscode.TextDocument,
    _context: vscode.FoldingContext,
    token: vscode.CancellationToken,
  ): vscode.FoldingRange[] {
    const lines = document.getText().split(/\r?\n/);
    const ranges: vscode.FoldingRange[] = [];
    const stack: Bracket[] = [];

    let inBlockComment = false;
    let blockCommentStart = -1;
    let inString = false;
    let inRune = false;
    let escaped = false;

    const addRange = (
      start: number,
      end: number,
      kind?: vscode.FoldingRangeKind,
    ): void => {
      if (start >= end) {
        return;
      }
      ranges.push(new vscode.FoldingRange(start, end, kind));
    };

    for (let lineIndex = 0; lineIndex < lines.length; lineIndex += 1) {
      if (token.isCancellationRequested) {
        break;
      }
      const line = lines[lineIndex] ?? '';
      for (let column = 0; column < line.length; column += 1) {
        const character = line[column];
        if (character === undefined) {
          break;
        }
        const next = line[column + 1];

        if (inBlockComment) {
          if (character === '*' && next === '/') {
            inBlockComment = false;
            addRange(
              blockCommentStart,
              lineIndex,
              vscode.FoldingRangeKind.Comment,
            );
            blockCommentStart = -1;
            column += 1;
          }
          continue;
        }

        if (inString || inRune) {
          if (escaped) {
            escaped = false;
            continue;
          }
          if (character === '\\') {
            escaped = true;
            continue;
          }
          if (inString && character === '"') {
            inString = false;
            continue;
          }
          if (inRune && character === "'") {
            inRune = false;
            continue;
          }
          continue;
        }

        if (character === '/' && next === '/') {
          break;
        }
        if (character === '/' && next === '*') {
          inBlockComment = true;
          blockCommentStart = lineIndex;
          column += 1;
          continue;
        }
        if (character === '"') {
          inString = true;
          continue;
        }
        if (character === "'") {
          inRune = true;
          continue;
        }

        if (
          character === '{' ||
          character === '[' ||
          character === '('
        ) {
          stack.push({ character, line: lineIndex, column });
          continue;
        }

        if (
          character === '}' ||
          character === ']' ||
          character === ')'
        ) {
          const expected =
            character === '}'
              ? '{'
              : character === ']'
                ? '['
                : '(';
          let openIndex = stack.length - 1;
          while (
            openIndex >= 0 &&
            stack[openIndex]?.character !== expected
          ) {
            openIndex -= 1;
          }
          if (openIndex >= 0) {
            const open = stack[openIndex];
            if (open !== undefined) {
              stack.splice(openIndex);
              addRange(open.line, lineIndex);
            }
          }
        }
      }

      // Mio 字符串和 rune 不能跨行；行末复位，避免错误状态影响下一行。
      if (inString || inRune) {
        inString = false;
        inRune = false;
        escaped = false;
      }
    }

    if (inBlockComment && blockCommentStart >= 0) {
      addRange(
        blockCommentStart,
        lines.length - 1,
        vscode.FoldingRangeKind.Comment,
      );
    }

    ranges.sort((left, right) =>
      left.start === right.start
        ? right.end - left.end
        : left.start - right.start,
    );
    return ranges;
  }
}
