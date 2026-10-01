package main

import (
	"fmt"
	"strconv"
	"strings"

	"claude-webext-patcher/gui"
	"claude-webext-patcher/utils"
)

// launchOptionsWarning heads the Advanced section of a settings screen, which holds an
// instance's launch options.
const launchOptionsWarning = "Don't turn these on unless you know what you're doing. They take effect the next time Claude starts."

// launchOptionsKey is the key of an instance's options in Settings.InstanceOptions,
// from the name it's launched with. The main instance's are always under "Main", also
// while it still launches as "modified" (its folder not renamed yet; see
// instances_migrate.go), so they survive the rename.
func launchOptionsKey(instance string) string {
	if instance == legacyMainInstanceName {
		return mainInstanceName
	}
	return instance
}

// launchOptions are an instance's launch options (utils.InstanceOptions) on a settings
// screen: their indexes in its Options.
type launchOptions struct {
	key                                              string // see launchOptionsKey
	remoteDebugging, inspector, disableQUIC, devMode int
}

// addLaunchOptions adds instance's launch options to a settings screen's options, in
// its Advanced section. instance is the name it's launched with.
func addLaunchOptions(options *[]gui.SetupOption, instance string) *launchOptions {
	key := launchOptionsKey(instance)
	current := utils.LoadSettings().InstanceOptions[key]
	add := func(opt gui.SetupOption) int { // returns the option's index in Apply's checked
		opt.Advanced = true
		*options = append(*options, opt)
		return len(*options) - 1
	}
	l := &launchOptions{key: key}
	l.remoteDebugging = add(gui.SetupOption{
		Label:   "Allow remote debugging on port",
		Checked: current.RemoteDebugging,
		Entry: &gui.SetupEntry{
			Value:    strconv.Itoa(current.Port()),
			Width:    80,
			Validate: func(text string) string { _, problem := parsePort(text, "remote debugging"); return problem },
		},
	})
	l.inspector = add(gui.SetupOption{
		Label:   "Node inspector on port",
		Checked: current.Inspector,
		Note:    "Debugs Claude's main process (e.g. from chrome://inspect). Needs advanced debug mode.",
		Entry: &gui.SetupEntry{
			Value:    strconv.Itoa(current.NodeInspectorPort()),
			Width:    80,
			Validate: func(text string) string { _, problem := parsePort(text, "Node inspector"); return problem },
		},
	})
	l.disableQUIC = add(gui.SetupOption{
		Label:   "Disable QUIC",
		Checked: current.DisableQUIC,
		Note:    "Uses HTTP/2 over TLS instead of HTTP/3, which Wireshark decodes and decompresses better.",
	})
	// Last: its multi-line field takes the mouse wheel, which would stop the screen
	// scrolling past it.
	l.devMode = add(gui.SetupOption{
		Label:   "Advanced debug mode",
		Checked: current.DevMode,
		Note:    "Turns on Claude's internal test features, set up through the environment variables given here. SSLKEYLOGFILE also covers Node's TLS in the main process.",
		Entry: &gui.SetupEntry{
			Value:       strings.Join(current.Env, "\n"),
			Lines:       3,
			Placeholder: `KEY=value, one per line (e.g. SSLKEYLOGFILE=C:\keys.log)`,
			Validate:    func(text string) string { _, problem := parseEnv(text); return problem },
		},
	})
	return l
}

// validate checks the launch options together on a settings screen. l may be nil (no
// launch options on the screen).
func (l *launchOptions) validate(checked []bool, values []string) string {
	if l == nil {
		return ""
	}
	if checked[l.inspector] && !checked[l.devMode] {
		return "The Node inspector needs advanced debug mode."
	}
	if checked[l.remoteDebugging] && checked[l.inspector] {
		// Both entries are valid ports by now.
		debug, _ := parsePort(values[l.remoteDebugging], "remote debugging")
		inspector, _ := parsePort(values[l.inspector], "Node inspector")
		if debug == inspector {
			return "Remote debugging and the Node inspector need different ports."
		}
	}
	return ""
}

// save stores the launch options from a confirmed settings screen. The entries' values
// are kept while their option is off.
func (l *launchOptions) save(checked []bool, values []string) {
	opts := utils.InstanceOptions{
		RemoteDebugging: checked[l.remoteDebugging],
		Inspector:       checked[l.inspector],
		DisableQUIC:     checked[l.disableQUIC],
		DevMode:         checked[l.devMode],
	}
	if port, problem := parsePort(values[l.remoteDebugging], "remote debugging"); problem == "" && port != utils.DefaultDebugPort {
		opts.DebugPort = port
	}
	if port, problem := parsePort(values[l.inspector], "Node inspector"); problem == "" && port != utils.DefaultInspectorPort {
		opts.InspectorPort = port
	}
	if env, problem := parseEnv(values[l.devMode]); problem == "" {
		opts.Env = env
	}
	err := utils.UpdateSettings(func(s *utils.Settings) {
		if opts.IsZero() {
			delete(s.InstanceOptions, l.key)
			return
		}
		if s.InstanceOptions == nil {
			s.InstanceOptions = map[string]utils.InstanceOptions{}
		}
		s.InstanceOptions[l.key] = opts
	})
	if err != nil {
		fmt.Printf("Warning: could not save the launch options: %v\n", err)
	}
}

// parsePort reads a port for what (e.g. "remote debugging"), or says why it isn't one.
func parsePort(text, what string) (int, string) {
	port, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || port < 1024 || port > 65535 {
		return 0, fmt.Sprintf("The %s port must be a number from 1024 to 65535.", what)
	}
	return port, ""
}

// parseEnv reads environment variables, one KEY=value per line (blank lines are
// skipped), or says why they can't be. No lines gives nil.
func parseEnv(text string) ([]string, string) {
	var env []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Sprintf("%q isn't KEY=value.", line)
		}
		env = append(env, line)
	}
	return env, ""
}
