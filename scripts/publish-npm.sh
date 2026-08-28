#!/usr/bin/env bash
# Stages the cross-compiled binaries into the per-platform npm packages and
# publishes them, then publishes the launcher package that depends on them.
#
# Order matters: the launcher declares the platform packages as optional
# dependencies, so publishing it first would leave a window where installs
# resolve nothing.
#
# Usage:
#   scripts/publish-npm.sh --version 0.1.0 [--dry-run]
#
# Expects the binaries in dist/, named as the release workflow names them:
#   dist/memory-manager_<goos>_<goarch>[.exe]

set -euo pipefail

VERSION=""
DRY_RUN=0
DIST="dist"

while [ $# -gt 0 ]; do
	case "$1" in
		--version) VERSION="$2"; shift 2 ;;
		--dist) DIST="$2"; shift 2 ;;
		--dry-run) DRY_RUN=1; shift ;;
		-h|--help) sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
done

if [ -z "$VERSION" ]; then
	echo "--version is required" >&2
	exit 2
fi
# Strip a leading "v" so a git tag can be passed straight through.
VERSION="${VERSION#v}"

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

# npm arch names differ from Go's: amd64 is x64. The npm name is the directory,
# the Go name builds the artifact filename.
targets="darwin-arm64:darwin:arm64 darwin-x64:darwin:amd64 linux-arm64:linux:arm64 linux-x64:linux:amd64 win32-arm64:windows:arm64 win32-x64:windows:amd64"

published=""

for target in $targets; do
	npm_name="${target%%:*}"
	rest="${target#*:}"
	goos="${rest%%:*}"
	goarch="${rest#*:}"

	binary="memory-manager"
	artifact="$DIST/memory-manager_${goos}_${goarch}"
	if [ "$goos" = "windows" ]; then
		binary="memory-manager.exe"
		artifact="${artifact}.exe"
	fi

	if [ ! -f "$artifact" ]; then
		echo "missing artifact: $artifact" >&2
		echo "build the release first, or pass --dist" >&2
		exit 1
	fi

	pkg_dir="npm/$npm_name"
	cp "$artifact" "$pkg_dir/$binary"
	chmod +x "$pkg_dir/$binary"

	# Keep every package on one version; npm has no way to express "same as the
	# parent" in a dependency range we control here.
	node -e '
		const fs = require("fs");
		const path = process.argv[1];
		const version = process.argv[2];
		const pkg = JSON.parse(fs.readFileSync(path, "utf8"));
		pkg.version = version;
		fs.writeFileSync(path, JSON.stringify(pkg, null, 2) + "\n");
	' "$pkg_dir/package.json" "$VERSION"

	echo "==> @memory-manager/$npm_name@$VERSION ($artifact)"
	if [ "$DRY_RUN" -eq 1 ]; then
		( cd "$pkg_dir" && npm publish --access public --dry-run )
	else
		( cd "$pkg_dir" && npm publish --access public )
	fi
	published="$published @memory-manager/$npm_name"
done

# The launcher, and its pins to the platform packages just published.
node -e '
	const fs = require("fs");
	const version = process.argv[1];
	const pkg = JSON.parse(fs.readFileSync("package.json", "utf8"));
	pkg.version = version;
	for (const dep of Object.keys(pkg.optionalDependencies || {})) {
		pkg.optionalDependencies[dep] = version;
	}
	fs.writeFileSync("package.json", JSON.stringify(pkg, null, 2) + "\n");
' "$VERSION"

echo "==> memory-manager-cli@$VERSION"
if [ "$DRY_RUN" -eq 1 ]; then
	npm publish --dry-run
else
	npm publish
fi

echo
echo "Published:$published memory-manager-cli"
echo "Verify with: npx memory-manager-cli@$VERSION version"
