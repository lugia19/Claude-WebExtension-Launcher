# ARM Build Notes and macOS ASAR Integrity Fix

## Overview
This document captures the original macOS crash issue (ASAR integrity), how it was resolved, and a concise comparison between the current fix branch and the master branch. The intent is to preserve clear build/patch notes for Apple Silicon (and Intel) macOS while keeping the main README lighter.

## Original problem
- macOS Claude app crashed on launch due to Electron’s ASAR integrity check failing.
- Root cause: `ElectronAsarIntegrity` in `Info.plist` expects the SHA256 of the ASAR header only. The previous patching logic hashed the entire `Resources/app.asar`, causing a mismatch.

## Resolution (what changed)
- Compute the SHA256 of the ASAR header string using Node and `@electron/asar`:
  - In `patcher/patcher.go`, `computeAsarHeaderHashHex()` invokes Node to call `getRawHeader(app.asar)` and hashes the exact `headerString` bytes returned.
  - Module resolution uses Node’s `createRequire()` against the installer `node_modules` and prefers `@electron/asar` with fallback to `asar`.
- Synchronize all `ElectronAsarIntegrity` entries in `Info.plist` files:
  - Walk `Claude.app/Contents/**/Info.plist` and replace the old hash with the newly computed header hash wherever it references `Resources/app.asar`.
- Documentation/comments:
  - Comments added in `patcher/patcher.go` explain why we hash the header string and how module resolution works.
  - This ARM_build_notes.md documents the fix and verification steps.

## Verify the fix
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
- These two hashes must match.

## Branch comparison (master vs fix)
- master (before fix):
  - Hashing logic targeted the entire `app.asar`, which could cause a crash on macOS if modified.
  - Lacked explicit notes on header-only hashing and module resolution behavior.
- fix/macos-asar-integrity (current):
  - Correctly computes SHA256 of the ASAR header string via `@electron/asar.getRawHeader()`.
  - Updates all `Info.plist` files that include `ElectronAsarIntegrity` for `Resources/app.asar`.
  - Adds targeted comments in `patcher/patcher.go` and this ARM_build_notes.md.
  - Adds `.gitignore` hygiene for local `bin/` and `claude_safe_debug.log`.

## Compatibility impact
- macOS-only behavior change: the header-hash computation and plist updates run only when `runtime.GOOS == "darwin"`.
- Windows/Linux: unaffected; no code path changes.
- Intel vs Apple Silicon: architecture-agnostic; Electron expects header-hash on both.

## Developer tips
- Force a fresh patch and debug-launch:
```bash
./bin/launcher --debug-launch --force-reinstall --no-term-relaunch
```

## Files of interest
- `patcher/patcher.go`
  - `computeAsarHeaderHashHex()`
  - `captureHashMismatch()` -> `captureHashFromPlist()` for expected hash
  - `replaceHashInExe()` macOS branch to update Info.plist entries
- `Claude.app/Contents/Resources/app.asar`
- `Claude.app/Contents/**/Info.plist`
