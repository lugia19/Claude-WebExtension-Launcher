package utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const metadataTimeout = 15 * time.Second

var (
	metadataClient = NewMetadataClient()
	// Claude packages are large; allow slow connections, but never wait forever.
	downloadClient = newHTTPClient(15 * time.Minute)
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
	return getURL(metadataClient, url)
}

func getURL(client *http.Client, url string) (*http.Response, error) {
	resp, err := client.Get(url)
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
	resp, err := getURL(downloadClient, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	var body io.Reader = resp.Body
	if progress != nil {
		body = &progressReader{r: body, total: resp.ContentLength, report: progress}
	}
	_, err = io.Copy(out, body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return err
	}
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
