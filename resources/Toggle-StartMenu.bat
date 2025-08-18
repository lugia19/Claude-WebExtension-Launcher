@echo off
setlocal enabledelayedexpansion

:: Toggle-StartMenu.bat
:: Toggles Claude Desktop (Extended) in Windows Start Menu

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
set "StartMenuShortcut=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Claude Desktop (Extended).lnk"
set "StartupShortcut=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\Claude Desktop (Extended).lnk"

:: Check current status
set "InStartMenu=0"
set "InStartup=0"
if exist "%StartMenuShortcut%" set "InStartMenu=1"
if exist "%StartupShortcut%" set "InStartup=1"

:: Display header
echo.
echo === Claude Desktop (Extended) - Start Menu Toggle ===
echo.

:: Display current status
echo Current Status:
if "%InStartup%"=="1" (
    echo   [ENABLED] In Start Menu via Startup folder
    echo   The launcher is accessible from Start Menu and launches at boot
    goto ShowStartupMenu
)
if "%InStartMenu%"=="1" (
    echo   [ENABLED] In Start Menu - Accessible from Start Menu
    goto ShowNormalMenu
)
echo   [DISABLED] Not in Start Menu

:ShowNormalMenu
echo.
:: Show menu for normal cases
echo What would you like to do?
echo 1. Enable Start Menu entry ^(add to Start Menu^)
echo 2. Disable Start Menu entry ^(remove from Start Menu^)
echo 3. Exit without changes
echo.
goto GetChoice

:ShowStartupMenu
echo.
:: Show menu when in Startup
echo What would you like to do?
echo 1. Already in Start Menu ^(via Startup folder^)
echo 2. Already in Start Menu ^(via Startup folder^)
echo 3. Exit without changes
echo.

:GetChoice
set /p "Choice=Enter your choice (1-3): "

if "%Choice%"=="1" goto HandleEnable
if "%Choice%"=="2" goto HandleDisable
if "%Choice%"=="3" goto ExitScript
echo Invalid choice. Please enter 1, 2, or 3.
goto GetChoice

:HandleEnable
:: If in Startup, it's already in Start Menu
if "%InStartup%"=="1" (
    echo The launcher is already in the Start Menu via the Startup folder.
    echo No changes needed.
    goto Done
)

:: If already in Start Menu directly
if "%InStartMenu%"=="1" (
    echo Start Menu entry is already enabled.
    goto Done
)

:: Create new shortcut using PowerShell
echo Creating Start Menu shortcut...
powershell -Command "$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut('%StartMenuShortcut%'); $s.TargetPath = '%LauncherExe%'; $s.WorkingDirectory = '%ScriptDir%'; $s.Description = 'Claude Desktop (Extended)'; $s.Save()" >nul 2>&1

if exist "%StartMenuShortcut%" (
    echo SUCCESS: Start Menu entry enabled!
    echo The launcher is now accessible from the Start Menu.
) else (
    echo ERROR: Failed to create Start Menu shortcut.
)
goto Done

:HandleDisable
:: If in Startup, can't remove it from here
if "%InStartup%"=="1" (
    echo The launcher appears in Start Menu via the Startup folder.
    echo Use Toggle-Startup.bat to remove it from Startup if you don't want it to launch at boot.
    goto Done
)

:: If not in Start Menu at all
if "%InStartMenu%"=="0" (
    echo Start Menu entry is already disabled.
    goto Done
)

:: Remove from Start Menu
del "%StartMenuShortcut%" >nul 2>&1
if not exist "%StartMenuShortcut%" (
    echo SUCCESS: Start Menu entry disabled!
    echo The launcher no longer appears in the Start Menu.
) else (
    echo ERROR: Failed to remove Start Menu shortcut.
)
goto Done

:ExitScript
echo No changes made. Exiting...
goto Done

:Done
echo.
pause
exit /b 0