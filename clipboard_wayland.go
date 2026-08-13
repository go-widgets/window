// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"io"
	"os"
	"time"

	"github.com/go-widgets/window/internal/wayland"
)

// The Wayland half of the Clipboard capability.
//
// Copying hands the compositor a SOURCE and waits to be asked; pasting is
// handed an OFFER and reads the other application's bytes out of a pipe. The
// data never passes through the compositor, so both halves are file descriptors
// — which is why this is a pipe and a deadline rather than a get and a set.
//
// A compositor with no wl_data_device_manager has no clipboard to offer. That is
// a fact about the session, not a failure: the capability answers "" and writing
// does nothing, exactly as it would on a back-end that does not implement
// Clipboard at all.

// clipMimeUTF8 is the type everything modern agrees on; clipMimePlain is the
// fallback older clients offer. They are tried in this order, which is the
// order of preference the protocol gives them.
const (
	clipMimeUTF8  = "text/plain;charset=utf-8"
	clipMimePlain = "text/plain"
)

// clipboardReadWait bounds a paste.
//
// The bytes come from ANOTHER application over a pipe: one that has died, hung,
// or simply never answers leaves the read outstanding for ever, and an
// unbounded wait here is a window frozen on Ctrl+V. The same reasoning as the
// X11 side, for the same reason, on a different mechanism.
//
// A var so the test for that case can prove the bound holds without spending a
// second of the suite's time waiting for a peer that was never going to answer.
var clipboardReadWait = time.Second

// clipPipe is os.Pipe, named so the one failure that cannot be provoked on a
// healthy machine — the process being out of descriptors — can still be shown to
// return no text rather than to panic on a nil file.
var clipPipe = os.Pipe

// clipboard lazily binds the data-device manager and this seat's device.
//
// It is lazy because an application that never touches the clipboard should not
// pay for the objects, and because a compositor without the global should cost
// one failed lookup rather than a failed window.
func (w *wlWindow) clipboard() (*wayland.DataDevice, bool) {
	if w.dataDev != nil {
		return w.dataDev, true
	}
	if w.clipUnavailable || w.seat == nil || w.registry == nil {
		return nil, false
	}
	m, err := w.registry.DataDeviceManager()
	if err != nil {
		w.clipUnavailable = true // the compositor will not grow one while we run
		return nil, false
	}
	w.dataDev = m.GetDevice(w.seat)
	w.ddm = m
	return w.dataDev, true
}

// SetClipboardText offers text to the rest of the session.
//
// Implements the Clipboard capability.
func (w *wlWindow) SetClipboardText(text string) {
	dev, ok := w.clipboard()
	if !ok {
		return
	}
	// The previous source is done: a client may own the selection through one
	// source at a time, and leaving the old one registered leaves its Send
	// callback alive holding the old text.
	if w.clipSource != nil {
		_ = w.clipSource.Destroy()
	}
	src := w.ddm.CreateSource()
	w.clipSource, w.clipText, w.clipOwned = src, text, true

	src.Send = func(mime string, fd int) {
		// text is captured, not read back off the window: this source promised
		// THIS string, and a paste in flight when the user copies something else
		// must still deliver what it was offered.
		//
		// The descriptor is ours to write and ours to close. A paster is blocked
		// on a read that ends only when this end closes, so the close is not
		// tidiness -- it is the end of the message.
		f := os.NewFile(uintptr(fd), "wl-clipboard-send")
		_, _ = io.WriteString(f, text)
		_ = f.Close()
	}
	src.Cancelled = func() {
		// Somebody else copied. What we were holding is no longer the
		// clipboard, and saying otherwise would hand out text the user
		// replaced.
		w.clipOwned, w.clipText = false, ""
		_ = src.Destroy()
		if w.clipSource == src {
			w.clipSource = nil
		}
	}

	// Declared in order of preference: the first type both sides understand is
	// the one that gets used.
	for _, mime := range []string{clipMimeUTF8, clipMimePlain} {
		if err := src.Offer(mime); err != nil {
			return // the connection is gone; this source will never be asked
		}
	}
	_ = dev.SetSelection(src)
}

// ClipboardText reads whatever the session has on the clipboard.
//
// Implements the Clipboard capability.
func (w *wlWindow) ClipboardText() string {
	dev, ok := w.clipboard()
	if !ok {
		return ""
	}
	// Our own text, without going near the compositor. Asking would have it ask
	// US, through an event we could only answer by dispatching -- and we are
	// the goroutine that dispatches, so it would be a deadlock with itself.
	if w.clipOwned {
		return w.clipText
	}
	off := dev.Selection()
	if off == nil {
		return "" // nothing copied in this session, or nothing we can read
	}
	mime, ok := pickTextMime(off.Mimes())
	if !ok {
		return "" // an image or a file list: nothing this can offer
	}

	r, wr, err := clipPipe()
	if err != nil {
		return ""
	}
	defer r.Close()
	if err := off.Receive(mime, int(wr.Fd())); err != nil {
		wr.Close()
		return ""
	}
	// OUR copy of the write end must go now: the read below ends when every
	// writer is closed, and holding one open would make it wait for ourselves.
	wr.Close()

	_ = r.SetReadDeadline(time.Now().Add(clipboardReadWait))
	b, err := io.ReadAll(r)
	if err != nil && len(b) == 0 {
		return "" // a peer that never answered is not a hang
	}
	return string(b)
}

// pickTextMime chooses the best text type an offer advertises.
//
// UTF-8 first because it is unambiguous; text/plain only as a fallback, since
// its encoding is whatever the other application felt like. Anything else is
// not text and is not guessed at.
func pickTextMime(mimes []string) (string, bool) {
	for _, want := range []string{clipMimeUTF8, clipMimePlain} {
		for _, m := range mimes {
			if m == want {
				return m, true
			}
		}
	}
	return "", false
}
