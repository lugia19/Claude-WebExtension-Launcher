#!/bin/bash

APP_NAME="Claude_WebExtension_Launcher"
PACKAGE_NAME="com.lugia19.claudewebextlauncher"

# Read version from main.go
VERSION=$(grep 'const Version = ' main.go | sed 's/.*"\(.*\)".*/\1/')
if [ -z "$VERSION" ]; then
    echo "ERROR: Could not find version in main.go"
    exit 1
fi

echo "Building version: $VERSION for all platforms"
echo "============================================"

# Create builds directory
mkdir -p builds

# Clean up any existing files
rm -f builds/*.zip
rm -rf builds/*.app

# Function to create macOS app bundle
create_macos_bundle() {
    local binary_name=$1
    local arch_suffix=$2
    
    echo "Creating macOS app bundle for $arch_suffix..."
    
    # Create directory structure
    mkdir -p "builds/$APP_NAME.app/Contents/MacOS"
    mkdir -p "builds/$APP_NAME.app/Contents/Resources"
    
    # Move binary
    mv "$binary_name" "builds/$APP_NAME.app/Contents/MacOS/$APP_NAME"
    chmod +x "builds/$APP_NAME.app/Contents/MacOS/$APP_NAME"
    
    # Create Info.plist with architecture-specific settings
    if [ "$arch_suffix" = "arm64" ]; then
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
    else
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
    <string>10.12</string>
    <key>LSArchitecturePriority</key>
    <array>
        <string>x86_64</string>
    </array>
</dict>
</plist>
EOF
    fi
    
    # Copy icon if it exists
    if [ -f "resources/icons/app.icns" ]; then
        cp "resources/icons/app.icns" "builds/$APP_NAME.app/Contents/Resources/"
        echo "  Icon added to app bundle"
    fi
    
    # Sign the app with ad-hoc signature
    echo "  Signing app..."
    codesign --remove-signature "builds/$APP_NAME.app" 2>/dev/null || true
    codesign --force --deep --sign - "builds/$APP_NAME.app"
    
    if [ $? -eq 0 ]; then
        echo "  ✅ App signed successfully"
    else
        echo "  ⚠️  Warning: Could not sign app, but continuing..."
    fi
    
    # Remove quarantine attributes
    xattr -cr "builds/$APP_NAME.app" 2>/dev/null || true
    
    # Create distribution zip
    echo "  Creating distribution zip..."
    cd builds
    zip -r "$APP_NAME-$VERSION-macos-$arch_suffix.zip" "$APP_NAME.app"
    cd ..
    
    echo "  ✅ Created: builds/$APP_NAME-$VERSION-macos-$arch_suffix.zip"
    
    # Clean up app bundle
    rm -rf "builds/$APP_NAME.app"
}

# Build 1: macOS Apple Silicon (ARM64)
echo ""
echo "1. Building macOS Apple Silicon (ARM64)..."
GOOS=darwin GOARCH=arm64 go build -o "$APP_NAME-mac-arm64"

if [ -f "$APP_NAME-mac-arm64" ]; then
    create_macos_bundle "$APP_NAME-mac-arm64" "arm64"
else
    echo "  ❌ ARM64 build failed!"
fi

# Build 2: macOS Intel (AMD64)
echo ""
echo "2. Building macOS Intel (AMD64)..."
GOOS=darwin GOARCH=amd64 go build -o "$APP_NAME-mac-amd64"

if [ -f "$APP_NAME-mac-amd64" ]; then
    create_macos_bundle "$APP_NAME-mac-amd64" "amd64"
else
    echo "  ❌ Intel macOS build failed!"
fi

# Build 3: Windows (AMD64)
echo ""
echo "3. Building Windows (AMD64)..."
GOOS=windows GOARCH=amd64 go build -o "$APP_NAME.exe"

if [ -f "$APP_NAME.exe" ]; then
    echo "  Creating Windows distribution zip..."
    zip "builds/$APP_NAME-$VERSION-windows.zip" "$APP_NAME.exe"
    echo "  ✅ Created: builds/$APP_NAME-$VERSION-windows.zip"
    rm "$APP_NAME.exe"
else
    echo "  ❌ Windows build failed!"
fi

# Summary
echo ""
echo "============================================"
echo "Build Summary:"
echo "============================================"

if [ -f "builds/$APP_NAME-$VERSION-macos-arm64.zip" ]; then
    echo "✅ macOS Apple Silicon: builds/$APP_NAME-$VERSION-macos-arm64.zip"
fi

if [ -f "builds/$APP_NAME-$VERSION-macos-amd64.zip" ]; then
    echo "✅ macOS Intel: builds/$APP_NAME-$VERSION-macos-amd64.zip"
fi

if [ -f "builds/$APP_NAME-$VERSION-windows.zip" ]; then
    echo "✅ Windows: builds/$APP_NAME-$VERSION-windows.zip"
fi

echo ""
echo "All builds complete! 🎉"
