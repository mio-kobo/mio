import * as vscode from 'vscode';
import {
  resolveMioExecutable,
  type CliResolution,
  type CliResolveOptions,
} from './cliResolver';

const CACHE_TTL_MS = 5_000;

export interface MioCliServiceOptions extends CliResolveOptions {
  cacheTtlMs?: number;
}

// 读取 mio.executablePath 并缓存短暂的 CLI 解析结果。
export class MioCliService implements vscode.Disposable {
  private cached:
    | {
        value: CliResolution;
        at: number;
      }
    | undefined;

  constructor(private readonly options: MioCliServiceOptions = {}) {}

  resolve(configuredPathOverride?: string): CliResolution {
    const override = configuredPathOverride?.trim() ?? '';
    if (override !== '') {
      return resolveMioExecutable({
        ...this.options,
        configuredPath: override,
      });
    }

    const now = Date.now();
    const ttl = this.options.cacheTtlMs ?? CACHE_TTL_MS;
    if (this.cached !== undefined && now - this.cached.at < ttl) {
      return this.cached.value;
    }

    const configuredPath = vscode.workspace
      .getConfiguration('mio')
      .get<string>('executablePath', '');
    const value = resolveMioExecutable({
      ...this.options,
      configuredPath,
    });
    this.cached = { value, at: now };
    return value;
  }

  invalidate(): void {
    this.cached = undefined;
  }

  showProblem(resolution: CliResolution): void {
    if (resolution.kind === 'found') {
      return;
    }
    void (async () => {
      const choice = await vscode.window.showErrorMessage(
        resolution.message,
        'Open Settings',
        'Installation Guide',
      );
      if (choice === 'Open Settings') {
        await vscode.commands.executeCommand(
          'workbench.action.openSettings',
          'mio.executablePath',
        );
      } else if (choice === 'Installation Guide') {
        await vscode.env.openExternal(
          vscode.Uri.parse('https://github.com/ExMikuPro/Mio#quick-start'),
        );
      }
    })();
  }

  dispose(): void {
    this.invalidate();
  }
}
