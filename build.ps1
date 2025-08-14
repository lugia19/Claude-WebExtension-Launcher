# build.ps1

$APP_NAME = "Claude_WebExtension_Launcher"
$PACKAGE_NAME = "com.lugia19.claudewebextlauncher"

# Read version from main.go
$mainGoContent = Get-Content ".\main.go" -Raw
if ($mainGoContent -match 'const\s+Version\s*=\s*"([^"]+)"') {
    $VERSION = $matches[1]
    Write-Host "Building version: $VERSION" -ForegroundColor Green
} else {
    Write-Host "ERROR: Could not find version in main.go" -ForegroundColor Red
    Write-Host "Make sure main.go contains: const Version = `"x.x.x`"" -ForegroundColor Yellow
    exit 1
}

# Create builds directory if it doesn't exist
if (!(Test-Path ".\builds")) {
    New-Item -ItemType Directory -Path ".\builds" | Out-Null
}

# Build for Windows
Write-Host "`nBuilding for Windows..." -ForegroundColor Cyan
$env:GOOS = "windows"
$env:GOARCH = "amd64"
& go build -o ".\builds\$APP_NAME.exe"

if (Test-Path ".\builds\$APP_NAME.exe") {
    # Add icon if rcedit exists
    if (Test-Path ".\resources\rcedit.exe") {
        & ".\resources\rcedit.exe" ".\builds\$APP_NAME.exe" --set-icon ".\resources\icons\app.ico"
    } else {
        Write-Host "Warning: rcedit.exe not found, skipping icon embedding" -ForegroundColor Yellow
    }

    Write-Host "Windows build complete: builds\$APP_NAME.exe" -ForegroundColor Green

    # Create Windows zip
    Write-Host "Creating Windows distribution zip..."
    Compress-Archive -Path ".\builds\$APP_NAME.exe" -DestinationPath ".\builds\$APP_NAME-$VERSION-windows.zip" -Force
    Write-Host "Created: builds\$APP_NAME-$VERSION-windows.zip" -ForegroundColor Green

    # Clean up loose file
    Remove-Item ".\builds\$APP_NAME.exe"
} else {
    Write-Host "Windows build failed!" -ForegroundColor Red
}

# Build for macOS
Write-Host "`nBuilding for macOS..." -ForegroundColor Cyan
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
& go build -o ".\$APP_NAME-mac"

if (!(Test-Path ".\$APP_NAME-mac")) {
    Write-Host "macOS build failed! Make sure Go can cross-compile to Darwin." -ForegroundColor Red
    Write-Host "Skipping macOS packaging..." -ForegroundColor Yellow
} else {
    Write-Host "Creating app bundle..."

    # Clean up old bundle if it exists
    if (Test-Path ".\builds\$APP_NAME.app") {
        Remove-Item -Recurse -Force ".\builds\$APP_NAME.app"
    }

    # Create directory structure
    $appBundle = ".\builds\$APP_NAME.app"
    New-Item -ItemType Directory -Path "$appBundle\Contents\MacOS" -Force | Out-Null
    New-Item -ItemType Directory -Path "$appBundle\Contents\Resources" -Force | Out-Null

    # Move binary
    Move-Item ".\$APP_NAME-mac" "$appBundle\Contents\MacOS\$APP_NAME"

    # Create Info.plist
    $infoPlist = @"
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
</dict>
</plist>
"@
    $infoPlist | Out-File -FilePath "$appBundle\Contents\Info.plist" -Encoding UTF8

    # Copy icon
    if (Test-Path ".\resources\icons\app.icns") {
        Copy-Item ".\resources\icons\app.icns" "$appBundle\Contents\Resources\" -Force
        Write-Host "Icon added to app bundle" -ForegroundColor Green
    } else {
        Write-Host "Warning: app.icns not found in resources\icons\" -ForegroundColor Yellow
    }

    Write-Host "macOS build complete: builds\$APP_NAME.app" -ForegroundColor Green

    # Create macOS zip
    Write-Host "Creating macOS distribution zip..."
    Compress-Archive -Path ".\builds\$APP_NAME.app" -DestinationPath ".\builds\$APP_NAME-$VERSION-macos.zip" -Force
    Write-Host "Created: builds\$APP_NAME-$VERSION-macos.zip" -ForegroundColor Green

    # Clean up the .app folder
    Remove-Item -Recurse -Force ".\builds\$APP_NAME.app"
}

# Summary
Write-Host "`nBuilds complete!" -ForegroundColor Green
if (Test-Path ".\builds\$APP_NAME-$VERSION-windows.zip") {
    Write-Host "- Windows: builds\$APP_NAME-$VERSION-windows.zip" -ForegroundColor White
}
if (Test-Path ".\builds\$APP_NAME-$VERSION-macos.zip") {
    Write-Host "- macOS: builds\$APP_NAME-$VERSION-macos.zip" -ForegroundColor White
}