// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// collRoot places each child at a fixed, generous rectangle: a table is no use
// at the 200x24 the stacking nativeRoot gives a field.
type collRoot struct {
	toolkit.Base
	kids  []toolkit.Widget
	rects []toolkit.Rect
}

func (r *collRoot) SetBounds(b toolkit.Rect) {
	r.Base.SetBounds(b)
	for i, k := range r.kids {
		k.SetBounds(r.rects[i])
	}
}
func (r *collRoot) Draw(p painter.Painter, th *toolkit.Theme) {
	for _, k := range r.kids {
		k.Draw(p, th)
	}
}
func (r *collRoot) Children() []toolkit.Widget { return r.kids }

func bigRows(n int) [][]string {
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = []string{"row " + strconv.Itoa(i), strconv.Itoa(i * 7), "x"}
	}
	return rows
}

// TestLiveCocoaCollections is the on-device proof of NativeTable, NativeTree and
// NativeTabs: real NSTableView, NSOutlineView and NSTabView embedded over the
// framebuffer, the selection bound both ways, and a 10,000-row update measured.
// WINDOW_COCOA_HOLD=<seconds> keeps the window open at the end so the
// accessibility tree can be listed from outside, and what a person (or System
// Events) selected in that time is reported back through the models.
func TestLiveCocoaCollections(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live macOS collections proof")
	}
	hold, _ := strconv.ParseFloat(os.Getenv("WINDOW_COCOA_HOLD"), 64)

	var (
		win      *Window
		setupErr error
		failures []string
	)
	fail := func(format string, a ...any) { failures = append(failures, format); t.Errorf(format, a...) }

	callOnMain(func() {
		win, setupErr = New("go-widgets/window collections proof", 460, 520, toolkit.DefaultDark())
		if setupErr != nil {
			return
		}
		table := toolkit.NewNativeTable([]string{"Name", "Size", "Kind"}, bigRows(50), 2)
		table.Key = "table"
		tree := toolkit.NewNativeTree([]toolkit.NativeTreeNode{
			{Title: "Documents", Expanded: true, Children: []toolkit.NativeTreeNode{{Title: "Letters"}, {Title: "Reports"}}},
			{Title: "Music", Children: []toolkit.NativeTreeNode{{Title: "Jazz"}}},
		}, "0.1")
		tree.Key = "tree"
		tabs := toolkit.NewNativeTabs([]string{"General", "Advanced", "About"}, 1)
		tabs.Key = "tabs"
		root := &collRoot{
			kids: []toolkit.Widget{table, tree, tabs},
			rects: []toolkit.Rect{
				{X: 10, Y: 10, W: 440, H: 200},
				{X: 10, Y: 220, W: 440, H: 200},
				{X: 10, Y: 430, W: 440, H: 40},
			},
		}
		win.bindAndSeed(root)
		spin(0.5)

		if n := len(win.nativeControls); n != 3 {
			fail("embedded controls = %d, want 3", n)
			return
		}
		tl, rl, bl := win.nativeControls["table"], win.nativeControls["tree"], win.nativeControls["tabs"]
		if tl == nil || rl == nil || bl == nil {
			fail("a collection control was not embedded: table=%v tree=%v tabs=%v", tl != nil, rl != nil, bl != nil)
			return
		}
		if !table.Claimed().Get() || !tree.Claimed().Get() || !tabs.Claimed().Get() {
			fail("a collection Native was not claimed")
		}
		if got := tl.ctl.Double(); got != 2 {
			fail("table row = %v, want 2", got)
		}
		if got := rl.ctl.StringValue(); got != "0.1" {
			fail("tree path = %q, want 0.1", got)
		}
		if got := bl.ctl.Double(); got != 1 {
			fail("tab = %v, want 1", got)
		}

		// app -> native: the models move, the next frame pushes them.
		table.Number().Set(7)
		tree.Text().Set("1.0")
		tabs.Number().Set(2)
		win.paintFrame(false)
		spin(0.2)
		if got := tl.ctl.Double(); got != 7 {
			fail("after Set, table row = %v, want 7", got)
		}
		if got := rl.ctl.StringValue(); got != "1.0" {
			fail("after Set, tree path = %q, want 1.0", got)
		}
		if got := bl.ctl.Double(); got != 2 {
			fail("after Set, tab = %v, want 2", got)
		}

		// A 10,000-row update, and an unchanged frame beside it.
		const rounds = 20
		table.SetRows(bigRows(10000))
		win.paintFrame(false)
		start := time.Now()
		for i := 0; i < rounds; i++ {
			win.paintFrame(false)
		}
		same := time.Since(start) / rounds
		start = time.Now()
		for i := 0; i < rounds; i++ {
			rows := bigRows(10000)
			rows[0][0] = "changed " + strconv.Itoa(i)
			table.SetRows(rows)
			win.paintFrame(false)
		}
		changed := time.Since(start) / rounds
		t.Logf("10000-row table, whole frame: unchanged Revision %v, new rows %v (includes building the rows)", same, changed)
		if got := tl.ctl.Double(); got != 7 {
			fail("after the rows changed the selection is %v, want it kept at 7", got)
		}

		if hold > 0 {
			t.Logf("holding the window open for %.0f s", hold)
			spin(hold)
			t.Logf("after the hold: table row %v, tree path %q, tab %v",
				table.Number().Get(), tree.Text().Get(), tabs.Number().Get())
		}
	})
	if setupErr != nil {
		t.Fatalf("collections setup failed: %v", setupErr)
	}
	_ = failures
}
