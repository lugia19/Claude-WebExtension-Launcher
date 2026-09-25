package patcher

import (
	"io"
	"net/http"
)

// DownloadProgress, when set, receives byte counts while Claude is being downloaded
// (total is -1 if the server didn't send a length). Used by the status window.
var DownloadProgress func(done, total int64)

// PrefetchedPackage / PrefetchedVersion are set in the elevated Windows patcher from
// the --package / --package-version flags: the Claude package the unelevated launcher
// already downloaded (see prefetch_windows.go). Unused on other platforms.
var (
	PrefetchedPackage string
	PrefetchedVersion string
)

// progressBody wraps a download response so DownloadProgress sees each chunk.
func progressBody(resp *http.Response) io.Reader {
	if DownloadProgress == nil {
		return resp.Body
	}
	return &progressReader{r: resp.Body, total: resp.ContentLength}
}

type progressReader struct {
	r           io.Reader
	done, total int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if cb := DownloadProgress; cb != nil {
		cb(p.done, p.total)
	}
	return n, err
}
