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

    :: Create Windows zip with exe and resources
    echo Creating Windows distribution zip...
    powershell Compress-Archive -Path '.\builds\%APP_NAME%.exe','.\builds\resources' -DestinationPath '.\builds\%APP_NAME%-windows.zip' -Force
    echo Created: builds\%APP_NAME%-windows.zip

    :: Clean up loose files (keep them in the zip)
    del .\builds\%APP_NAME%.exe
    rd /s /q .\builds\resources
) else (
    echo Windows build failed!
)

echo.
echo Converting icon for macOS...
magick convert .\resources\icons\app.ico -resize 512x512 .\app.icns
if not exist .\app.icns (
    echo Warning: Icon conversion failed
)

echo.
echo Building for macOS...
set GOOS=darwin
set GOARCH=amd64
go build -o .\%APP_NAME%-mac
if not exist .\%APP_NAME%-mac (
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
move .\%APP_NAME%-mac ".\builds\%APP_NAME%.app\Contents\MacOS\%APP_NAME%"

:: Copy resources folder next to the macOS executable
echo Copying resources folder to app bundle...
xcopy /E /I /Y ".\resources" ".\builds\%APP_NAME%.app\Contents\MacOS\resources" >nul

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

:: Copy icon and then delete the temporary icns
if exist .\app.icns (
    copy .\app.icns ".\builds\%APP_NAME%.app\Contents\Resources\" >nul
    del .\app.icns
    echo Icon added to app bundle
)

echo macOS build complete: builds\%APP_NAME%.app

:: Zip it up for distribution
echo Creating macOS distribution zip...
powershell Compress-Archive -Path '.\builds\%APP_NAME%.app' -DestinationPath '.\builds\%APP_NAME%-mac.zip' -Force
echo Created: builds\%APP_NAME%-mac.zip

:: Clean up the .app folder after zipping
rd /s /q ".\builds\%APP_NAME%.app"

:cleanup
:: Clean up any temporary files
if exist .\app.icns del .\app.icns

echo.
echo Builds complete!
if exist .\builds\%APP_NAME%-windows.zip echo - Windows: builds\%APP_NAME%-windows.zip
if exist .\builds\%APP_NAME%-mac.zip echo - macOS: builds\%APP_NAME%-mac.zip
echo - Directory: builds\%APP_NAME%\