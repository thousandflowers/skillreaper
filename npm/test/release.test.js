import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  binaryHome,
  downloadTo,
  checksumForAsset,
  checksumsDownloadUrl,
  isSourceCheckout,
  latestDownloadUrl,
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

test("source checkout resolves the latest release, real versions stay pinned", () => {
  assert.equal(isSourceCheckout("0.0.0"), true);
  assert.equal(isSourceCheckout("dev"), true);
  assert.equal(isSourceCheckout(undefined), true);
  assert.equal(isSourceCheckout("0.3.2"), false);

  assert.equal(
    latestDownloadUrl("checksums.txt"),
    "https://github.com/thousandflowers/skillreaper/releases/latest/download/checksums.txt"
  );
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

test("the binary lives next to the package when it can, else in the user's cache", () => {
  const base = { packageDir: "/pkg/bin", version: "0.9.0", home: "/home/me", env: {}, platformName: "linux" };
  assert.equal(binaryHome({ ...base, writable: () => true }), "/pkg/bin");
  assert.equal(binaryHome({ ...base, writable: () => false }), join("/home/me", ".cache", "skillreaper", "0.9.0"));
  assert.equal(
    binaryHome({ ...base, writable: () => false, env: { XDG_CACHE_HOME: "/xdg" } }),
    join("/xdg", "skillreaper", "0.9.0"),
  );
  assert.equal(
    binaryHome({ ...base, platformName: "win32", writable: () => false, env: { LOCALAPPDATA: "C:\\Local" } }),
    join("C:\\Local", "skillreaper", "0.9.0"),
  );
  assert.equal(binaryHome({ ...base, platformName: "win32", writable: () => false }), join("/home/me", ".cache", "skillreaper", "0.9.0"));
  // a source checkout fetches "latest": a cache keyed on it would never move on
  assert.equal(binaryHome({ ...base, version: "0.0.0", writable: () => false }), "/pkg/bin");
});

// A local server that sends n chunks, one every gapMs, then maybe stops sending.
async function slowServer(n, gapMs, stallAfter = Infinity) {
  const { createServer } = await import("node:http");
  const server = createServer(async (_req, res) => {
    res.writeHead(200, { "content-type": "application/octet-stream" });
    for (let i = 0; i < n; i++) {
      if (i === stallAfter) return; // keep the socket open, send nothing more
      res.write("x".repeat(1024));
      await new Promise((r) => setTimeout(r, gapMs));
    }
    res.end();
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  return { url: `http://127.0.0.1:${server.address().port}/asset`, close: () => server.closeAllConnections() || server.close() };
}

test("a slow download that keeps arriving is not cut off", async () => {
  const srv = await slowServer(12, 50); // ~600 ms in all, far over the 200 ms limits below
  const dir = await mkdtemp(join(tmpdir(), "skillreaper-npm-"));
  try {
    await downloadTo(srv.url, join(dir, "asset"), { headerMs: 200, idleMs: 200 });
  } finally {
    srv.close();
    await rm(dir, { recursive: true, force: true });
  }
});

test("a download that stops arriving gives up with a reason", async () => {
  const srv = await slowServer(12, 10, 2);
  const dir = await mkdtemp(join(tmpdir(), "skillreaper-npm-"));
  try {
    await assert.rejects(downloadTo(srv.url, join(dir, "asset"), { headerMs: 200, idleMs: 200 }), /stalled/);
  } finally {
    srv.close();
    await rm(dir, { recursive: true, force: true });
  }
});
