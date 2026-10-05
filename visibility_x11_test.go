// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-widgets/window/internal/x11"
)

// x11Request is one request as it went out on the wire.
type x11Request struct {
	op   byte
	body []byte // everything after the 4-byte header
}

// requests splits what a window wrote into its requests.
func requests(b []byte) []x11Request {
	var out []x11Request
	for len(b) >= 4 {
		n := int(le.Uint16(b[2:4])) * 4
		if n < 4 || n > len(b) {
			break
		}
		out = append(out, x11Request{op: b[0], body: b[4:n]})
		b = b[n:]
	}
	return out
}

func opsOf(rs []x11Request) []byte {
	ops := make([]byte, len(rs))
	for i, r := range rs {
		ops[i] = r.op
	}
	return ops
}

const (
	xMapWindow       = 8
	xUnmapWindow     = 10
	xConfigureWindow = 12
	xSendEvent       = 25
)

// TestX11HideShowRaiseSpeakToTheWindowManager (go-widgets/window#134).
//
// A tray application's "Open" has to bring back the window that is already
// there, and closing it has to be able to leave the application in the tray;
// before this there was no way to do either, and an application found its own
// window through the platform to do it. What a window manager is told is the
// whole of the feature, so the requests are asserted one by one.
func TestX11HideShowRaiseSpeakToTheWindowManager(t *testing.T) {
	w, ft := dialFake(t, Config{Title: "T", Width: 40, Height: 30})
	var b Backend = w
	if _, ok := b.(Visibility); !ok {
		t.Fatal("the X11 window does not implement Visibility")
	}
	if w.netActiveAtom != atomNetActive {
		t.Fatalf("_NET_ACTIVE_WINDOW = %#x, want it interned at bring-up (%#x)", w.netActiveAtom, atomNetActive)
	}
	const root = 0x123 // setupReply's root window

	ft.out.Reset()
	if err := Hide(b); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	rs := requests(ft.out.Bytes())
	if !bytes.Equal(opsOf(rs), []byte{xUnmapWindow, xSendEvent}) {
		t.Fatalf("Hide sent ops %v, want UnmapWindow then the synthetic UnmapNotify", opsOf(rs))
	}
	if got := le.Uint32(rs[0].body); got != w.win {
		t.Errorf("Hide unmapped %#x, want our window %#x", got, w.win)
	}
	if dest, ev := le.Uint32(rs[1].body[0:4]), rs[1].body[8:]; dest != root || ev[0] != 18 || le.Uint32(ev[8:12]) != w.win {
		t.Errorf("Hide told %#x about event %d for %#x, want the root told UnmapNotify for ours", dest, ev[0], le.Uint32(ev[8:12]))
	}

	ft.out.Reset()
	if err := Show(b); err != nil {
		t.Fatalf("Show: %v", err)
	}
	rs = requests(ft.out.Bytes())
	if !bytes.Equal(opsOf(rs), []byte{xMapWindow}) || le.Uint32(rs[0].body) != w.win {
		t.Fatalf("Show sent %v, want one MapWindow of our window", opsOf(rs))
	}

	ft.out.Reset()
	if err := Raise(b); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	rs = requests(ft.out.Bytes())
	if !bytes.Equal(opsOf(rs), []byte{xMapWindow, xConfigureWindow, xSendEvent}) {
		t.Fatalf("Raise sent ops %v, want MapWindow, ConfigureWindow, _NET_ACTIVE_WINDOW", opsOf(rs))
	}
	ev := rs[2].body[8:]
	if le.Uint32(rs[2].body[0:4]) != root || ev[0] != 33 || le.Uint32(ev[4:8]) != w.win || le.Uint32(ev[8:12]) != atomNetActive {
		t.Errorf("Raise's client message = dest %#x code %d window %#x type %#x, want _NET_ACTIVE_WINDOW for ours at the root",
			le.Uint32(rs[2].body[0:4]), ev[0], le.Uint32(ev[4:8]), le.Uint32(ev[8:12]))
	}
}

// A server that would not intern _NET_ACTIVE_WINDOW still gets the stacking
// request, which is what a window manager without EWMH honours.
func TestX11RaiseWithoutEWMHStillStacks(t *testing.T) {
	w, ft := dialFake(t, Config{Width: 40, Height: 30})
	w.netActiveAtom = 0
	ft.out.Reset()
	if err := w.Raise(); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if ops := opsOf(requests(ft.out.Bytes())); !bytes.Equal(ops, []byte{xMapWindow, xConfigureWindow}) {
		t.Errorf("Raise without the atom sent %v, want MapWindow then ConfigureWindow", ops)
	}
}

func TestX11VisibilityAfterCloseIsAnError(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 40, Height: 30})
	_ = w.Close()
	for name, call := range map[string]func() error{"Show": w.Show, "Hide": w.Hide, "Raise": w.Raise} {
		if err := call(); !errors.Is(err, ErrClosed) {
			t.Errorf("%s after Close = %v, want ErrClosed", name, err)
		}
	}
}

func TestX11VisibilityReportsAWriteError(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Window) error
		// after is how many requests succeed before the write starts failing.
		after int
	}{
		{"Show", (*Window).Show, 0},
		{"Hide", (*Window).Hide, 0},
		{"Raise/map", (*Window).Raise, 0},
		{"Raise/stack", (*Window).Raise, 1},
		{"Raise/activate", (*Window).Raise, 2},
	} {
		fw := &countingFailWriter{in: bytes.NewReader(serverScript())}
		conn, err := x11.Handshake(fw, le, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		w, err := newWindow(conn, Config{Width: 40, Height: 30})
		if err != nil {
			t.Fatal(err)
		}
		fw.okLeft, fw.armed = tc.after, true
		if err := tc.call(w); err == nil {
			t.Errorf("%s: a failed write was reported as success", tc.name)
		}
	}
}

// countingFailWriter lets okLeft writes through once armed, then fails.
type countingFailWriter struct {
	in     *bytes.Reader
	armed  bool
	okLeft int
}

func (f *countingFailWriter) Read(p []byte) (int, error) { return f.in.Read(p) }
func (f *countingFailWriter) Write(p []byte) (int, error) {
	if f.armed {
		if f.okLeft == 0 {
			return 0, errors.New("injected write failure")
		}
		f.okLeft--
	}
	return len(p), nil
}
func (f *countingFailWriter) Close() error { return nil }

// A back-end without the capability says so, by name, rather than doing
// nothing: a tray item that silently fails to bring a window back is the bug
// this API exists to end.
func TestVisibilityOnABackendWithoutIt(t *testing.T) {
	b := nopVisibilityBackend{}
	for name, call := range map[string]func(Backend) error{"Show": Show, "Hide": Hide, "Raise": Raise} {
		err := call(b)
		if !errors.Is(err, ErrNotSupported) {
			t.Errorf("%s = %v, want ErrNotSupported", name, err)
		}
	}
}

type nopVisibilityBackend struct{ Backend }

func (nopVisibilityBackend) String() string { return "nop" }
