import { createHash } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { arch, platform } from "node:os";
import { basename } from "node:path";
import { pipeline } from "node:stream/promises";

const REPO_RELEASES = "https://github.com/thousandflowers/skillreaper/releases";
const FETCH_TIMEOUT_MS = 30_000;

const osMap = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const archMap = {
  x64: "amd64",
  arm64: "arm64",
};

// A source checkout (git clone) carries the sentinel version below instead
// of a real release. npm publish rewrites it to the tag version, so the
// sentinel is only ever seen when installing from source — in which case we
// fetch the latest published release rather than a stale pinned one.
const SOURCE_VERSIONS = new Set(["0.0.0", "0.0.0-dev", "dev"]);

export function isSourceCheckout(version) {
  return !version || SOURCE_VERSIONS.has(version);
}

export function releaseTagForVersion(version) {
  if (!version) {
    throw new Error("package version is required to select a release");
  }
  return version.startsWith("v") ? version : `v${version}`;
}

export function latestDownloadUrl(assetName) {
  return `${REPO_RELEASES}/latest/download/${encodeURIComponent(assetName)}`;
}

export function platformTarget(platformName = platform(), archName = arch()) {
  const os = osMap[platformName];
  const releaseArch = archMap[archName];

  if (!os || !releaseArch) {
    throw new Error(`unsupported platform: ${platformName} ${archName}`);
  }

  return {
    os,
    archName: releaseArch,
    binaryName: os === "windows" ? "skillreaper.exe" : "skillreaper",
    tarball: `skillreaper_${os}_${releaseArch}.tar.gz`,
  };
}

export function releaseDownloadUrl(tag, assetName) {
  return `${REPO_RELEASES}/download/${encodeURIComponent(tag)}/${encodeURIComponent(assetName)}`;
}

export function checksumsDownloadUrl(tag) {
  return releaseDownloadUrl(tag, "checksums.txt");
}

export function checksumForAsset(checksumsText, assetName) {
  const wanted = basename(assetName);

  for (const rawLine of checksumsText.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line) continue;

    const match = line.match(/^([a-fA-F0-9]{64})\s+[* ]?(.+)$/);
    if (!match) continue;

    if (basename(match[2].trim()) === wanted) {
      return match[1].toLowerCase();
    }
  }

  throw new Error(`checksum for ${wanted} not found in checksums.txt`);
}

export async function sha256File(filePath) {
  const hash = createHash("sha256");
  const stream = createReadStream(filePath);

  for await (const chunk of stream) {
    hash.update(chunk);
  }

  return hash.digest("hex");
}

export async function verifySha256File(filePath, expectedHash) {
  const actualHash = await sha256File(filePath);
  if (actualHash !== expectedHash.toLowerCase()) {
    throw new Error(
      `checksum mismatch for ${basename(filePath)}: expected ${expectedHash}, got ${actualHash}`
    );
  }
}

async function fetchOk(url) {
  const resp = await fetch(url, { signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
  }
  return resp;
}

export async function downloadVerifiedReleaseAsset({ version, assetName, destination }) {
  const source = isSourceCheckout(version);
  const checksumsUrl = source
    ? latestDownloadUrl("checksums.txt")
    : checksumsDownloadUrl(releaseTagForVersion(version));
  const assetUrl = source
    ? latestDownloadUrl(assetName)
    : releaseDownloadUrl(releaseTagForVersion(version), assetName);

  // Checksums and asset come from the same release, so the hash check below
  // still guarantees integrity even when resolving "latest".
  const checksumsResp = await fetchOk(checksumsUrl);
  const expectedHash = checksumForAsset(await checksumsResp.text(), assetName);
  const assetResp = await fetchOk(assetUrl);

  if (!assetResp.body) {
    throw new Error(`empty response body for ${assetName}`);
  }

  await pipeline(assetResp.body, createWriteStream(destination));
  await verifySha256File(destination, expectedHash);
}
