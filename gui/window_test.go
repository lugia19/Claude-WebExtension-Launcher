package gui

import (
	"errors"
	"testing"
	"time"
)

func TestExitIfHung(t *testing.T) {
	shutdownGrace = 10 * time.Millisecond
	setup := func() (*window, chan struct{}, chan struct{}) {
		w := &window{s: newStatus(nil, "")}
		w.s.markClosed()
		finished := make(chan struct{})
		close(finished)
		return w, finished, make(chan struct{})
	}

	// Fyne hung: exit gets what Run would have returned.
	w, finished, returned := setup()
	want := errors.New("failed")
	got := make(chan error, 1)
	w.exitIfHung(finished, returned, &want, func(err error) { got <- err })
	select {
	case err := <-got:
		if err != want {
			t.Errorf("exit got %v, want %v", err, want)
		}
	default:
		t.Error("exit wasn't called")
	}

	// Run returned: exit isn't called.
	w, finished, returned = setup()
	close(returned)
	var none error
	w.exitIfHung(finished, returned, &none, func(error) { t.Error("exit called after Run returned") })
}
