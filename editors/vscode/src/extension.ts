import * as vscode from 'vscode';
import { MioCliService } from './cli/cliService';
import { ProcessRunner } from './cli/processRunner';
import { MioCommands } from './commands/commands';
import { MioDiagnosticManager } from './diagnostics/diagnosticManager';
import { MioFoldingRangeProvider } from './editor/foldingProvider';
import {
  MioLanguageClient,
  type MioDiagnosticsOwner,
  type MioLanguageServerState,
} from './lsp/client';

interface Runtime {
  readonly output: vscode.OutputChannel;
  dispose(): void;
}

let runtime: Runtime | undefined;

export interface MioExtensionApi {
  readonly outputChannel: vscode.OutputChannel;
  clearDiagnostics(uri: vscode.Uri): void;
  getLanguageServerState(): MioLanguageServerState;
  getDiagnosticsOwner(): MioDiagnosticsOwner;
  dispose(): void;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export async function activate(
  context: vscode.ExtensionContext,
): Promise<MioExtensionApi> {
  const output = vscode.window.createOutputChannel('Mio');
  const runner = new ProcessRunner();
  const cli = new MioCliService();
  const diagnostics = new MioDiagnosticManager({ output, runner, cli });
  const commands = new MioCommands({ output, runner, cli, diagnostics });
  const languageClient = new MioLanguageClient({ output, cli });

  // server 就绪时由它独占诊断；其余状态（禁用、失败、停止）回到 CLI 回退。
  const languageStateListener = languageClient.onDidChangeState((state) => {
    const lspOwnsDiagnostics = state === 'ready';
    diagnostics.setFallbackEnabled(!lspOwnsDiagnostics);
    output.appendLine(
      `[lsp] state=${state} diagnostics=${lspOwnsDiagnostics ? 'language server' : 'CLI fallback'}`,
    );
  });

  const guard = (
    action: () => Promise<void>,
  ): (() => Promise<void>) => {
    return async () => {
      try {
        await action();
      } catch (error) {
        const message = errorMessage(error);
        output.appendLine(`[command] ${message}`);
        void vscode.window
          .showErrorMessage(`Mio command failed: ${message}`, 'Open Output')
          .then((choice) => {
            if (choice === 'Open Output') {
              output.show(true);
            }
          });
      }
    };
  };

  const commandDisposables = [
    vscode.commands.registerCommand(
      'mio.checkCurrentFile',
      guard(() => commands.checkCurrentFile()),
    ),
    vscode.commands.registerCommand(
      'mio.runCurrentFile',
      guard(() => commands.runCurrentFile()),
    ),
    vscode.commands.registerCommand(
      'mio.buildCurrentFile',
      guard(() => commands.buildCurrentFile()),
    ),
    vscode.commands.registerCommand(
      'mio.showCompilerVersion',
      guard(() => commands.showCompilerVersion()),
    ),
    vscode.commands.registerCommand('mio.openOutput', () =>
      commands.openOutput(),
    ),
  ];

  const folding = vscode.languages.registerFoldingRangeProvider(
    { language: 'mio' },
    new MioFoldingRangeProvider(),
  );
  const saveListener = vscode.workspace.onDidSaveTextDocument((document) => {
    diagnostics.onDidSave(document);
  });
  const closeListener = vscode.workspace.onDidCloseTextDocument((document) => {
    diagnostics.onDidClose(document);
  });
  const configurationListener =
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (!event.affectsConfiguration('mio')) {
        return;
      }
      cli.invalidate();
      if (
        event.affectsConfiguration('mio.executablePath') ||
        event.affectsConfiguration('mio.languageServer.enabled')
      ) {
        void languageClient.restart(context);
        return;
      }
      if (languageClient.state !== 'ready') {
        diagnostics.onConfigurationChanged();
      }
      languageClient.syncConfiguration();
    });

  let disposed = false;
  const disposable: vscode.Disposable = {
    dispose: () => {
      if (disposed) {
        return;
      }
      disposed = true;
      saveListener.dispose();
      closeListener.dispose();
      configurationListener.dispose();
      languageStateListener.dispose();
      folding.dispose();
      for (const command of commandDisposables) {
        command.dispose();
      }
      languageClient.dispose();
      diagnostics.dispose();
      cli.dispose();
      runner.dispose();
      output.dispose();
    },
  };

  context.subscriptions.push(disposable);
  runtime = { output, dispose: () => disposable.dispose() };

  const api: MioExtensionApi = {
    outputChannel: output,
    clearDiagnostics: (uri) => diagnostics.clearDocument(uri),
    getLanguageServerState: () => languageClient.state,
    getDiagnosticsOwner: () => diagnostics.diagnosticsOwner,
    dispose: () => disposable.dispose(),
  };

  output.appendLine('Mio — Programming Language Tools activated.');
  try {
    await languageClient.start(context);
  } catch (error) {
    output.appendLine(`[lsp] ${errorMessage(error)}`);
  }
  output.appendLine('Mio — Programming Language Tools ready.');
  return api;
}

export function deactivate(): void {
  runtime?.dispose();
  runtime = undefined;
}
