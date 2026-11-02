package utils

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetadataTimeoutCancelsStalledRequest(t *testing.T) {
	for _, bodyStarted := range []bool{false, true} {
		t.Run(map[bool]string{false: "waiting for headers", true: "reading body"}[bodyStarted], func(t *testing.T) {
			cancelled := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if bodyStarted {
					w.Write([]byte("{"))
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
				close(cancelled)
			}))
			defer srv.Close()
			client := newHTTPClient(100 * time.Millisecond)
			defer client.CloseIdleConnections()
			resp, err := getURL(client, srv.URL)
			if err == nil {
				_, err = io.ReadAll(resp.Body)
				resp.Body.Close()
			}
			var timeout net.Error
			if !errors.As(err, &timeout) || !timeout.Timeout() {
				t.Fatalf("expected timeout, got %v", err)
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("timed-out request was not cancelled")
			}
		})
	}
}

func TestDownloadFailureRemovesPartialFile(t *testing.T) {
	old := downloadClient
	downloadClient = newHTTPClient(100 * time.Millisecond)
	t.Cleanup(func() {
		downloadClient.CloseIdleConnections()
		downloadClient = old
	})
	for _, fail := range []string{"HTTP error", "truncated", "stalled"} {
		t.Run(fail, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if fail == "HTTP error" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Length", "1000")
				w.Write([]byte("partial"))
				w.(http.Flusher).Flush()
				if fail == "stalled" {
					<-r.Context().Done()
				}
			}))
			defer srv.Close()
			dst := filepath.Join(t.TempDir(), "download.zip")
			if err := DownloadFile(srv.URL, dst, nil); err == nil {
				t.Fatal("failed response was accepted")
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatalf("partial download remains: %v", err)
			}
		})
	}
}

func TestDownloadReportsProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "7")
		w.Write([]byte("package"))
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "download")
	var done, total int64
	if err := DownloadFile(srv.URL, dst, func(d, n int64) { done, total = d, n }); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "package" || done != 7 || total != 7 {
		t.Fatalf("download=%q, err=%v, progress=%d/%d", got, err, done, total)
	}
}
