// Alarm polyfill
const alarms = new Map();
const CUT_ALARM_PREFIX = "CUT_ALARM:"
CUTwebView.webContents.on('console-message', (event) => {
    const message = event.message
    if (message.startsWith(CUT_ALARM_PREFIX)) {
        console.log('[Node] Alarm command received:', message);
        try {
            const data = JSON.parse(message.substring(CUT_ALARM_PREFIX.length));
            if (data.action === 'create') {
                // Clear existing alarm with same name
                if (alarms.has(data.name)) {
                    clearTimeout(alarms.get(data.name));
                    alarms.delete(data.name);
                }

                let timerId;

                if (data.periodInMinutes) {
                    // Recurring alarm
                    timerId = setInterval(() => {
                        fireAlarm(data.name);
                    }, data.periodInMinutes * 60 * 1000);

                } else if (data.when) {
                    // One-time alarm at specific time
                    const delay = data.when - Date.now();
                    if (delay > 0) {
                        timerId = setTimeout(() => {
                            fireAlarm(data.name);
                            alarms.delete(data.name);
                        }, delay);
                    }

                } else if (data.delayInMinutes) {
                    // One-time alarm with delay
                    timerId = setTimeout(() => {
                        fireAlarm(data.name);
                        alarms.delete(data.name);
                    }, data.delayInMinutes * 60 * 1000);
                }

                if (timerId) {
                    alarms.set(data.name, timerId);
                }

            } else if (data.action === 'clear') {
                if (alarms.has(data.name)) {
                    clearTimeout(alarms.get(data.name));
                    alarms.delete(data.name);
                }
            }
        } catch (err) {
            console.error('Alarm error:', err);
        }
    }
});

function fireAlarm(name) {
    CUTwebView.webContents.executeJavaScript(`
        window.dispatchEvent(new CustomEvent('electronAlarmFired', {
            detail: { name: '${name}' }
        }));
    `);
}
