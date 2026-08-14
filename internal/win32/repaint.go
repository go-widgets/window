// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import "sync/atomic"

// The OS-independent half of the Repainter capability: deciding whether a
// wakeup needs to be posted at all.
//
// It lives in an untagged file, like the rest of this package's decisions, so
// it is exercised on every GOOS rather than only where the DLLs are.

// WMAppRepaint is the private message a repaint asks for. WM_APP (0x8000) and
// above are reserved by Windows for an application's own use, so nothing in the
// system will ever send this to our window.
const WMAppRepaint = 0x8000 // WM_APP

// repaintFlag keeps at most one wakeup in flight.
//
// A caller repainting at 60 Hz against a pump busy with a slow frame would
// otherwise post a message per tick, and on catching up would redraw once per
// queued message — spending the recovery on frames nobody will see. Windows
// coalesces WM_PAINT for us but not WM_APP, so this is ours to do.
type repaintFlag struct{ pending atomic.Bool }

// arm reports whether the caller should post a wakeup: true exactly once until
// take is called.
func (f *repaintFlag) arm() bool { return !f.pending.Swap(true) }

// disarm undoes an arm whose message could not be posted, so a later Repaint is
// not silenced by a wakeup that never left.
func (f *repaintFlag) disarm() { f.pending.Store(false) }

// take marks the wakeup as consumed, which is what lets the next one be posted.
func (f *repaintFlag) take() { f.pending.Store(false) }
