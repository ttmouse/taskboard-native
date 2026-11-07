#!/bin/bash
# Builds the macOS universal app, publishes the signed update files and the
# installers to a GitHub release of the version in mygo.json, then makes the
# release public.
#
#   scripts/release-macos.sh            # bump mygo.json first
#   scripts/release-macos.sh --dry-run  # build and sign, upload nothing
#
# Needs the update signing key, in MYGO_UPDATER_PRIVATE_KEY or the default
# mygo keygen location, and gh logged in as a user who can publish releases.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

dry_run=false
[ "${1:-}" = "--dry-run" ] && dry_run=true

version=$(python3 -c 'import json; print(json.load(open("mygo.json"))["version"])')
tag="v$version"

if gh release view "$tag" >/dev/null 2>&1 && ! $dry_run; then
	echo "release $tag 已存在，先改 mygo.json 的版本号" >&2
	exit 1
fi

key_file="$HOME/Library/Application Support/mygo/update-keys/mygo-update.key"
if [ -n "${MYGO_UPDATER_PRIVATE_KEY:-}" ]; then
	key=$MYGO_UPDATER_PRIVATE_KEY
elif [ -f "$key_file" ]; then
	key=$(cat "$key_file")
else
	echo "找不到更新签名私钥：设置 MYGO_UPDATER_PRIVATE_KEY，或先跑 mygo keygen" >&2
	exit 1
fi

python3 scripts/generate_version.py

out=build/darwin-universal
export MYGO_UPDATER_PRIVATE_KEY=$key
export GH_TOKEN=${GH_TOKEN:-$(gh auth token)}

if $dry_run; then
	mygo build -platform darwin/universal
else
	mygo build -platform darwin/universal -upload
fi

# The dmg carries the app users install; it is not signed by the update key,
# so give them a checksum the install script verifies.
(
	cd "$out"
	shasum -a 256 *darwin-universal.tar.gz *.dmg >SHA256SUMS
)

$dry_run && { echo "dry run：产物在 $out"; exit 0; }

gh release upload "$tag" "$out/SHA256SUMS" scripts/install-macos.sh --clobber
gh release edit "$tag" --draft=false --latest --title "Taskboard $version"

echo "已发布 https://github.com/ttmouse/taskboard-native/releases/tag/$tag"
