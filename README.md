# Claude Desktop WebExtension Installer

A custom installer for Claude Desktop that includes built-in extensions (and the ability to install your own).

**Note**: The extensions in question are _Web_ extensions! Not to be confused with Local MCPs, which the client also calls Extensions and come in the dxt format.

**Disclaimer**: This is an unofficial, third-party modification of Claude Desktop that enables extension support. By using this installer, you acknowledge that:
- You are doing so at your own risk and discretion
- This project is neither affiliated with nor endorsed by Anthropic
- You are responsible for ensuring your use complies with all applicable terms and agreements

## Overview

This installer generates a modified version of the Claude Desktop client with extension support enabled. It creates a standalone installation that can coexist with the official Claude Desktop client, automatically keeping both the client and extensions up to date.

## Known Issues

### Extension not showing up

This can happen due to reasons I'm not really sure of. Restarting the application is enough.

### First Launch Network Service Crash (macOS only)
On first launch, you might see a crash dialog about the network service. This is (likely) because the modified app needs Keychain permission to be granted, given that it uses an ad-hoc signature. Just ignore it.

### MacOS issues
MacOS REALLY doesn't like apps that aren't notarized (aka, that haven't paid the 99$ apple tax).

If the app doesn't work, you might need to go to settings and security to let it run, or clear the quarantine flag:
`xattr -cr /path/to/Claude_WebExtension_Launcher.app`

Not much I can do. I can't afford the subscription, and even if I could, this wouldn't be allowed on the app store.

## Installation

### Supported Platforms
- **macOS** - Intel and Apple Silicon
- **Windows** - Windows 10/11

### Quick Start
Download the latest installer from [Releases](../../releases) and run it. The installer will handle everything automatically.

## Features

The installer provides:

- **Latest Claude Desktop** - Automatically downloads and updates to the most recent supported version
- **Custom Icon** - Distinguishes your extended installation from the standard client
- **Extension Support** - Unpacks resources and enables extension loading capabilities. You can add your own unpacked extensions in the extensions folder. NOTE: Most extensions will need to be adapted to work.
- **Pre-installed Extensions** - Includes Usage Tracker (and Toolbox coming soon, including all my userscripts from [here](https://github.com/lugia19/Claude-Toolbox))
- **Automatic Updates** - Keeps both the client and extensions current
- **Standalone Installation** - Runs independently alongside the official Claude Desktop

## How It Works

1. Downloads the latest compatible Claude Desktop client, creating a separate install
2. Modifies the application to enable extension loading
3. Applies a custom icon for easy identification
4. Installs the default extensions

## macOS ASAR Integrity (Crash Fix)

On macOS, Electron validates the integrity of `Resources/app.asar` at runtime using the SHA256 of the ASAR header (not the whole file). If this hash in `Info.plist` does not match, the app can crash immediately on launch.

What this installer does:
- Computes the SHA256 of the ASAR header using `@electron/asar.getRawHeader()` and hashes the exact header bytes (`headerString`).
- Updates every `Info.plist` under `Claude.app/Contents/` that includes `ElectronAsarIntegrity` for `Resources/app.asar` to the new hash.
- Uses Node’s `createRequire()` against the installer’s `node_modules` to reliably resolve `@electron/asar` (or `asar` fallback).

Verify the hash matches:
- Compute header hash manually:
  ```bash
  node -e 'const c=require("node:crypto");const{createRequire}=require("node:module");const req=createRequire(process.argv[2]+"/");let asar;try{asar=req("@electron/asar")}catch{asar=req("asar")}const r=asar.getRawHeader(process.argv[3]);const raw=r&&r.headerString?r.headerString:r;console.log(c.createHash("sha256").update(typeof raw==="string"?Buffer.from(raw):raw).digest("hex"))' \
    "$HOME/Library/Application Support/Claude WebExtension Launcher/node_modules" \
    "$HOME/Library/Application Support/Claude WebExtension Launcher/app-latest/Claude.app/Contents/Resources/app.asar"
  ```
- Read the plist value:
  ```bash
  /usr/libexec/PlistBuddy -c 'Print :ElectronAsarIntegrity:Resources/app.asar:hash' \
    "$HOME/Library/Application Support/Claude WebExtension Launcher/app-latest/Claude.app/Contents/Info.plist"
  ```
- These two hashes must be identical.

Tip: You can force a fresh patch and debug-launch with:
```bash
./bin/launcher --debug-launch --force-reinstall --no-term-relaunch
```

## Privacy

The installer only modifies your local Claude Desktop installation. No data is collected or transmitted by the installer itself. Individual extensions may have their own privacy policies.

## Troubleshooting

If you encounter issues:
- Ensure you have the latest version of the installer
- Check that your system meets the platform requirements
- The extended installation can be completely removed by deleting the installation folder
