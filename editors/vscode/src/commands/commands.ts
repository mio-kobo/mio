import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';
import { MioCliService } from '../cli/cliService';
import {
  ProcessRunError,
  ProcessRunner,
  type ProcessRunResult,
} from '../cli/processRunner';
import type { MioDiagnosticManager } from '../diagnostics/diagnosticManager';
import { findBunkoRoot, resolveBuildOutput } from '../util/paths';

const RUN_TIMEOUT_MS = 120_000;
const BUILD_TIMEOUT_MS = 120_000;
const VERSION_TIMEOUT_MS = 15_000;

export interface CommandDependencies {
  output: vscode.OutputChannel;
  runner: ProcessRunner;
  cli: MioCliService;
  diagnostics: MioDiagnosticManager;
}

function quoteDisplayArgument(value: string): string {
  return /[\s"']/u.test(value) ? JSON.stringify(value) : value;
}

// 实现五个 Mio 命令；所有外部进程都通过 ProcessRunner 以参数数组执行。
export class MioCommands {
  constructor(private readonly deps: CommandDependencies) {}

  async checkCurrentFile(): Promise<void> {
    const editor = this.activeMioEditor();
    if (editor === undefined) {
      return;
    }
    const document = editor.document;
    if (document.uri.scheme !== 'file') {
      void vscode.window.showWarningMessage(
        'Save this file as a .mio file before running Mio check.',
      );
      return;
    }
    try {
      await this.deps.diagnostics.checkNow(document);
    } catch (error) {
      await this.reportProcessError('check', error);
    }
  }

  async runCurrentFile(): Promise<void> {
    const editor = this.activeMioEditor();
    if (editor === undefined) {
      return;
    }
    const document = editor.document;
    if (!(await this.saveIfNeeded(document))) {
      return;
    }
    const executable = this.requireCli();
    if (executable === undefined) {
      return;
    }
    const filePath = document.uri.fsPath;
    try {
      await vscode.window.withProgress(
        {
          location: vscode.ProgressLocation.Notification,
          title: 'Mio: Run Current File',
          cancellable: true,
        },
        async (_progress, token) => {
          const result = await this.deps.runner.run(
            executable,
            ['run', filePath],
            {
              cwd: path.dirname(filePath),
              env: this.bunkoEnv(filePath),
              timeoutMs: RUN_TIMEOUT_MS,
              token,
            },
          );
          this.appendProcessOutput('run', [filePath], result);
          this.deps.output.show(true);
          if (result.cancelled) {
            void vscode.window.showInformationMessage(
              'Mio run cancelled.',
            );
            return;
          }
          if (result.code === 0) {
            void vscode.window.showInformationMessage(
              'Mio run finished successfully.',
            );
          } else {
            void vscode.window.showErrorMessage(
              `Mio run failed with exit code ${result.code ?? 'unknown'}.`,
              'Open Output',
            ).then((choice) => {
              if (choice === 'Open Output') {
                this.deps.output.show(true);
              }
            });
          }
        },
      );
    } catch (error) {
      await this.reportProcessError('run', error);
    }
  }

  async buildCurrentFile(): Promise<void> {
    const editor = this.activeMioEditor();
    if (editor === undefined) {
      return;
    }
    const document = editor.document;
    if (!(await this.saveIfNeeded(document))) {
      return;
    }
    const executable = this.requireCli();
    if (executable === undefined) {
      return;
    }

    const filePath = document.uri.fsPath;
    const configuration = vscode.workspace.getConfiguration(
      'mio',
      document.uri,
    );
    const configuredDirectory = configuration.get<string>(
      'outputDirectory',
      '',
    );
    const workspaceFolder = vscode.workspace.getWorkspaceFolder(
      document.uri,
    )?.uri.fsPath;
    const build = resolveBuildOutput(
      filePath,
      configuredDirectory,
      workspaceFolder,
    );

    try {
      await fs.promises.mkdir(build.outputDirectory, { recursive: true });
    } catch (error) {
      void vscode.window.showErrorMessage(
        `Unable to create Mio output directory "${build.outputDirectory}": ${
          error instanceof Error ? error.message : String(error)
        }`,
      );
      return;
    }

    try {
      await vscode.window.withProgress(
        {
          location: vscode.ProgressLocation.Notification,
          title: 'Mio: Build Current File',
          cancellable: true,
        },
        async (_progress, token) => {
          const result = await this.deps.runner.run(
            executable,
            ['build', filePath, '-o', build.outputPath],
            {
              cwd: path.dirname(filePath),
              env: this.bunkoEnv(filePath),
              timeoutMs: BUILD_TIMEOUT_MS,
              token,
            },
          );
          this.appendProcessOutput(
            'build',
            [filePath, '-o', build.outputPath],
            result,
          );
          if (result.cancelled) {
            void vscode.window.showInformationMessage(
              'Mio build cancelled.',
            );
            return;
          }
          if (result.code === 0) {
            const choice = await vscode.window.showInformationMessage(
              `Mio build succeeded: ${build.outputPath}`,
              'Reveal Output',
              'Open Output',
            );
            if (choice === 'Reveal Output') {
              await vscode.commands.executeCommand(
                'revealFileInOS',
                vscode.Uri.file(build.outputPath),
              );
            } else if (choice === 'Open Output') {
              this.deps.output.show(true);
            }
          } else {
            this.deps.output.show(true);
            void vscode.window.showErrorMessage(
              `Mio build failed with exit code ${result.code ?? 'unknown'}.`,
              'Open Output',
            ).then((choice) => {
              if (choice === 'Open Output') {
                this.deps.output.show(true);
              }
            });
          }
        },
      );
    } catch (error) {
      await this.reportProcessError('build', error);
    }
  }

  async showCompilerVersion(): Promise<void> {
    const executable = this.requireCli();
    if (executable === undefined) {
      return;
    }
    try {
      const result = await this.deps.runner.run(
        executable,
        ['version'],
        { timeoutMs: VERSION_TIMEOUT_MS },
      );
      this.appendProcessOutput('version', [], result);
      if (result.code === 0) {
        const version =
          result.stdout.trim() || result.stderr.trim() || 'Mio';
        void vscode.window.showInformationMessage(version);
      } else {
        this.deps.output.show(true);
        void vscode.window.showErrorMessage(
          'Unable to read the Mio compiler version.',
          'Open Output',
        ).then((choice) => {
          if (choice === 'Open Output') {
            this.deps.output.show(true);
          }
        });
      }
    } catch (error) {
      await this.reportProcessError('version', error);
    }
  }

  openOutput(): void {
    this.deps.output.show(true);
  }

  private activeMioEditor(): vscode.TextEditor | undefined {
    const editor = vscode.window.activeTextEditor;
    if (editor === undefined) {
      void vscode.window.showWarningMessage('Open a .mio file first.');
      return undefined;
    }
    if (editor.document.languageId !== 'mio') {
      void vscode.window.showWarningMessage(
        'The active editor is not a Mio file.',
      );
      return undefined;
    }
    return editor;
  }

  private async saveIfNeeded(
    document: vscode.TextDocument,
  ): Promise<boolean> {
    if (document.uri.scheme !== 'file') {
      void vscode.window.showWarningMessage(
        'Save this file as a .mio file before running a Mio command.',
      );
      return false;
    }
    if (!document.isDirty) {
      return true;
    }
    const saved = await document.save();
    if (!saved) {
      void vscode.window.showErrorMessage(
        `Failed to save ${document.uri.fsPath}.`,
      );
      return false;
    }
    return true;
  }

  private requireCli(): string | undefined {
    const resolution = this.deps.cli.resolve();
    if (resolution.kind !== 'found') {
      this.deps.cli.showProblem(resolution);
      return undefined;
    }
    return resolution.executable;
  }

  private bunkoEnv(filePath: string): NodeJS.ProcessEnv | undefined {
    const bunkoRoot = findBunkoRoot(filePath);
    return bunkoRoot === undefined ? undefined : { MIO_BUNKO: bunkoRoot };
  }

  private appendProcessOutput(
    command: string,
    args: readonly string[],
    result: ProcessRunResult,
  ): void {
    const output = this.deps.output;
    const display = [command, ...args]
      .map(quoteDisplayArgument)
      .join(' ');
    output.appendLine('');
    output.appendLine(`> mio ${display}`);

    if (result.stdout !== '') {
      output.append(
        result.stdout.endsWith('\n')
          ? result.stdout
          : `${result.stdout}\n`,
      );
    }
    if (result.stderr !== '') {
      output.append(
        result.stderr.endsWith('\n')
          ? result.stderr
          : `${result.stderr}\n`,
      );
    }
    if (result.truncated) {
      output.appendLine('[output truncated]');
    }
    if (result.timedOut) {
      output.appendLine('[process timed out]');
    }
    if (result.cancelled) {
      output.appendLine('[process cancelled]');
    }
    output.appendLine(
      `[exit code: ${result.code ?? 'unknown'}${
        result.signal !== null ? `, signal: ${result.signal}` : ''
      }]`,
    );
  }

  private async reportProcessError(
    command: string,
    error: unknown,
  ): Promise<void> {
    const message =
      error instanceof Error ? error.message : String(error);
    this.deps.output.appendLine(`[${command}] ${message}`);

    if (
      error instanceof ProcessRunError &&
      (error.code === 'ENOENT' || error.code === 'EACCES')
    ) {
      const choice = await vscode.window.showErrorMessage(
        message,
        'Open Settings',
        'Open Output',
      );
      if (choice === 'Open Settings') {
        await vscode.commands.executeCommand(
          'workbench.action.openSettings',
          'mio.executablePath',
        );
      } else if (choice === 'Open Output') {
        this.deps.output.show(true);
      }
      return;
    }

    const choice = await vscode.window.showErrorMessage(
      message,
      'Open Output',
    );
    if (choice === 'Open Output') {
      this.deps.output.show(true);
    }
  }
}
