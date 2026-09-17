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

/** errorLogPath returns the file that records the last failed run. */
function errorLogPath() {
  return path.join(claudeRoot(), "memory-manager", "last-error.log");
}

/**
 * recordFailure leaves a trace a later run can find.
 *
 * Exiting zero is the right call — a memory that will not sync must not stop
 * anyone working — but it means a broken, missing or replaced binary shows up
 * only as one line of stderr in a wall of session output, and nobody reads
 * that line. Since the loss this tool exists to prevent is a memory that never
 * arrives, the failure has to outlive the session that saw it. `sync` reports
 * this file on its next successful run.
 *
 * Every error here is swallowed: a launcher that cannot write its own log must
 * still not take the session down with it.
 */
function recordFailure(subcommand, message) {
  try {
    const p = errorLogPath();
    fs.mkdirSync(path.dirname(p), { recursive: true });
    fs.writeFileSync(p, `${new Date().toISOString()} ${subcommand}: ${message}\n`);
  } catch {
    // Nothing to do, and nothing worth saying: stderr already carried the message.
  }
}

/** clearFailure removes the record after a run that worked. */
function clearFailure() {
  try {
    fs.rmSync(errorLogPath(), { force: true });
  } catch {
    // A stale record is a smaller problem than a failed session start.
  }
}

/**
 * findBinary locates the executable.
 *
 * The order matches how people actually end up with it: an explicit override
 * first, then the location the install script writes to, then anything already
 * on PATH, which covers both "go install" and the npm package once it ships.
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
    // Name only an install that works right now. Until the first release is
    // tagged there is no npm package and no downloadable build, and sending a
    // new user at one they cannot install wastes the one line they will read.
    process.stderr.write(
      "memory-manager: binary not found, memory was not synced. " +
        "Install it with \"go install github.com/Arlezz/memory-manager/cmd/memory-manager@latest\" " +
        "(Go 1.23+, with the Go bin directory on PATH), " +
        "or set MEMORY_MANAGER_BIN to a binary you already have.\n"
    );
    process.exit(0);
  }

  const result = spawnSync(bin, [subcommand, projectDir(), "-quiet"], {
    stdio: ["ignore", "inherit", "inherit"],
  });

  if (result.error) {
    const msg = `could not run ${bin}: ${result.error.message}`;
    process.stderr.write(`memory-manager: ${msg}\n`);
    recordFailure(subcommand, msg);
  } else if (result.status !== 0) {
    // The binary already explained itself on stderr. Say what it means for the
    // session, then let the session continue.
    const msg = `${subcommand} exited with ${result.status}; the session continued on local memory`;
    process.stderr.write(`memory-manager: ${msg}\n`);
    recordFailure(subcommand, msg);
  } else {
    // A run that worked clears the record, so the file always describes the
    // last failure and never an old one that has since been fixed.
    clearFailure();
  }

  // Always zero. See the note at the top of this file.
  process.exit(0);
}

main();
