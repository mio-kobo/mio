import * as vscode from 'vscode';
import { MioCliService } from './cli/cliService';
import { ProcessRunner } from './cli/processRunner';
import { MioCommands } from './commands/commands';
import { MioDiagnosticManager } from './diagnostics/diagnosticManager';
import { MioFoldingRangeProvider } from './editor/foldingProvider';
import { createMioLanguageClient } from './lsp/client';

interface Runtime {
  readonly output: vscode.OutputChannel;
  dispose(): void;
}

let runtime: Runtime | undefined;

export interface MioExtensionApi {
  readonly outputChannel: vscode.OutputChannel;
  clearDiagnostics(uri: vscode.Uri): void;
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
  const languageClient = createMioLanguageClient(output);

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
      if (event.affectsConfiguration('mio')) {
        cli.invalidate();
        diagnostics.onConfigurationChanged();
      }
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
      folding.dispose();
      for (const command of commandDisposables) {
        command.dispose();
      }
      void languageClient
        .stop()
        .catch((error: unknown) => {
          output.appendLine(`[lsp] ${errorMessage(error)}`);
        });
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
    dispose: () => disposable.dispose(),
  };

  output.appendLine('Mio Language Support activated.');
  try {
    await languageClient.start(context);
  } catch (error) {
    output.appendLine(`[lsp] ${errorMessage(error)}`);
  }
  output.appendLine('Mio Language Support ready.');
  return api;
}

export function deactivate(): void {
  runtime?.dispose();
  runtime = undefined;
}
