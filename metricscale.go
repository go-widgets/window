// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "github.com/go-widgets/toolkit"

// Telling the toolkit what a point is worth.
//
// A back-end that honours [NativeScale] hands the widget tree a framebuffer at
// the panel's own resolution -- twice the pixels on the usual laptop. The tree
// lays out in those pixels, so unless something tells it that a point is now two
// of them, every padding, radius, border and control stays at its logical size
// and the whole interface comes out half as large on the sharpest screens. The
// window is crisp and the UI is tiny, which is worse than not having asked.
//
// The toolkit's [toolkit.SetMetricScale] is the documented knob for exactly
// this, and until now nothing called it. The back-end is the only party that
// knows the scale, so the back-end sets it.
//
// It is a package-level setting, which is right for what it describes: a process
// draws one surface at a time on its main thread, and the scale is a property of
// the screen that surface is on. It is set only when the caller asked for
// NativeScale -- a window that never asked leaves it alone, so an application
// that was not thinking about DPI is byte-for-byte unchanged.

// applyMetricScale tells the toolkit how many framebuffer pixels a logical point
// is worth, for a window that asked for the panel's own resolution.
//
// Non-positive scales are ignored by the toolkit itself, and a scale of one is
// what it already holds, so both are harmless; passing them keeps every caller a
// single line.
func applyMetricScale(requested, actual float64) {
	if requested != NativeScale {
		return
	}
	toolkit.SetMetricScale(actual)
}
