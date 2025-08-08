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
