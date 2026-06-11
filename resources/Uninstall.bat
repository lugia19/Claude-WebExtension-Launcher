@echo off
setlocal

:: Uninstall.bat - Removes the patched Claude WebExtension Launcher installation

set "InstallDir=%ProgramFiles%\WindowsApps\ClaudeWebExtLauncher"

:: If called with ELEVATED arg, skip straight to deletion
if "%~1"=="ELEVATED" goto DoUninstall

if not exist "%InstallDir%" (
    echo Nothing to uninstall - install directory does not exist.
    pause
    exit /b 0
)

echo.
echo === Claude WebExtension Launcher - Uninstall ===
echo.
echo This will remove the patched Claude Desktop installation at:
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

:: Self-elevate
echo Requesting administrator privileges...
powershell -Command "Start-Process -FilePath '%~f0' -ArgumentList 'ELEVATED' -Verb RunAs"
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

if not exist "%InstallDir%" (
    echo.
    echo Uninstall complete.
) else (
    echo.
    echo ERROR: Failed to remove install directory.
)

pause
exit /b 0
