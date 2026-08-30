# Claude Desktop WebExtension Installer

A custom installer for Claude Desktop that includes built-in extensions (and the ability to install your own).

**Note**: The extensions in question are _Web_ extensions! Not to be confused with Local MCPs, which the client also calls Extensions and come in the dxt format.

**Disclaimer**: This is an unofficial, third-party modification of Claude Desktop that enables extension support. By using this installer, you acknowledge that:
- You are doing so at your own risk and discretion
- This project is neither affiliated with nor endorsed by Anthropic
- You are responsible for ensuring your use complies with all applicable terms and agreements

## Multi-instance (new)

You can now run multiple desktop client instances by just launching the client with --instance [name], eg --instance work.
Useful if you want to have multiple windows open, or if you have multiple accounts.

## Known limitations

### Multi-instance login requires using a code
When using `--instance` to run additional instances, those instances are not registered to handle Claude's magic links. When logging in to a non-default instance, use the **"Use a login code instead"** option to log in with a one-time code.

### Windows requires admin perms
This is to make Cowork function. The app will block cowork if the application is not inside of C:\Program Files\WindowsApps, which requires admin permissions to be written to and read from.

### Cowork does not work on MacOS (Corrupt install)
This is because the app is signed, and cowork checks for the signature.
On windows, this is circumvented by not modifying the exe and instead using a .dll, but that cannot be done on MacOS.


I would recommend keeping a separate, unmodified install for it.

## Overview

This installer generates a modified version of the Claude Desktop client with extension support enabled. It creates a standalone installation that can coexist with the official Claude Desktop client, automatically keeping both the client and extensions up to date.

## Known Issues

### Extension not showing up

This can happen due to reasons I'm not really sure of. Restarting the application is enough.

### Windows defender flags it as malware

Yep, Waca- etc are a pretty common false positive. Pyinstaller-built exes used to also trigger it. There isn't really anything I can do about that.
<details>
<summary>How does patching work on windows?</summary>
Basically, it uses the fact that exes will load DLLs that are next to the exe first, to load a modified version.dll.
Its source code is inside the ClaudeDLL folder, but it basically just tells the exe that whatever hash app.asar has, it's the right one.
It's a way to circumvent the asar integrity check without modifying the exe itself, which is what I used to do (and it broke cowork).
</details>

### Refuses to open on MacOS (Insecure/Not Verified)
You might need to go to Settings -> Privacy and Security and click "Open anyway".

MacOS REALLY doesn't like apps that aren't notarized (aka, that haven't paid the 99$ apple tax).
Not much I can do. I can't afford the subscription, and even if I could, this wouldn't be allowed on the app store.

### First Launch Network Service Crash (macOS only)
On first launch, you might see a crash dialog about the network service. This is (likely) because the modified app needs Keychain permission to be granted, given that it uses an ad-hoc signature. Just ignore it.

### Debug flag

If anything else happens or goes wrong, execute the launcher with the --debug flag to be able to see the full logs.

## Installation

### Supported Platforms
- **macOS** - Intel and Apple Silicon
- **Windows** - Windows 10/11 (x64 and ARM64)
- **Linux** - x86_64 (amd64) and arm64

> On Windows there's a single (x64) download — no need to pick an architecture. On an ARM64 PC
> it detects the host and installs the native arm64 Claude automatically (Windows 11 on ARM).
> On Linux the launcher downloads the official Anthropic `.deb` from the APT repository and
> patches a standalone copy, so it runs entirely alongside the official install.

### Quick Start
Download the latest installer from [Releases](https://github.com/lugia19/Claude-WebExtension-Launcher/releases) and run it. The installer will handle everything automatically.

### Linux

The Linux build follows the same **standalone** model as the other platforms. On first run it fetches
the official `claude-desktop_*.deb` from Anthropic's APT repository, extracts a private copy into an
`app-latest/` folder, and patches *that copy* (repacks `app.asar` to load extensions and whitelists
the `chrome-extension://` protocol). It does **not** install into your system and does **not** touch
your system Claude install.

> **Terminology.** The "official Claude" is the one installed via your distro (e.g.
> `/usr/lib/claude-desktop/` and its `~/.config/Claude/` user-data) — it is left completely alone.
> What this launcher runs is a separate, patched copy in the launcher's own directory; that patched
> copy is the one that supports WebExtensions. See the "Linux installation" and "WebExtension usage"
> sections below.

#### Linux installation

The launcher is distributed as a single `Claude_WebExtension_Launcher` binary and needs
**no root privileges** and **no system package installation**. The recommended way to get it is the
release ZIP.

##### Release ZIP (recommended)

1. Download the latest release for your architecture from
   [Releases](https://github.com/lugia19/Claude-WebExtension-Launcher/releases):
   - `Claude_WebExtension_Launcher-<VERSION>-linux-amd64.zip` for x86_64 / AMD64
   - `Claude_WebExtension_Launcher-<VERSION>-linux-arm64.zip` for ARM64
2. Unzip it into the folder where you want the standalone install to live (it keeps everything in its
   own folder, so it can be fully removed later by deleting that folder).

```sh
unzip Claude_WebExtension_Launcher-<VERSION>-linux-amd64.zip
chmod +x Claude_WebExtension_Launcher
./Claude_WebExtension_Launcher
```

On first run the launcher:

1. downloads the official Anthropic Claude `.deb` from the APT repository,
2. verifies its SHA-256 against Anthropic's APT Packages metadata,
3. extracts a private copy into an `app-latest/` folder alongside the launcher,
4. patches **only that standalone copy** (repacks `app.asar` to load WebExtensions and whitelists the
   `chrome-extension://` protocol),
5. installs/updates the WebExtensions into `web-extensions/`,
6. generates a desktop entry pointing at the launcher,
7. launches the patched Claude.

The launcher **does not** require root and **does not** modify your existing system Claude
installation.

> **Terminology.** The "official Claude" is the one installed via your distro (e.g.
> `/usr/lib/claude-desktop/` and its `~/.config/Claude/` user-data) — it is left completely alone.
> What this launcher runs is a separate, patched copy it manages on its own:

```text
/path/to/launcher/
├── Claude_WebExtension_Launcher
├── app-latest/                 # patched standalone Claude (managed)
└── web-extensions/             # WebExtensions loaded into the patched copy
```

> **In short:** the launcher creates and manages its own standalone Claude installation. It does
> **not** patch or replace the user's existing official Claude Desktop installation.

##### How it fits together

A quick summary of the standalone flow:

```text
Release ZIP  →  Launcher  →  Official Anthropic .deb
                                      ↓
                           SHA-256 verification
                                      ↓
                           Private app-latest/  →  ASAR patch  →  web-extensions/
                                      ↓
                                    Standalone Claude
```

##### Security

- The `.deb` SHA-256 is verified **before** anything is extracted. The expected hash comes from
  Anthropic's APT Packages metadata for the selected version.
- A failed hash check aborts the installation and removes the invalid download.
- The launcher does **not** automatically use `sudo`, and it does **not** modify
  `/usr/lib/claude-desktop` or any other system path.
- The desktop entry is created in the user's XDG application directory
  (`~/.local/share/applications/` or `$XDG_DATA_HOME/applications/`).
- The sandbox diagnostic only reports configuration problems and does not automatically change
  permissions.

##### Building from source

Normal users should use the **release ZIP** above. Building from source is only for contributors and
developers, and requires the [Go toolchain](https://go.dev/dl/):

```sh
GOOS=linux GOARCH=amd64 go build -o Claude_WebExtension_Launcher .     # x86_64
GOOS=linux GOARCH=arm64 go build -o Claude_WebExtension_Launcher .     # ARM64
```

Place the resulting binary in the folder where you want the standalone install to live.

##### Desktop integration

On first run the launcher automatically creates a desktop entry at

```text
~/.local/share/applications/claude-webext-launcher.desktop
```

(or the equivalent under `$XDG_DATA_HOME/applications/`). The generated entry points directly at the
**actual launcher executable path**, so you do not need to edit any `/opt/...` paths manually.

A manual template also ships at `resources/claude-webext-launcher.desktop`; if you prefer to install
it by hand, replace its `Exec` line with your actual binary path, then:

```sh
cp <path>/claude-webext-launcher.desktop ~/.local/share/applications/
```

##### APT / Debian / Ubuntu

The sections below cover installing the **official Claude** package system-wide. The launcher itself
never requires this — if you only want the standalone launcher, use the release ZIP and skip these
sections.

Anthropic publishes an official APT repository:
`https://downloads.claude.ai/claude-desktop/apt/stable` (suite `stable`, component `main`,
architectures `amd64` and `arm64`, package name `claude-desktop`).

```sh
sources_line='deb [trusted=yes] https://downloads.claude.ai/claude-desktop/apt/stable stable main'
echo "$sources_line" | sudo tee /etc/apt/sources.list.d/claude-desktop.list
sudo apt update
sudo apt install claude-desktop
```

> **⚠ Security caveat for the manual APT method.** This method installs the **official Claude**
> package system-wide via APT; the launcher itself does **not** need it. Anthropic signs the
> repository's `InRelease` file, but no public signing-key URL is published, so an ordinary
> (key-verified) `sources.list` entry cannot be configured — the `[trusted=yes]` above disables normal
> repository signature verification for that source entry. If you prefer a verified install, use the
> **manual `.deb`** method below, which packages its own key/payload. If you do not need a system-wide
> Claude installation, you can simply use the launcher release ZIP and skip this whole section.

##### Manual `.deb` (Debian/Ubuntu-based distros)

Anthropic distributes the official Linux package as a `.deb`. The simplest verified way to install it
system-wide on Debian/Ubuntu-based distros is to download the `.deb` and let `apt` handle it:

```sh
ver=1.40609.0                                            # check for the latest version
ach=amd64                                                # or arm64
wget "https://downloads.claude.ai/claude-desktop/apt/stable/pool/main/c/claude-desktop/claude-desktop_${ver}_${ach}.deb"
sudo apt install ./claude-desktop_${ver}_${ach}.deb
```

##### Arch-based distros (Arch, CachyOS, Manjaro, …)

Anthropic has **no official package for Arch-based distros**; its official Linux distribution channel
is the `.deb`/APT repository, and a `.deb` is **not** a native Arch package. If you specifically want
the official system package, unpack the `.deb` manually instead of trying to install it with pacman:

```sh
mkdir -p ~/claude-deb && cd ~/claude-deb && ar x claude-desktop_*.deb
tar -xJf data.tar.xz
./usr/lib/claude-desktop/claude-desktop --no-sandbox
```

Again, the launcher itself **does not require** the official Claude to be installed system-wide — it
downloads and extracts its own private standalone copy automatically.

##### Verifying the install

After the launcher has built its standalone copy, you can confirm nothing about the official install
changed and the standalone is genuine:

```sh
# Standalone executable is byte-for-byte the official binary:
sha256sum app-latest/claude-desktop /usr/lib/claude-desktop/claude-desktop
# app-latest should differ (it is repacked to enable extensions); the backup is the pristine source:
sha256sum app-latest/resources/app.asar        # repacked (differs from official)
sha256sum app-latest/resources/app.asar.backup # byte-identical to the pristine .deb asar
```

#### WebExtension usage (Linux)

"WebExtension" here means **browser-style Chrome/Electron extensions** — an unpacked folder that
contains a `manifest.json`. This is **not** the same as MCP or the Claude DevTools (DXT) extension
system, which are configured elsewhere and are not loaded by this launcher.

**Where extensions live:** a `web-extensions/` directory next to the launcher binary. Any subfolder
that contains a `manifest.json` is automatically discovered and loaded when the patched Claude starts:

```text
web-extensions/
├── usage-tracker/          # Usage Tracker / Token Counter
│   └── manifest.json
├── userscript-toolbox/     # Claude QoL
│   └── manifest.json
└── my-extension/           # any unpacked extension you add
    └── manifest.json
```

**Adding / removing:** drop an unpacked extension folder into `web-extensions/`, or delete a folder to
remove that extension. Because extensions are not hot-reloaded, **restart the patched Claude** after
adding or removing any extension. On first run the launcher seeds the bundled extensions
(`usage-tracker`, `userscript-toolbox`, and an internal `sentinel`) and updates them to their latest
releases.

**Usage Tracker / Token Counter:** the bundled `usage-tracker` adds the token/usage counter UI to the
patched Claude session. It requires you to be **logged into Claude** before usage data can be
displayed.

> **"Claude" vs "official Claude" here.** Log in inside the patched standalone copy as usual. It uses
> its own per-instance user-data (`~/.config/Claude-modified`, or `Claude-<instance>` for named
> instances), so it does not share a profile with the official app by default.

**Troubleshooting extension loading:**
```sh
# Launch the patched copy attached to a terminal for verbose logs:
app-latest/claude-desktop --instance=modified
# or launch through the launcher in debug mode:
ELECTRON_ENABLE_LOGGING=1 ./Claude_WebExtension_Launcher --debug
```
Extension messages appear in the console under `ELECTRON_ENABLE_LOGGING=1` only. A healthy load shows
lines such as `[UsageTracker] Done initializing.`, `[UsageTracker] UsageUI: Ready`, and
`EXT_LOG:SENTINEL_EXT_LOADED`. If these are absent, check that `web-extensions/` sits beside the
launcher and that the extension has a `manifest.json`.

##### Multiple instances

You can run several patched copies side by side with `--instance`:

```bash
app-latest/claude-desktop --instance=<name>
```

Each named instance uses its own isolated Claude configuration (e.g. `~/.config/Claude-<name>`), so
you can keep multiple accounts or windows separate. For normal day-to-day use, just run the launcher —
it uses `--instance=modified` by default, so no extra flags are needed.

##### Sandbox note

On Linux, Electron's SUID sandbox can require a setuid-root `chrome-sandbox` helper. The launcher
only **diagnoses** this at startup and prints a warning if it looks misconfigured — it never runs
`sudo` or changes permissions automatically. Most modern systems can use the unprivileged
user-namespace sandbox instead, so manual SUID configuration is only needed if your environment
actually requires it.

#### Linux known limitations
- ASAR integrity validation is only enforced by Electron on macOS/Windows, so a repacked `app.asar`
  needs no hash update on Linux (this is why the Linux path is simpler).
- The extensions execute inside the claude.ai page; the Usage Tracker's on-page token-count display
  appears once you log in (it deliberately waits on the login screen).
- The Linux build is beta: test that your specific extensions work before relying on them.

## Features

The installer provides:

- **Latest Claude Desktop** - Automatically downloads and updates to the most recent supported version
- **Custom Icon** - Distinguishes your extended installation from the standard client
- **Extension Support** - Unpacks resources and enables extension loading capabilities. You can add your own unpacked extensions in the extensions folder. NOTE: Most extensions will need to be adapted to work.
- **Pre-installed Extensions** - Includes [Usage Tracker](https://github.com/lugia19/Claude-Usage-Extension/) and [Claude QoL](https://github.com/lugia19/Claude-QoL)
- **Automatic Updates** - Keeps both the client and extensions current
- **Standalone Installation** - Runs independently alongside the official Claude Desktop

## How It Works

1. Downloads the latest compatible Claude Desktop client, creating a separate install
2. Modifies the application to enable extension loading
3. Applies a custom icon for easy identification
4. Installs the default extensions

## Privacy

The installer only creates a local modified Claude Desktop installation. No data is collected or transmitted by the installer itself. Individual extensions may have their own privacy policies.

## Troubleshooting

If you encounter issues:
- Ensure you have the latest version of the installer
- Check that your system meets the platform requirements
- The extended installation can be completely removed by deleting the installation folder
