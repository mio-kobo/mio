// 纯函数形式的启动参数构造，便于在普通 Node 单元测试中验证。
export interface MioServerLaunch {
  command: string;
  args: string[];
  options: {
    cwd?: string;
    env?: NodeJS.ProcessEnv;
  };
}

export interface MioServerLaunchInput {
  executable: string;
  workspaceFolder?: string | undefined;
  bunkoRoot?: string | undefined;
  baseEnv?: NodeJS.ProcessEnv | undefined;
}

// 启动 `mio lsp`。仅通过参数数组传递，不拼接 shell 命令。
export function buildServerLaunch(
  input: MioServerLaunchInput,
): MioServerLaunch {
  const options: MioServerLaunch['options'] = {};
  if (input.workspaceFolder !== undefined && input.workspaceFolder !== '') {
    options.cwd = input.workspaceFolder;
  }
  const env = { ...(input.baseEnv ?? process.env) };
  if (input.bunkoRoot !== undefined && input.bunkoRoot !== '') {
    env.MIO_BUNKO = input.bunkoRoot;
  }
  options.env = env;
  return {
    command: input.executable,
    args: ['lsp'],
    options,
  };
}
