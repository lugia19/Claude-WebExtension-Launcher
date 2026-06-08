"use strict";

const { app, session, Notification } = require("electron");
const path = require("path");
const fs = require("fs");

// ================================================================
// Instance isolation — redirect userData before anything reads it
// ================================================================
const instanceArg = process.argv.find(a => a.startsWith("--instance="));
const instanceName = instanceArg ? instanceArg.split("=")[1] : "modified";
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
