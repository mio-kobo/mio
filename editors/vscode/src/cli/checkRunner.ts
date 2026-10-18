import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import {
  parseMioDiagnostics,
  type MioDiagnosticPayload,
} from './diagnostics';
import {
  ProcessRunError,
  type CancellationLike,
  type ProcessRunner,
} from './processRunner';

export type CheckKind =
  | 'passed'
  | 'diagnostics'
  | 'failed'
  | 'toolError'
  | 'cancelled';

export interface CheckRequest {
  runner: ProcessRunner;
  executable: string;
  filePath: string;
  documentText: string;
  useTempPackage: boolean;
  cwd?: string | undefined;
  bunkoRoot?: string | undefined;
  timeoutMs?: number;
  token?: CancellationLike | undefined;
}

export interface CheckResult {
  kind: CheckKind;
  payloads: MioDiagnosticPayload[];
  checkedPath: string;
  pathMap: Map<string, string>;
  stdout: string;
  stderr: string;
  exitCode: number | null;
  timedOut: boolean;
  cancelled: boolean;
  message?: string;
}

const DEFAULT_TIMEOUT_MS = 30_000;

// 运行 mio check。脏文档会复制当前目录的 .mio 文件到临时包，只替换当前文件内容。
export async function runMioCheck(request: CheckRequest): Promise<CheckResult> {
  const pathMap = new Map<string, string>();
  let checkedPath = request.filePath;
  let temporaryDirectory: string | undefined;

  try {
    if (request.useTempPackage) {
      temporaryDirectory = await fs.promises.mkdtemp(
        path.join(os.tmpdir(), 'mio-check-'),
      );
      const sourceDirectory = path.dirname(request.filePath);
      const baseName = path.basename(request.filePath);
      const entries = await fs.promises.readdir(sourceDirectory, {
        withFileTypes: true,
      });

      for (const entry of entries) {
        if (
          !entry.isFile() ||
          entry.name === baseName ||
          !entry.name.toLowerCase().endsWith('.mio')
        ) {
          continue;
        }
        const original = path.join(sourceDirectory, entry.name);
        const target = path.join(temporaryDirectory, entry.name);
        await fs.promises.copyFile(original, target);
        pathMap.set(target, original);
      }

      checkedPath = path.join(temporaryDirectory, baseName);
      await fs.promises.writeFile(
        checkedPath,
        request.documentText,
        'utf8',
      );
      pathMap.set(checkedPath, request.filePath);
    } else {
      pathMap.set(checkedPath, checkedPath);
    }

    const timeoutMs = request.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    const result = await request.runner.run(
      request.executable,
      ['check', '--format=json', '--color=never', checkedPath],
      {
        cwd: request.cwd ?? path.dirname(request.filePath),
        env:
          request.bunkoRoot === undefined
            ? undefined
            : { MIO_BUNKO: request.bunkoRoot },
        timeoutMs,
        token: request.token,
      },
    );

    if (result.cancelled) {
      return {
        kind: 'cancelled',
        payloads: [],
        checkedPath,
        pathMap,
        stdout: result.stdout,
        stderr: result.stderr,
        exitCode: result.code,
        timedOut: false,
        cancelled: true,
      };
    }

    if (result.timedOut) {
      return {
        kind: 'toolError',
        payloads: [],
        checkedPath,
        pathMap,
        stdout: result.stdout,
        stderr: result.stderr,
        exitCode: result.code,
        timedOut: true,
        cancelled: false,
        message: `Mio check timed out after ${Math.round(timeoutMs / 1000)} seconds.`,
      };
    }

    const payloads = parseMioDiagnostics(result.stderr);
    if (payloads.length === 0) {
      payloads.push(...parseMioDiagnostics(result.stdout));
    }

    if (payloads.length > 0) {
      return {
        kind: 'diagnostics',
        payloads,
        checkedPath,
        pathMap,
        stdout: result.stdout,
        stderr: result.stderr,
        exitCode: result.code,
        timedOut: false,
        cancelled: false,
      };
    }

    if (result.code === 0) {
      return {
        kind: 'passed',
        payloads: [],
        checkedPath,
        pathMap,
        stdout: result.stdout,
        stderr: result.stderr,
        exitCode: result.code,
        timedOut: false,
        cancelled: false,
      };
    }

    const output = [result.stderr.trim(), result.stdout.trim()]
      .filter((value) => value !== '')
      .join('\n');
    return {
      kind: 'failed',
      payloads: [],
      checkedPath,
      pathMap,
      stdout: result.stdout,
      stderr: result.stderr,
      exitCode: result.code,
      timedOut: false,
      cancelled: false,
      message:
        output !== ''
          ? output
          : `Mio check exited with code ${result.code ?? 'unknown'}.`,
    };
  } catch (error) {
    const message =
      error instanceof ProcessRunError
        ? error.message
        : error instanceof Error
          ? `Mio check failed: ${error.message}`
          : `Mio check failed: ${String(error)}`;
    return {
      kind: 'toolError',
      payloads: [],
      checkedPath,
      pathMap,
      stdout: '',
      stderr: '',
      exitCode: null,
      timedOut: false,
      cancelled: false,
      message,
    };
  } finally {
    if (temporaryDirectory !== undefined) {
      await fs.promises
        .rm(temporaryDirectory, { recursive: true, force: true })
        .catch(() => undefined);
    }
  }
}
