import * as path from 'node:path';
import * as vscode from 'vscode';
import {
  LanguageClient,
  State,
  type LanguageClientOptions,
  type ServerOptions,
} from 'vscode-languageclient/node';
import type { MioCliService } from '../cli/cliService';
import { findBunkoRoot } from '../util/paths';
import { buildServerLaunch } from './serverOptions';

export type MioLanguageServerState =
  | 'disabled'
  | 'starting'
  | 'ready'
  | 'failed'
  | 'stopped';

export type MioDiagnosticsOwner = 'lsp' | 'cli';

export type MioLanguageServerStateListener = (
  state: MioLanguageServerState,
) => void;

// 语言服务器生命周期接口；扩展与测试都通过它观察状态。
export interface MioLanguageClientHost {
  readonly state: MioLanguageServerState;
  start(context: vscode.ExtensionContext): Promise<void>;
  stop(): Promise<void>;
  restart(context: vscode.ExtensionContext): Promise<void>;
  syncConfiguration(): void;
  onDidChangeState(
    listener: MioLanguageServerStateListener,
  ): vscode.Disposable;
  dispose(): void;
}

export interface MioLanguageClientOptions {
  output: vscode.OutputChannel;
  cli: MioCliService;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

// 通过 vscode-languageclient 连接 `mio lsp`。
// server 未就绪时保持 CLI 回退，就绪后由 server 独占诊断。
export class MioLanguageClient implements MioLanguageClientHost {
  private client: LanguageClient | undefined;
  private currentState: MioLanguageServerState = 'disabled';
  private readonly listeners = new Set<MioLanguageServerStateListener>();
  private stateSubscription: vscode.Disposable | undefined;
  private starting = false;
  private stopping = false;
  private disposed = false;
  private readonly outputChannel: vscode.OutputChannel;

  constructor(private readonly options: MioLanguageClientOptions) {
    // 扩展先释放 OutputChannel 时，language client 的进程退出回调仍可能写入；
    // 代理在 dispose 后静默丢弃，避免 "Channel has been closed" 未捕获异常。
    this.outputChannel = {
      name: options.output.name,
      append: (value: string) => this.writeOutput(() => options.output.append(value)),
      appendLine: (value: string) =>
        this.writeOutput(() => options.output.appendLine(value)),
      replace: (value: string) =>
        this.writeOutput(() => options.output.replace(value)),
      clear: () => this.writeOutput(() => options.output.clear()),
      show: (first?: vscode.ViewColumn | boolean, second?: boolean) =>
        this.writeOutput(() =>
          options.output.show(first as vscode.ViewColumn, second),
        ),
      hide: () => this.writeOutput(() => options.output.hide()),
      dispose: () => {
        // 输出通道由扩展统一释放。
      },
    };
  }

  get state(): MioLanguageServerState {
    return this.currentState;
  }

  get ready(): boolean {
    return this.currentState === 'ready' && this.client !== undefined;
  }

  async start(_context: vscode.ExtensionContext): Promise<void> {
    if (this.client !== undefined || this.starting) {
      return;
    }
    if (!this.enabled) {
      this.log('language server is disabled by mio.languageServer.enabled');
      this.setState('disabled');
      return;
    }

    const resolution = this.options.cli.resolve();
    if (resolution.kind !== 'found') {
      this.log(`language server not started: ${resolution.message}`);
      this.setState('failed');
      return;
    }

    this.starting = true;
    this.setState('starting');
    let client: LanguageClient | undefined;
    try {
      const workspaceFolder =
        vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
      const bunkoRoot =
        workspaceFolder === undefined
          ? undefined
          : findBunkoRoot(path.join(workspaceFolder, 'mio-workspace.mio'));
      const launch = buildServerLaunch({
        executable: resolution.executable,
        workspaceFolder,
        bunkoRoot,
        baseEnv: process.env,
      });
      const serverOptions: ServerOptions = launch;
      const clientOptions: LanguageClientOptions = {
        documentSelector: [
          { scheme: 'file', language: 'mio' },
          { scheme: 'untitled', language: 'mio' },
        ],
        outputChannel: this.outputChannel,
        synchronize: { configurationSection: 'mio' },
        initializationOptions: {
          diagnosticsEnabled: this.diagnosticsEnabled,
          diagnosticsDelayMs: 150,
        },
      };

      client = new LanguageClient(
        'mio',
        'MioCore Language Server',
        serverOptions,
        clientOptions,
      );
      this.client = client;
      this.stateSubscription = client.onDidChangeState((event) => {
        if (this.client !== client || this.stopping) {
          return;
        }
        // vscode-languageclient 会在崩溃后自动重启；保留 client 引用，
        // 只有真正 Running 时才把诊断交还给 server。
        if (event.newState === State.Starting) {
          this.setState('starting');
          return;
        }
        if (event.newState === State.Running) {
          this.log('language server running');
          this.setState('ready');
          this.syncConfiguration();
          return;
        }
        if (event.newState === State.Stopped) {
          this.log('language server stopped');
          this.setState('stopped');
        }
      });

      await client.start();
      if (this.client !== client) {
        return;
      }
      this.log('language server ready');
      this.setState('ready');
      this.syncConfiguration();
    } catch (error) {
      this.log(`language server unavailable: ${errorMessage(error)}`);
      if (client !== undefined) {
        this.stateSubscription?.dispose();
        this.stateSubscription = undefined;
        this.client = undefined;
        try {
          await client.stop();
        } catch {
          // 启动失败时进程可能已经退出。
        }
        try {
          await client.dispose();
        } catch {
          // 忽略重复释放。
        }
      }
      this.setState('failed');
    } finally {
      this.starting = false;
    }
  }

  async stop(): Promise<void> {
    await this.shutdownClient();
    this.setState('stopped');
  }

  async restart(context: vscode.ExtensionContext): Promise<void> {
    await this.shutdownClient();
    await this.start(context);
  }

  syncConfiguration(): void {
    const client = this.client;
    if (client === undefined) {
      return;
    }
    const configuration = vscode.workspace.getConfiguration('mio');
    const settings = {
      mio: {
        diagnostics: {
          enabled: configuration.get<boolean>('diagnostics.enabled', true),
        },
        languageServer: {
          enabled: configuration.get<boolean>('languageServer.enabled', true),
        },
      },
    };
    client
      .sendNotification('workspace/didChangeConfiguration', { settings })
      .catch((error: unknown) => {
        this.log(`configuration sync failed: ${errorMessage(error)}`);
      });
  }

  onDidChangeState(
    listener: MioLanguageServerStateListener,
  ): vscode.Disposable {
    this.listeners.add(listener);
    return {
      dispose: () => {
        this.listeners.delete(listener);
      },
    };
  }

  dispose(): void {
    this.disposed = true;
    void this.shutdownClient();
    this.listeners.clear();
  }

  private async shutdownClient(): Promise<void> {
    this.stopping = true;
    const client = this.client;
    this.client = undefined;
    this.stateSubscription?.dispose();
    this.stateSubscription = undefined;
    if (client !== undefined) {
      try {
        await client.stop();
      } catch (error) {
        this.log(`language server stop failed: ${errorMessage(error)}`);
      }
      try {
        await client.dispose();
      } catch {
        // 忽略重复释放。
      }
    }
    this.stopping = false;
  }

  private get enabled(): boolean {
    return vscode.workspace
      .getConfiguration('mio')
      .get<boolean>('languageServer.enabled', true);
  }

  private get diagnosticsEnabled(): boolean {
    return vscode.workspace
      .getConfiguration('mio')
      .get<boolean>('diagnostics.enabled', true);
  }

  private setState(state: MioLanguageServerState): void {
    if (this.currentState === state) {
      return;
    }
    this.currentState = state;
    for (const listener of [...this.listeners]) {
      try {
        listener(state);
      } catch (error) {
        this.log(`state listener failed: ${errorMessage(error)}`);
      }
    }
  }

  private log(message: string): void {
    this.writeOutput(() => this.options.output.appendLine(`[lsp] ${message}`));
  }

  private writeOutput(action: () => void): void {
    if (this.disposed) {
      return;
    }
    try {
      action();
    } catch {
      // OutputChannel 可能已被宿主关闭。
    }
  }
}
