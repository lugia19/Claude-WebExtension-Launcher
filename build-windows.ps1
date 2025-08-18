# build-windows.ps1

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
		Write-Host "Embedding icon..." -ForegroundColor Cyan
		& ".\resources\rcedit.exe" ".\builds\$APP_NAME.exe" --set-icon ".\resources\icons\app.ico"
		if ($LASTEXITCODE -eq 0) {
			Write-Host "Icon embedded successfully" -ForegroundColor Green
		}
		else {
			Write-Host "Warning: Failed to embed icon" -ForegroundColor Yellow
		}
	}
 else {
		Write-Host "Warning: rcedit.exe not found, skipping icon embedding" -ForegroundColor Yellow
	}

	Write-Host "Windows build complete: builds\$APP_NAME.exe" -ForegroundColor Green
    
	# Create distribution zip using PowerShell's Compress-Archive
	Write-Host "`nCreating distribution zip..." -ForegroundColor Cyan
    
	$zipPath = ".\builds\$APP_NAME-$VERSION-windows.zip"
	$tempDir = ".\builds\temp-windows"
    
	# Remove old zip if it exists
	if (Test-Path $zipPath) {
		Remove-Item $zipPath -Force
	}
    
	# Create temporary directory for packaging
	if (Test-Path $tempDir) {
		Remove-Item $tempDir -Recurse -Force
	}
	New-Item -ItemType Directory -Path $tempDir | Out-Null
    
	# Copy executable and batch scripts to temp directory
	Copy-Item ".\builds\$APP_NAME.exe" "$tempDir\$APP_NAME.exe"
	Copy-Item ".\resources\Toggle-Startup.bat" "$tempDir\Toggle-Startup.bat"
	Copy-Item ".\resources\Toggle-StartMenu.bat" "$tempDir\Toggle-StartMenu.bat"
    
	# Create the zip file from temp directory
	Compress-Archive -Path "$tempDir\*" -DestinationPath $zipPath -CompressionLevel Optimal
    
	if (Test-Path $zipPath) {
		# Get file size for display
		$zipSize = (Get-Item $zipPath).Length / 1MB
		$zipSizeFormatted = "{0:N2} MB" -f $zipSize
        
		Write-Host "Created: builds\$APP_NAME-$VERSION-windows.zip ($zipSizeFormatted)" -ForegroundColor Green
        
		# Clean up temporary files
		Remove-Item ".\builds\$APP_NAME.exe"
		Remove-Item $tempDir -Recurse -Force
        
		Write-Host "`nBuild complete!" -ForegroundColor Green
		Write-Host "Distribution package: builds\$APP_NAME-$VERSION-windows.zip" -ForegroundColor White
	}
 else {
		Write-Host "Failed to create zip file!" -ForegroundColor Red
		exit 1
	}
}
else {
	Write-Host "Windows build failed!" -ForegroundColor Red
	exit 1
}