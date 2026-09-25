package main

import "testing"

func TestDesktopExec(t *testing.T) {
	cases := []struct {
		field string
		args  []string
		want  string
	}{
		{"", []string{"/opt/launcher"}, `/opt/launcher`},
		{"%U", []string{"/home/me/app-latest/claude-desktop", "--instance=modified"}, `/home/me/app-latest/claude-desktop --instance=modified %U`},
		{"", []string{"/home/me/My Apps/launcher", "--instance=work"}, `"/home/me/My Apps/launcher" --instance=work`},
		{"", []string{`/tmp/a"b$c`}, `"/tmp/a\"b\$c"`},
		{"", []string{"/tmp/100%/x"}, `/tmp/100%%/x`},
	}
	for _, c := range cases {
		if got := desktopExec(c.field, c.args...); got != c.want {
			t.Errorf("desktopExec(%q, %q) = %s, want %s", c.field, c.args, got, c.want)
		}
	}
}

func TestDesktopEntry(t *testing.T) {
	got := desktopEntry(
		desktopField{"Type", "Application"},
		desktopField{"Name", "Claude Desktop (Extended)"},
	)
	want := "[Desktop Entry]\nType=Application\nName=Claude Desktop (Extended)\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEntryNames(t *testing.T) {
	if entryName(defaultInstanceName) != shortcutName || entryArgs(defaultInstanceName) != nil {
		t.Fatal("default instance should use the plain name and no arguments")
	}
	if entryName("work") != shortcutName+" - work" {
		t.Fatalf("named instance: %q", entryName("work"))
	}
	if a := entryArgs("work"); len(a) != 1 || a[0] != "--instance=work" {
		t.Fatalf("named instance args: %q", a)
	}
}
