package patcher

import (
	"claude-webext-patcher/utils"
	"fmt"
)

// DownloadProgress, when set, receives byte counts while Claude is being downloaded
// (total is -1 if the server didn't send a length). Used by the status window.
var DownloadProgress func(done, total int64)

// PrefetchedPackage / PrefetchedVersion are the Claude package the launcher
// downloaded (see Prefetch), handed to the worker via --package / --package-version.
// downloadAndExtract installs from it.
var (
	PrefetchedPackage string
	PrefetchedVersion string
)

// downloadFile fetches url into dst, reporting progress through DownloadProgress.
// A partial file is removed on failure.
func downloadFile(url, dst string) error {
	fmt.Printf("Downloading from: %s\n", url)
	if err := utils.DownloadFile(url, dst, DownloadProgress); err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	fmt.Printf("Downloaded: %s\n", dst)
	return nil
}
