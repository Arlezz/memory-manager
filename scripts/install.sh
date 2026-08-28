#!/usr/bin/env sh
# Installs memory-manager and wires it into Claude Code's SessionStart hook.
#
# The settings file is merged, never replaced: it already holds the user's model,
# theme and plugin configuration.
#
# Usage:
#   ./install.sh [--version TAG] [--from-path ./memory-manager] [--no-hook]

set -eu

REPO="Arlezz/memory-manager"
VERSION="latest"
FROM_PATH=""
NO_HOOK=0

while [ $# -gt 0 ]; do
	case "$1" in
		--version) VERSION="$2"; shift 2 ;;
		--from-path) FROM_PATH="$2"; shift 2 ;;
		--no-hook) NO_HOOK=1; shift ;;
		-h|--help) sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
done

claude_root() {
	if [ -n "${CLAUDE_CONFIG_DIR:-}" ]; then
		printf '%s\n' "$CLAUDE_CONFIG_DIR"
	else
		printf '%s\n' "$HOME/.claude"
	fi
}

target_triple() {
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	case "$os" in
		linux) os=linux ;;
		darwin) os=darwin ;;
		*) echo "unsupported OS: $os" >&2; exit 1 ;;
	esac
	arch=$(uname -m)
	case "$arch" in
		x86_64|amd64) arch=amd64 ;;
		arm64|aarch64) arch=arm64 ;;
		*) echo "unsupported architecture: $arch" >&2; exit 1 ;;
	esac
	printf '%s_%s\n' "$os" "$arch"
}

CLAUDE_ROOT=$(claude_root)
BIN_DIR="$CLAUDE_ROOT/memory-manager/bin"
BIN_PATH="$BIN_DIR/memory-manager"

mkdir -p "$BIN_DIR"

if [ -n "$FROM_PATH" ]; then
	[ -f "$FROM_PATH" ] || { echo "no such file: $FROM_PATH" >&2; exit 1; }
	cp "$FROM_PATH" "$BIN_PATH"
	echo "Installed $FROM_PATH -> $BIN_PATH"
else
	ASSET="memory-manager_$(target_triple)"
	if [ "$VERSION" = "latest" ]; then
		URL="https://github.com/$REPO/releases/latest/download/$ASSET"
	else
		URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
	fi
	echo "Downloading $URL"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$URL" -o "$BIN_PATH"
	else
		wget -q "$URL" -O "$BIN_PATH"
	fi
	echo "Installed -> $BIN_PATH"
fi

chmod +x "$BIN_PATH"
"$BIN_PATH" version

if [ "$NO_HOOK" -eq 1 ]; then
	echo "Skipped the hook. Add it yourself with: $BIN_PATH sync -quiet"
	exit 0
fi

SETTINGS="$CLAUDE_ROOT/settings.json"
# SessionStart pulls both layers in; SessionEnd sends back what the session wrote.
SYNC_COMMAND="\"$BIN_PATH\" sync -quiet"
PUSH_COMMAND="\"$BIN_PATH\" push -quiet"

# The merge needs a JSON parser. python3 is used rather than jq because it is
# present on stock macOS and on every distro image we care about.
if ! command -v python3 >/dev/null 2>&1; then
	echo "python3 is required to merge settings.json safely." >&2
	echo "Install it, or re-run with --no-hook and add these commands manually:" >&2
	echo "  SessionStart -> $SYNC_COMMAND" >&2
	echo "  SessionEnd   -> $PUSH_COMMAND" >&2
	exit 1
fi

if [ -f "$SETTINGS" ]; then
	cp "$SETTINGS" "$SETTINGS.memory-manager-backup"
	echo "Backed up settings to $SETTINGS.memory-manager-backup"
fi

SETTINGS="$SETTINGS" SYNC_COMMAND="$SYNC_COMMAND" PUSH_COMMAND="$PUSH_COMMAND" python3 - <<'PY'
import json, os, sys

path = os.environ["SETTINGS"]
commands = {
    "SessionStart": os.environ["SYNC_COMMAND"],
    "SessionEnd": os.environ["PUSH_COMMAND"],
}

try:
    with open(path, encoding="utf-8") as fh:
        settings = json.load(fh)
except FileNotFoundError:
    settings = {}
except json.JSONDecodeError as exc:
    sys.exit(f"settings.json is not valid JSON; fix it before installing so nothing is lost: {exc}")

hooks = settings.setdefault("hooks", {})


# Drop only our own entry so re-running is idempotent and other hooks on the
# same event survive untouched.
def is_ours(entry):
    return any("memory-manager" in (h.get("command") or "") for h in entry.get("hooks", []))


for event, command in commands.items():
    kept = [e for e in hooks.get(event, []) if not is_ours(e)]
    kept.append({"hooks": [{"type": "command", "command": command}]})
    hooks[event] = kept

with open(path, "w", encoding="utf-8") as fh:
    json.dump(settings, fh, indent=2, ensure_ascii=False)
    fh.write("\n")
PY

echo "Wired SessionStart hook: $SYNC_COMMAND"
echo "Wired SessionEnd hook:   $PUSH_COMMAND"
echo
echo "Next:"
echo "  1. memory-manager config -personal-repo <your private memory repo URL>"
echo "  2. memory-manager migrate           # review the plan"
echo "  3. memory-manager migrate -apply    # adopt the existing memory"
