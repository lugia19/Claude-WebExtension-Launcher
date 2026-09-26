# build-all.ps1
#
# Builds the Windows zip here and the Linux amd64 zip in WSL. The window (Fyne) needs
# cgo, so each platform is built on that platform: macOS on a Mac (build-all.sh), and
# CI builds everything (.github/workflows/build.yml).

& .\build-windows.ps1
$windowsOk = $LASTEXITCODE -eq 0

# WSL needs gcc, the X11/GL headers (see build-linux.sh) and Go, in ~/sdk/go or on the
# PATH.
Write-Host "`nBuilding for Linux (amd64) in WSL..." -ForegroundColor Cyan
wsl -e sh -c "GO=`$HOME/sdk/go/bin/go; [ -x `$GO ] || GO=go; GO=`$GO bash build-linux.sh"
$linuxOk = $LASTEXITCODE -eq 0

Write-Host ""
if ($windowsOk -and $linuxOk) {
    Write-Host "Builds complete!" -ForegroundColor Green
}
else {
    if (!$windowsOk) { Write-Host "Windows build failed!" -ForegroundColor Red }
    if (!$linuxOk) { Write-Host "Linux build failed!" -ForegroundColor Red }
    exit 1
}
