import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

// 展开用户目录，便于在设置中填写 ~/bin/mio。
export function expandHome(value: string, homeDir: string = os.homedir()): string {
  if (value === '~') {
    return homeDir;
  }
  if (value.startsWith('~/') || value.startsWith('~\\')) {
    return path.join(homeDir, value.slice(2));
  }
  return value;
}

export function isDirectory(value: string): boolean {
  try {
    return fs.statSync(value).isDirectory();
  } catch {
    return false;
  }
}

export function isFile(value: string): boolean {
  try {
    return fs.statSync(value).isFile();
  } catch {
    return false;
  }
}

export function isExecutable(value: string): boolean {
  try {
    fs.accessSync(value, fs.constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

// 与 CLI 的 bunkoRoot 保持一致：优先 MIO_BUNKO，然后从源文件向上查找 bunko/。
export function findBunkoRoot(
  filePath: string,
  env: NodeJS.ProcessEnv = process.env,
): string | undefined {
  const configured = env.MIO_BUNKO?.trim();
  if (configured && isDirectory(configured)) {
    return configured;
  }

  let current = path.dirname(path.resolve(filePath));
  for (;;) {
    const candidate = path.join(current, 'bunko');
    if (isDirectory(candidate)) {
      return candidate;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      return undefined;
    }
    current = parent;
  }
}

export interface BuildOutput {
  outputDirectory: string;
  outputPath: string;
}

// 根据 mio.outputDirectory 计算构建输出路径；空值保持 CLI 默认行为。
export function resolveBuildOutput(
  sourcePath: string,
  configuredDirectory: string,
  workspaceFolder: string | undefined,
): BuildOutput {
  const directory = path.dirname(sourcePath);
  const baseName = path.basename(sourcePath, path.extname(sourcePath));
  const configured = configuredDirectory.trim();

  if (configured === '') {
    return {
      outputDirectory: directory,
      outputPath: path.join(directory, `${baseName}.mochi`),
    };
  }

  const fallback = workspaceFolder ?? directory;
  const expanded = expandHome(
    configured.replace(/\$\{workspaceFolder\}/g, fallback),
  );
  const outputDirectory = path.isAbsolute(expanded)
    ? path.normalize(expanded)
    : path.resolve(fallback, expanded);

  return {
    outputDirectory,
    outputPath: path.join(outputDirectory, `${baseName}.mochi`),
  };
}

export function samePath(left: string, right: string): boolean {
  const a = path.resolve(left);
  const b = path.resolve(right);
  if (process.platform === 'win32') {
    return a.toLowerCase() === b.toLowerCase();
  }
  return a === b;
}
