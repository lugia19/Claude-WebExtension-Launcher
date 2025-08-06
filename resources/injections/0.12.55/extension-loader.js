// Convenience aliases
const CUTfs = require('fs');
const CUTpath = require('path');
const CUTelectron = ue;
const CUTmainWindow = e;
const CUTwebView = r;
const CUTsession = CUTelectron.session;


let currentPath = CUTelectron.app.getAppPath();
let extPath = null;

// Go up until we find an 'extensions' sibling
while (currentPath !== CUTpath.dirname(currentPath)) {
    currentPath = CUTpath.dirname(currentPath);
    const testPath = CUTpath.join(currentPath, 'extensions');
    if (CUTfs.existsSync(testPath)) {
        extPath = testPath;
        break;
    }
}

// Load extensions if we found the folder
if (extPath) {
    // Wait for the page to be ready before loading extensions
    CUTwebView.webContents.once('did-finish-load', () => {
        console.log('Page loaded, loading extensions...');
        CUTfs.readdirSync(extPath).forEach(f => {
            const p = CUTpath.join(extPath, f);
            if (CUTfs.existsSync(CUTpath.join(p, 'manifest.json'))) {
                console.log('Loading extension:', f);
                CUTsession.defaultSession.extensions.loadExtension(p);
            }
        });
    });
}

// Window event handlers for tab activity simulation
CUTmainWindow.on('focus', () => {
    CUTwebView && CUTwebView.webContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronTabActivated', {
                detail: { tabId: 1, windowId: 1 }
            }));
        `).catch(() => { });
});

CUTmainWindow.on('blur', () => {
    CUTwebView && CUTwebView.webContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronTabDeactivated', {
                detail: { tabId: 1, windowId: 1 }
            }));
        `).catch(() => { });
});

CUTmainWindow.on('minimize', () => {
    CUTwebView && CUTwebView.webContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronTabRemoved', {
                detail: { tabId: 1, removeInfo: {} }
            }));
        `).catch(() => { });
});

CUTmainWindow.on('restore', () => {
    CUTwebView && CUTwebView.webContents.executeJavaScript(`
            window.dispatchEvent(new CustomEvent('electronTabActivated', {
                detail: { tabId: 1, windowId: 1 }
            }));
        `).catch(() => { });
});

//Notification listener
const CUT_NOTIFICATION_PREFIX = 'CUT_NOTIFICATION:';
CUTwebView.webContents.on('console-message', (details) => {
    if (details.message.startsWith(CUT_NOTIFICATION_PREFIX)) {
        try {
            const content = details.message.substring(CUT_NOTIFICATION_PREFIX.length);
            let options;

            // Try to parse as JSON first
            try {
                options = JSON.parse(content);
            } catch (e) {
                // If not JSON, treat as plain text message
                options = {
                    title: 'Claude Usage Tracker',
                    message: content.trim()
                };
            }

            // Build path to the icon
            const iconPath = CUTpath.join(CUTpath.dirname(CUTelectron.app.getAppPath()), 'Tray-Win32.ico');

            const notification = new CUTelectron.Notification({
                title: options.title,
                body: options.message,
                icon: iconPath
            });
            notification.show();
        } catch (error) {
            console.error('Failed to create notification:', error);
        }
    }
});

//Injection end