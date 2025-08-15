#!/bin/bash

APP_NAME="Claude_WebExtension_Launcher"
PACKAGE_NAME="com.lugia19.claudewebextlauncher"

# Read version from main.go
VERSION=$(grep 'const Version = ' main.go | sed 's/.*"\(.*\)".*/\1/')
if [ -z "$VERSION" ]; then
    echo "ERROR: Could not find version in main.go"
    exit 1
fi

echo "Building version: $VERSION for Apple Silicon (ARM64)"

# Create builds directory
mkdir -p builds

# Build for Apple Silicon (ARM64)
echo "Building for macOS ARM64..."
GOOS=darwin GOARCH=arm64 go build -o "$APP_NAME-mac-arm64"

if [ ! -f "$APP_NAME-mac-arm64" ]; then
    echo "ARM64 build failed!"
    exit 1
fi

echo "Creating app bundle..."

# Clean up old bundle
rm -rf "builds/$APP_NAME.app"

# Create directory structure
mkdir -p "builds/$APP_NAME.app/Contents/MacOS"
mkdir -p "builds/$APP_NAME.app/Contents/Resources"

# Move binary
mv "$APP_NAME-mac-arm64" "builds/$APP_NAME.app/Contents/MacOS/$APP_NAME"

# Make executable
chmod +x "builds/$APP_NAME.app/Contents/MacOS/$APP_NAME"

# Create Info.plist
cat > "builds/$APP_NAME.app/Contents/Info.plist" << EOF
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
    <string>11.0</string>
    <key>LSArchitecturePriority</key>
    <array>
        <string>arm64</string>
    </array>
</dict>
</plist>
EOF

# Copy icon if it exists
if [ -f "resources/icons/app.icns" ]; then
    cp "resources/icons/app.icns" "builds/$APP_NAME.app/Contents/Resources/"
    echo "Icon added to app bundle"
fi

# Sign the app with ad-hoc signature
echo "Signing app..."
codesign --remove-signature "builds/$APP_NAME.app" 2>/dev/null || true
codesign --force --deep --sign - "builds/$APP_NAME.app"

if [ $? -eq 0 ]; then
    echo "✅ App signed successfully"
else
    echo "⚠️  Warning: Could not sign app, but continuing..."
fi

# Remove quarantine attributes
xattr -cr "builds/$APP_NAME.app" 2>/dev/null || true

echo "macOS ARM64 build complete: builds/$APP_NAME.app"

# Create distribution zip
echo "Creating macOS ARM64 distribution zip..."
cd builds
zip -r "$APP_NAME-$VERSION-macos-arm64.zip" "$APP_NAME.app"
cd ..

echo "Created: builds/$APP_NAME-$VERSION-macos-arm64.zip"
echo "Build complete!"
