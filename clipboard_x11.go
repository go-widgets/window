// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"time"

	"github.com/go-widgets/window/internal/x11"
)

// The X11 half of the Clipboard capability.
//
// No build tag, like the rest of this package: the X11 protocol is pure Go and
// compiles everywhere, and window.go -- which holds the state below -- is
// untagged too. Only the live proof is linux-only, because only it needs a
// server.
//
// Nothing here resembles the macOS one, and that is the platform's doing rather
// than ours: there is no clipboard object to read and write. A selection is
// owned by a window, and the owner is asked for the text every time somebody
// pastes. So copying claims ownership and then answers questions for as long as
// the application lives, and pasting asks the current owner and waits for the
// answer to come back as an event.
//
// clipboardWait is how long a paste will wait for that answer. The protocol
// offers no timeout of its own: an owner that died between claiming the
// selection and being asked for it simply never replies, and an unbounded wait
// would freeze the window on Ctrl+V with nothing to show for it. A second is far
// longer than a live owner needs and short enough not to read as a hang.
const clipboardWait = time.Second

// clipboardAtoms are the four atoms a text selection needs. They are interned
// once, on first use, because an application that never touches the clipboard
// should not pay for them at startup.
type clipboardAtoms struct {
	clipboard uint32 // CLIPBOARD, the selection Ctrl+C uses (not PRIMARY)
	utf8      uint32 // UTF8_STRING, the target we speak
	targets   uint32 // TARGETS, the "what can you produce?" query
	prop      uint32 // a property on our own window, the mailbox a reply lands in
	ok        bool
}

func (w *Window) clipAtoms() (clipboardAtoms, bool) {
	if w.clip.ok {
		return w.clip, true
	}
	var a clipboardAtoms
	var err error
	if a.clipboard, err = w.conn.InternAtom("CLIPBOARD", false); err != nil {
		return a, false
	}
	if a.utf8, err = w.conn.InternAtom("UTF8_STRING", false); err != nil {
		return a, false
	}
	if a.targets, err = w.conn.InternAtom("TARGETS", false); err != nil {
		return a, false
	}
	if a.prop, err = w.conn.InternAtom("GO_WIDGETS_CLIPBOARD", false); err != nil {
		return a, false
	}
	a.ok = true
	w.clip = a
	return a, true
}

// SetClipboardText claims the CLIPBOARD selection and remembers the text, which
// is all copying is here. The text is handed out later, one requestor at a
// time, by answerSelectionRequest.
//
// Implements the Clipboard capability.
func (w *Window) SetClipboardText(text string) {
	a, ok := w.clipAtoms()
	if !ok {
		return
	}
	w.clipText = text
	if err := w.conn.SetSelectionOwner(w.win, a.clipboard, x11.CurrentTime); err != nil {
		return
	}
	w.clipOwned = true
}

// ClipboardText asks the current owner for the selection and waits for it.
//
// Implements the Clipboard capability.
func (w *Window) ClipboardText() string {
	a, ok := w.clipAtoms()
	if !ok {
		return ""
	}
	// Our own text, without a round trip through the server: an owner is not
	// required to answer itself, and asking would deadlock a single-threaded
	// application against its own event loop.
	if w.clipOwned {
		return w.clipText
	}
	owner, err := w.conn.GetSelectionOwner(a.clipboard)
	if err != nil || owner == 0 {
		return "" // nothing has been copied in this session
	}
	if err := w.conn.ConvertSelection(w.win, a.clipboard, a.utf8, a.prop, x11.CurrentTime); err != nil {
		return ""
	}
	if !w.awaitSelection(a) {
		return ""
	}
	_, format, data, err := w.conn.GetProperty(w.win, a.prop, 0, true, 1<<16)
	if err != nil || format != 8 {
		return ""
	}
	return string(data)
}

// awaitSelection reads events until the SelectionNotify for our request
// arrives, and reports whether it brought anything.
//
// Two rules keep this from breaking the application around it. Events that are
// not the reply are pushed BACK, because they belong to the widget tree and
// this is not the place to dispatch them -- doing so would re-enter the tree
// from inside a paste. And a SelectionRequest IS answered here, because an
// owner that stops answering while it pastes deadlocks against whoever it is
// pasting from.
func (w *Window) awaitSelection(a clipboardAtoms) bool {
	bounded := w.conn.SetReadDeadline(time.Now().Add(clipboardWait))
	if bounded {
		defer w.conn.SetReadDeadline(time.Time{})
	}
	var deferred []x11.Event
	defer func() {
		for i := len(deferred) - 1; i >= 0; i-- {
			w.conn.PushEvent(deferred[i])
		}
	}()
	for {
		ev, err := w.conn.NextEvent()
		if err != nil {
			return false // including the deadline: a dead owner is not a hang
		}
		switch ev.Code {
		case xcodeSelectionNotify:
			return ev.Property != 0 // 0 is a refusal, and an empty string is right
		case xcodeSelectionRequest:
			w.answerSelectionRequest(ev, a)
		default:
			deferred = append(deferred, ev)
		}
	}
}

// answerSelectionRequest hands our text to a requestor, or refuses.
//
// A refusal is a SelectionNotify naming property 0. It matters that it is sent:
// a requestor that gets no reply at all can only wait for its own timeout, so
// silence turns "I cannot give you that" into a second of somebody else's
// window not responding.
func (w *Window) answerSelectionRequest(ev x11.Event, a clipboardAtoms) {
	prop := ev.Property
	if prop == 0 {
		prop = ev.Target // an obsolete requestor means "same name as the target"
	}
	switch {
	case ev.Target == a.targets:
		// What we can produce, as an atom list: the query itself, and UTF-8.
		list := make([]byte, 8)
		w.conn.Order().PutUint32(list[0:4], a.targets)
		w.conn.Order().PutUint32(list[4:8], a.utf8)
		if err := w.conn.ChangeProperty(ev.Requestor, prop, x11.AtomAtom, 32, 2, list); err != nil {
			prop = 0
		}
	case ev.Target == a.utf8 || ev.Target == x11.AtomString:
		text := []byte(w.clipText)
		if err := w.conn.ChangeProperty(ev.Requestor, prop, ev.Target, 8, len(text), text); err != nil {
			prop = 0
		}
	default:
		prop = 0 // a target we cannot produce
	}
	_ = w.conn.SendSelectionNotify(ev.Requestor, ev.Selection, ev.Target, prop, ev.Time)
}

// handleSelectionEvent deals with the two selection events that reach the main
// loop, and reports whether it consumed the event.
//
// Losing the selection is not an error: somebody else copied. Dropping the text
// then is what stops us answering with something the user replaced long ago.
func (w *Window) handleSelectionEvent(ev x11.Event) bool {
	a, ok := w.clipAtoms()
	if !ok {
		return false
	}
	switch ev.Code {
	case xcodeSelectionRequest:
		w.answerSelectionRequest(ev, a)
		return true
	case xcodeSelectionClear:
		w.clipOwned, w.clipText = false, ""
		return true
	}
	return false
}
