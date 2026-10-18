import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

export type CliResolution =
  | {
      kind: 'found';
      executable: string;
      source: 'configured' | 'path';
    }
  | {
      kind: 'notFound';
      message: string;
    }
  | {
      kind: 'invalid';
      executable: string;
      message: string;
    };

export interface CliResolveOptions {
  configuredPath?: string;
  envPath?: string | undefined;
  platform?: NodeJS.Platform;
  homeDir?: string;
  fileExists?: (value: string) => boolean;
  isExecutable?: (value: string) => boolean;
}

function defaultFileExists(value: string): boolean {
  try {
    return fs.statSync(value).isFile();
  } catch {
    return false;
  }
}

function defaultIsExecutable(value: string): boolean {
  try {
    fs.accessSync(value, fs.constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function stripSurroundingQuotes(value: string): string {
  const trimmed = value.trim();
  if (trimmed.length >= 2) {
    const first = trimmed[0];
    const last = trimmed[trimmed.length - 1];
    if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
      return trimmed.slice(1, -1).trim();
    }
  }
  return trimmed;
}

function normalizeConfiguredPath(value: string, homeDir: string): string {
  const unquoted = stripSurroundingQuotes(value);
  if (unquoted === '') {
    return '';
  }
  let expanded = unquoted;
  if (expanded === '~') {
    expanded = homeDir;
  } else if (expanded.startsWith('~/') || expanded.startsWith('~\\')) {
    expanded = path.join(homeDir, expanded.slice(2));
  }
  return path.resolve(expanded);
}

// 解析 Mio CLI。显式配置优先，其次只搜索系统 PATH，不执行项目内同名文件。
export function resolveMioExecutable(
  options: CliResolveOptions = {},
): CliResolution {
  const platform = options.platform ?? process.platform;
  const homeDir = options.homeDir ?? os.homedir();
  const fileExists = options.fileExists ?? defaultFileExists;
  const isExecutable = options.isExecutable ?? defaultIsExecutable;

  const configured = normalizeConfiguredPath(
    options.configuredPath ?? '',
    homeDir,
  );
  if (configured !== '') {
    if (!fileExists(configured)) {
      return {
        kind: 'invalid',
        executable: configured,
        message: `Configured Mio CLI does not exist: ${configured}. Update mio.executablePath.`,
      };
    }
    if (platform === 'win32' && /\.(cmd|bat)$/i.test(configured)) {
      return {
        kind: 'invalid',
        executable: configured,
        message:
          'Mio Language Support only launches native executables. ' +
          'Configure the underlying mio.exe instead of a .cmd or .bat launcher.',
      };
    }
    if (platform !== 'win32' && !isExecutable(configured)) {
      return {
        kind: 'invalid',
        executable: configured,
        message: `Mio CLI is not executable: ${configured}. Run chmod +x or update mio.executablePath.`,
      };
    }
    return { kind: 'found', executable: configured, source: 'configured' };
  }

  const executableNames =
    platform === 'win32' ? ['mio.exe', 'mio'] : ['mio'];
  const pathValue = options.envPath ?? process.env.PATH ?? '';
  const directories = pathValue
    .split(path.delimiter)
    .map((entry) => entry.trim())
    .filter((entry) => entry !== '');

  let permissionProblem: string | undefined;
  for (const directory of directories) {
    for (const name of executableNames) {
      const candidate = path.join(directory, name);
      if (!fileExists(candidate)) {
        continue;
      }
      if (platform !== 'win32' && !isExecutable(candidate)) {
        permissionProblem ??= candidate;
        continue;
      }
      return { kind: 'found', executable: candidate, source: 'path' };
    }
  }

  if (permissionProblem !== undefined) {
    return {
      kind: 'invalid',
      executable: permissionProblem,
      message: `Mio CLI was found but is not executable: ${permissionProblem}.`,
    };
  }

  return {
    kind: 'notFound',
    message:
      'Mio CLI was not found. Install Mio, add mio/mio.exe to PATH, ' +
      'or set mio.executablePath in Settings.',
  };
}
