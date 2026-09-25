//go:build !windows

package main

func SetupSessionSharing(instanceName string) {}

func RepairSessionSharing(instanceName string) {}

func CleanupOfficialJunctions() {}

func sessionsShared() bool                                             { return false }
func unshareSessions(instances []string, copyIntoInstances bool) error { return nil }
func safeToDelete(dir string) bool                                     { return true }
