#!/usr/bin/env bash
# Smoke test: build the launcher from this checkout, let it download, patch and launch
# the latest Claude (--debug: no window, Claude attached to this terminal), and check
# that Claude loads every extension and stays up. Used by .github/workflows/smoke.yml on
# every OS (Git Bash on Windows); it installs for real, so run it locally only on a
# machine where that's fine.
set -u

case "$(uname -s)" in
	MINGW* | MSYS* | CYGWIN*) os=windows ;;
	Darwin) os=macos ;;
	*) os=linux ;;
esac

case $os in
	windows)
		exe=./launcher.exe
		launcher_log="$LOCALAPPDATA/ClaudeWebExtLauncher/launcher.log"
		;;
	macos)
		exe=./launcher
		launcher_log="$HOME/Library/Application Support/Claude WebExtension Launcher/launcher.log"
		;;
	linux)
		exe=./launcher
		launcher_log="${XDG_DATA_HOME:-$HOME/.local/share}/claude-webext-launcher/launcher.log"
		;;
esac

go build -o "$exe" . || exit 1

out=smoke.log
: >"$out"
if [ $os = linux ]; then
	xvfb-run -a "$exe" --debug --no-pause >"$out" 2>&1 &
else
	"$exe" --debug --no-pause >"$out" 2>&1 &
fi
launcher=$!
tail -f "$out" &
tailer=$!

stop_claude() {
	case $os in
		windows) powershell -NoProfile -Command "Get-Process claude -ErrorAction SilentlyContinue | Where-Object { \$_.Path -like '*ClaudeWebExtLauncher*' } | Stop-Process -Force" ;;
		macos) pkill -f 'Claude.app/Contents/MacOS/Claude' ;;
		linux) pkill -f 'claude-webext-launcher/app-latest/' ;;
	esac
	kill $launcher 2>/dev/null
	wait $launcher 2>/dev/null
	kill $tailer 2>/dev/null
}

# Extra state for working out why a run failed.
diagnose() {
	echo "--- Claude's own logs ---"
	for dir in "${APPDATA:-}/Claude-Main/logs" "$HOME/.config/Claude-Main/logs" \
		"$HOME/Library/Logs/Claude-Main" "$HOME/Library/Application Support/Claude-Main/logs"; do
		[ -f "$dir/main.log" ] || continue
		echo "$dir/main.log (wrapper lines, then the last 60):"
		grep -E 'webext|Loading|Extensions loaded|Sentinel' "$dir/main.log" | head -n 20
		tail -n 60 "$dir/main.log"
	done
	echo "--- Claude processes ---"
	case $os in
		windows)
			powershell -NoProfile -Command "Get-CimInstance Win32_Process -Filter \"Name='claude.exe'\" | Select-Object ProcessId, ParentProcessId, CommandLine | Format-List"
			echo "--- whoami /priv ---"
			whoami //priv
			echo "--- WindowsApps ACL ---"
			icacls 'C:\Program Files\WindowsApps'
			;;
		*) ps -eo pid,ppid,args | grep -i '[c]laude' ;;
	esac
}

fail() {
	stop_claude
	echo
	echo "SMOKE TEST FAILED: $1"
	if [ -f "$launcher_log" ]; then
		echo "--- launcher.log (includes the worker) ---"
		cat "$launcher_log"
	fi
	diagnose
	exit 1
}

# Downloading and patching Claude comes first, so allow a while.
deadline=$((SECONDS + 600))
until grep -q 'Extensions loaded:' "$out"; do
	kill -0 $launcher 2>/dev/null || fail "the launcher exited before Claude loaded its extensions"
	[ $SECONDS -lt $deadline ] || fail "Claude didn't load its extensions within 10 minutes"
	sleep 2
done

# The launcher updating itself would mean testing the released build, not this one.
grep -q 'Update available:' "$out" && fail "the launcher updated itself, so this isn't testing the checkout"

loaded=$(grep -o 'Extensions loaded: [0-9]*/[0-9]*' "$out" | head -n1 | cut -d' ' -f3)
[ "${loaded%/*}" = "${loaded#*/}" ] || fail "only $loaded extensions loaded"

sleep 20
kill -0 $launcher 2>/dev/null || fail "Claude didn't stay up after loading its extensions"

stop_claude
echo
echo "Smoke test passed: $loaded extensions loaded, and Claude stayed up for 20 s"
