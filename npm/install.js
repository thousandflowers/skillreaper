#!/usr/bin/env node
// Postinstall script: download the matching Go binary for this platform.
// Requires the first GitHub release to exist.
// Falls back to building from source when the release is missing.

import { execSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync } from "node:fs";
import { createWriteStream } from "node:fs";
import { homedir, platform, arch } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { pipeline } from "node:stream/promises";

const __dirname = dirname(fileURLToPath(import.meta.url));
const { name, version } = JSON.parse(readFileSync(join(__dirname, "package.json"), "utf-8"));

const osMap = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};
const archMap = {
  x64: "amd64",
  arm64: "arm64",
};

const os = osMap[platform()];
const archName = archMap[arch()];

if (!os || !archName) {
  console.error(`unsupported platform: ${platform()} ${arch()}`);
  process.exit(1);
}

const installDir = resolve(__dirname, "..", "bin");
mkdirSync(installDir, { recursive: true });

const binaryName = os === "windows" ? "skillreaper.exe" : "skillreaper";
const targetPath = join(installDir, binaryName);

if (existsSync(targetPath)) {
  console.log(`skillreaper already installed at ${targetPath}`);
  process.exit(0);
}

const tarball = `skillreaper_${os}_${archName}.tar.gz`;
const url = `https://github.com/thousandflowers/skillreaper/releases/latest/download/${tarball}`;

console.log(`downloading skillreaper ${version} for ${os}/${archName}...`);

try {
  const resp = await fetch(url);
  if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);

  const tmp = join(installDir, tarball);
  const fileStream = createWriteStream(tmp);
  await pipeline(resp.body, fileStream);

  execSync(
    os === "windows"
      ? `tar -xzf "${tmp}" -o "${binaryName}" -C "${installDir}"`
      : `tar -xzf "${tmp}" -C "${installDir}" "${binaryName}" && chmod +x "${targetPath}"`,
    { stdio: "inherit" }
  );

  execSync(`rm -f "${tmp}"`, { stdio: "inherit" });
  console.log(`installed skillreaper ${version} at ${targetPath}`);
} catch (err) {
  console.warn(`download failed: ${err.message}`);
  console.warn("falling back to building from source (requires Go)...");
  try {
    execSync("go version", { stdio: "pipe" });
    const src = resolve(import.meta.dirname, "..");
    execSync(`go build -o "${targetPath}" ./cmd/reap/`, { cwd: src, stdio: "inherit" });
    console.log(`built from source at ${targetPath}`);
  } catch (goErr) {
    console.error(`source build also failed: ${goErr.message}`);
    process.exit(1);
  }
}
