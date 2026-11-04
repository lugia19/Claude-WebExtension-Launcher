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
		user_data="$APPDATA/Claude-Main"
		;;
	macos)
		exe=./launcher
		launcher_log="$HOME/Library/Application Support/Claude WebExtension Launcher/launcher.log"
		user_data="$HOME/Library/Application Support/Claude-Main"
		;;
	linux)
		exe=./launcher
		launcher_log="${XDG_DATA_HOME:-$HOME/.local/share}/claude-webext-launcher/launcher.log"
		user_data="${XDG_CONFIG_HOME:-$HOME/.config}/Claude-Main"
		;;
esac

# wrapper.js writes how many extensions it loaded here (stdout isn't reliable: on Linux,
# Claude's console output after startup doesn't reach it).
status_file="$user_data/webext-status.json"

go build -o "$exe" . || exit 1
rm -f "$status_file"

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
		[ -d "$dir" ] || continue
		echo "$dir: wrapper.js lines in main.log, then its last 40 lines"
		grep -F -e '[webext]' -e 'Loading web extensions' -e 'Extensions loaded' -e '[Sentinel]' "$dir/main.log"
		tail -n 40 "$dir/main.log"
		# The whole folder, uploaded by smoke.yml.
		mkdir -p smoke-logs/claude && cp -r "$dir"/. smoke-logs/claude/
	done
	cp smoke.log smoke-logs/ 2>/dev/null
	[ -f "$launcher_log" ] && cp "$launcher_log" smoke-logs/
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
until [ -s "$status_file" ]; do
	kill -0 $launcher 2>/dev/null || fail "the launcher exited before Claude loaded its extensions"
	[ $SECONDS -lt $deadline ] || fail "Claude didn't load its extensions within 10 minutes"
	sleep 2
done

# The launcher updating itself would mean testing the released build, not this one.
grep -q 'Update available:' "$out" && fail "the launcher updated itself, so this isn't testing the checkout"

echo "webext-status.json: $(cat "$status_file")"
n=$(grep -o '"loaded":[0-9]*' "$status_file" | cut -d: -f2)
m=$(grep -o '"total":[0-9]*' "$status_file" | cut -d: -f2)
[ -n "$m" ] && [ "$m" -gt 0 ] || fail "no extensions were found to load"
[ "$n" = "$m" ] || fail "only $n of $m extensions loaded"
loaded="$n/$m"

sleep 20
kill -0 $launcher 2>/dev/null || fail "Claude didn't stay up after loading its extensions"

stop_claude
# Warnings don't fail the run, but they're worth seeing (the worker only logs to the file).
if [ -f "$launcher_log" ] && grep -q '^Warning' "$launcher_log"; then
	echo "--- warnings in launcher.log ---"
	grep -A3 '^Warning' "$launcher_log"
fi
echo
echo "Smoke test passed: $loaded extensions loaded, and Claude stayed up for 20 s"
