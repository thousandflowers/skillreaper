#!/usr/bin/env node
import { spawn, spawnSync } from 'node:child_process';
import { accessSync, constants as fsConstants, existsSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync } from 'node:fs';
import { homedir, constants as osConstants } from 'node:os';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { binaryHome, downloadVerifiedReleaseAsset, isSourceCheckout, platformTarget } from '../lib/release.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const { version } = JSON.parse(readFileSync(resolve(__dirname, '..', 'package.json'), 'utf-8'));
const versionLabel = isSourceCheckout(version) ? 'latest' : version;
const { os, archName, binaryName, tarball } = platformTarget();
const writable = (dir) => {
  try {
    accessSync(dir, fsConstants.W_OK);
    return true;
  } catch {
    return false;
  }
};
const packaged = resolve(__dirname, binaryName);
// A global install owned by root leaves bin/ read-only: the binary then goes to
// the user's cache, where every later run finds it.
const home = existsSync(packaged)
  ? __dirname
  : binaryHome({ packageDir: __dirname, version, home: homedir(), env: process.env, platformName: process.platform, writable });
const bin = resolve(home, binaryName);

function run(command, args) {
  const result = spawnSync(command, args, { stdio: 'inherit' });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(`${command} exited with status ${result.status}`);
  }
}

async function download() {
  mkdirSync(home, { recursive: true });
  // Everything happens in a folder of this run's own, and the binary is moved
  // into place whole at the end. Two first runs at once shared one archive
  // path: one removed it under the other, and a binary still being unpacked
  // already looked installed.
  const stage = mkdtempSync(resolve(home, '.download-'));
  const tmp = resolve(stage, tarball);
  console.error(`downloading skillreaper ${versionLabel} for ${os}/${archName}...`);

  try {
    await downloadVerifiedReleaseAsset({ version, assetName: tarball, destination: tmp });
    run('tar', ['-xzf', tmp, '-C', stage, binaryName]);
    if (os !== 'windows') run('chmod', ['+x', resolve(stage, binaryName)]);
    try {
      renameSync(resolve(stage, binaryName), bin);
    } catch (err) {
      if (!existsSync(bin)) throw err; // another run put it there first: that one is as good
    }
    console.error(`installed skillreaper ${versionLabel} at ${bin}`);
  } catch (err) {
    console.error(`download failed: ${err.message}`);
    process.exitCode = 1;
  } finally {
    rmSync(stage, { recursive: true, force: true });
  }
  if (process.exitCode) process.exit(process.exitCode);
}

if (!existsSync(bin)) {
  await download();
}

// Spawned, not spawnSync: a blocked wrapper dies on the first signal and hands
// the shell back while the tool is still finishing, with nobody to tell it to
// stop on SIGTERM. Ctrl-C reaches the whole foreground group by itself, so that
// one is only waited out; the others are passed on.
const child = spawn(bin, process.argv.slice(2), {
  stdio: 'inherit',
  env: { ...process.env, SKILLREAPER_WRAPPER: process.argv[1] },
});
process.on('SIGINT', () => {});
for (const signal of ['SIGTERM', 'SIGHUP']) {
  process.on(signal, () => child.kill(signal));
}
child.on('error', (err) => {
  console.error(`could not start skillreaper: ${err.message}`);
  process.exit(1);
});
child.on('exit', (code, signal) => {
  process.exit(code ?? 128 + (osConstants.signals[signal] ?? 0));
});
