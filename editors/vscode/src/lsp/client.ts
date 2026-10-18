import type * as vscode from 'vscode';

// Mio 仓库当前没有 LSP Server。保留稳定接口，后续可直接替换为 vscode-languageclient 实现。
export interface MioLanguageClientHost {
  readonly available: boolean;
  start(context: vscode.ExtensionContext): Promise<void>;
  stop(): Promise<void>;
}

// 当前 MVP 不注册任何伪语言功能；语义能力必须来自 Mio Compiler / Resolver / TypeChecker。
export class UnavailableMioLanguageClient
  implements MioLanguageClientHost
{
  readonly available = false;

  async start(_context: vscode.ExtensionContext): Promise<void> {
    // 暂无 Mio LSP Server。
  }

  async stop(): Promise<void> {
    // 暂无需要停止的客户端。
  }
}

export function createMioLanguageClient(
  output: vscode.OutputChannel,
): MioLanguageClientHost {
  output.appendLine(
    '[lsp] No Mio language server is bundled; semantic features are not registered.',
  );
  return new UnavailableMioLanguageClient();
}
