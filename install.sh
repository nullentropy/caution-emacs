#!/bin/sh
set -eu

repo=${CAUTION_EMACS_REPO:-nullentropy/caution-emacs}
url=${CAUTION_EMACS_URL:-}
dest=${CAUTION_EMACS_DEST:-/Applications}
asset=CautionEmacs-macos-universal.zip

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

[ "$(uname -s)" = Darwin ] || fail "Caution Emacs is a macOS app"
command -v emacs >/dev/null 2>&1 || echo "install.sh: emacs is not on your PATH; install it first, for example: brew install emacs" >&2

if [ -z "$url" ]; then
	case $repo in
	*/*) ;;
	*) fail "CAUTION_EMACS_REPO must be in owner/name form, such as nullentropy/caution-emacs, got: $repo" ;;
	esac
	url="https://github.com/$repo/releases/latest/download/$asset"
fi

[ -d "$dest" ] || fail "$dest does not exist"
[ -w "$dest" ] || fail "cannot write to $dest; run as an admin user or set CAUTION_EMACS_DEST"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "Downloading $url"
curl -fSL --progress-bar -o "$work/$asset" "$url" || fail "download failed: $url"
ditto -x -k "$work/$asset" "$work/unzipped" || fail "could not unzip $asset"
[ -d "$work/unzipped/Caution Emacs.app" ] || fail "$asset has no Caution Emacs.app"

app="$dest/Caution Emacs.app"
exe="$app/Contents/MacOS/"
if pgrep -f "$exe" >/dev/null 2>&1; then
	echo "Quitting the running Caution Emacs"
	osascript -e "tell application \"$app\" to quit" >/dev/null 2>&1 || true
	i=0
	while pgrep -f "$exe" >/dev/null 2>&1 && [ $i -lt 20 ]; do
		sleep 0.5
		i=$((i + 1))
	done
	if pgrep -f "$exe" >/dev/null 2>&1; then
		pkill -TERM -f "$exe" || true
		sleep 2
		pkill -KILL -f "$exe" 2>/dev/null || true
	fi
fi

rm -rf "$app"
mv "$work/unzipped/Caution Emacs.app" "$app"
echo "Installed Caution Emacs to $app"
