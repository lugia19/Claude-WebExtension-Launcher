#!/bin/bash
# Builds the macOS app bundles and their zips, on a Mac. The window (Fyne) needs cgo, so
# each platform is built on that platform: Windows and Linux come from build-all.ps1 /
# build-windows.ps1 / build-linux.sh.
#
#   ./build-all.sh            both architectures
#   ./build-all.sh arm64      just one (arm64 or amd64)
set -euo pipefail

APP_NAME="Claude_WebExtension_Launcher"
PACKAGE_NAME="com.lugia19.claudewebextlauncher"
MIN_MACOS="12.0" # Go 1.25 needs macOS 12

VERSION=$(grep 'const Version = ' main.go | sed 's/.*"\(.*\)".*/\1/')
if [ -z "$VERSION" ]; then
    echo "ERROR: Could not find version in main.go"
    exit 1
fi

export CGO_ENABLED=1
export MACOSX_DEPLOYMENT_TARGET="$MIN_MACOS" # the cgo code too, not just the plist
if [ $# -eq 0 ]; then
    set -- arm64 amd64
fi

echo "Building version $VERSION for macOS ($*)"
mkdir -p builds

for arch in "$@"; do
    case "$arch" in
        arm64) clang_arch=arm64 ;;
        amd64) clang_arch=x86_64 ;;
        *) echo "ERROR: unknown architecture $arch"; exit 1 ;;
    esac
    echo ""
    echo "Building macOS $arch..."
    bundle="builds/$APP_NAME.app"
    zip_name="$APP_NAME-$VERSION-macos-$arch.zip"
    rm -rf "$bundle" "builds/$zip_name"
    mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"

    CC="clang -arch $clang_arch" GOOS=darwin GOARCH=$arch go build -o "$bundle/Contents/MacOS/$APP_NAME"
    chmod +x "$bundle/Contents/MacOS/$APP_NAME"

    cat > "$bundle/Contents/Info.plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>$APP_NAME</string>
    <key>CFBundleIconFile</key>
    <string>app.icns</string>
    <key>CFBundleIdentifier</key>
    <string>$PACKAGE_NAME</string>
    <key>CFBundleName</key>
    <string>$APP_NAME</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleVersion</key>
    <string>$VERSION</string>
    <key>CFBundleShortVersionString</key>
    <string>$VERSION</string>
    <key>LSMinimumSystemVersion</key>
    <string>$MIN_MACOS</string>
    <key>LSArchitecturePriority</key>
    <array>
        <string>$clang_arch</string>
    </array>
</dict>
</plist>
EOF

    cp resources/icons/app.icns "$bundle/Contents/Resources/"

    # Ad-hoc signature; without one, Apple Silicon refuses to run the app at all.
    codesign --remove-signature "$bundle" 2>/dev/null || true
    if ! codesign --force --deep --sign - "$bundle"; then
        echo "  Warning: could not sign the app, continuing"
    fi
    xattr -cr "$bundle" 2>/dev/null || true

    (cd builds && zip -qr "$zip_name" "$APP_NAME.app")
    rm -rf "$bundle"
    echo "  Created: builds/$zip_name"
done

echo ""
echo "All builds complete!"
