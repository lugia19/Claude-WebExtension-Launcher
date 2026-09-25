//go:build !windows

package main

// registerInstall: only Windows has a list of installed apps to register in.
func registerInstall(installed string) {}
