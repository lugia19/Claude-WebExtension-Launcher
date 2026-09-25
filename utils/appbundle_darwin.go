package utils

import (
	"fmt"
	"os"
	"os/exec"
)

// InstallAppBundle copies the app bundle src to dst, replacing whatever is there. The
// copy is made next to dst first and swapped in with renames, so dst is never half
// written; that also works while the app at dst is running (it keeps its files open).
// The quarantine flag is dropped from the copy, so Gatekeeper doesn't run it from a
// random read-only location (app translocation).
func InstallAppBundle(src, dst string) error {
	staged, old := dst+".new", dst+".old"
	os.RemoveAll(staged)
	os.RemoveAll(old)
	// ditto keeps the bundle's symlinks, permissions and ad-hoc signature intact.
	if out, err := exec.Command("ditto", src, staged).CombinedOutput(); err != nil {
		os.RemoveAll(staged)
		return fmt.Errorf("copying %s: %v: %s", src, err, out)
	}
	exec.Command("xattr", "-cr", staged).Run()

	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			os.RemoveAll(staged)
			return fmt.Errorf("moving the old app aside: %w", err)
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		os.Rename(old, dst) // put the old one back
		return fmt.Errorf("moving the new app in: %w", err)
	}
	os.RemoveAll(old)
	return nil
}
