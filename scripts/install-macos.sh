#!/bin/sh
# Installs Taskboard for macOS from its latest GitHub release.
#
#   curl -fsSL https://github.com/ttmouse/taskboard-native/releases/latest/download/install-macos.sh | sh
#   sh install-macos.sh 0.1.107        # a version other than the latest
#
# The app is signed ad hoc, not with a Developer ID, so Gatekeeper would
# refuse the copy inside the disk image. This script removes that quarantine
# flag after copying, which is the same as the command the README gives.
set -eu

repo=ttmouse/taskboard-native
version=${1:-}
version=${version#v}

fail() {
	echo "安装失败：$*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "缺少命令 $1"
}

[ "$(uname -s)" = Darwin ] || fail "这个脚本只支持 macOS"

need curl
need hdiutil
need sed
need ditto

api=https://api.github.com/repos/$repo
[ -n "${GITHUB_TOKEN:-}" ] && auth="Authorization: Bearer $GITHUB_TOKEN" || auth=""

if [ -z "$version" ]; then
	release=$(curl -fsSL ${auth:+-H "$auth"} "$api/releases/latest") ||
		fail "读取最新版本失败，检查网络后重试"
	version=$(printf '%s' "$release" | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$version" ] || fail "没能从最新 release 里解析出版本号"
else
	release=$(curl -fsSL ${auth:+-H "$auth"} "$api/releases/tags/v$version") ||
		fail "找不到版本 $version"
fi

# GitHub renames spaces in asset names to dots, so read the dmg's URL from
# the release itself instead of building it from the name.
dmg_url=$(printf '%s' "$release" |
	sed 's/[{}]/\n/g' |
	grep '"browser_download_url"' |
	grep '\.dmg"' |
	sed 's/.*"\(https:[^"]*\)".*/\1/' |
	head -n 1)
[ -n "$dmg_url" ] || fail "release v$version 里没有 dmg"
sums_url="${dmg_url%/*}/SHA256SUMS"

work=$(mktemp -d)
mount=$work/mnt
dmg=$work/Taskboard.dmg
cleanup() {
	[ -d "$mount" ] && hdiutil detach "$mount" -quiet >/dev/null 2>&1
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

echo "下载 Taskboard $version"
curl -fL --progress-bar -o "$dmg" "$dmg_url" ||
	fail "下载 $dmg_url 失败，确认这个版本存在"

if curl -fsSL -o "$work/SHA256SUMS" "$sums_url" 2>/dev/null; then
	expected=$(sed -n 's/^\([0-9a-f]\{64\}\) .*\.dmg$/\1/p' "$work/SHA256SUMS")
	[ -n "$expected" ] || fail "校验文件里没有这个 dmg 的 SHA-256"
	actual=$(shasum -a 256 "$dmg" | cut -d ' ' -f 1)
	[ "$expected" = "$actual" ] || fail "SHA-256 不匹配（期望 $expected，实际 $actual）"
fi

mkdir -p "$mount"
hdiutil attach "$dmg" -nobrowse -readonly -quiet -mountpoint "$mount" ||
	fail "挂载磁盘镜像失败"

source_app=
for candidate in "$mount"/*.app; do
	[ -d "$candidate" ] && source_app=$candidate && break
done
[ -n "$source_app" ] || fail "磁盘镜像里没有 .app"

if [ -w /Applications ]; then
	target=/Applications/Taskboard.app
else
	mkdir -p "$HOME/Applications"
	target=$HOME/Applications/Taskboard.app
fi

# Quit a running copy first; replacing a live bundle leaves the old process
# holding the port and the database.
osascript -e 'tell application "Taskboard" to quit' >/dev/null 2>&1 || true
rm -rf "$target"
ditto "$source_app" "$target" || fail "复制到 $target 失败"
xattr -dr com.apple.quarantine "$target" 2>/dev/null || true

echo "已安装到 $target"
echo "打开：open \"$target\"，或直接在启动台里点 Taskboard。"
