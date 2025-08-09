// Convenience aliases
const CUTfs = require('fs');
const CUTpath = require('path');
const CUTelectron = ue;
const CUTmainWindow = e;
const CUTwebView = r;
const CUTsession = CUTelectron.session;

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

 // Now load extensions
if (extPath) {
    const hadExtensions = CUTsession.defaultSession.extensions.getAllExtensions().length > 0;

    CUTwebView.webContents.once('did-finish-load', () => {
        console.log('Loading web extensions...');
        let loadedAny = false;

        CUTfs.readdirSync(extPath).forEach(f => {
            const p = CUTpath.join(extPath, f);
            if (CUTfs.existsSync(CUTpath.join(p, 'manifest.json'))) {
                console.log('Loading extension:', f);
                CUTsession.defaultSession.extensions.loadExtension(p);
                loadedAny = true;
            }
        });

        if (!hadExtensions && loadedAny) {
            console.log('First time loading web extensions, reloading page...');
            setTimeout(() => {
                CUTwebView.webContents.reload();
            }, 100);
        }
    });
}

//Generic logging function
CUTwebView.webContents.on('console-message', (event) => {
    const message = event.message
    if (message.startsWith("EXT_LOG:")) {
        console.log(message)
    }
});