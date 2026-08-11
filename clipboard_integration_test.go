// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// The live X11 clipboard proof. It runs only under -tags=integration with
// WINDOW_X11_INTEGRATION set and a reachable X server (Xvfb in CI).
//
// A clipboard exchange needs two parties, and it gets two REAL ones: two
// windows, each with its own connection to the server, one owning the selection
// and one asking for it. The names begin with TestLiveX11 because that is what
// the CI lane filters on -- a test outside that filter never runs and only
// looks like proof. Nothing here is faked — the text goes out through the
// server and comes back through it, which is the only way to find out whether
// the wire layout is right. A single window checked against itself would prove
// nothing, since it never goes near the protocol.
package window

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-widgets/window/internal/x11"
)

func skipUnlessX11(t *testing.T) {
	t.Helper()
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 (and have an X server) to run the live clipboard proof")
	}
}

// twoWindows returns the owner and the asker, opened once for the whole file.
//
// Once, not per test, because opening a fresh pair for each of them had the
// server resetting the connection partway down the file -- and the tests that
// hit it SKIPPED, which in a lane whose job is to prove something is worse than
// failing: two of these were quietly not running at all and the lane was green.
// Now the pair is shared, and a server that cannot be reached is a failure.
var (
	pairOnce  sync.Once
	pairOwner *Window
	pairAsker *Window
	pairErr   error
)

func twoWindows(t *testing.T) (owner, asker *Window) {
	t.Helper()
	pairOnce.Do(func() {
		a, err := Open(Config{Title: "clipboard owner", Width: 120, Height: 80})
		if err != nil {
			pairErr = err
			return
		}
		b, err := Open(Config{Title: "clipboard asker", Width: 120, Height: 80})
		if err != nil {
			_ = a.Close()
			pairErr = err
			return
		}
		pairOwner, pairAsker = a.(*Window), b.(*Window)
	})
	if pairErr != nil {
		t.Fatalf("opening a window on the X server this lane provides: %v", pairErr)
	}
	// Each test starts from nobody owning the selection AND from an empty
	// queue on both connections. The second half matters as much as the first:
	// a test that deliberately leaves a request unanswered (the silent-owner
	// one) leaves it sitting in the owner's socket, and the next test's pump
	// answers THAT instead of its own -- which is how this fixture first sent a
	// paste an answer meant for the test before it.
	pairOwner.clipOwned, pairOwner.clipText = false, ""
	if a, ok := pairOwner.clipAtoms(); ok {
		_ = pairOwner.conn.SetSelectionOwner(0, a.clipboard, x11.CurrentTime)
	}
	drain(pairOwner)
	drain(pairAsker)
	return pairOwner, pairAsker
}

// drain reads whatever is already queued and throws it away.
func drain(w *Window) {
	for {
		ready, supported := w.conn.WaitReadable(80 * time.Millisecond)
		if !supported || !ready {
			return
		}
		if _, err := w.conn.NextEvent(); err != nil {
			return
		}
	}
}

// pump answers selection requests on the owner's connection for a while, which
// is what its event loop would be doing if this were an application.
func pump(t *testing.T, w *Window, d time.Duration) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handled := 0
		defer func() { t.Logf("pump handled %d selection events", handled) }()
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			ready, supported := w.conn.WaitReadable(50 * time.Millisecond)
			if !supported {
				return
			}
			if !ready {
				continue // nobody has asked yet
			}
			ev, err := w.conn.NextEvent()
			if err != nil {
				return
			}
			if w.handleSelectionEvent(ev) {
				handled++
			} else {
				t.Logf("pump saw a non-selection event, code %d", ev.Code)
			}
		}
	}()
	return done
}

func TestLiveX11ClipboardCrossesTwoWindows(t *testing.T) {
	skipUnlessX11(t)
	owner, asker := twoWindows(t)

	const text = "go-widgets clipboard — accentué, 日本語, 🎯"
	owner.SetClipboardText(text)
	stop := pump(t, owner, 3*time.Second)

	if got := asker.ClipboardText(); got != text {
		t.Errorf("the asker read %q, the owner copied %q", got, text)
	}
	<-stop
}

// An owner reads its own text without going near the server: asking itself
// would be a single-threaded application waiting for a reply it is not running
// the loop to answer.
func TestLiveX11ClipboardOwnerReadsItsOwnText(t *testing.T) {
	skipUnlessX11(t)
	owner, _ := twoWindows(t)

	owner.SetClipboardText("mine")
	if got := owner.ClipboardText(); got != "mine" {
		t.Errorf("the owner read %q of its own text", got)
	}
}

// Nothing copied in this session is an empty string, not an error and not a
// wait: with no owner there is nobody to answer.
func TestLiveX11ClipboardEmptyWhenNobodyOwnsIt(t *testing.T) {
	skipUnlessX11(t)
	_, asker := twoWindows(t)

	start := time.Now()
	if got := asker.ClipboardText(); got != "" {
		t.Errorf("read %q from a selection nobody owns", got)
	}
	if d := time.Since(start); d > clipboardWait {
		t.Errorf("took %v to find out nobody owns it; it is a round trip, not a wait", d)
	}
}

// The one that cannot happen on a healthy server, and the reason for the
// deadline: an owner that claims the selection and then never answers. Without
// a bound this blocks for ever, which in an application is a window frozen on
// Ctrl+V.
func TestLiveX11ClipboardDoesNotHangOnASilentOwner(t *testing.T) {
	skipUnlessX11(t)
	owner, asker := twoWindows(t)

	owner.SetClipboardText("never answered") // claimed, but nothing will pump

	start := time.Now()
	got := asker.ClipboardText()
	elapsed := time.Since(start)

	if got != "" {
		t.Errorf("read %q from an owner that never answered", got)
	}
	if elapsed < clipboardWait {
		t.Errorf("gave up after %v, before the %v it should wait", elapsed, clipboardWait)
	}
	if elapsed > 3*clipboardWait {
		t.Errorf("waited %v on a silent owner; the deadline did not bound it", elapsed)
	}
}

// Events that arrive while a paste is waiting belong to the application. They
// must still be there afterwards -- losing one loses a click.
func TestLiveX11ClipboardKeepsEventsThatArriveDuringAPaste(t *testing.T) {
	skipUnlessX11(t)
	owner, asker := twoWindows(t)

	owner.SetClipboardText("text")
	if a, ok := owner.clipAtoms(); ok {
		who, err := owner.conn.GetSelectionOwner(a.clipboard)
		t.Logf("before the paste: selection owner=%#x ours=%#x err=%v", who, owner.win, err)
	}
	stop := pump(t, owner, 3*time.Second)

	if got := asker.ClipboardText(); got != "text" {
		t.Errorf("paste read %q", got)
	}
	<-stop

	// Whatever the server sent the asker while it waited is still queued, so a
	// subsequent read returns it rather than nothing.
	if ready, supported := asker.conn.WaitReadable(200 * time.Millisecond); supported && ready {
		if _, err := asker.conn.NextEvent(); err != nil {
			t.Errorf("an event the server sent during the paste could not be read back: %v", err)
		}
	}
}
