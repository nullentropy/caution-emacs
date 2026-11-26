#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ "$(uname -s)" != Darwin ]; then
	echo "scripts/app.sh builds the macOS app; on other systems use go run ." >&2
	exit 1
fi
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
go build -o "$bin/caution-emacs" .
go run github.com/nullentropy/caution/cmd/appbundle -bin "$bin/caution-emacs" -name "Caution Emacs" \
	-id dev.caution.emacs -icon icon.png -o dist/
codesign --force -s - "dist/Caution Emacs.app"
echo "dist/Caution Emacs.app"
