@echo off
setlocal enabledelayedexpansion

:: Toggle-Startup.bat
:: Toggles Claude WebExtension Launcher in Windows Startup folder

:: Get the directory where this script is located
set "ScriptDir=%~dp0"
:: Remove trailing backslash
set "ScriptDir=%ScriptDir:~0,-1%"

:: Find the launcher executable
set "LauncherExe=%ScriptDir%\Claude_WebExtension_Launcher.exe"
if not exist "%LauncherExe%" (
    echo ERROR: Could not find Claude_WebExtension_Launcher.exe in the same directory as this script.
    echo Make sure this script is in the same folder as the launcher executable.
    pause
    exit /b 1
)

:: Define both shortcut paths
set "StartupShortcut=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\Claude Desktop (Extended).lnk"
set "StartMenuShortcut=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Claude Desktop (Extended).lnk"

:: Check current status
set "InStartup=0"
set "InStartMenu=0"
if exist "%StartupShortcut%" set "InStartup=1"
if exist "%StartMenuShortcut%" set "InStartMenu=1"

:: Display header
echo.
echo === Claude WebExtension Launcher - Startup Toggle ===
echo.

:: Display current status
echo Current Status:
if "%InStartup%"=="1" (
    echo   [ENABLED] In Startup folder - Launches automatically at Windows boot
) else (
    echo   [DISABLED] Not in Startup folder - Does not launch at boot
)

if "%InStartMenu%"=="1" (
    echo   [Note] Also in Start Menu
)
echo.

:: Show menu
echo What would you like to do?
echo 1. Enable startup ^(add to Startup folder^)
echo 2. Disable startup ^(remove from Startup folder^)
echo 3. Exit without changes
echo.

:GetChoice
set /p "Choice=Enter your choice (1-3): "

if "%Choice%"=="1" goto EnableStartup
if "%Choice%"=="2" goto DisableStartup
if "%Choice%"=="3" goto ExitScript
echo Invalid choice. Please enter 1, 2, or 3.
goto GetChoice

:EnableStartup
if "%InStartup%"=="1" (
    echo Startup is already enabled.
    goto Done
)

:: Check if shortcut exists in Start Menu and can be moved
if "%InStartMenu%"=="1" (
    echo Found existing shortcut in Start Menu, moving to Startup folder...
    move "%StartMenuShortcut%" "%StartupShortcut%" >nul 2>&1
    if !errorlevel! equ 0 (
        echo SUCCESS: Startup enabled! Shortcut moved from Start Menu.
        echo The launcher will now start automatically when Windows boots.
        goto Done
    ) else (
        echo WARNING: Could not move existing shortcut. Creating a new one...
    )
)

:: Create new shortcut using PowerShell
echo Creating startup shortcut...
powershell -Command "$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut('%StartupShortcut%'); $s.TargetPath = '%LauncherExe%'; $s.WorkingDirectory = '%ScriptDir%'; $s.Description = 'Claude Desktop (Extended)'; $s.Save()" >nul 2>&1

if exist "%StartupShortcut%" (
    echo SUCCESS: Startup enabled!
    echo The launcher will now start automatically when Windows boots.
) else (
    echo ERROR: Failed to create startup shortcut.
)
goto Done

:DisableStartup
if "%InStartup%"=="0" (
    echo Startup is already disabled.
    goto Done
)

:: Just remove from Startup, don't move to Start Menu
del "%StartupShortcut%" >nul 2>&1
if not exist "%StartupShortcut%" (
    echo SUCCESS: Startup disabled!
    echo The launcher will no longer start automatically when Windows boots.
    if "%InStartMenu%"=="1" (
        echo Note: Shortcut remains in Start Menu for manual access.
    )
) else (
    echo ERROR: Failed to remove startup shortcut.
)
goto Done

:ExitScript
echo No changes made. Exiting...
goto Done

:Done
echo.
pause
exit /b 0