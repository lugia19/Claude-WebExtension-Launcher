# build-all.ps1

$APP_NAME = "Claude_WebExtension_Launcher"
$PACKAGE_NAME = "com.lugia19.claudewebextlauncher"

# Read version from main.go
$mainGoContent = Get-Content ".\main.go" -Raw
if ($mainGoContent -match 'const\s+Version\s*=\s*"([^"]+)"') {
    $VERSION = $matches[1]
    Write-Host "Building version: $VERSION" -ForegroundColor Green
}
else {
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
    }
    else {
        Write-Host "Warning: rcedit.exe not found, skipping icon embedding" -ForegroundColor Yellow
    }

    Write-Host "Windows build complete: builds\$APP_NAME.exe" -ForegroundColor Green
}
else {
    Write-Host "Windows build failed!" -ForegroundColor Red
}

# Function to create macOS app bundle
function Create-MacOSBundle {
    param(
        [string]$BinaryPath,
        [string]$Architecture,
        [string]$MinimumOS
    )
    
    $archSuffix = if ($Architecture -eq "arm64") { "-arm64" } else { "" }
    $appBundle = ".\builds\$APP_NAME$archSuffix.app"
    
    Write-Host "Creating app bundle for $Architecture..."

    # Clean up old bundle if it exists
    if (Test-Path $appBundle) {
        Remove-Item -Recurse -Force $appBundle
    }

    # Create directory structure
    New-Item -ItemType Directory -Path "$appBundle\Contents\MacOS" -Force | Out-Null
    New-Item -ItemType Directory -Path "$appBundle\Contents\Resources" -Force | Out-Null

    # Move binary
    Move-Item $BinaryPath "$appBundle\Contents\MacOS\$APP_NAME"

    # Create Info.plist with architecture-specific settings
    $archPriority = if ($Architecture -eq "arm64") {
        @"
    <key>LSArchitecturePriority</key>
    <array>
        <string>arm64</string>
    </array>
"@
    }
    else { "" }

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
    <string>$MinimumOS</string>$archPriority
</dict>
</plist>
"@
    $infoPlist | Out-File -FilePath "$appBundle\Contents\Info.plist" -Encoding UTF8

    # Copy icon
    if (Test-Path ".\resources\icons\app.icns") {
        Copy-Item ".\resources\icons\app.icns" "$appBundle\Contents\Resources\" -Force
        Write-Host "Icon added to app bundle" -ForegroundColor Green
    }
    else {
        Write-Host "Warning: app.icns not found in resources\icons\" -ForegroundColor Yellow
    }

    return $appBundle
}

# Build for macOS Intel (x64)
Write-Host "`nBuilding for macOS Intel (x64)..." -ForegroundColor Cyan
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
& go build -o ".\$APP_NAME-mac-intel"

if (Test-Path ".\$APP_NAME-mac-intel") {
    $intelBundle = Create-MacOSBundle -BinaryPath ".\$APP_NAME-mac-intel" -Architecture "amd64" -MinimumOS "10.12"
    Write-Host "macOS Intel build complete: $intelBundle" -ForegroundColor Green
}
else {
    Write-Host "macOS Intel build failed! Make sure Go can cross-compile to Darwin." -ForegroundColor Red
}

# Build for macOS ARM64 (Apple Silicon)
Write-Host "`nBuilding for macOS ARM64 (Apple Silicon)..." -ForegroundColor Cyan
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
& go build -o ".\$APP_NAME-mac-arm64"

if (Test-Path ".\$APP_NAME-mac-arm64") {
    $arm64Bundle = Create-MacOSBundle -BinaryPath ".\$APP_NAME-mac-arm64" -Architecture "arm64" -MinimumOS "11.0"
    Write-Host "macOS ARM64 build complete: $arm64Bundle" -ForegroundColor Green
}
else {
    Write-Host "macOS ARM64 build failed! Make sure Go can cross-compile to Darwin ARM64." -ForegroundColor Red
}

# Ad-hoc sign macOS app bundles with rcodesign
if (Test-Path ".\resources\rcodesign.exe") {
    Write-Host "`nAd-hoc signing macOS app bundles..." -ForegroundColor Cyan

    # Sign Intel bundle if it exists
    if ($intelBundle -and (Test-Path $intelBundle)) {
        Write-Host "Signing Intel app bundle..." -ForegroundColor Cyan
        & ".\resources\rcodesign.exe" sign $intelBundle
        if ($LASTEXITCODE -eq 0) {
            Write-Host "Intel app bundle signed successfully" -ForegroundColor Green
        } else {
            Write-Host "Warning: Failed to sign Intel app bundle" -ForegroundColor Yellow
        }
    }

    # Sign ARM64 bundle if it exists
    if ($arm64Bundle -and (Test-Path $arm64Bundle)) {
        Write-Host "Signing ARM64 app bundle..." -ForegroundColor Cyan
        & ".\resources\rcodesign.exe" sign $arm64Bundle
        if ($LASTEXITCODE -eq 0) {
            Write-Host "ARM64 app bundle signed successfully" -ForegroundColor Green
        } else {
            Write-Host "Warning: Failed to sign ARM64 app bundle" -ForegroundColor Yellow
        }
    }
} else {
    Write-Host "`nWarning: rcodesign.exe not found in resources folder, skipping ad-hoc signing" -ForegroundColor Yellow
    Write-Host "Download from: https://github.com/indygreg/apple-platform-rs/releases" -ForegroundColor Yellow
}

# Create distribution zips using WSL
Write-Host "`nCreating distribution zips with WSL..."

# Helper function to convert Windows path to WSL path
function ConvertTo-WSLPath {
    param([string]$WindowsPath)
    $resolved = (Resolve-Path $WindowsPath).Path
    $resolved = $resolved -replace '\\', '/'
    if ($resolved -match '^([A-Z]):(.*)') {
        return "/mnt/$($matches[1].ToLower())$($matches[2])"
    }
    return $resolved
}

# Get current directory in WSL format
$currentDirWSL = ConvertTo-WSLPath (Get-Location).Path

# Windows zip (no executable bit needed)
if (Test-Path ".\builds\$APP_NAME-$VERSION-windows.zip") {
    Remove-Item ".\builds\$APP_NAME-$VERSION-windows.zip"
}
if (Test-Path ".\builds\$APP_NAME.exe") {
    $tempDir = ".\builds\temp-windows"
    
    # Create temporary directory for packaging
    if (Test-Path $tempDir) {
        Remove-Item $tempDir -Recurse -Force
    }
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    
    # Copy executable and batch scripts to temp directory
    Copy-Item ".\builds\$APP_NAME.exe" "$tempDir\$APP_NAME.exe"
    Copy-Item ".\resources\Toggle-Startup.bat" "$tempDir\Toggle-Startup.bat"
    Copy-Item ".\resources\Toggle-StartMenu.bat" "$tempDir\Toggle-StartMenu.bat"
    
    $tempDirWSL = ConvertTo-WSLPath $tempDir
    
    wsl sh -c "cd '$tempDirWSL' && zip '$currentDirWSL/builds/$APP_NAME-$VERSION-windows.zip' *"
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Created: builds\$APP_NAME-$VERSION-windows.zip" -ForegroundColor Green
        Remove-Item ".\builds\$APP_NAME.exe"
        Remove-Item $tempDir -Recurse -Force
    }
}

# macOS Intel zip
if ($intelBundle -and (Test-Path $intelBundle)) {
    $bundleName = Split-Path $intelBundle -Leaf
    $zipName = "$APP_NAME-$VERSION-macos-amd64.zip"
    
    # Set executable bit and create zip
    wsl sh -c "cd '$currentDirWSL/builds' && chmod +x '$bundleName/Contents/MacOS/$APP_NAME' && zip -r '$zipName' '$bundleName'"
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Created: builds\$zipName" -ForegroundColor Green
    }
    Remove-Item -Recurse -Force $intelBundle
}

# macOS ARM64 zip
if ($arm64Bundle -and (Test-Path $arm64Bundle)) {
    $bundleName = Split-Path $arm64Bundle -Leaf
    $zipName = "$APP_NAME-$VERSION-macos-arm64.zip"
    
    # Set executable bit and create zip
    wsl sh -c "cd '$currentDirWSL/builds' && chmod +x '$bundleName/Contents/MacOS/$APP_NAME' && zip -r '$zipName' '$bundleName'"
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Created: builds\$zipName" -ForegroundColor Green
    }
    Remove-Item -Recurse -Force $arm64Bundle
}

# Summary
Write-Host "`nBuilds complete!" -ForegroundColor Green
if (Test-Path ".\builds\$APP_NAME-$VERSION-windows.zip") {
    Write-Host "- Windows: builds\$APP_NAME-$VERSION-windows.zip" -ForegroundColor White
}
if (Test-Path ".\builds\$APP_NAME-$VERSION-macos-amd64.zip") {
    Write-Host "- macOS Intel: builds\$APP_NAME-$VERSION-macos-amd64.zip" -ForegroundColor White
}
if (Test-Path ".\builds\$APP_NAME-$VERSION-macos-arm64.zip") {
    Write-Host "- macOS ARM64: builds\$APP_NAME-$VERSION-macos-arm64.zip" -ForegroundColor White
}
