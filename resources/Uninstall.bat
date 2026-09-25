@echo off
setlocal

:: Uninstall.bat - Removes the patched Claude WebExtension Launcher installation

set "InstallDir=%ProgramFiles%\WindowsApps\ClaudeWebExtLauncher"
set "LauncherDir=%LOCALAPPDATA%\ClaudeWebExtLauncher"

:: If called with ELEVATED arg, skip straight to deletion
if "%~1"=="ELEVATED" goto DoUninstall

:: The launcher installs itself before it installs Claude, so either can exist alone
:: (e.g. the Claude install was cancelled).
if not exist "%InstallDir%" if not exist "%LauncherDir%\Claude_WebExtension_Launcher.exe" (
    echo Nothing to uninstall - neither the launcher nor the patched Claude is installed.
    pause
    exit /b 0
)

echo.
echo === Claude WebExtension Launcher - Uninstall ===
echo.
echo This will remove the launcher, its shortcuts, and the patched Claude Desktop at:
echo   %InstallDir%
echo.
echo Your conversation data will NOT be deleted.
echo.

set /p "Confirm=Are you sure? (Y/N): "
if /i not "%Confirm%"=="Y" (
    echo Cancelled.
    pause
    exit /b 0
)

:: A running launcher (e.g. its instance list left open) can't be deleted. The result is
:: checked on its own line: whatever follows | runs in a separate cmd, where an exit
:: wouldn't stop this script.
tasklist /fi "imagename eq Claude_WebExtension_Launcher.exe" 2>nul | find /i "Claude_WebExtension_Launcher.exe" >nul
if not errorlevel 1 (
    echo.
    echo The launcher is still running. Close its window, then run this again.
    pause
    exit /b 1
)

:: First the patched Claude, which needs admin rights: an elevated copy of this script
:: removes it, and this one waits for it. The launcher only goes once Claude has, so
:: refusing the admin prompt doesn't leave Claude without the launcher that manages it.
if exist "%InstallDir%" (
    echo Requesting administrator privileges...
    powershell -NoProfile -Command "try { $p = Start-Process -FilePath '%~f0' -ArgumentList 'ELEVATED' -Verb RunAs -Wait -PassThru; exit $p.ExitCode } catch { exit 1 }"
    if errorlevel 1 (
        echo.
        echo The patched Claude was not removed, so the launcher was kept too.
        pause
        exit /b 1
    )
)

:: Remove the Start Menu / Startup shortcuts: the launcher's ("Claude Desktop
:: (Extended)") and the instances' ("Claude (<name>)"). Done here, unelevated, so
:: %APPDATA% is this user's.
echo Removing Start Menu and Startup shortcuts...
for %%D in ("%APPDATA%\Microsoft\Windows\Start Menu\Programs" "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup") do (
    del /q "%%~D\Claude Desktop (Extended).lnk" >nul 2>&1
    del /q "%%~D\Claude (*).lnk" >nul 2>&1
)

:: Remove the installed launcher (it installs itself here on first run). Its logs and
:: settings stay, like the conversation data.
echo Removing the installed launcher...
del /q "%LauncherDir%\Claude_WebExtension_Launcher.exe" >nul 2>&1
del /q "%LauncherDir%\launcher-version.txt" >nul 2>&1
if exist "%LauncherDir%\Claude_WebExtension_Launcher.exe" (
    echo.
    echo ERROR: Could not remove %LauncherDir%\Claude_WebExtension_Launcher.exe
    echo ^(is the launcher still running?^). Close it and run this again.
    pause
    exit /b 1
)

echo.
echo Uninstall complete.
pause
exit /b 0

:DoUninstall
echo.

:: Remove the Cowork service, but only if it points at OUR install (don't disturb an
:: official packaged service). Stop it first so cowork-svc.exe isn't locked.
sc qc CoworkVMService 2>nul | findstr /i "ClaudeWebExtLauncher" >nul && (
    echo Stopping and removing Cowork service...
    sc stop CoworkVMService >nul 2>&1
    sc delete CoworkVMService >nul 2>&1
)

echo Removing Cowork firewall rules...
netsh advfirewall firewall delete rule name="ClaudeWebExtLauncher-Cowork-In" >nul 2>&1
netsh advfirewall firewall delete rule name="ClaudeWebExtLauncher-Cowork-Out" >nul 2>&1

echo.
echo Removing %InstallDir%...
rmdir /s /q "%InstallDir%"

:: The exit code tells the unelevated copy (waiting for this one) whether to go on
:: and remove the launcher.
if exist "%InstallDir%" (
    echo.
    echo ERROR: Failed to remove install directory.
    pause
    exit /b 1
)
echo Patched Claude removed.
exit /b 0
