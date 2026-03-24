// Convenience aliases
const CUTmainWindow = PLACEHOLDER_MAINWINDOW;
const CUTwebView = PLACEHOLDER_WEBVIEW;

const CUTfs = require('fs');
const CUTpath = require('path');
const CUTelectron = require("electron");
const CUTsession = CUTelectron.session;

// Clear session cache to prevent stale SPA from loading before extensions
CUTsession.defaultSession.clearCache();

let currentPath = CUTelectron.app.getAppPath();
let extPath = null;

// Go up until we find an 'web-extensions' sibling
while (currentPath !== CUTpath.dirname(currentPath)) {
    currentPath = CUTpath.dirname(currentPath);
    const testPath = CUTpath.join(currentPath, 'web-extensions');
    if (CUTfs.existsSync(testPath)) {
        extPath = testPath;
        break;
    }
}

// Load extensions and await them before page navigation
if (extPath) {
    const extDirs = CUTfs.readdirSync(extPath).filter(f =>
        CUTfs.existsSync(CUTpath.join(extPath, f, 'manifest.json'))
    );

    if (extDirs.length > 0) {
        console.log('Loading web extensions...');
        const loadPromises = extDirs.map(f => {
            const p = CUTpath.join(extPath, f);
            console.log('Loading extension:', f);
            return CUTsession.defaultSession.extensions.loadExtension(p).catch(err => {
                console.error('Failed to load extension:', f, err);
            });
        });

        Promise.all(loadPromises).then(() => {
            const loaded = CUTsession.defaultSession.extensions.getAllExtensions().length;
            console.log(`Extensions loaded: ${loaded}/${extDirs.length}`);
            if (loaded < extDirs.length) {
                console.log('Not all extensions loaded, reloading page...');
                CUTwebView.webContents.reloadIgnoringCache();
            }
        });
    }
}

//Generic logging function
CUTwebView.webContents.on('console-message', (event) => {
    const message = event.message
    if (message.startsWith("EXT_LOG:")) {
        console.log(message)
    }
});
