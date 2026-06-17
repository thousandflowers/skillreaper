#!/usr/bin/env node
// Postinstall script: download the matching Go binary for this platform.
// Requires a GitHub release matching npm package version to exist.
// Falls back to building from source when the release is missing.

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, unlinkSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { downloadVerifiedReleaseAsset, platformTarget } from "./lib/release.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const { version } = JSON.parse(readFileSync(join(__dirname, "package.json"), "utf-8"));
const { os, archName, binaryName, tarball } = platformTarget();

const installDir = resolve(__dirname, "bin");
mkdirSync(installDir, { recursive: true });

const targetPath = join(installDir, binaryName);

if (existsSync(targetPath)) {
  console.log(`skillreaper already installed at ${targetPath}`);
  process.exit(0);
}

console.log(`downloading skillreaper ${version} for ${os}/${archName}...`);

const tmp = join(installDir, tarball);

try {
  await downloadVerifiedReleaseAsset({ version, assetName: tarball, destination: tmp });

  execFileSync("tar", ["-xzf", tmp, "-C", installDir, binaryName], { stdio: "inherit" });
  if (os !== "windows") {
    execFileSync("chmod", ["+x", targetPath], { stdio: "inherit" });
  }

  unlinkSync(tmp);
  console.log(`installed skillreaper ${version} at ${targetPath}`);
} catch (err) {
  if (existsSync(tmp)) {
    unlinkSync(tmp);
  }
  console.warn(`download failed: ${err.message}`);
  console.warn("falling back to building from source (requires Go)...");
  try {
    execFileSync("go", ["version"], { stdio: "pipe" });
    const src = resolve(__dirname, "..");
    execFileSync("go", ["build", "-o", targetPath, "./cmd/reap/"], { cwd: src, stdio: "inherit" });
    console.log(`built from source at ${targetPath}`);
  } catch (goErr) {
    console.error(`source build also failed: ${goErr.message}`);
    process.exit(1);
  }
}
