import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(scriptDir, '..', '..');
const tmpDir = resolve(repoRoot, 'tmp');
const configPath = resolve(repoRoot, '.golangci.yaml');
const tempConfigPath = resolve(tmpDir, 'golangci-no-modernize.yaml');

mkdirSync(tmpDir, { recursive: true });

const configText = readFileSync(configPath, 'utf8');
const patchedConfigText = configText.replace(/^[ \t]*- modernize\r?\n/m, '');
writeFileSync(tempConfigPath, patchedConfigText, 'utf8');

const git = (args, options = {}) =>
  execFileSync('git', ['-C', repoRoot, ...args], {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
    ...options,
  }).trim();

const hasRef = (ref) => {
  try {
    execFileSync('git', ['-C', repoRoot, 'rev-parse', '--verify', ref], { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
};

const dirtyTrackedTree = git(['status', '--porcelain', '--untracked-files=no']) !== '';
const baseRev = process.env.LINT_BASE_REV?.trim() || (dirtyTrackedTree ? 'HEAD' : hasRef('HEAD~1') ? 'HEAD~1' : 'HEAD');

const result = spawnSync('golangci-lint', ['run', '--config', tempConfigPath, '--new-from-rev', baseRev], {
  cwd: repoRoot,
  stdio: 'inherit',
});

rmSync(tempConfigPath, { force: true });

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

process.exit(result.status ?? 1);
