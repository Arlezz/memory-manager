#!/usr/bin/env node
"use strict";

// Runs the memory-manager binary from a Claude Code hook.
//
// The plugin is distributed as source through the marketplace, so it cannot
// carry a per-platform binary. This launcher finds one instead, and guarantees
// two things the hook boundary needs:
//
//   1. It never blocks a session. Whatever happens - no binary, a git failure,
//      a crash - the exit code is 0 and the reason goes to stderr where the
//      user can see it. A hook that fails a session start is worse than the
//      manual copying this tool replaces.
//   2. It never goes silent. Every degradation prints one line, because the
//      failure mode of a sync tool is working for weeks out of date without
//      knowing.

const { spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const BIN_NAME = process.platform === "win32" ? "memory-manager.exe" : "memory-manager";

/** claudeRoot returns the user's Claude Code configuration directory. */
function claudeRoot() {
  if (process.env.CLAUDE_CONFIG_DIR) {
    return process.env.CLAUDE_CONFIG_DIR;
  }
  return path.join(os.homedir(), ".claude");
}

/**
 * findBinary locates the executable.
 *
 * The order matches how people actually end up with it: an explicit override
 * first, then the location the install script writes to, then anything already
 * on PATH (which covers the npm install).
 */
function findBinary() {
  const override = process.env.MEMORY_MANAGER_BIN;
  if (override) {
    return fs.existsSync(override) ? override : null;
  }

  const installed = path.join(claudeRoot(), "memory-manager", "bin", BIN_NAME);
  if (fs.existsSync(installed)) {
    return installed;
  }

  // Resolved through PATH by spawn; verified with a cheap call below so a
  // missing binary produces our own message rather than an ENOENT stack.
  const probe = spawnSync(BIN_NAME, ["version"], { stdio: "ignore" });
  if (!probe.error) {
    return BIN_NAME;
  }
  return null;
}

/**
 * projectDir returns the directory to operate on.
 *
 * Claude Code sets CLAUDE_PROJECT_DIR for hooks; the working directory is the
 * fallback. Getting this wrong would sync the wrong project, so it is worth
 * preferring the explicit value.
 */
function projectDir() {
  return process.env.CLAUDE_PROJECT_DIR || process.cwd();
}

function main() {
  const subcommand = process.argv[2];
  if (!subcommand) {
    process.stderr.write("memory-manager: no subcommand given to the hook launcher\n");
    process.exit(0);
  }

  const bin = findBinary();
  if (!bin) {
    process.stderr.write(
      "memory-manager: binary not found, memory was not synced. " +
        "Install it with \"npm i -g memory-manager-cli\", or set MEMORY_MANAGER_BIN.\n"
    );
    process.exit(0);
  }

  const result = spawnSync(bin, [subcommand, projectDir(), "-quiet"], {
    stdio: ["ignore", "inherit", "inherit"],
  });

  if (result.error) {
    process.stderr.write(`memory-manager: could not run ${bin}: ${result.error.message}\n`);
  } else if (result.status !== 0) {
    // The binary already explained itself on stderr. Say what it means for the
    // session, then let the session continue.
    process.stderr.write(
      `memory-manager: ${subcommand} exited with ${result.status}; the session continues on local memory\n`
    );
  }

  // Always zero. See the note at the top of this file.
  process.exit(0);
}

main();
