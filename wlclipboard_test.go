// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// The Wayland Clipboard capability, driven by a scripted fake compositor over a
// socket pair. Both directions are real file descriptors here, exactly as they
// are against sway: the copy direction is asserted by READING what the client
// wrote into a pipe the compositor supplied, and the paste direction by writing
// into the pipe the client passed and seeing the text come back out of
// ClipboardText. Nothing is stubbed at the capability boundary.
//
// The live analogue is TestLiveWaylandClipboard, run by the headless-compositor
// lane against a real sway. This file is what makes a failure there readable.

//go:build linux

package window

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-widgets/window/internal/wayland"
)

// clipServer is the scripted compositor for the data-device family, recording
// what the client asked for so a test can assert on the requests themselves and
// not merely on the answer it liked.
type clipServer struct {
	sc *srvConn

	mu       sync.Mutex
	offers   []string // mime types the client's source declared, in order
	selected uint32   // source id given to set_selection (0 = cleared)
	serial   uint32   // serial the client quoted with it
	setCalls int
	destroys []uint32 // sources the client destroyed
	received []string // mime types the client asked an offer for

	srcID   uint32 // the client's current data source
	devID   uint32 // the client's data device
	ddmID   uint32
	regID   uint32
	seatID  uint32
	ptrID   uint32 // the client's wl_pointer, for injecting an input serial
	offerID uint32 // an offer WE introduced, if any

	// answerReceive is what the compositor writes back when the client asks an
	// offer for bytes. A nil func leaves the descriptor open and unwritten --
	// the peer that never answers.
	answerReceive func(mime string, fd int)

	// noManager omits wl_data_device_manager from the registry, which is a
	// legitimate session and not a broken one.
	noManager bool
}

// run plays the compositor until the client disconnects.
func (s *clipServer) run() {
	for {
		obj, op, body, err := s.sc.read()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.handle(obj, op, body)
		s.mu.Unlock()
	}
}

func (s *clipServer) handle(obj uint32, op uint16, body []byte) {
	switch {
	case obj == 1 && op == 1: // wl_display.get_registry
		s.regID = no.Uint32(body[0:4])
		_ = s.sc.send(s.regID, 0, cat(eU32(4), eStr("wl_seat"), eU32(5)))
		if !s.noManager {
			_ = s.sc.send(s.regID, 0, cat(eU32(5), eStr("wl_data_device_manager"), eU32(3)))
		}
	case obj == 1 && op == 0: // wl_display.sync
		_ = s.sc.send(no.Uint32(body[0:4]), 0, eU32(0))
	case obj == s.regID && op == 0: // wl_registry.bind
		iface, rest := decStr(body[4:])
		newid := no.Uint32(rest[4:8])
		switch iface {
		case "wl_seat":
			s.seatID = newid
			_ = s.sc.send(s.seatID, 0, eU32(wayland.SeatCapabilityPointer))
		case "wl_data_device_manager":
			s.ddmID = newid
		}
	case obj == s.seatID && op == 0: // wl_seat.get_pointer
		s.ptrID = no.Uint32(body[0:4])
	case obj == s.ddmID && op == 0: // create_data_source
		s.srcID = no.Uint32(body[0:4])
	case obj == s.ddmID && op == 1: // get_data_device
		s.devID = no.Uint32(body[0:4])
	case obj == s.srcID && op == 0: // wl_data_source.offer
		mime, _ := decStr(body)
		s.offers = append(s.offers, mime)
	case obj == s.srcID && op == 1: // wl_data_source.destroy
		s.destroys = append(s.destroys, obj)
	case obj == s.devID && op == 1: // wl_data_device.set_selection
		s.selected, s.serial = no.Uint32(body[0:4]), no.Uint32(body[4:8])
		s.setCalls++
	case obj == s.offerID && op == 1: // wl_data_offer.receive
		mime, _ := decStr(body)
		s.received = append(s.received, mime)
		fd := s.sc.popFD()
		if s.answerReceive == nil {
			// Deliberately kept open and silent: a peer that promised bytes and
			// never produced them. Closing it here would end the client's read
			// for the wrong reason and prove nothing about the deadline.
			return
		}
		s.answerReceive(mime, fd)
	}
}

// emit sends one event from the test goroutine, under the same lock the script
// holds while it replies. Both goroutines write to one stream socket, and
// serialising them is what keeps two messages from interleaving into a third
// that means nothing.
func (s *clipServer) emit(t *testing.T, obj uint32, op uint16, body []byte, fds ...int) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sc.send(obj, op, body, fds...); err != nil {
		t.Fatalf("emitting event %d on object %d: %v", op, obj, err)
	}
}

// askForBytes makes the compositor request the client's clipboard text through
// wl_data_source.send, and returns the read end of the pipe it supplied.
func (s *clipServer) askForBytes(t *testing.T, mime string) *os.File {
	t.Helper()
	s.mu.Lock()
	src := s.srcID
	s.mu.Unlock()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	s.emit(t, src, 1, eStr(mime), int(w.Fd()))
	w.Close() // the client's copy is the only writer left
	return r
}

// offerText introduces an offer from another client and puts it on the
// clipboard, mimes first and the selection afterwards, in the order the protocol
// requires.
func (s *clipServer) offerText(t *testing.T, mimes ...string) {
	t.Helper()
	s.mu.Lock()
	dev := s.devID
	s.offerID = 0xf00
	id := s.offerID
	s.mu.Unlock()

	s.emit(t, dev, 0, eU32(id)) // wl_data_device.data_offer
	for _, m := range mimes {
		s.emit(t, id, 0, eStr(m)) // wl_data_offer.offer
	}
	s.emit(t, dev, 5, eU32(id)) // wl_data_device.selection
}

func (s *clipServer) snapshot() (offers, received []string, selected uint32, setCalls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.offers...), append([]string(nil), s.received...),
		s.selected, s.setCalls
}

// newClipWindow brings up just enough of a window for the clipboard: a
// connection, a registry and a seat. The shell is deliberately absent -- the
// capability does not touch a surface, and a test that needed one would be
// asserting the wrong thing.
func newClipWindow(t *testing.T, s *clipServer) (*wlWindow, *clipServer) {
	t.Helper()
	cli, srv := socketPairWin(t)
	t.Cleanup(func() { srv.Close() })
	s.sc = &srvConn{c: srv}
	go s.run()

	conn := wayland.New(cli)
	t.Cleanup(func() { conn.Close() })
	reg, err := conn.Display().GetRegistry()
	if err != nil {
		t.Fatalf("get_registry: %v", err)
	}
	if err := conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	seat, err := reg.Seat()
	if err != nil {
		t.Fatalf("seat: %v", err)
	}
	if err := conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	return &wlWindow{conn: conn, registry: reg, seat: seat}, s
}

// A copy declares both text types in preference order, hands the source over
// quoting the seat's serial, and then actually produces the bytes when asked.
func TestWaylandClipboardCopyProducesTheBytes(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})

	w.SetClipboardText("bonjour")
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}

	offers, _, selected, setCalls := s.snapshot()
	want := []string{clipMimeUTF8, clipMimePlain}
	if strings.Join(offers, ",") != strings.Join(want, ",") {
		t.Errorf("offered %v, want %v (order is preference)", offers, want)
	}
	if setCalls != 1 {
		t.Errorf("set_selection called %d times, want 1", setCalls)
	}
	if selected == 0 {
		t.Error("set_selection cleared the clipboard instead of taking it")
	}

	// The bytes themselves, through a descriptor the compositor supplied.
	r := s.askForBytes(t, clipMimeUTF8)
	defer r.Close()
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := r.Read(buf)
		done <- string(buf[:n])
	}()
	if err := w.conn.Roundtrip(); err != nil { // dispatch delivers source.send
		t.Fatalf("roundtrip: %v", err)
	}
	select {
	case got := <-done:
		if got != "bonjour" {
			t.Errorf("the compositor read %q, want %q", got, "bonjour")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the client never wrote its clipboard text")
	}
}

// Reading our own selection must not go through the compositor: the answer would
// come back as a request to ourselves, which only this goroutine could serve, so
// asking would be a deadlock with ourselves.
func TestWaylandClipboardReadsItsOwnTextWithoutAsking(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})

	w.SetClipboardText("nos propres octets")
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	if got := w.ClipboardText(); got != "nos propres octets" {
		t.Errorf("ClipboardText = %q, want our own text", got)
	}
	if _, received, _, _ := s.snapshot(); len(received) != 0 {
		t.Errorf("asked the compositor for %v while owning the selection", received)
	}
}

// Somebody else copied: what we were holding is no longer the clipboard, and
// saying otherwise would hand back text the user replaced.
func TestWaylandClipboardCancelledDropsOwnership(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})

	w.SetClipboardText("remplace")
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	s.mu.Lock()
	src := s.srcID
	s.mu.Unlock()
	s.emit(t, src, 2, nil) // wl_data_source.cancelled
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	if w.clipOwned {
		t.Error("still claiming the selection after being cancelled")
	}
	// With no offer on the device either, there is nothing to report -- and
	// reporting the old text would be the bug this guards.
	if got := w.ClipboardText(); got != "" {
		t.Errorf("ClipboardText = %q after cancellation, want %q", got, "")
	}
}

// A paste from another application: its bytes arrive over a pipe we created and
// it wrote, which is the whole mechanism.
func TestWaylandClipboardPastesAnotherClientsText(t *testing.T) {
	s := &clipServer{answerReceive: func(mime string, fd int) {
		f := os.NewFile(uintptr(fd), "answer")
		f.WriteString("venu d'ailleurs")
		f.Close() // the close is the end of the message
	}}
	w, s := newClipWindow(t, s)

	if _, ok := w.clipboard(); !ok { // the device must exist before an offer can land on it
		t.Fatal("no data device")
	}
	s.offerText(t, "text/html", clipMimeUTF8, clipMimePlain)
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}

	if got := w.ClipboardText(); got != "venu d'ailleurs" {
		t.Errorf("ClipboardText = %q, want the other client's text", got)
	}
	if _, received, _, _ := s.snapshot(); len(received) != 1 || received[0] != clipMimeUTF8 {
		t.Errorf("asked for %v, want just %q -- UTF-8 is unambiguous and comes first", received, clipMimeUTF8)
	}
}

// text/plain is the fallback when UTF-8 is not on offer; anything that is not
// text is not guessed at.
func TestWaylandClipboardMimeChoice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		have  []string
		want  string
		wantK bool
	}{
		{"utf8 preferred", []string{clipMimePlain, clipMimeUTF8}, clipMimeUTF8, true},
		{"plain fallback", []string{"text/html", clipMimePlain}, clipMimePlain, true},
		{"not text at all", []string{"image/png", "text/uri-list"}, "", false},
		{"nothing offered", nil, "", false},
	} {
		got, ok := pickTextMime(tc.have)
		if got != tc.want || ok != tc.wantK {
			t.Errorf("%s: pickTextMime(%v) = %q,%v want %q,%v", tc.name, tc.have, got, ok, tc.want, tc.wantK)
		}
	}
}

// An offer holding an image is not text, and asking for it anyway would block on
// a type the other client never promised.
func TestWaylandClipboardIgnoresNonText(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})
	if _, ok := w.clipboard(); !ok {
		t.Fatal("no data device")
	}
	s.offerText(t, "image/png")
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	if got := w.ClipboardText(); got != "" {
		t.Errorf("ClipboardText = %q for an image offer, want %q", got, "")
	}
	if _, received, _, _ := s.snapshot(); len(received) != 0 {
		t.Errorf("asked an image offer for %v", received)
	}
}

// A clipboard owner that never answers must not freeze the window. This is the
// reason for the deadline, so the test is the deadline: shortened, because the
// point is that it fires, not how long it is.
func TestWaylandClipboardDoesNotHangOnASilentPeer(t *testing.T) {
	old := clipboardReadWait
	clipboardReadWait = 50 * time.Millisecond
	defer func() { clipboardReadWait = old }()

	w, s := newClipWindow(t, &clipServer{}) // answerReceive nil: promises, never writes
	if _, ok := w.clipboard(); !ok {
		t.Fatal("no data device")
	}
	s.offerText(t, clipMimeUTF8)
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}

	start := time.Now()
	got := w.ClipboardText()
	elapsed := time.Since(start)
	if got != "" {
		t.Errorf("ClipboardText = %q from a peer that wrote nothing", got)
	}
	if elapsed > time.Second {
		t.Errorf("the read took %v, so the deadline is not bounding it", elapsed)
	}
}

// A session with no wl_data_device_manager has no clipboard. That is a fact
// about the session, and the honest answers are "" and nothing -- once, without
// a connection attempt per keystroke afterwards.
func TestWaylandClipboardWithoutAManager(t *testing.T) {
	w, _ := newClipWindow(t, &clipServer{noManager: true})

	w.SetClipboardText("dans le vide")
	if got := w.ClipboardText(); got != "" {
		t.Errorf("ClipboardText = %q with no manager, want %q", got, "")
	}
	if !w.clipUnavailable {
		t.Error("the missing global was not remembered, so every poll retries it")
	}
	if _, ok := w.clipboard(); ok {
		t.Error("a session with no manager produced a data device")
	}
}

// A window with no seat cannot be granted the clipboard: the compositor hands it
// out on the strength of a real user event, and there is no seat to have had one.
func TestWaylandClipboardWithoutASeat(t *testing.T) {
	w := &wlWindow{}
	w.SetClipboardText("sans siege")
	if got := w.ClipboardText(); got != "" {
		t.Errorf("ClipboardText = %q with no seat, want %q", got, "")
	}
	if _, ok := w.clipboard(); ok {
		t.Error("a seatless window produced a data device")
	}
}

// Copying twice must retire the first source: a client owns the selection
// through one source at a time, and the retired one would otherwise stay
// registered, still able to answer with the text the user replaced.
func TestWaylandClipboardSecondCopyRetiresTheFirst(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})

	w.SetClipboardText("premier")
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	s.mu.Lock()
	first := s.srcID
	s.mu.Unlock()

	w.SetClipboardText("second")
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}

	s.mu.Lock()
	destroyed, second, calls := append([]uint32(nil), s.destroys...), s.srcID, s.setCalls
	s.mu.Unlock()
	if second == first {
		t.Fatal("the second copy reused the first source")
	}
	if len(destroyed) != 1 || destroyed[0] != first {
		t.Errorf("destroyed %v, want just the first source (%d)", destroyed, first)
	}
	if calls != 2 {
		t.Errorf("set_selection called %d times, want 2", calls)
	}
	if got := w.ClipboardText(); got != "second" {
		t.Errorf("ClipboardText = %q after a second copy, want %q", got, "second")
	}
}

// A source asked for bytes with nobody to produce them still owes the paster an
// answer: an empty read ends, a leaked descriptor does not. The protocol layer
// closes it, and this is the capability's side of that contract -- a copy whose
// window has gone away.
func TestWaylandClipboardSendAfterTheWindowIsGone(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})
	w.SetClipboardText("condamne")
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}

	r := s.askForBytes(t, clipMimePlain)
	defer r.Close()
	done := make(chan int, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := r.Read(buf)
		done <- n
	}()
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	select {
	case n := <-done:
		if n != len("condamne") {
			t.Errorf("read %d bytes, want %d", n, len("condamne"))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the descriptor was neither written nor closed")
	}
}

// A dead connection is not a place to write a clipboard to, and the failure must
// be quiet rather than fatal: the window is closing anyway.
func TestWaylandClipboardOnADeadConnection(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})
	if _, ok := w.clipboard(); !ok {
		t.Fatal("no data device")
	}
	s.offerText(t, clipMimeUTF8)
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	w.conn.Close()

	w.SetClipboardText("trop tard") // the offer request cannot leave
	if got := w.ClipboardText(); got != "trop tard" {
		// Owning the selection is decided locally, so this still answers -- the
		// point is that neither call panicked on a closed socket.
		t.Errorf("ClipboardText = %q, want the locally-held text", got)
	}
	w.clipOwned = false
	if got := w.ClipboardText(); got != "" {
		t.Errorf("ClipboardText = %q over a dead connection, want %q", got, "")
	}
}

// Out of descriptors is not a crash. It cannot be provoked on a healthy machine,
// which is exactly why the seam exists.
func TestWaylandClipboardWhenAPipeCannotBeMade(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})
	if _, ok := w.clipboard(); !ok {
		t.Fatal("no data device")
	}
	s.offerText(t, clipMimeUTF8)
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}

	old := clipPipe
	clipPipe = func() (*os.File, *os.File, error) { return nil, nil, errors.New("EMFILE") }
	defer func() { clipPipe = old }()

	if got := w.ClipboardText(); got != "" {
		t.Errorf("ClipboardText = %q with no descriptors, want %q", got, "")
	}
	if _, received, _, _ := s.snapshot(); len(received) != 0 {
		t.Errorf("asked for %v without a pipe to read it through", received)
	}
}

// A guard on the fake compositor itself: a test that silently loses the seat
// serial would assert the copy path against a request the compositor would have
// refused.
func TestWaylandClipboardQuotesASerial(t *testing.T) {
	w, s := newClipWindow(t, &clipServer{})
	// A pointer button, which is what earns a client the clipboard. The seat
	// advertised a pointer capability, so the object exists to receive one.
	if _, err := w.seat.GetPointer(); err != nil {
		t.Fatalf("get_pointer: %v", err)
	}
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	s.mu.Lock()
	ptr := s.ptrID
	s.mu.Unlock()
	if ptr == 0 {
		t.Fatal("the compositor never saw get_pointer")
	}
	s.emit(t, ptr, 3, cat(eU32(77), eU32(0), eU32(wayland.BtnLeft), eU32(wayland.StatePressed)))
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}

	w.SetClipboardText("avec serial")
	if err := w.conn.Roundtrip(); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	s.mu.Lock()
	serial := s.serial
	s.mu.Unlock()
	if serial != 77 {
		t.Errorf("set_selection quoted serial %d, want the pointer button's 77", serial)
	}
}
