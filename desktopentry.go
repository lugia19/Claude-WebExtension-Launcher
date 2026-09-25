package main

import "strings"

// Linux .desktop file generation (freedesktop Desktop Entry spec). No build tag, so
// it can be unit-tested anywhere.

type desktopField struct{ key, value string }

// desktopEntry renders a [Desktop Entry] group with the fields in order.
func desktopEntry(fields ...desktopField) string {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	for _, f := range fields {
		b.WriteString(f.key + "=" + f.value + "\n")
	}
	return b.String()
}

// desktopExec builds an Exec= value from a program and its arguments. Arguments are
// quoted when they contain anything the spec reserves, with `"`, "`", `$` and `\`
// escaped inside the quotes. A literal % must be doubled, since %U and friends are
// field codes; fieldCode (e.g. "%U") is appended unquoted if non-empty.
func desktopExec(fieldCode string, args ...string) string {
	quoted := make([]string, 0, len(args)+1)
	for _, a := range args {
		a = strings.ReplaceAll(a, "%", "%%")
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\><~|&;$*?#()`") {
			a = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(a) + `"`
		}
		quoted = append(quoted, a)
	}
	if fieldCode != "" {
		quoted = append(quoted, fieldCode)
	}
	return strings.Join(quoted, " ")
}
