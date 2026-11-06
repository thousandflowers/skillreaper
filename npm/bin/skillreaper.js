#!/usr/bin/env node
import { spawn, spawnSync } from 'node:child_process';
import { existsSync, readFileSync, unlinkSync } from 'node:fs';
import { constants as osConstants } from 'node:os';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { downloadVerifiedReleaseAsset, isSourceCheckout, platformTarget } from '../lib/release.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const { version } = JSON.parse(readFileSync(resolve(__dirname, '..', 'package.json'), 'utf-8'));
const versionLabel = isSourceCheckout(version) ? 'latest' : version;
const { os, archName, binaryName, tarball } = platformTarget();
const bin = resolve(__dirname, binaryName);

function run(command, args) {
  const result = spawnSync(command, args, { stdio: 'inherit' });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(`${command} exited with status ${result.status}`);
  }
}

async function download() {
  const tmp = resolve(__dirname, tarball);
  console.error(`downloading skillreaper ${versionLabel} for ${os}/${archName}...`);

  try {
    await downloadVerifiedReleaseAsset({ version, assetName: tarball, destination: tmp });
    run('tar', ['-xzf', tmp, '-C', __dirname, binaryName]);
    if (os !== 'windows') run('chmod', ['+x', bin]);
    unlinkSync(tmp);
    console.error(`installed skillreaper ${versionLabel} at ${bin}`);
  } catch (err) {
    if (existsSync(tmp)) unlinkSync(tmp);
    console.error(`download failed: ${err.message}`);
    process.exit(1);
  }
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
