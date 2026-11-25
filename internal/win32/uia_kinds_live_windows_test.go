// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

package win32

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/toolkit"
)

// uiaDump prints the UI Automation tree under a window handle as a client sees
// it: depth, control type, name, class, IsEnabled and bounding rectangle.
const uiaDump = `
Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes
$w = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]%d)
$walker = [System.Windows.Automation.TreeWalker]::RawViewWalker
function Walk($el, $d) {
	if ($d -gt 5) { return }
	$r = $el.Current.BoundingRectangle
	'{0}{1} "{2}" class={3} enabled={4} rect=({5},{6} {7}x{8})' -f (' ' * (2 * $d)), $el.Current.ControlType.ProgrammaticName.Replace('ControlType.', ''), $el.Current.Name, $el.Current.ClassName, $el.Current.IsEnabled, [int]$r.X, [int]$r.Y, [int]$r.Width, [int]$r.Height
	$c = $walker.GetFirstChild($el)
	while ($null -ne $c) { Walk $c ($d + 1); $c = $walker.GetNextSibling($c) }
}
Walk $w 0
`

// shot saves a PNG of the screen area of a window to path, with PowerShell and
// System.Drawing: a witness other than ours.
const shot = `
Add-Type -AssemblyName System.Drawing
$r = New-Object 'int[]' 4
Add-Type -Namespace U -Name W -MemberDefinition '[System.Runtime.InteropServices.DllImport("user32.dll")] public static extern bool GetWindowRect(System.IntPtr h, int[] r);'
[void][U.W]::GetWindowRect([IntPtr]%d, $r)
$b = New-Object System.Drawing.Bitmap ($r[2] - $r[0]), ($r[3] - $r[1])
$g = [System.Drawing.Graphics]::FromImage($b)
$g.CopyFromScreen($r[0], $r[1], 0, 0, $b.Size)
$b.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)
`

// proofDir is where the captures go: the user config directory, never a
// repository. It refuses a directory with a .git above it.
func proofDir(t *testing.T) string {
	t.Helper()
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "go-widgets-window", "uia-proofs")
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			t.Fatalf("capture directory %s is inside a repository", dir)
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLiveUIANativeKinds shows, to a real UI Automation client, what the Win32
// host's native kinds, its context menu and its dialogs look like: control
// type, name, enabled state, rectangle, and a screenshot of each.
func TestLiveUIANativeKinds(t *testing.T) {
	skipUnlessLive(t)
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	dir := proofDir(t)

	win, err := New("go-widgets/window uia kinds", 520, 420, toolkit.DefaultDark())
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	pick := 0
	specs := []toolkit.NativeControl{
		{Kind: toolkit.NativeTable, Key: "table", Rect: toolkit.Rect{X: 10, Y: 10, W: 300, H: 110}, Visible: true,
			Columns: []string{"id", "name"}, Rows: tableRows(20), Revision: 1, Number: 2},
		{Kind: toolkit.NativeTree, Key: "tree", Rect: toolkit.Rect{X: 10, Y: 130, W: 200, H: 110}, Visible: true,
			Tree: sampleTree(), Revision: 1, Text: "0.1"},
		{Kind: toolkit.NativeTabs, Key: "tabs", Rect: toolkit.Rect{X: 10, Y: 250, W: 300, H: 60}, Visible: true,
			Items: []string{"one", "two", "three"}, Revision: 1, Number: 1},
		{Kind: toolkit.NativeButton, Key: "menu", Rect: toolkit.Rect{X: 330, Y: 10, W: 120, H: 40}, Visible: true,
			Text: "Menu", Menu: []toolkit.NativeMenuItem{{Label: "Open", Pick: func() { pick++ }}, {}, {Label: "Disabled"}}},
	}
	root := &specRoot{specs: specs}
	win.root = root
	win.syncNative(root)
	menuBtn := win.nativeControls["menu"].hwnd

	go func() {
		defer win.Close()
		time.Sleep(2 * time.Second)
		own := win.hwnd
		capture := func(name string, h uintptr) {
			out := powershell(t, fmt.Sprintf(uiaDump, h))
			t.Logf("%s: UI Automation tree\n%s", name, out)
			p := filepath.Join(dir, name+".png")
			powershell(t, fmt.Sprintf(shot, h, strings.ReplaceAll(p, "'", "''")))
			if st, err := os.Stat(p); err != nil || st.Size() == 0 {
				t.Errorf("%s: no screenshot at %s", name, p)
			} else {
				t.Logf("%s: screenshot %s (%d bytes)", name, p, st.Size())
			}
		}
		capture("native-kinds", own)

		// The context menu: a real popup, read while the menu loop is modal.
		win32.PostMessage(win32.HWND(menuBtn), wmContextMenu, win32.WPARAM(menuBtn), 0x00640064)
		if h, class := otherDialog(own, 10*time.Second); h != 0 {
			t.Logf("popup window class %q", class)
			capture("context-menu", h)
			keybdEvent.Call(0x1B, 0, 0, 0) // Escape down
			keybdEvent.Call(0x1B, 0, 2, 0) // Escape up
		} else {
			t.Errorf("no popup menu appeared")
		}
		time.Sleep(time.Second)

		// A dialog: the alert, read from outside.
		res := make(chan DialogResult, 1)
		if err := win.Dialog(DialogRequest{Kind: DialogAlert, Message: "Save changes?", Detail: "detail",
			Buttons: []string{"Save", "Don't save", "Cancel"}, Done: func(r DialogResult) { res <- r }}); err != nil {
			t.Errorf("Dialog: %v", err)
			return
		}
		h, class := otherDialog(own, 20*time.Second)
		if h == 0 {
			t.Errorf("no dialog appeared")
			return
		}
		t.Logf("dialog class %q", class)
		capture("alert-dialog", h)
		win32.PostMessage(win32.HWND(h), tdmClickButton, win32.WPARAM(buttonIDBase+1), 0)
		select {
		case r := <-res:
			t.Logf("alert result %+v", r)
		case <-time.After(20 * time.Second):
			win32.PostMessage(win32.HWND(h), wmCloseLive, 0, 0)
			t.Errorf("the alert callback never ran")
		}
	}()
	if err := win.Run(root); err != nil {
		t.Errorf("Run: %v", err)
	}
}

var keybdEvent = user32.NewProc("keybd_event")
