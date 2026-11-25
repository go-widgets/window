// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"sync"

	objc "github.com/go-macos/objc"
)

// Native dialogs on AppKit: NSOpenPanel, NSSavePanel, NSAlert.
//
// A request is queued and a selector is performed on the main thread (as Show
// and Hide are); that selector pops the request, runs the dialog with -runModal
// -- AppKit's own nested modal loop, so the window keeps being serviced -- and
// hands the outcome to the request's Done on a NEW goroutine, so Done may block
// without holding the UI thread.

// DialogKind picks the dialog.
type DialogKind int

const (
	DialogOpen DialogKind = iota
	DialogSave
	DialogAlert
)

// DialogRequest is a dialog in primitive terms; the parent package fills it
// from its public options.
type DialogRequest struct {
	Kind        DialogKind
	Title       string
	Name        string   // save: suggested file name
	Message     string   // alert
	Detail      string   // alert
	Buttons     []string // alert
	Exts        []string // open/save: every extension of every filter, flattened
	Multiple    bool
	Directories bool
	Done        func(DialogResult)
}

// DialogResult is the outcome. Cancelled is the user's refusal; Paths is what
// the platform returned, untouched; Button indexes the alert's buttons.
type DialogResult struct {
	Paths     []string
	Button    int
	Cancelled bool
}

var (
	dialogMu    sync.Mutex
	dialogQueue []DialogRequest

	selDialogNow = objc.RegisterName("goWidgetsDialogNow")

	selAllowedFileTypes  = objc.RegisterName("setAllowedFileTypes:")
	selSetMessageText    = objc.RegisterName("setMessageText:")
	selSetInformative    = objc.RegisterName("setInformativeText:")
	selAddButton         = objc.RegisterName("addButtonWithTitle:")
	selRunModal          = objc.RegisterName("runModal")
	selSetNameField      = objc.RegisterName("setNameFieldStringValue:")
	selSetCanChooseFiles = objc.RegisterName("setCanChooseFiles:")
	selSetCanChooseDirs  = objc.RegisterName("setCanChooseDirectories:")
	selSetMultiple       = objc.RegisterName("setAllowsMultipleSelection:")
	selURLs              = objc.RegisterName("URLs")
	selURL               = objc.RegisterName("URL")
	selPath              = objc.RegisterName("path")
	selMutableArray      = objc.RegisterName("array")
)

func dialogMethods() []objc.MethodDef {
	return []objc.MethodDef{{Cmd: selDialogNow, Fn: viewDialogNow}}
}

// Dialog queues req and returns at once; Done is called exactly once, later,
// unless the window is gone, in which case ErrClosed is returned and it never
// is. Safe from any goroutine.
func (w *Window) Dialog(req DialogRequest) error {
	if w == nil || w.view == 0 || w.gone.Load() {
		return ErrClosed
	}
	dialogMu.Lock()
	dialogQueue = append(dialogQueue, req)
	dialogMu.Unlock()
	w.view.Send(selPerformOnMain, selDialogNow, objc.ID(0), false)
	return nil
}

func popDialog() (DialogRequest, bool) {
	dialogMu.Lock()
	defer dialogMu.Unlock()
	if len(dialogQueue) == 0 {
		return DialogRequest{}, false
	}
	r := dialogQueue[0]
	dialogQueue = dialogQueue[1:]
	return r, true
}

// viewDialogNow is the main-thread half. It deliberately does not check that
// self is the active window: a queued request is the caller's, and must be
// answered even if the window closed meanwhile.
func viewDialogNow(_ objc.ID, _ objc.SEL) {
	req, ok := popDialog()
	if !ok {
		return
	}
	res := runDialog(req)
	go req.Done(res)
}

const (
	modalOK          = 1
	alertFirstButton = 1000
)

func runDialog(req DialogRequest) DialogResult {
	objc.App().Send(selActivateIgnoring, true)
	switch req.Kind {
	case DialogAlert:
		return runAlert(req)
	case DialogSave:
		return runSave(req)
	default:
		return runOpen(req)
	}
}

func nsStringArray(ss []string) objc.ID {
	arr := objc.ID(objc.GetClass("NSMutableArray")).Send(selMutableArray)
	for _, s := range ss {
		arr.Send(selAddObject, objc.NSString(s))
	}
	return arr
}

func runOpen(req DialogRequest) DialogResult {
	p := objc.ID(objc.GetClass("NSOpenPanel")).Send(objc.RegisterName("openPanel"))
	if req.Title != "" {
		p.Send(selSetMessageText2, objc.NSString(req.Title))
	}
	p.Send(selSetMultiple, req.Multiple)
	p.Send(selSetCanChooseDirs, req.Directories)
	p.Send(selSetCanChooseFiles, !req.Directories)
	if len(req.Exts) > 0 && !req.Directories {
		// allowedFileTypes is deprecated (macOS 12) for allowedContentTypes, but
		// it takes the plain extensions we are given with no UTType lookup that
		// can silently drop an unknown one.
		p.Send(selAllowedFileTypes, nsStringArray(req.Exts))
	}
	if objc.Send[int](p, selRunModal) != modalOK {
		return DialogResult{Cancelled: true}
	}
	urls := p.Send(selURLs)
	n := objc.Send[int](urls, selCount)
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		paths = append(paths, objc.GoString(urls.Send(selObjectAtIndex, i).Send(selPath)))
	}
	return DialogResult{Paths: paths}
}

var selSetMessageText2 = objc.RegisterName("setMessage:")

func runSave(req DialogRequest) DialogResult {
	p := objc.ID(objc.GetClass("NSSavePanel")).Send(objc.RegisterName("savePanel"))
	if req.Title != "" {
		p.Send(selSetMessageText2, objc.NSString(req.Title))
	}
	if req.Name != "" {
		p.Send(selSetNameField, objc.NSString(req.Name))
	}
	if len(req.Exts) > 0 {
		p.Send(selAllowedFileTypes, nsStringArray(req.Exts))
	}
	if objc.Send[int](p, selRunModal) != modalOK {
		return DialogResult{Cancelled: true}
	}
	return DialogResult{Paths: []string{objc.GoString(p.Send(selURL).Send(selPath))}}
}

func runAlert(req DialogRequest) DialogResult {
	a := objc.ID(objc.GetClass("NSAlert")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("init"))
	a.Send(selSetMessageText, objc.NSString(req.Message))
	if req.Detail != "" {
		a.Send(selSetInformative, objc.NSString(req.Detail))
	}
	for _, b := range req.Buttons {
		a.Send(selAddButton, objc.NSString(b))
	}
	r := objc.Send[int](a, selRunModal)
	a.Send(objc.RegisterName("release"))
	return DialogResult{Button: alertButton(r)}
}

// alertButton maps runModal's answer to a button index. The implicit OK button
// (an alert with no buttons) answers 1, which is below the first added button.
func alertButton(r int) int {
	if r < alertFirstButton {
		return 0
	}
	return r - alertFirstButton
}
