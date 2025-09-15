//Notification listener
const CUT_NOTIFICATION_PREFIX = 'CUT_NOTIFICATION:';
CUTwebView.webContents.on('console-message', (event) => {
    const message = event.message;
    if (message.startsWith(CUT_NOTIFICATION_PREFIX)) {
        console.log('[Node] Notification command received:', message);
        try {
            const content = message.substring(CUT_NOTIFICATION_PREFIX.length);
            let options;

            try {
                options = JSON.parse(content);
            } catch (e) {
                options = {
                    title: 'Claude Usage Tracker',
                    message: content.trim()
                };
            }

            console.log('[Node] Creating notification:', options);

            const iconPath = CUTpath.join(CUTpath.dirname(CUTelectron.app.getAppPath()), 'Tray-Win32.ico');

            const notification = new CUTelectron.Notification({
                title: options.title,
                body: options.message || options.body,
                icon: iconPath
            });
            notification.show();
        } catch (error) {
            console.error('[Node] Failed to create notification:', error);
        }
    }
});