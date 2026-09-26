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

# The window (Fyne) needs cgo, so every build needs a C compiler for its target, and
# can only be built on that platform: Windows here (gcc from WinLibs/MinGW-w64 on the
# PATH), Linux through WSL (gcc and the X11/GL headers). macOS is built on a Mac
# (build-all.sh).
$env:CGO_ENABLED = "1"
if (!(Get-Command gcc -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: gcc not found. Install WinLibs (winget install BrechtSanders.WinLibs.POSIX.UCRT) and open a new terminal." -ForegroundColor Red
    exit 1
}

# Build for Windows
Write-Host "`nBuilding for Windows..." -ForegroundColor Cyan
$env:GOOS = "windows"
$env:GOARCH = "amd64"
& go build -ldflags "-H=windowsgui" -o ".\builds\$APP_NAME.exe"

if (Test-Path ".\builds\$APP_NAME.exe") {
    Write-Host "Windows build complete: builds\$APP_NAME.exe" -ForegroundColor Green
}
else {
    Write-Host "Windows build failed!" -ForegroundColor Red
}

# Build for Linux (amd64) in WSL, with its Go (~/sdk/go, or on the PATH). ARM64 needs
# an ARM64 machine.
Write-Host "`nBuilding for Linux (amd64) in WSL..." -ForegroundColor Cyan
wsl -e sh -c "GO=`$HOME/sdk/go/bin/go; [ -x `$GO ] || GO=go; CGO_ENABLED=1 `$GO build -o 'builds/$APP_NAME-linux-amd64'"
if (Test-Path ".\builds\$APP_NAME-linux-amd64") {
    Write-Host "Linux amd64 build complete: builds\$APP_NAME-linux-amd64" -ForegroundColor Green
}
else {
    Write-Host "Linux amd64 build failed! WSL needs Go, gcc, and libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev." -ForegroundColor Red
}
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED

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
    
    # Copy the executable to the temp directory
    Copy-Item ".\builds\$APP_NAME.exe" "$tempDir\$APP_NAME.exe"
    
    $tempDirWSL = ConvertTo-WSLPath $tempDir
    
    wsl sh -c "cd '$tempDirWSL' && zip '$currentDirWSL/builds/$APP_NAME-$VERSION-windows.zip' *"
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Created: builds\$APP_NAME-$VERSION-windows.zip" -ForegroundColor Green
        Remove-Item ".\builds\$APP_NAME.exe"
        Remove-Item $tempDir -Recurse -Force
    }
}

# Linux zips (the binary is renamed to the plain app name the self-updater looks for)
$arch = "amd64"
if (Test-Path ".\builds\$APP_NAME-linux-$arch") {
    $zipName = "$APP_NAME-$VERSION-linux-$arch.zip"
    $tempDir = ".\builds\temp-linux-$arch"
    if (Test-Path $tempDir) {
        Remove-Item $tempDir -Recurse -Force
    }
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    Move-Item ".\builds\$APP_NAME-linux-$arch" "$tempDir\$APP_NAME"

    $tempDirWSL = ConvertTo-WSLPath $tempDir
    wsl sh -c "cd '$tempDirWSL' && chmod +x '$APP_NAME' && rm -f '$currentDirWSL/builds/$zipName' && zip '$currentDirWSL/builds/$zipName' '$APP_NAME'"

    if ($LASTEXITCODE -eq 0) {
        Write-Host "Created: builds\$zipName" -ForegroundColor Green
    }
    Remove-Item $tempDir -Recurse -Force
}

# Summary
Write-Host "`nBuilds complete!" -ForegroundColor Green
if (Test-Path ".\builds\$APP_NAME-$VERSION-windows.zip") {
    Write-Host "- Windows: builds\$APP_NAME-$VERSION-windows.zip" -ForegroundColor White
}
if (Test-Path ".\builds\$APP_NAME-$VERSION-linux-amd64.zip") {
    Write-Host "- Linux AMD64: builds\$APP_NAME-$VERSION-linux-amd64.zip" -ForegroundColor White
}
