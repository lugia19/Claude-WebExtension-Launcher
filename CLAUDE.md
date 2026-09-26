# CLAUDE.md - AI Assistant Guide

This document provides comprehensive guidance for AI assistants working with the Claude WebExtension Launcher codebase.

## Project Overview

**Claude WebExtension Launcher** is a Go-based installer/launcher that creates a modified version of Claude Desktop with browser-style web extension support. This is an unofficial third-party modification that enables loading unpacked Chrome/Edge extensions into Claude Desktop.

- **Current Version**: 1.1.1
- **Language**: Go 1.21
- **Platforms**: macOS (Intel & ARM64), Windows 10/11
- **License**: See LICENSE file
- **Repository**: https://github.com/lugia19/claude-webext-patcher

### Key Features
- Downloads and patches Claude Desktop to enable extension loading
- Automatically updates the launcher, Claude client, and extensions
- Includes pre-installed extensions (Usage Tracker, Toolbox)
- Runs standalone alongside official Claude Desktop
- Custom icon for easy identification

## Architecture & Directory Structure

```
Claude-WebExtension-Launcher/
├── main.go                      # Entry point - orchestrates all operations
├── embedded.go                  # Embeds resources using Go embed.FS
├── go.mod/go.sum               # Minimal dependencies (Go stdlib only)
│
├── patcher/                    # Core patching functionality
│   └── patcher.go              # Downloads, patches, and manages Claude (~800 LOC)
│
├── extensions/                 # Extension management
│   └── extensions.go           # GitHub release-based updater (~180 LOC)
│
├── selfupdate/                 # Launcher self-update mechanism
│   └── selfupdate.go           # Update checking and installation (~420 LOC)
│
├── utils/                      # Utility functions
│   └── paths.go                # Platform-specific path resolution
│
├── resources/                  # Embedded resources
│   ├── icons/                  # app.ico (Windows), app.icns (macOS)
│   ├── injections/             # JavaScript injected into Claude
│   │   └── 0.12.55/           # Version-specific patches
│   │       ├── extension_loader.js      # Main extension loading logic
│   │       ├── alarm_polyfill.js        # chrome.alarms API polyfill
│   │       ├── notification_polyfill.js # Notification API polyfill
│   │       └── tabevents_polyfill.js    # Tab event simulation
│   ├── Toggle-Startup.bat      # Windows startup shortcut helper
│   ├── Toggle-StartMenu.bat    # Windows start menu helper
│   └── rcedit.exe              # Windows icon embedding tool
│
└── build-all.sh                # Multi-platform build script
    build-all.ps1               # PowerShell build script
    build-macos-arm64.sh        # macOS ARM64 quick build
    build-windows.ps1           # Windows-specific build
```

## Key Modules and Their Responsibilities

### main.go (Entry Point - 121 lines)

**Purpose**: Orchestrates the entire launcher workflow

**Execution Flow**:
1. `FinishUpdateIfNeeded()` - Complete any pending launcher updates
2. `CheckAndUpdate()` - Check for launcher updates from GitHub
3. `EnsurePatched()` - Ensure Claude is installed and patched
4. `UpdateAll()` - Update all extensions from GitHub
5. Clear browser cache (Service Worker & WebStorage)
6. Launch the modified Claude executable

**Key Variables**:
- `Version = "1.1.1"` - Current launcher version
- `launchClaudeInTerminal` - Debug flag for terminal output

**macOS-Specific Behavior**:
- Detects if running without terminal (`TERM` env var)
- Re-launches in Terminal.app using AppleScript if needed

**Location**: `/main.go:1`

---

### patcher/patcher.go (Core Patching - ~800 lines)

**Purpose**: Downloads Claude Desktop, applies patches to enable extensions, manages integrity checks

**Key Constants**:
- `supportedVersions` - Maps Claude versions to their patch functions
  - Currently only supports `"0.12.55"`
- `windowsReleasesURL` - Official Windows update URL
- `macosReleasesURL` - Official macOS update manifest
- `AppFolder` - Points to `app-latest/` directory

**Main Functions**:

1. **`EnsurePatched()`** - Main entry point
   - Checks if Claude needs updating
   - Downloads/extracts if needed
   - Applies patches if needed
   - Writes `claude-version.txt`

2. **`getLatestSupportedVersion()`** - Fetches latest Claude version
   - macOS: Queries `update_manifest.json`
   - Windows: Parses `RELEASES` file

3. **`downloadAndExtract()`** - Downloads and extracts Claude
   - Windows: Extracts from `.nupkg` (zip), uses `lib/net45/` directory
   - macOS: Keeps full `.app` bundle, handles symlinks
   - **Removes ShipIt** to prevent Claude's self-updates

4. **`applyPatches()`** - Core patching mechanism
   - Unpacks `app.asar` (Electron archive) using asar tool
   - Beautifies minified JavaScript using js-beautify
   - Applies version-specific patch functions
   - Repacks `app.asar`
   - Updates hash in executable/plist to match modified archive
   - Ad-hoc signs macOS app (requires Keychain permission)

5. **`patch_0_12_55()`** - Patch for Claude v0.12.55
   - Injects extension loader at pattern: `return a(), i(), e.on("resize"`
   - Adds `"chrome-extension:"` to protocol whitelist
   - Combines 4 injection files into single block

6. **`replaceIcons()`** - Icon replacement
   - Windows: Uses `rcedit.exe` to embed icon in executable
   - macOS: Replaces `electron.icns` in bundle

**Critical Technical Details**:

**Hash Verification Flow**:
1. Electron checks `app.asar` hash on startup
2. When modified, Claude shows hash mismatch error
3. Launcher captures this error by running Claude
4. Launcher patches the expected hash into executable/Info.plist
5. This allows modified app.asar to pass integrity checks

**Platform Differences**:
- **macOS**: Hash stored in `Info.plist`, requires ad-hoc code signing
- **Windows**: Hash hardcoded in executable binary, replaced as bytes

**Dependencies**:
- Node.js (required for `asar` and `js-beautify` tools)
- `asar` npm package for unpacking/repacking Electron archives
- `js-beautify` npm package for making minified code readable

**Location**: `/patcher/patcher.go:1`

---

### extensions/extensions.go (Extension Management - ~180 lines)

**Purpose**: Manages browser extensions via GitHub releases

**Extension Configuration**:
```go
var extensions = []Extension{
    {Owner: "lugia19", Repo: "Claude-Usage-Extension", Folder: "usage-tracker"},
    {Owner: "lugia19", Repo: "Claude-Toolbox", Folder: "userscript-toolbox"},
}
```

**Main Functions**:

1. **`UpdateAll()`** - Main entry point
   - Creates `web-extensions/` directory
   - For each extension:
     - Reads current version from `manifest.json`
     - Queries GitHub API for latest release
     - Compares semantic versions
     - Downloads and extracts if newer available

2. **`downloadAndExtractExtension()`**
   - Downloads from GitHub release assets
   - Looks for "electron" zip file
   - Extracts to `web-extensions/{folder}/`

3. **`compareVersions()`**
   - Semantic version comparison (splits on dots)
   - Returns -1 (older), 0 (equal), 1 (newer)

**Extension Storage Locations**:
- **macOS**: `~/Library/Application Support/Claude WebExtension Launcher/web-extensions/`
- **Windows**: `{executable-dir}/web-extensions/`

**Adding New Extensions**:
To add a new extension, update the `extensions` slice in `extensions.go`:
```go
{Owner: "username", Repo: "repo-name", Folder: "local-folder-name"}
```

**Location**: `/extensions/extensions.go:1`

---

### selfupdate/selfupdate.go (Self-Update - ~420 lines)

**Purpose**: Enables the launcher to auto-update from GitHub releases

**Key Functions**:

1. **`FinishUpdateIfNeeded()`** - Completes pending updates
   - Windows: Replaces `.exe` with `.new.exe` if exists
   - macOS: Handles bundle replacement via shell script
   - Retries deletion with delays if previous process still running

2. **`CheckAndUpdate()`** - Main update check
   - Queries: `https://api.github.com/repos/lugia19/claude-webext-patcher/releases/latest`
   - Finds platform-specific asset (e.g., `-windows.zip`, `-macos-arm64.zip`)
   - Downloads to `update-temp.zip`
   - Extracts and stages update

**Update Flow**:
```
Launcher starts
  ↓
FinishUpdateIfNeeded() - Complete any staged updates
  ↓
CheckAndUpdate() - Check GitHub for new version
  ↓
If newer version available:
  - Download platform-specific ZIP
  - Extract to update-temp/
  - macOS: Replace .app bundle, relaunch via osascript
  - Windows: Stage as .new.exe, copy helpers, relaunch
  - Old launcher exits
  ↓
New launcher detects staged update
  - Completes replacement
  - Runs normal Claude launch
```

**Platform-Specific Installation**:
- **macOS**: Uses shell script to atomically replace app bundle
- **Windows**: Stages as `.new.exe`, copies batch helpers, retries with delays

**Location**: `/selfupdate/selfupdate.go:1`

---

### utils/paths.go (Path Resolution - 34 lines)

**Purpose**: Provides platform-specific path resolution

**Key Function**:
- `ResolvePath(relativePath)` - Returns platform-appropriate absolute path

**Path Strategy**:
- **macOS**: `~/Library/Application Support/Claude WebExtension Launcher/`
  - Allows app to move independently of data
- **Windows**: Executable directory
- **Other**: Executable directory

**Usage**:
All modules use this for finding:
- Claude installation (`app-latest/`)
- Web extensions (`web-extensions/`)
- Update temp files

**Location**: `/utils/paths.go:1`

---

## Resource Files

### Injection JavaScript Files (Version 0.12.55)

**extension_loader.js** (~54 lines)
- Finds `web-extensions/` directory by walking up from app path
- Loads extensions using `session.defaultSession.extensions.loadExtension()`
- Reloads page on first extension load
- Provides logging hook for extension console messages

**alarm_polyfill.js** (~87 lines)
- Implements `chrome.alarms` API polyfill
- Uses Node.js `setInterval`/`setTimeout`
- Supports `create`, `clear`, `get` operations
- Maps `periodInMinutes`, `when`, `delayInMinutes` to timers

**notification_polyfill.js** (~34 lines)
- Implements system notifications using Electron's `Notification` API
- Converts extension notification calls to system notifications

**tabevents_polyfill.js** (~33 lines)
- Simulates tab events for Chrome tab API
- Maps window events to custom events:
  - `focus` → `electronTabActivated`
  - `blur` → `electronTabDeactivated`
  - `minimize` → `electronTabRemoved`
  - `restore` → `electronTabActivated`

**Location**: `/resources/injections/0.12.55/`

---

## Build Process

### build-all.sh (Bash - ~204 lines)

Builds all three platform targets from macOS/Linux:

**1. macOS ARM64 (Apple Silicon)**
```bash
GOOS=darwin GOARCH=arm64 go build -o Claude_WebExtension_Launcher-mac-arm64
```
- Creates `.app` bundle structure
- Generates `Info.plist` with ARM64 architecture priority
- Signs with ad-hoc signature
- Creates distribution ZIP

**2. macOS Intel (AMD64)**
```bash
GOOS=darwin GOARCH=amd64 go build -o Claude_WebExtension_Launcher-mac-amd64
```
- Creates `.app` bundle with Intel architecture priority
- Same bundling and signing process

**3. Windows (AMD64)**
```bash
GOOS=windows GOARCH=amd64 go build -o Claude_WebExtension_Launcher.exe
```
- Creates ZIP containing:
  - `Claude_WebExtension_Launcher.exe`
  - `Toggle-Startup.bat`
  - `Toggle-StartMenu.bat`

**Output**: `builds/` directory with version-tagged ZIPs

**macOS App Bundle Structure**:
```
Claude_WebExtension_Launcher.app/
├── Contents/
    ├── Info.plist           # Bundle configuration
    ├── MacOS/
    │   └── Claude_WebExtension_Launcher  # Executable
    └── Resources/
        └── app.icns         # App icon
```

**Location**: `/build-all.sh:1`

---

## Development Workflows

### Making Code Changes

1. **Modify Go source files** as needed
2. **Test locally** before building releases
3. **Update version** in `main.go` constant `Version`
4. **Run build script** appropriate for your platform
5. **Test built binaries** on target platforms
6. **Commit and push** changes

### Adding Support for New Claude Versions

1. **Create new injection directory**: `resources/injections/{version}/`
2. **Copy existing injections** as starting point
3. **Find injection point** in Claude's minified JavaScript
4. **Update pattern matching** in new patch function
5. **Add patch function** (e.g., `patch_0_13_0()`)
6. **Add to supportedVersions map**:
   ```go
   "0.13.0": {
       {
           Files: []string{"path/to/file.js"},
           Func:  patch_0_13_0,
       },
   }
   ```

**Finding Injection Points**:
1. Unpack Claude's `app.asar` manually
2. Beautify JavaScript files with js-beautify
3. Search for application initialization code
4. Find unique pattern near where extension loader should run
5. Test pattern matches only once in the file

### Adding New Extensions

1. **Update extensions slice** in `extensions/extensions.go`:
   ```go
   {Owner: "github-user", Repo: "repo-name", Folder: "local-folder-name"}
   ```
2. **Ensure GitHub release** has "electron" ZIP asset
3. **Test update flow** to verify download works

### Testing Changes

**Quick Local Test**:
```bash
go run .
```

**Build for Current Platform**:
```bash
# macOS ARM64
./build-macos-arm64.sh

# Windows (PowerShell)
.\build-windows.ps1
```

**Build All Platforms** (from macOS/Linux):
```bash
./build-all.sh
```

---

## Platform-Specific Behaviors

### macOS

**Path Resolution**:
- Uses `~/Library/Application Support/Claude WebExtension Launcher/`
- Allows app bundle to move independently

**App Bundle**:
- Creates proper `.app` structure
- Handles symlinks during extraction
- Sets executable permissions (chmod 755)

**Code Signing**:
- Requires ad-hoc signature after patching
- Needs Keychain permission on first launch
- May show network service crash initially (ignore it)

**Info.plist Hash**:
- Stores asar hash in `Info.plist`
- Replaces hash after patching

**Terminal Launching**:
- Detects if running without terminal
- Re-launches in Terminal.app via AppleScript

### Windows

**Path Resolution**:
- Uses executable directory directly

**Archive Format**:
- Claude shipped as `.nupkg` (zip format)
- Extracts only `lib/net45/` directory

**Icon Embedding**:
- Uses `rcedit.exe` to embed icon in executable
- Done at build time, not runtime

**Hash Storage**:
- Hardcoded in executable binary
- Found and replaced as bytes

**Helper Scripts**:
- `Toggle-Startup.bat` - Creates/removes startup shortcut
- `Toggle-StartMenu.bat` - Creates/removes start menu shortcut
- Use PowerShell COM objects for shortcuts

---

## Important Technical Details

### Electron Architecture

**app.asar**:
- Electron's archive format for bundling application code
- Similar to TAR, contains all JavaScript/HTML/CSS
- Integrity-checked on startup via hash

**Patching Process**:
1. Unpack `app.asar` using `asar` npm tool
2. Beautify minified JS (required for pattern matching)
3. Inject extension loader code
4. Repack `app.asar`
5. Update hash in executable/plist
6. Sign app (macOS only)

**Why Beautification is Required**:
- Claude ships with minified JavaScript
- Pattern matching on minified code is unreliable
- Beautification makes code readable and patterns stable

### Hash Verification Mechanism

**The Problem**:
- Electron verifies `app.asar` integrity on startup
- Hash is hardcoded in executable/Info.plist
- Modifying `app.asar` breaks the hash check
- Claude refuses to start with hash mismatch

**The Solution**:
1. Patch `app.asar` with extension code
2. Run Claude to capture hash mismatch error
3. Extract expected hash from error message
4. Replace old hash in executable/plist with new hash
5. Claude now accepts modified `app.asar`

**Implementation**:
- `captureHashMismatch()` - Runs Claude, captures error
- `replaceHashInExe()` - Updates hash in binary/plist

### Extension Loading Mechanism

**How Extensions Work**:

1. **Patching Phase**:
   - Extension loader injected into Electron renderer process
   - Protocol whitelist modified to allow `chrome-extension://` URLs

2. **Runtime Phase**:
   - When Claude loads, `extension_loader.js` runs
   - Searches up directory tree for `web-extensions/` folder
   - Uses Electron's `extensions.loadExtension()` API
   - Loads each folder containing `manifest.json`

3. **Polyfills**:
   - Many Chrome APIs not available in Electron
   - Polyfills provide compatibility layer
   - `chrome.alarms` → Node.js timers
   - `chrome.notifications` → Electron notifications
   - Tab events → Window event simulation

### Update Mechanisms

**Three-Level Update System**:

1. **Launcher Self-Updates** (every run)
   - Monitors GitHub releases
   - Platform-specific ZIPs
   - Atomic replacement strategy

2. **Claude Desktop Updates** (every run)
   - Monitors official Claude releases
   - Only downloads supported versions
   - Applies version-specific patches

3. **Extension Updates** (every run)
   - Monitors GitHub releases per extension
   - Semantic version comparison
   - Downloads "electron" ZIP assets

---

## Common Issues and Solutions

### Issue: Extensions Not Showing Up

**Symptoms**: Extension loaded but not visible in Claude

**Solutions**:
- Restart Claude application
- Check browser console for errors
- Verify `manifest.json` is valid
- Check extension folder in `web-extensions/`

### Issue: Windows Defender Flags as Malware

**Symptoms**: Windows Defender quarantines executable

**Cause**: `rcedit.exe` triggers false positives (common with resource editors)

**Solutions**:
- Add exception in Windows Defender
- This is a known false positive, safe to allow
- Developers cannot fix without code signing certificate ($$$)

### Issue: macOS "Not Verified" Warning

**Symptoms**: macOS refuses to open, shows security warning

**Cause**: App not notarized (requires $99/year Apple Developer subscription)

**Solutions**:
- Go to System Preferences → Privacy & Security
- Click "Open Anyway" button
- This is expected for non-notarized apps

### Issue: First Launch Network Service Crash (macOS)

**Symptoms**: Crash dialog about network service on first launch

**Cause**: Modified app needs Keychain permission for ad-hoc signature

**Solutions**:
- Ignore the crash dialog
- Restart Claude
- Grant Keychain permission if prompted
- Should work normally after first launch

### Issue: Claude Version Not Supported

**Symptoms**: Launcher says Claude version is unsupported

**Cause**: Only version 0.12.55 has patches defined

**Solutions**:
- Wait for maintainer to add support for new version
- Or contribute patches for new version (see "Adding Support for New Claude Versions")

---

## Dependencies

### Build Dependencies

- **Go 1.21+** - Required for building
- **Node.js** - Required at runtime for `asar` and `js-beautify` tools
  - `npm install -g @electron/asar` - Electron archive tool
  - `npm install -g js-beautify` - JavaScript beautifier

### Runtime Dependencies

- **Node.js** - Must be in PATH for patching
- **asar** - Installed globally via npm
- **js-beautify** - Installed globally via npm

**Platform-Specific**:
- **macOS**: `codesign` (included in Xcode Command Line Tools)
- **Windows**: `rcedit.exe` (embedded in resources)

### Go Module Dependencies

**None!** - Uses only Go standard library:
- `archive/zip` - ZIP file handling
- `embed` - Embedded resources
- `encoding/json` - JSON parsing
- `net/http` - HTTP requests
- `os/exec` - Process execution
- `path/filepath` - Path manipulation

---

## Testing Guidelines

### Before Committing

1. **Test on target platform** if possible
2. **Verify builds succeed** with build scripts
3. **Check version number** is updated in `main.go`
4. **Test update mechanism** with previous version
5. **Verify extensions load** in modified Claude

### Manual Testing Checklist

- [ ] Launcher starts without errors
- [ ] Claude downloads and patches successfully
- [ ] Extensions update correctly
- [ ] Modified Claude launches
- [ ] Extensions visible and functional
- [ ] Cache clearing works
- [ ] Self-update mechanism works
- [ ] Platform-specific features work (startup helpers, etc.)

### Automated Testing

Currently, this project has **no automated tests**. All testing is manual.

**Potential Test Areas**:
- Version comparison logic
- Path resolution on different platforms
- Extension detection and loading
- Update checking and downloading

---

## Code Style and Conventions

### Go Code Style

- Follow standard Go formatting (`gofmt`)
- Use descriptive variable names
- Add comments for exported functions
- Keep functions focused and small when possible
- Error handling: log and continue when possible, exit on critical errors

### File Organization

- One package per directory
- Package name matches directory name
- Keep related functionality together
- Use `utils/` for shared utility functions

### Naming Conventions

**Variables**:
- `camelCase` for local variables
- `PascalCase` for exported variables/functions
- Descriptive names (avoid single letters except loop counters)

**Constants**:
- `PascalCase` for exported constants
- `camelCase` for internal constants
- ALL_CAPS for environment-related constants

**Functions**:
- `PascalCase` for exported functions
- `camelCase` for private functions
- Verb-based names (e.g., `EnsurePatched`, `UpdateAll`)

### Error Handling

```go
// Pattern: Return errors up, handle at appropriate level
if err := someFunction(); err != nil {
    return fmt.Errorf("context: %v", err)
}

// Pattern: Log and continue for non-critical errors
if err := updateExtensions(); err != nil {
    fmt.Printf("Warning: %v\n", err)
    // Continue execution
}

// Pattern: Exit for critical errors
if err := patcher.EnsurePatched(); err != nil {
    fmt.Printf("Error: %v\n", err)
    os.Exit(1)
}
```

---

## Security Considerations

### What This Tool Does

- Downloads official Claude Desktop from Google Cloud Storage
- Modifies local Electron application to enable extensions
- Does NOT transmit any data externally
- Does NOT modify Claude's network communication

### Privacy

- Launcher only modifies local installation
- No telemetry or analytics
- Individual extensions may have their own privacy policies
- Check extension source code before installing

### Code Signing

- macOS: Ad-hoc signature (not trusted by Apple)
- Windows: No code signing (triggers Defender warnings)
- Users must explicitly trust the application

### Extension Security

- Extensions run with full Electron privileges
- Only install extensions from trusted sources
- Review extension code before installing
- Extensions have access to Claude's data and API

---

## Contributing Guidelines

### Before Making Changes

1. **Understand the architecture** - Read this document thoroughly
2. **Check existing issues** - See if your idea is already discussed
3. **Test locally first** - Verify changes work on your platform
4. **Consider all platforms** - macOS and Windows behave differently

### Pull Request Checklist

- [ ] Code follows existing style conventions
- [ ] Version number updated in `main.go` if needed
- [ ] Tested on at least one platform
- [ ] Comments added for complex logic
- [ ] No hardcoded paths (use `utils.ResolvePath()`)
- [ ] Error handling is appropriate
- [ ] Platform-specific code is properly guarded

### Adding New Features

1. **Discuss first** - Open an issue to discuss approach
2. **Keep it modular** - Add new packages if needed
3. **Maintain compatibility** - Don't break existing functionality
4. **Document thoroughly** - Update this CLAUDE.md file
5. **Test extensively** - Test on all supported platforms if possible

---

## Key Files Reference

| File | Purpose | Lines | Key Functions |
|------|---------|-------|---------------|
| `main.go` | Entry point, orchestration | 121 | `main()` |
| `patcher/patcher.go` | Claude download & patching | ~800 | `EnsurePatched()`, `applyPatches()`, `patch_0_12_55()` |
| `extensions/extensions.go` | Extension management | ~180 | `UpdateAll()`, `compareVersions()` |
| `selfupdate/selfupdate.go` | Launcher self-update | ~420 | `CheckAndUpdate()`, `FinishUpdateIfNeeded()` |
| `utils/paths.go` | Path resolution | 34 | `ResolvePath()` |
| `embedded.go` | Resource embedding | 8 | (embed directive) |

---

## Useful Commands

### Development

```bash
# Run locally without building
go run .

# Build for current platform
go build -o launcher

# Format all Go code
go fmt ./...

# Check for errors
go vet ./...
```

### Building

```bash
# Build all platforms (macOS/Linux)
./build-all.sh

# Build macOS ARM64 only
./build-macos-arm64.sh

# Build Windows (PowerShell)
.\build-windows.ps1
```

### Debugging

```bash
# Run Claude in terminal to see debug output
# Edit main.go: const launchClaudeInTerminal = true
go run .
```

### Manual Testing

```bash
# Check launcher version
./Claude_WebExtension_Launcher.app/Contents/MacOS/Claude_WebExtension_Launcher

# Check Claude installation
ls ~/Library/Application\ Support/Claude\ WebExtension\ Launcher/app-latest/

# Check extensions
ls ~/Library/Application\ Support/Claude\ WebExtension\ Launcher/web-extensions/
```

---

## External Resources

### Project Links

- **GitHub Repository**: https://github.com/lugia19/claude-webext-patcher
- **Releases**: https://github.com/lugia19/claude-webext-patcher/releases
- **Issues**: https://github.com/lugia19/claude-webext-patcher/issues

### Related Projects

- **Claude Usage Extension**: https://github.com/lugia19/Claude-Usage-Extension
- **Claude Toolbox**: https://github.com/lugia19/Claude-Toolbox

### Technical Documentation

- **Electron Documentation**: https://www.electronjs.org/docs/latest/
- **Chrome Extension API**: https://developer.chrome.com/docs/extensions/reference/
- **Go Embed Documentation**: https://pkg.go.dev/embed
- **asar Format**: https://github.com/electron/asar

---

## Quick Start for AI Assistants

When working with this codebase:

1. **Understand the flow**: `main.go` → `selfupdate` → `patcher` → `extensions` → launch Claude
2. **Platform matters**: Always check `runtime.GOOS` for platform-specific code
3. **Paths are abstracted**: Use `utils.ResolvePath()`, never hardcode paths
4. **Version-specific patches**: Each Claude version needs custom patches
5. **Testing is manual**: No automated tests exist, verify changes manually
6. **Hash integrity is critical**: Don't skip hash replacement steps
7. **Node.js is required**: Runtime dependency for asar/js-beautify

### Common Tasks

**Add support for new Claude version**:
1. Create `resources/injections/{version}/` directory
2. Add patch function in `patcher/patcher.go`
3. Add to `supportedVersions` map
4. Test extensively

**Add new extension**:
1. Update `extensions` slice in `extensions/extensions.go`
2. Ensure GitHub release has "electron" ZIP
3. Test update flow

**Fix build issues**:
1. Check Go version (need 1.21+)
2. Verify Node.js in PATH
3. Check platform-specific tools (codesign, rcedit)

**Debug patching**:
1. Set `launchClaudeInTerminal = true` in `main.go`
2. Run with `go run .`
3. Check terminal output for errors

---

## Summary

This launcher is a sophisticated tool that modifies Claude Desktop at the binary level to enable web extension support. It handles:

- Multi-platform builds and deployments
- Automatic updates for launcher, Claude, and extensions
- Electron app.asar patching with integrity verification
- Platform-specific code signing and icon embedding
- Extension loading and Chrome API polyfills

The codebase is well-structured with clear separation of concerns, minimal dependencies, and platform-aware implementations. Understanding the hash verification mechanism and Electron architecture is crucial for making modifications.

When in doubt, test thoroughly and remember: this tool modifies a third-party application at the binary level, so precision and care are essential.
