package extensions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const extensionUpdateMarker = ".launcher-extension-update"

func markExtensionUpdate(stage, destination string) error {
	return os.WriteFile(filepath.Join(stage, extensionUpdateMarker), extensionUpdateIdentity(destination), 0600)
}

func extensionUpdateIdentity(destination string) []byte {
	return []byte("claude-webext-launcher-extension-update-v1\n" + filepath.Base(destination) + "\n")
}

// The launcher's patch lock excludes other workers while updates and recovery run.
// A process killed before its deferred cleanup can leave a staging directory; if
// it died between the two renames, that directory also holds the installed copy.
func recoverExtensionUpdates(destination string) error {
	stages, err := extensionUpdateDirs(destination)
	if err != nil {
		return err
	}
	for _, stage := range stages {
		previous := filepath.Join(stage, "previous")
		if info, err := os.Lstat(previous); err == nil {
			if !info.IsDir() {
				return fmt.Errorf("unexpected extension backup at %s; kept for recovery", previous)
			}
			if _, err := os.Lstat(destination); os.IsNotExist(err) {
				if err := os.Rename(previous, destination); err != nil {
					return fmt.Errorf("restoring interrupted update from %s: %w", previous, err)
				}
			} else if err != nil {
				return err
			} else if err := validateManifest(destination); err != nil {
				return fmt.Errorf("installed extension is invalid; kept backup at %s: %w", previous, err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.RemoveAll(stage); err != nil {
			return fmt.Errorf("cleaning interrupted extension update: %w", err)
		}
	}
	return nil
}

func extensionUpdateDirs(destination string) ([]string, error) {
	parent := filepath.Dir(destination)
	entries, err := os.ReadDir(parent)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prefix := "." + filepath.Base(destination) + "-update-"
	var stages []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		stage := filepath.Join(parent, entry.Name())
		// A name alone doesn't establish ownership. Unknown/older unmarked
		// folders and folders the wrapper could load as extensions are preserved.
		if _, err := os.Lstat(filepath.Join(stage, "manifest.json")); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		marker := filepath.Join(stage, extensionUpdateMarker)
		info, err := os.Lstat(marker)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		expected := extensionUpdateIdentity(destination)
		if !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
			continue
		}
		data, err := os.ReadFile(marker)
		if err != nil {
			return nil, err
		}
		if string(data) == string(expected) {
			stages = append(stages, stage)
		}
	}
	return stages, nil
}
