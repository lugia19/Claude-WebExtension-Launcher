#!/bin/bash
# Builds the Linux zip for this machine's architecture (the window needs cgo, so no
# cross-compiling): builds/Claude_WebExtension_Launcher-<version>-linux-<arch>.zip,
# holding the binary under the plain name the self-updater looks for.
# Needs gcc and libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev. The binary
# needs at least the glibc it's built against, so release builds come from an older
# distro (CI uses Ubuntu 22.04).
set -euo pipefail

APP_NAME="Claude_WebExtension_Launcher"
GO="${GO:-go}"

VERSION=$(grep 'const Version = ' main.go | sed 's/.*"\(.*\)".*/\1/')
if [ -z "$VERSION" ]; then
    echo "ERROR: Could not find version in main.go"
    exit 1
fi
ARCH=$("$GO" env GOARCH)

echo "Building version $VERSION for Linux ($ARCH)"
out="builds/linux-$ARCH"
zip_name="$APP_NAME-$VERSION-linux-$ARCH.zip"
rm -rf "$out" "builds/$zip_name"
mkdir -p "$out"

CGO_ENABLED=1 "$GO" build -o "$out/$APP_NAME"
chmod +x "$out/$APP_NAME"
(cd "$out" && zip -q "../$zip_name" "$APP_NAME")
rm -rf "$out"
echo "Created: builds/$zip_name"
