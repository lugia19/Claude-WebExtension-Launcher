"use strict";

const { app, session, Notification } = require("electron");
const path = require("path");
const fs = require("fs");

// ================================================================
// Instance isolation — redirect userData before anything reads it
// ================================================================
// Parse instance name supporting both `--instance=name` and `--instance name`.
// Without one it's the main instance, "Main". That used to be "modified": the launcher
// renames its data folder, but until it has, keep using the old one (e.g. Claude
// started from a taskbar pin before the launcher's first run since the rename).
let instanceName = "Main";
if (!fs.existsSync(path.join(app.getPath("appData"), app.getName() + "-Main")) &&
    fs.existsSync(path.join(app.getPath("appData"), app.getName() + "-modified"))) {
    instanceName = "modified";
}
const eqArg = process.argv.find(a => a.startsWith("--instance="));
const spaceIdx = process.argv.indexOf("--instance");

if (eqArg) {
    // Internal launcher format: --instance=name
    instanceName = eqArg.split("=")[1];
} else if (spaceIdx !== -1 && spaceIdx + 1 < process.argv.length) {
    // User/shortcut format: --instance name
    // Guard against accidentally grabbing a trailing flag if the value is missing
    const nextArg = process.argv[spaceIdx + 1];
    if (!nextArg.startsWith("--")) {
        instanceName = nextArg;
    }
}
app.setPath("userData", path.join(
    app.getPath("appData"),
    app.getName() + "-" + instanceName
));

// ================================================================
// Multi-instance lock — monkey-patch before the app code calls it
// ================================================================
const _originalRequestLock = app.requestSingleInstanceLock.bind(app);
app.requestSingleInstanceLock = function(...args) {
    const originalName = app.getName();
    app.setName(originalName + "-" + instanceName);
    const result = _originalRequestLock(...args);
    app.setName(originalName);
    return result;
};

// ================================================================
// Remote debugging and developer mode — set per instance in the launcher
// ================================================================
// Claude refuses to start when a debugging switch is on the command line, unless its
// E2E token check passes. The launcher adds a marker flag next to the switches it adds,
// so only a launch from the launcher gets through; the same switches without it are
// still refused, as in the official app.
// - --webext-dev-mode (advanced debug mode): the patched token checks (patchDevModeGate
//   in patcher.go) pass, which also lets the debugging switches through and turns on
//   Claude's test features. version.dll turns on the --inspect fuse for it too.
// - --webext-remote-debugging: the debugging switches are hidden from Claude's check.
//   Chromium has already read them from the real command line, so the port still opens.
if (process.argv.includes("--webext-dev-mode")) {
    globalThis.__webextDevMode = true;
    console.log("[webext] Advanced debug mode on");
    // SSLKEYLOGFILE only covers Chromium's networking. Node's own TLS in this process
    // (https, fetch, WebSockets; all through tls.connect) gets its session keys
    // appended to the same file, from each socket's keylog event: Electron ignores
    // Node's --tls-keylog in a packaged app.
    const keyLog = process.env.SSLKEYLOGFILE;
    if (keyLog) {
        let fd = null; // opened on first use; written synchronously, so the keys are in
                       // the file before the connection's traffic
        const tls = require("tls");
        const connect = tls.connect;
        tls.connect = function (...args) {
            const socket = connect.apply(this, args);
            socket.on("keylog", line => {
                try {
                    fd ??= fs.openSync(keyLog, "a");
                    fs.writeSync(fd, line);
                } catch {}
            });
            return socket;
        };
        console.log("[webext] Logging Node TLS keys to " + keyLog);
    }
} else if (process.argv.includes("--webext-remote-debugging")) {
    // Normalized like Claude's own check: no leading -, -- or /, lowercase, no =value.
    const switchName = a => a.replace(/^(?:--|-|\/)/, "").toLowerCase().split("=", 1)[0];
    for (let i = process.argv.length - 1; i > 0; i--) {
        const name = switchName(process.argv[i]);
        if (name.startsWith("remote-debugging-port") || name.startsWith("remote-debugging-pipe")) {
            process.argv.splice(i, 1); // in place: other code holds this array
        }
    }
    console.log("[webext] Remote debugging allowed");
}

// ================================================================
// Find the web-extensions directory by walking up from app path
// ================================================================
let extPath = null;
let searchDir = app.getAppPath();
while (searchDir !== path.dirname(searchDir)) {
    searchDir = path.dirname(searchDir);
    const candidate = path.join(searchDir, "web-extensions");
    if (fs.existsSync(candidate)) {
        extPath = candidate;
        break;
    }
}

// ================================================================
// Extension loading — runs as soon as the app is ready
// ================================================================
const SENTINEL_STRING = "SENTINEL_EXT_LOADED";
const SENTINEL_MAX_RELOADS = 2;
const SENTINEL_TIMEOUT_MS = 5000;
let sentinelReloadCount = 0;
let sentinelReceived = false;

app.on("ready", () => {
    session.defaultSession.clearCache();

    if (!extPath) return;

    const extDirs = fs.readdirSync(extPath).filter(f =>
        fs.existsSync(path.join(extPath, f, "manifest.json"))
    );

    if (extDirs.length === 0) return;

    console.log("Loading web extensions...");
    const loadPromises = extDirs.map(f => {
        const p = path.join(extPath, f);
        console.log("Loading extension:", f);
        return session.defaultSession.extensions.loadExtension(p).catch(err => {
            console.error("Failed to load extension:", f, err);
        });
    });

    Promise.all(loadPromises).then(() => {
        const loaded = session.defaultSession.extensions.getAllExtensions().length;
        console.log(`Extensions loaded: ${loaded}/${extDirs.length}`);
        if (loaded < extDirs.length && claudeWebContents) {
            console.log("Not all extensions loaded, reloading page...");
            sentinelReloadCount++;
            claudeWebContents.reloadIgnoringCache();
        }
    });
});

// ================================================================
// Window & webContents capture + polyfill setup
// ================================================================
let mainWindow = null;
let claudeWebContents = null;
let polyfillsReady = false;

function setupPolyfills() {
    if (polyfillsReady || !mainWindow || !claudeWebContents) return;
    polyfillsReady = true;

    const alarms = new Map();

    claudeWebContents.on("console-message", (event) => {
        const message = event.message;
        if (!message) return;

        // Extension logging + sentinel detection
        if (message.startsWith("EXT_LOG:")) {
            console.log(message);
            if (message.includes(SENTINEL_STRING)) {
                sentinelReceived = true;
                console.log("[Sentinel] Content script execution confirmed.");
            }
            return;
        }

        // Alarm polyfill
        if (message.startsWith("CUT_ALARM:")) {
            console.log("[Node] Alarm command received:", message);
            try {
                const data = JSON.parse(message.substring("CUT_ALARM:".length));
                if (data.action === "create") {
                    const existing = alarms.get(data.name);
                    if (existing) {
                        const sameParams =
                            existing.params.periodInMinutes === data.periodInMinutes &&
                            existing.params.when === data.when &&
                            existing.params.delayInMinutes === data.delayInMinutes;
                        if (sameParams) {
                            console.log(`[Node] Alarm '${data.name}' already exists with same params, skipping`);
                            return;
                        }
                        clearTimeout(existing.timerId);
                    }

                    let timerId;
                    if (data.periodInMinutes) {
                        timerId = setInterval(() => fireAlarm(data.name), data.periodInMinutes * 60 * 1000);
                    } else if (data.when) {
                        const delay = data.when - Date.now();
                        if (delay > 0) {
                            timerId = setTimeout(() => { fireAlarm(data.name); alarms.delete(data.name); }, delay);
                        }
                    } else if (data.delayInMinutes) {
                        timerId = setTimeout(() => { fireAlarm(data.name); alarms.delete(data.name); }, data.delayInMinutes * 60 * 1000);
                    }

                    if (timerId) {
                        alarms.set(data.name, {
                            timerId,
                            params: {
                                periodInMinutes: data.periodInMinutes,
                                when: data.when,
                                delayInMinutes: data.delayInMinutes
                            }
                        });
                    }
                } else if (data.action === "clear") {
                    const entry = alarms.get(data.name);
                    if (entry) {
                        clearTimeout(entry.timerId);
                        alarms.delete(data.name);
                    }
                }
            } catch (err) {
                console.error("Alarm error:", err);
            }
            return;
        }

        // Notification polyfill
        if (message.startsWith("CUT_NOTIFICATION:")) {
            console.log("[Node] Notification command received:", message);
            try {
                const content = message.substring("CUT_NOTIFICATION:".length);
                let options;
                try {
                    options = JSON.parse(content);
                } catch (e) {
                    options = { title: "Claude Usage Tracker", message: content.trim() };
                }
                const iconPath = path.join(path.dirname(app.getAppPath()), "Tray-Win32.ico");
                const notification = new Notification({
                    title: options.title,
                    body: options.message || options.body,
                    icon: iconPath
                });
                notification.show();
            } catch (error) {
                console.error("[Node] Failed to create notification:", error);
            }
            return;
        }
    });

    function fireAlarm(name) {
        console.log(`[Node] Firing alarm ${name}!`);
        claudeWebContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronAlarmFired', {
                detail: { name: '${name}' }
            }));
        `).catch(() => {});
    }

    // Sentinel watchdog
    const hasSentinel = extPath && fs.existsSync(path.join(extPath, "sentinel", "manifest.json"));
    if (hasSentinel) {
        function checkSentinel() {
            setTimeout(() => {
                if (sentinelReceived) return;
                if (sentinelReloadCount < SENTINEL_MAX_RELOADS) {
                    sentinelReloadCount++;
                    console.log(`[Sentinel] Content scripts did not execute within ${SENTINEL_TIMEOUT_MS}ms. Reloading (attempt ${sentinelReloadCount}/${SENTINEL_MAX_RELOADS})...`);
                    sentinelReceived = false;
                    claudeWebContents.reloadIgnoringCache();
                    checkSentinel();
                } else {
                    console.log(`[Sentinel] Content scripts still not executing after ${SENTINEL_MAX_RELOADS} reloads. Giving up.`);
                }
            }, SENTINEL_TIMEOUT_MS);
        }
        checkSentinel();
    }

    // Tab events polyfill
    mainWindow.on("focus", () => {
        claudeWebContents && claudeWebContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronTabActivated', { detail: { tabId: 1, windowId: 1 } }));
        `).catch(() => {});
    });

    mainWindow.on("blur", () => {
        claudeWebContents && claudeWebContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronTabDeactivated', { detail: { tabId: 1, windowId: 1 } }));
        `).catch(() => {});
    });

    mainWindow.on("minimize", () => {
        claudeWebContents && claudeWebContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronTabRemoved', { detail: { tabId: 1, removeInfo: {} } }));
        `).catch(() => {});
    });

    mainWindow.on("restore", () => {
        claudeWebContents && claudeWebContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronTabActivated', { detail: { tabId: 1, windowId: 1 } }));
        `).catch(() => {});
    });
}

app.on("browser-window-created", (event, win) => {
    if (!mainWindow) {
        mainWindow = win;
        setupPolyfills();
    }
});

app.on("web-contents-created", (event, contents) => {
    if (claudeWebContents) return;

    const detect = (url) => {
        if (claudeWebContents) return;
        if (url && url.includes("claude.ai")) {
            claudeWebContents = contents;
            setupPolyfills();
        }
    };

    contents.on("did-start-navigation", (details) => {
        detect((details && details.url) || "");
    });

    contents.once("dom-ready", () => {
        detect(contents.getURL());
    });
});

// ================================================================
// Boot the original app
// ================================================================
require("./index.pre.js");
