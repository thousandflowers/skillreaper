#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import { createWriteStream, existsSync } from 'node:fs';
import { platform, arch } from 'node:os';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const bin = resolve(__dirname, 'skillreaper');

async function download() {
  const osMap = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
  const archMap = { x64: 'amd64', arm64: 'arm64' };
  const os = osMap[platform()];
  const archName = archMap[arch()];

  if (!os || !archName) {
    console.error(`unsupported platform: ${platform()} ${arch()}`);
    process.exit(1);
  }

  const tarball = `skillreaper_${os}_${archName}.tar.gz`;
  const url = `https://github.com/thousandflowers/skillreaper/releases/latest/download/${tarball}`;
  console.error(`downloading skillreaper for ${os}/${archName}...`);

  const resp = await fetch(url);
  if (!resp.ok) {
    console.error(`download failed: HTTP ${resp.status}`);
    process.exit(1);
  }

  const tmp = resolve(__dirname, tarball);
  const { pipeline } = await import('node:stream/promises');
  await pipeline(resp.body, createWriteStream(tmp));

  spawnSync('tar', ['-xzf', tmp, '-C', __dirname], { stdio: 'inherit' });
  if (os !== 'windows') spawnSync('chmod', ['+x', bin], { stdio: 'inherit' });
  spawnSync('rm', ['-f', tmp], { stdio: 'inherit' });
  console.error(`installed skillreaper at ${bin}`);
}

if (!existsSync(bin)) {
  await download();
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
process.exit(result.status ?? 1);
