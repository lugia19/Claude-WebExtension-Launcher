package patcher

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

// DownloadProgress, when set, receives byte counts while Claude is being downloaded
// (total is -1 if the server didn't send a length). Used by the status window.
var DownloadProgress func(done, total int64)

// downloadFile fetches url into dst, reporting progress through DownloadProgress.
// A partial file is removed on failure.
func downloadFile(url, dst string) error {
	fmt.Printf("Downloading from: %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("downloading: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating file: %v", err)
	}
	var body io.Reader = resp.Body
	if DownloadProgress != nil {
		body = &progressReader{r: resp.Body, total: resp.ContentLength, report: DownloadProgress}
	}
	_, err = io.Copy(out, body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return fmt.Errorf("saving file: %v", err)
	}
	fmt.Printf("Downloaded: %s\n", dst)
	return nil
}

type progressReader struct {
	r           io.Reader
	done, total int64
	report      func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.report(p.done, p.total)
	return n, err
}
