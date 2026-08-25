#!/usr/bin/env node

import { spawn } from "node:child_process";
import fs from "node:fs";
import fsp from "node:fs/promises";
import path from "node:path";
import { performance } from "node:perf_hooks";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const rootDir = process.env.JAVBOSS_DEV_ROOT || path.resolve(scriptDir, "..", "..");
const devDir = path.join(rootDir, ".dev");
const binaryName = process.platform === "win32" ? "javboss-dev.exe" : "javboss-dev";
const binaryPath = path.join(devDir, binaryName);
const nextBinaryPath = path.join(devDir, `${binaryName}.next`);

const ignoredTopLevelDirs = new Set([
  ".dev",
  ".git",
  ".gocache",
  "bin",
  "data",
  "node_modules",
  "release",
]);

let backend = null;
let compiler = null;
let buildRunning = false;
let buildQueued = false;
let debounceTimer = null;
let shuttingDown = false;

function isBackendSource(filename) {
  if (!filename) return false;
  const normalized = filename.split(path.sep).join("/");
  const parts = normalized.split("/");
  if (ignoredTopLevelDirs.has(parts[0])) return false;
  if (parts.includes("node_modules") || normalized.startsWith("internal/bin/")) return false;
  return normalized.endsWith(".go") || normalized === "go.mod" || normalized === "go.sum";
}

function waitForClose(child) {
  return new Promise((resolve) => {
    if (!child || child.exitCode !== null || child.signalCode !== null) {
      resolve();
      return;
    }
    child.once("close", resolve);
  });
}

async function stopBackend() {
  const child = backend;
  if (!child) return;
  backend = null;

  child.kill("SIGTERM");
  const closed = waitForClose(child);
  const timedOut = await Promise.race([
    closed.then(() => false),
    new Promise((resolve) => setTimeout(() => resolve(true), 3000)),
  ]);
  if (timedOut && child.exitCode === null && child.signalCode === null) {
    child.kill("SIGKILL");
    await waitForClose(child);
  }
}

function buildBackend() {
  return new Promise((resolve) => {
    const startedAt = performance.now();
    const child = spawn(
      "go",
      ["build", "-o", nextBinaryPath, "./cmd/server"],
      { cwd: rootDir, env: process.env, stdio: "inherit" },
    );
    compiler = child;
    child.on("error", (err) => {
      console.error(`[reload] 无法启动 Go 编译器：${err.message}`);
      resolve(false);
    });
    child.on("close", (code) => {
      if (compiler === child) compiler = null;
      const elapsed = ((performance.now() - startedAt) / 1000).toFixed(2);
      if (code === 0) {
        console.log(`[reload] 后端编译完成（${elapsed}s）`);
        resolve(true);
      } else {
        console.error(`[reload] 后端编译失败（${elapsed}s），修复后保存会自动重试`);
        resolve(false);
      }
    });
  });
}

async function stopCompiler() {
  const child = compiler;
  if (!child) return;
  compiler = null;
  child.kill("SIGTERM");
  await waitForClose(child);
}

async function replaceBinary() {
  await stopBackend();
  if (process.platform === "win32") {
    await fsp.rm(binaryPath, { force: true });
  }
  await fsp.rename(nextBinaryPath, binaryPath);
}

function startBackend() {
  const child = spawn(binaryPath, [], {
    cwd: rootDir,
    env: process.env,
    stdio: "inherit",
  });
  backend = child;
  child.on("error", (err) => {
    console.error(`[reload] 后端启动失败：${err.message}`);
  });
  child.on("close", (code, signal) => {
    if (backend === child) backend = null;
    if (!shuttingDown && code !== 0) {
      const reason = signal ? `signal ${signal}` : `code ${code}`;
      console.error(`[reload] 后端已退出（${reason}）；保存 Go 文件后会重新启动`);
    }
  });
  console.log("[reload] 后端已启动，监听 Go 文件改动");
}

async function rebuild() {
  if (buildRunning || shuttingDown) return;
  buildRunning = true;
  try {
    while (buildQueued && !shuttingDown) {
      buildQueued = false;
      if (!(await buildBackend())) continue;
      await replaceBinary();
      if (!shuttingDown) startBackend();
    }
  } finally {
    buildRunning = false;
  }
}

function queueBuild() {
  buildQueued = true;
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => {
    rebuild().catch((err) => console.error(`[reload] ${err.message}`));
  }, 120);
}

await fsp.mkdir(devDir, { recursive: true });

function watchSourceDir(dir, prefix, recursive) {
  const watcher = fs.watch(dir, { recursive }, (_eventType, filename) => {
    if (!filename) return;
    const relativeName = prefix ? path.join(prefix, filename) : filename;
    if (isBackendSource(relativeName)) queueBuild();
  });
  watcher.on("error", (err) => {
    console.error(`[reload] 文件监听失败（${prefix || "."}）：${err.message}`);
    process.exitCode = 1;
  });
  return watcher;
}

// Watching the whole repository would also register thousands of watches for
// node_modules and the Go build cache before the callback can filter them.
const watchers = [
  watchSourceDir(rootDir, "", false),
  watchSourceDir(path.join(rootDir, "cmd"), "cmd", true),
  watchSourceDir(path.join(rootDir, "internal"), "internal", true),
];

async function shutdown() {
  if (shuttingDown) return;
  shuttingDown = true;
  clearTimeout(debounceTimer);
  for (const watcher of watchers) watcher.close();
  await stopCompiler();
  await stopBackend();
}

process.on("SIGINT", () => shutdown().finally(() => process.exit(0)));
process.on("SIGTERM", () => shutdown().finally(() => process.exit(0)));

queueBuild();
