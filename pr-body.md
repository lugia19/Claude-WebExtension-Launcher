## Summary

Adds Linux support per #25: detect the system-installed Claude Desktop and patch it in place, rather than maintaining a separate downloaded copy. Anthropic doesn't publish a Linux download artifact the existing `buildAndSwap` flow could use, so this reuses the existing shared patch primitives (`applyPatches` / `installWrapper` / `patchProtocolArray`) against the system install instead.

## Scope

Keeping it minimal per the discussion on #25, removed `--doctor`, `--install-desktop`, `add`/`remove`/`list`/`enable`/`disable` subcommands, CRX/Chrome Web Store handling, GoReleaser/AUR packaging. Just the minimum changes needed to make the existing tool work on Linux

## Files Modified/Added:

- `main.go`
- `main_other.go`
- `main_windows.go`
- `patcher/patcher.go`
- `patcher/patcher_other.go`
- `patcher/patcher_windows.go`
- `utils/paths_other.go`
- `resources/injections/generic/wrapper.js`
- `patcher/linux_test.go` (new tests)

## How it works

1. Detects the system install at the well-known package paths (`/usr/lib/claude-desktop`, `/opt/claude-desktop`), this is path-based, not distro-detection tho, and it matches where the official .deb/.rpm/.pkg packages install on Arch, Debian/Ubuntu, and Fedora alike.
2. Stages `app.asar` and runs it through the existing shared patch pipeline
3. Writes the patched asar back via `sudo` (tries a direct copy first, falls back to `sudo cp`, graceful skip-and-launch warning if sudo isn't available non-interactively)
4. A hash stamp (of the patched asar we wrote, not just any asar we've seen) triggers a re-patch on next launch if a system update (apt / pacman / dnf) overwrote it
5. The wrapper now checks `CLAUDE_WEBEXT_DIR` (set by the launcher on Linux) before falling back to its existing walk-up-from-app-path search, since the system install isn't next to the launcher's data directory the way the macOS managed does it
6. Linux protocol-array patch: the Linux Vite build quotes the scheme set with backticks (`` [`devtools:`,`file:`,`app:`] ``), so the existing double-quote-only matcher is augmented to also handle the backtick form

## Testing

- `gofmt -l .` clean
- `go vet ./...` clean
- `go test ./...` passes, incl. new `patcher/linux_test.go`:
  - `TestDetectLinuxInstall` / `TestDetectLinuxInstallMissing`, install detection against a fake FS
  - `TestSystemAsarHashMatches`, hash-stamp comparison
  - `TestPatchProtocolArrayDoubleQuoted` / `TestPatchProtocolArrayBacktickQuoted` / `TestPatchProtocolArrayAlreadyPresent` / `TestPatchProtocolArrayNoMatch`, protocol allow-list patch against both quote forms and the no-match case
- Cross-compiled clean (x5): linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- Live end-to-end against a real install (Claude Desktop 1.40609.0 / Electron 42.10.0, Arch Linux, `/usr/lib/claude-desktop`):
  - asar patched in place via the launcher's own code path
  - wrapper `package.json` main redirect + `chrome-extension:` protocol allow-list verified in the repacked asar (confirmed in `index.chunk-Ci77nY41.js` as `` [`devtools:`,`file:`,`app:`,`chrome-extension:`] ``)
  - all three extensions (usage-tracker, userscript-toolbox, sentinel) loaded and confirmed running via console output (`[UsageTracker] UsageUI: Ready`, `[QOL-SearchInterceptor] Installed`, `EXT_LOG:SENTINEL_EXT_LOADED`), captured through the launcher's final code path, not a hand-set env var
  - hash-stamp correctly skips re-patch on the next launch ("System asar already patched")
  - graceful fallback when `sudo` isn't available non-interactively



## Notes

Happy to be the ongoing Linux maintainer if you find this useful, I made this deliberately stripped-down Linux support layer as we talked about, hope this helps :]
