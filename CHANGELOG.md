# Changelog

## 2025-08-17

- macOS: Fix Electron ASAR integrity crash
  - Compute SHA256 of the ASAR header string via `@electron/asar.getRawHeader()` and hash `headerString` bytes.
  - Replace all `ElectronAsarIntegrity` hashes across `Claude.app/Contents/**/Info.plist` that reference `Resources/app.asar`.
  - Resolve Node modules via `createRequire()` against the installer `node_modules` path; prefer `@electron/asar` (Node >= 22) with fallback to `asar`.
- Docs: Add README section explaining ASAR integrity, verification steps, and debug re-install command.
- Code comments: Clarify why we hash the header string and how Node module resolution works.
