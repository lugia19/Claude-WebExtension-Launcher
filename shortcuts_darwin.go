package main

// macOS has nothing to manage here: the launcher .app goes in Applications, and
// startup is System Settings > General > Login Items. See shortcuts.go.

func menuEntrySupported() bool                  { return false }
func hasMenuEntry(instance string) bool         { return false }
func addMenuEntry(instance string) error        { return nil }
func removeMenuEntry(instance string) error     { return nil }
func hasStartup(instance string) bool           { return false }
func setStartup(instance string, on bool) error { return nil }
