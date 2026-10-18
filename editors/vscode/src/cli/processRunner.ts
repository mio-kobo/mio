import { spawn, type ChildProcess } from 'node:child_process';

// 与 vscode.CancellationToken 结构兼容，便于在不依赖 VS Code 的单元测试中使用。
export interface CancellationLike {
  readonly isCancellationRequested: boolean;
  onCancellationRequested(listener: () => void): { dispose(): void };
}

export interface ProcessRunOptions {
  cwd?: string | undefined;
  env?: NodeJS.ProcessEnv | undefined;
  timeoutMs?: number;
  token?: CancellationLike | undefined;
  maxOutputBytes?: number;
}

export interface ProcessRunResult {
  code: number | null;
  signal: NodeJS.Signals | null;
  stdout: string;
  stderr: string;
  timedOut: boolean;
  cancelled: boolean;
  truncated: boolean;
}

export class ProcessRunError extends Error {
  readonly code: string | undefined;

  constructor(message: string, code?: string, cause?: unknown) {
    super(message, cause === undefined ? undefined : { cause });
    this.name = 'ProcessRunError';
    this.code = code;
  }
}

const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_MAX_OUTPUT_BYTES = 4 * 1024 * 1024;
const KILL_GRACE_MS = 1_500;

// 启动外部 Mio CLI。始终使用参数数组并关闭 shell，路径和参数不会被拼接执行。
export class ProcessRunner {
  private readonly running = new Set<ChildProcess>();

  run(
    executable: string,
    args: readonly string[],
    options: ProcessRunOptions = {},
  ): Promise<ProcessRunResult> {
    return new Promise<ProcessRunResult>((resolve, reject) => {
      let child: ChildProcess;
      try {
        child = spawn(executable, [...args], {
          cwd: options.cwd,
          env: options.env === undefined ? process.env : { ...process.env, ...options.env },
          shell: false,
          windowsHide: true,
          stdio: ['ignore', 'pipe', 'pipe'],
        });
      } catch (error) {
        reject(this.startError(executable, error));
        return;
      }

      this.running.add(child);

      const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
      const maxOutputBytes = options.maxOutputBytes ?? DEFAULT_MAX_OUTPUT_BYTES;
      const stdoutChunks: Buffer[] = [];
      const stderrChunks: Buffer[] = [];

      let stdoutBytes = 0;
      let stderrBytes = 0;
      let truncated = false;
      let settled = false;
      let timedOut = false;
      let cancelled = false;

      const collect = (chunk: Buffer, chunks: Buffer[], used: number): number => {
        const remaining = maxOutputBytes - used;
        if (remaining <= 0) {
          truncated = true;
          return used;
        }
        if (chunk.length > remaining) {
          chunks.push(chunk.subarray(0, remaining));
          truncated = true;
          return maxOutputBytes;
        }
        chunks.push(chunk);
        return used + chunk.length;
      };

      const onStdout = (chunk: Buffer): void => {
        stdoutBytes = collect(chunk, stdoutChunks, stdoutBytes);
      };
      const onStderr = (chunk: Buffer): void => {
        stderrBytes = collect(chunk, stderrChunks, stderrBytes);
      };

      child.stdout?.on('data', onStdout);
      child.stderr?.on('data', onStderr);

      let timeout: NodeJS.Timeout | undefined;
      if (timeoutMs > 0) {
        timeout = setTimeout(() => {
          timedOut = true;
          this.terminate(child);
        }, timeoutMs);
        timeout.unref();
      }

      const cancellation = options.token?.onCancellationRequested(() => {
        cancelled = true;
        this.terminate(child);
      });

      const cleanup = (): void => {
        if (timeout !== undefined) {
          clearTimeout(timeout);
        }
        cancellation?.dispose();
        child.stdout?.off('data', onStdout);
        child.stderr?.off('data', onStderr);
        this.running.delete(child);
      };

      child.once('error', (error) => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        reject(this.startError(executable, error));
      });

      child.once('close', (code, signal) => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        resolve({
          code,
          signal,
          stdout: Buffer.concat(stdoutChunks).toString('utf8'),
          stderr: Buffer.concat(stderrChunks).toString('utf8'),
          timedOut,
          cancelled: cancelled || (options.token?.isCancellationRequested ?? false),
          truncated,
        });
      });
    });
  }

  dispose(): void {
    for (const child of this.running) {
      this.terminate(child);
    }
    this.running.clear();
  }

  private terminate(child: ChildProcess): void {
    if (child.exitCode !== null || child.signalCode !== null) {
      return;
    }
    try {
      child.kill();
    } catch {
      // 进程可能已经退出。
    }
    const force = setTimeout(() => {
      try {
        child.kill('SIGKILL');
      } catch {
        // 进程可能已经退出。
      }
    }, KILL_GRACE_MS);
    force.unref();
  }

  private startError(executable: string, error: unknown): ProcessRunError {
    const message = error instanceof Error ? error.message : String(error);
    const code = (error as NodeJS.ErrnoException | undefined)?.code;
    return new ProcessRunError(
      `Failed to start Mio CLI "${executable}": ${message}`,
      typeof code === 'string' ? code : undefined,
      error,
    );
  }
}
