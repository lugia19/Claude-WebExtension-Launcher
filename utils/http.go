package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	metadataTimeout     = 15 * time.Second
	downloadIdleTimeout = 60 * time.Second
)

var (
	metadataClient = NewMetadataClient()
	// Downloads have no total deadline: progress, not package size or connection
	// speed, determines when they time out.
	downloadClient = &http.Client{}
)

func newHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = metadataTimeout
	return &http.Client{Timeout: timeout, Transport: transport}
}

// NewMetadataClient returns a bounded client. Callers resolving redirects can
// customize CheckRedirect without losing the request and response-header limits.
func NewMetadataClient() *http.Client {
	return newHTTPClient(metadataTimeout)
}

// GetURL fetches metadata and rejects HTTP error pages before callers decode them.
// The caller must close the returned body; the timeout also covers reading it.
func GetURL(url string) (*http.Response, error) {
	return getURL(context.Background(), metadataClient, url)
}

func getURL(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return resp, nil
}

// DownloadFile writes a complete successful response to dst. Timeouts cancel the
// request and partial files are removed, including on write or close failures.
// progress receives total=-1 when the server doesn't send a Content-Length.
func DownloadFile(url, dst string, progress func(done, total int64)) error {
	return downloadFile(downloadClient, downloadIdleTimeout, url, dst, progress)
}

func downloadFile(client *http.Client, idle time.Duration, url, dst string, progress func(done, total int64)) error {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	timer := time.AfterFunc(idle, func() { cancel(fmt.Errorf("download received no bytes for %s", idle)) })
	defer timer.Stop()
	resp, err := getURL(ctx, client, url)
	if err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	body := &progressReader{r: resp.Body, total: resp.ContentLength, report: progress, timer: timer, idle: idle}
	_, err = io.Copy(out, body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		return err
	}
	return nil
}

type progressReader struct {
	r           io.Reader
	done, total int64
	report      func(done, total int64)
	timer       *time.Timer
	idle        time.Duration
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if n > 0 {
		p.timer.Reset(p.idle)
	}
	if p.report != nil {
		p.report(p.done, p.total)
	}
	return n, err
}
