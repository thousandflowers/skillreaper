import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  checksumForAsset,
  checksumsDownloadUrl,
  platformTarget,
  releaseDownloadUrl,
  releaseTagForVersion,
  verifySha256File,
} from "../lib/release.js";

test("release URLs are pinned to the package version tag", () => {
  const tag = releaseTagForVersion("0.1.2");
  const url = releaseDownloadUrl(tag, "skillreaper_darwin_arm64.tar.gz");

  assert.equal(tag, "v0.1.2");
  assert.equal(
    url,
    "https://github.com/thousandflowers/skillreaper/releases/download/v0.1.2/skillreaper_darwin_arm64.tar.gz"
  );
  assert.equal(checksumsDownloadUrl(tag).includes("/releases/latest/"), false);
  assert.equal(url.includes("/releases/latest/"), false);
});

test("platform target uses release archive and binary names", () => {
  assert.deepEqual(platformTarget("darwin", "arm64"), {
    os: "darwin",
    archName: "arm64",
    binaryName: "skillreaper",
    tarball: "skillreaper_darwin_arm64.tar.gz",
  });
  assert.deepEqual(platformTarget("win32", "x64"), {
    os: "windows",
    archName: "amd64",
    binaryName: "skillreaper.exe",
    tarball: "skillreaper_windows_amd64.tar.gz",
  });
});

test("checksum parser selects the matching release asset", () => {
  const checksums = `
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  skillreaper_darwin_amd64.tar.gz
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  skillreaper_darwin_arm64.tar.gz
`;

  assert.equal(
    checksumForAsset(checksums, "skillreaper_darwin_arm64.tar.gz"),
    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  );
});

test("checksum verification rejects mismatched archives", async () => {
  const dir = await mkdtemp(join(tmpdir(), "skillreaper-release-"));
  const file = join(dir, "skillreaper_darwin_arm64.tar.gz");

  try {
    await writeFile(file, "archive bytes");
    const expectedHash = createHash("sha256").update("archive bytes").digest("hex");

    await verifySha256File(file, expectedHash);
    await assert.rejects(
      verifySha256File(
        file,
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      ),
      /checksum mismatch/
    );
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});
