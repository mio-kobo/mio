import * as vscode from 'vscode';
import { runMioCheck, type CheckResult } from '../cli/checkRunner';
import { MioCliService } from '../cli/cliService';
import {
  formatDiagnosticMessage,
  isSameDocumentPath,
  mapDiagnosticPath,
  severityFromPayload,
  toDiagnosticRange,
} from '../cli/diagnostics';
import type { ProcessRunner } from '../cli/processRunner';
import { findBunkoRoot } from '../util/paths';

const DEBOUNCE_MS = 350;

interface Job {
  generation: number;
  tokenSource: vscode.CancellationTokenSource;
}

export interface DiagnosticManagerOptions {
  output: vscode.OutputChannel;
  runner: ProcessRunner;
  cli: MioCliService;
}

// 管理保存后检查、手动检查、旧进程取消和 Diagnostics 生命周期。
export class MioDiagnosticManager implements vscode.Disposable {
  private readonly collection =
    vscode.languages.createDiagnosticCollection('mio');
  private readonly jobs = new Map<string, Job>();
  private readonly timers = new Map<string, NodeJS.Timeout>();
  private readonly generations = new Map<string, number>();
  private disposed = false;
  private fallbackEnabled = true;

  constructor(private readonly options: DiagnosticManagerOptions) {}

  onDidSave(document: vscode.TextDocument): void {
    if (!this.fallbackEnabled || !this.enabled || !this.onSave) {
      return;
    }
    if (document.languageId !== 'mio' || document.uri.scheme !== 'file') {
      return;
    }
    this.schedule(document);
  }

  async checkNow(document: vscode.TextDocument): Promise<boolean> {
    if (!this.fallbackEnabled) {
      void vscode.window.showInformationMessage(
        'Mio diagnostics are provided by the Mio language server.',
      );
      return true;
    }
    const key = document.uri.toString();
    this.clearTimer(key);
    this.cancelJob(key);
    return this.run(document, true);
  }

  onDidClose(document: vscode.TextDocument): void {
    this.clearDocument(document.uri);
  }

  get diagnosticsOwner(): 'cli' | 'lsp' {
    return this.fallbackEnabled ? 'cli' : 'lsp';
  }

  // LSP 就绪时停用 CLI 回退并清空旧诊断；失败或停止时恢复。
  setFallbackEnabled(enabled: boolean): void {
    if (this.fallbackEnabled === enabled) {
      return;
    }
    this.fallbackEnabled = enabled;
    if (!enabled) {
      this.clearAllTimers();
      this.cancelAllJobs();
      this.collection.clear();
      this.generations.clear();
      return;
    }
    for (const document of vscode.workspace.textDocuments) {
      if (document.languageId === 'mio' && document.uri.scheme === 'file') {
        this.schedule(document);
      }
    }
  }

  // 关闭文件、清理测试资源和工作区切换共用同一条释放路径。
  clearDocument(uri: vscode.Uri): void {
    const key = uri.toString();
    this.clearTimer(key);
    this.cancelJob(key);
    this.collection.delete(uri);
    this.generations.delete(key);
  }

  onConfigurationChanged(): void {
    this.clearAllTimers();
    this.cancelAllJobs();
    this.collection.clear();
    this.generations.clear();
  }

  dispose(): void {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    this.clearAllTimers();
    this.cancelAllJobs();
    this.collection.dispose();
    this.generations.clear();
  }

  private get enabled(): boolean {
    return vscode.workspace
      .getConfiguration('mio')
      .get<boolean>('diagnostics.enabled', true);
  }

  private get onSave(): boolean {
    return vscode.workspace
      .getConfiguration('mio')
      .get<boolean>('diagnostics.onSave', true);
  }

  private schedule(document: vscode.TextDocument): void {
    const key = document.uri.toString();
    this.clearTimer(key);
    const timer = setTimeout(() => {
      this.timers.delete(key);
      void this.run(document, false).catch((error: unknown) => {
        this.reportUnexpected(error);
      });
    }, DEBOUNCE_MS);
    timer.unref();
    this.timers.set(key, timer);
  }

  private clearTimer(key: string): void {
    const timer = this.timers.get(key);
    if (timer !== undefined) {
      clearTimeout(timer);
      this.timers.delete(key);
    }
  }

  private clearAllTimers(): void {
    for (const timer of this.timers.values()) {
      clearTimeout(timer);
    }
    this.timers.clear();
  }

  private cancelJob(key: string): void {
    const job = this.jobs.get(key);
    if (job === undefined) {
      return;
    }
    this.jobs.delete(key);
    job.tokenSource.cancel();
    job.tokenSource.dispose();
  }

  private cancelAllJobs(): void {
    for (const key of [...this.jobs.keys()]) {
      this.cancelJob(key);
    }
  }

  private async run(
    document: vscode.TextDocument,
    notify: boolean,
  ): Promise<boolean> {
    if (!this.fallbackEnabled) {
      return false;
    }
    const key = document.uri.toString();
    const generation = (this.generations.get(key) ?? 0) + 1;
    this.generations.set(key, generation);
    this.cancelJob(key);

    if (document.languageId !== 'mio' || document.uri.scheme !== 'file') {
      if (notify) {
        void vscode.window.showWarningMessage(
          'Save this file as a .mio file before running Mio check.',
        );
      }
      return false;
    }

    if (!this.enabled && !notify) {
      this.collection.delete(document.uri);
      return false;
    }

    const resolution = this.options.cli.resolve();
    if (resolution.kind !== 'found') {
      if (notify) {
        this.options.cli.showProblem(resolution);
      } else {
        this.options.output.appendLine(`[check] ${resolution.message}`);
      }
      this.collection.delete(document.uri);
      return false;
    }

    const tokenSource = new vscode.CancellationTokenSource();
    this.jobs.set(key, { generation, tokenSource });
    this.options.output.appendLine(
      `[check] ${document.uri.fsPath}${document.isDirty ? ' (unsaved buffer)' : ''}`,
    );

    try {
      const result = await runMioCheck({
        runner: this.options.runner,
        executable: resolution.executable,
        filePath: document.uri.fsPath,
        documentText: document.getText(),
        useTempPackage: document.isDirty,
        bunkoRoot: findBunkoRoot(document.uri.fsPath),
        token: tokenSource.token,
      });

      if (this.disposed || this.generations.get(key) !== generation) {
        return false;
      }
      this.jobs.delete(key);
      return this.applyResult(document, result, notify);
    } finally {
      tokenSource.dispose();
    }
  }

  private applyResult(
    document: vscode.TextDocument,
    result: CheckResult,
    notify: boolean,
  ): boolean {
    switch (result.kind) {
      case 'passed':
        this.collection.delete(document.uri);
        this.options.output.appendLine('[check] passed');
        if (notify) {
          void vscode.window.showInformationMessage('Mio check passed.');
        }
        return true;

      case 'cancelled':
        return false;

      case 'diagnostics': {
        const diagnostics = this.toDiagnostics(document, result);
        this.collection.set(document.uri, diagnostics);
        this.options.output.appendLine(
          `[check] ${diagnostics.length} diagnostic(s)`,
        );
        if (notify) {
          const hasError = diagnostics.some(
            (diagnostic) =>
              diagnostic.severity === vscode.DiagnosticSeverity.Error,
          );
          const message = `Mio check found ${diagnostics.length} problem(s).`;
          if (hasError) {
            void vscode.window
              .showErrorMessage(message, 'Open Output')
              .then((choice) => {
                if (choice === 'Open Output') {
                  this.options.output.show(true);
                }
              });
          } else {
            void vscode.window.showWarningMessage(message);
          }
        }
        return true;
      }

      case 'failed': {
        this.collection.set(document.uri, [
          this.fallbackDiagnostic(
            result.message ?? 'Mio check failed.',
          ),
        ]);
        this.options.output.appendLine(
          `[check] failed: ${result.message ?? 'unknown error'}`,
        );
        if (result.stderr.trim() !== '') {
          this.options.output.appendLine(result.stderr.trim());
        }
        if (notify) {
          void vscode.window
            .showErrorMessage(
              result.message ?? 'Mio check failed.',
              'Open Output',
            )
            .then((choice) => {
              if (choice === 'Open Output') {
                this.options.output.show(true);
              }
            });
        }
        return false;
      }

      case 'toolError':
        this.collection.delete(document.uri);
        this.options.output.appendLine(
          `[check] unable to run Mio: ${result.message ?? 'unknown error'}`,
        );
        if (result.stderr.trim() !== '') {
          this.options.output.appendLine(result.stderr.trim());
        }
        if (notify) {
          void vscode.window
            .showErrorMessage(
              result.message ?? 'Unable to run Mio.',
              'Open Output',
            )
            .then((choice) => {
              if (choice === 'Open Output') {
                this.options.output.show(true);
              }
            });
        }
        return false;
    }
  }

  private toDiagnostics(
    document: vscode.TextDocument,
    result: CheckResult,
  ): vscode.Diagnostic[] {
    const text = document.getText();
    const diagnostics: vscode.Diagnostic[] = [];

    for (const payload of result.payloads) {
      const mappedPath = mapDiagnosticPath(
        payload.path,
        result.pathMap,
        result.checkedPath,
      );
      const belongsToDocument = isSameDocumentPath(
        mappedPath,
        document.uri.fsPath,
      );
      const rangeInfo = belongsToDocument
        ? toDiagnosticRange(payload, text)
        : undefined;
      let message = formatDiagnosticMessage(payload);
      let range: vscode.Range;

      if (rangeInfo !== undefined) {
        range = new vscode.Range(
          document.positionAt(rangeInfo.start),
          document.positionAt(rangeInfo.end),
        );
      } else {
        range = new vscode.Range(0, 0, 0, 0);
        if (belongsToDocument) {
          message +=
            '\nNo source range was provided by the Mio CLI; the diagnostic is shown at the start of the file.';
        } else {
          message += `\nReported file: ${mappedPath}`;
        }
      }

      const severity =
        severityFromPayload(payload) === 'warning'
          ? vscode.DiagnosticSeverity.Warning
          : vscode.DiagnosticSeverity.Error;
      const diagnostic = new vscode.Diagnostic(range, message, severity);
      diagnostic.source = 'mio';
      if (payload.code !== undefined && payload.code !== '') {
        diagnostic.code = payload.code;
      }
      diagnostics.push(diagnostic);
    }

    return diagnostics;
  }

  private fallbackDiagnostic(message: string): vscode.Diagnostic {
    const diagnostic = new vscode.Diagnostic(
      new vscode.Range(0, 0, 0, 0),
      message,
      vscode.DiagnosticSeverity.Error,
    );
    diagnostic.source = 'mio';
    return diagnostic;
  }

  private reportUnexpected(error: unknown): void {
    const message =
      error instanceof Error ? error.message : String(error);
    this.options.output.appendLine(`[check] unexpected error: ${message}`);
  }
}
