#!/usr/bin/env node
"use strict";

// Thin launcher for the npm distribution.
//
// The Go binary is shipped in six per-platform packages declared as optional
// dependencies, so npm downloads only the one that matches. This file resolves
// that package and executes the binary, forwarding every argument, stream and
// signal so `memory-manager` over npm behaves exactly like the native binary.

const { spawn } = require("child_process");
const fs = require("fs");
const path = require("path");

const PLATFORM_PACKAGES = {
  "darwin-arm64": "@memory-manager/darwin-arm64",
  "darwin-x64": "@memory-manager/darwin-x64",
  "linux-arm64": "@memory-manager/linux-arm64",
  "linux-x64": "@memory-manager/linux-x64",
  "win32-arm64": "@memory-manager/win32-arm64",
  "win32-x64": "@memory-manager/win32-x64",
};

function binaryName() {
  return process.platform === "win32" ? "memory-manager.exe" : "memory-manager";
}

/**
 * resolveBinary finds the platform binary, or exits with an explanation.
 *
 * The common cause of a miss is `--no-optional`, which silently skips every
 * platform package, so the message names it rather than leaving the user with a
 * bare "not found".
 */
function resolveBinary() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORM_PACKAGES[key];

  if (!pkg) {
    console.error(`memory-manager: unsupported platform ${key}`);
    console.error(`Supported: ${Object.keys(PLATFORM_PACKAGES).join(", ")}`);
    console.error("Build from source instead: go build ./cmd/memory-manager");
    process.exit(1);
  }

  try {
    const dir = path.dirname(require.resolve(`${pkg}/package.json`));
    const bin = path.join(dir, binaryName());
    if (fs.existsSync(bin)) {
      return bin;
    }
  } catch (err) {
    // Fall through to the shared message below.
  }

  console.error(`memory-manager: the binary package ${pkg} is missing.`);
  console.error("If this was installed with --no-optional, reinstall without it:");
  console.error("  npm install -g memory-manager-cli");
  process.exit(1);
}

function main() {
  const bin = resolveBinary();

  const child = spawn(bin, process.argv.slice(2), { stdio: "inherit" });

  // Forward termination so Ctrl-C reaches the binary rather than orphaning it.
  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) {
    process.on(signal, () => {
      if (!child.killed) {
        child.kill(signal);
      }
    });
  }

  child.on("error", (err) => {
    console.error(`memory-manager: could not run ${bin}: ${err.message}`);
    process.exit(1);
  });

  child.on("exit", (code, signal) => {
    if (signal) {
      // Reproduce the signal death instead of flattening it into an exit code.
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code === null ? 1 : code);
  });
}

main();
