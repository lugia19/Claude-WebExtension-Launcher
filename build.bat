@echo off
set APP_NAME=Claude_WebExtension_Launcher
set PACKAGE_NAME=com.lugia19.claudewebextlauncher

:: Create builds directory if it doesn't exist
if not exist ".\builds" mkdir ".\builds"

echo Building for Windows...
set GOOS=windows
set GOARCH=amd64
go build -o .\builds\%APP_NAME%.exe
if exist .\builds\%APP_NAME%.exe (
    .\resources\rcedit.exe .\builds\%APP_NAME%.exe --set-icon .\resources\icons\app.ico
    echo Windows build complete: builds\%APP_NAME%.exe

    :: Copy resources folder next to exe
    echo Copying resources folder...
    xcopy /E /I /Y ".\resources" ".\builds\resources" >nul

    :: Copy version.txt next to exe
    echo Copying version.txt...
    copy ".\version.txt" ".\builds\version.txt" >nul

    :: Create Windows zip with exe, resources, and version.txt
    echo Creating Windows distribution zip...
    powershell Compress-Archive -Path '.\builds\%APP_NAME%.exe','.\builds\resources','.\builds\version.txt' -DestinationPath '.\builds\%APP_NAME%-windows.zip' -Force
    echo Created: builds\%APP_NAME%-windows.zip

    :: Clean up loose files (keep them in the zip)
    del .\builds\%APP_NAME%.exe
    del .\builds\version.txt
    rd /s /q .\builds\resources
) else (
    echo Windows build failed!
)

echo.
echo Building for macOS...
set GOOS=darwin
set GOARCH=amd64
go build -o .\%APP_NAME%-macos
if not exist .\%APP_NAME%-macos (
    echo macOS build failed! Make sure Go can cross-compile to Darwin.
    echo Skipping macOS packaging...
    goto :cleanup
)

echo Creating app bundle...

:: Clean up old bundle if it exists
if exist ".\builds\%APP_NAME%.app" rd /s /q ".\builds\%APP_NAME%.app"

:: Create directory structure
mkdir ".\builds\%APP_NAME%.app" 2>nul
mkdir ".\builds\%APP_NAME%.app\Contents" 2>nul
mkdir ".\builds\%APP_NAME%.app\Contents\MacOS" 2>nul
mkdir ".\builds\%APP_NAME%.app\Contents\Resources" 2>nul

:: Copy binary
move .\%APP_NAME%-macos ".\builds\%APP_NAME%.app\Contents\MacOS\%APP_NAME%"

:: Copy resources folder next to the macOS executable
echo Copying resources folder to app bundle...
xcopy /E /I /Y ".\resources" ".\builds\%APP_NAME%.app\Contents\MacOS\resources" >nul

:: Copy version.txt next to the macOS executable
echo Copying version.txt to app bundle...
copy ".\version.txt" ".\builds\%APP_NAME%.app\Contents\MacOS\version.txt" >nul

:: Create Info.plist
(
    echo ^<?xml version="1.0" encoding="UTF-8"?^>
    echo ^<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"^>
    echo ^<plist version="1.0"^>
    echo ^<dict^>
    echo     ^<key^>CFBundleExecutable^</key^>
    echo     ^<string^>%APP_NAME%^</string^>
    echo     ^<key^>CFBundleIconFile^</key^>
    echo     ^<string^>app.icns^</string^>
    echo     ^<key^>CFBundleIdentifier^</key^>
    echo     ^<string^>%PACKAGE_NAME%^</string^>
    echo     ^<key^>CFBundleName^</key^>
    echo     ^<string^>%APP_NAME%^</string^>
    echo     ^<key^>CFBundlePackageType^</key^>
    echo     ^<string^>APPL^</string^>
    echo     ^<key^>LSMinimumSystemVersion^</key^>
    echo     ^<string^>10.12^</string^>
    echo ^</dict^>
    echo ^</plist^>
) > ".\builds\%APP_NAME%.app\Contents\Info.plist"

:: Copy icon from resources/icons
if exist ".\resources\icons\app.icns" (
    copy ".\resources\icons\app.icns" ".\builds\%APP_NAME%.app\Contents\Resources\" >nul
    echo Icon added to app bundle
) else (
    echo Warning: app.icns not found in resources\icons\
)

echo macOS build complete: builds\%APP_NAME%.app

:: Zip it up for distribution
echo Creating macOS distribution zip...
powershell Compress-Archive -Path '.\builds\%APP_NAME%.app' -DestinationPath '.\builds\%APP_NAME%-macos.zip' -Force
echo Created: builds\%APP_NAME%-macos.zip

:: Clean up the .app folder after zipping
rd /s /q ".\builds\%APP_NAME%.app"

:cleanup
echo.
echo Builds complete!
if exist .\builds\%APP_NAME%-windows.zip echo - Windows: builds\%APP_NAME%-windows.zip
if exist .\builds\%APP_NAME%-macos.zip echo - macOS: builds\%APP_NAME%-macos.zip
echo - Directory: builds\%APP_NAME%\