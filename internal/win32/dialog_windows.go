// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/go-mswin/win32"
)

// The Win32 half of the Dialogs capability.
//
//   - Open and save are the common item dialogs, IFileOpenDialog and
//     IFileSaveDialog (IFileDialog::Show, GetResult, IShellItem::GetDisplayName
//     with SIGDN_FILESYSPATH). Show returns HRESULT_FROM_WIN32(ERROR_CANCELLED)
//     when the user dismisses it.
//   - Alert is TaskDialogIndirect with one custom button per label. It exists
//     only in comctl32 v6, which a Go executable has no manifest for; the
//     activation context the SysLink host already builds supplies it, and when
//     that or the export is unavailable the alert falls back to MessageBoxW
//     (one button: MB_OK; two: MB_YESNO; more: an error).
//
// Threading. Dialog queues the request and POSTS WMAppDialog to the window, so
// it is safe from any goroutine; the dialog itself runs inside that message on
// the window's own thread, which is the COM STA the common dialogs need and the
// thread that owns the owner window. Requests run one at a time in order (a
// dialog is modal and pumps messages, so a second WMAppDialog arriving inside it
// only finds the queue non-empty and returns). The result is handed to Done on a
// goroutine of its own.
//
// Reading the vtables by index follows shobjidl_core.h: IUnknown 0-2, then for
// IFileDialog Show 3, SetFileTypes 4, SetFileTypeIndex 5, GetFileTypeIndex 6,
// Advise 7, Unadvise 8, SetOptions 9, GetOptions 10, SetDefaultFolder 11,
// SetFolder 12, GetFolder 13, GetCurrentSelection 14, SetFileName 15,
// GetFileName 16, SetTitle 17, SetOkButtonLabel 18, SetFileNameLabel 19,
// GetResult 20, AddPlace 21, SetDefaultExtension 22; IFileOpenDialog adds
// GetResults at 27; IShellItem GetDisplayName 5; IShellItemArray GetCount 7 and
// GetItemAt 8.

// WMAppDialog is the private message Dialog posts. It sits well away from the
// small WM_APP offsets the other private messages use.
const WMAppDialog = WMAppRepaint + 0x40

type dialogQueue struct {
	mu   sync.Mutex
	reqs []DialogRequest
	busy bool // UI thread only
}

const (
	clsctxInprocServer = 1
	coinitApartment    = 2
	sigdnFileSysPath   = 0x80058000
	rpcEChangedMode    = 0x80010106
)

var (
	ole32               = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx  = ole32.NewProc("CoInitializeEx")
	procCoUninitialize  = ole32.NewProc("CoUninitialize")
	procCoCreateInst    = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree   = ole32.NewProc("CoTaskMemFree")
	procIsWindowEnabled = user32.NewProc("IsWindowEnabled")
	procMessageBoxW     = user32.NewProc("MessageBoxW")

	clsidFileOpenDialog = guid{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	clsidFileSaveDialog = guid{0xC0B4E2F3, 0xBA21, 0x4773, [8]byte{0x8D, 0xBA, 0x33, 0x5E, 0xC9, 0x46, 0xEB, 0x8B}}
	iidFileOpenDialog   = guid{0xD57C7288, 0xD4AD, 0x4768, [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidFileSaveDialog   = guid{0x84BCCD23, 0x5FDE, 0x4CDB, [8]byte{0xAE, 0xA4, 0xAF, 0x64, 0xB8, 0x3D, 0x78, 0xAB}}

	tdOnce sync.Once
	tdProc *syscall.Proc // TaskDialogIndirect from comctl32 v6, nil when unavailable
)

// Dialog queues req and returns at once. Done is called exactly once, later, on
// a goroutine of its own, unless ErrClosed is returned, in which case it never
// is. Safe from any goroutine.
func (w *Window) Dialog(req DialogRequest) error {
	if req.Done == nil {
		return errors.New("win32: dialog without a callback")
	}
	hwnd := w.hwnd
	if w.gone.Load() || hwnd == 0 {
		return ErrClosed
	}
	w.dlg.mu.Lock()
	w.dlg.reqs = append(w.dlg.reqs, req)
	w.dlg.mu.Unlock()
	if !win32.PostMessage(win32.HWND(hwnd), WMAppDialog, 0, 0) {
		w.dlg.mu.Lock()
		w.dlg.reqs = w.dlg.reqs[:len(w.dlg.reqs)-1]
		w.dlg.mu.Unlock()
		return ErrClosed
	}
	return nil
}

func (q *dialogQueue) pop() (DialogRequest, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.reqs) == 0 {
		return DialogRequest{}, false
	}
	r := q.reqs[0]
	q.reqs = q.reqs[1:]
	return r, true
}

// failDialogs answers every request still queued when the window is destroyed.
func (w *Window) failDialogs() {
	for {
		req, ok := w.dlg.pop()
		if !ok {
			return
		}
		go req.Done(DialogResult{Err: ErrDialogClosed})
	}
}

// onDialog is WMAppDialog's handler, on the UI thread.
func (w *Window) onDialog() {
	if w.dlg.busy {
		return // a dialog is up; it drains the queue when it returns
	}
	w.dlg.busy = true
	defer func() { w.dlg.busy = false }()
	for {
		req, ok := w.dlg.pop()
		if !ok {
			return
		}
		res := w.runDialog(req)
		go req.Done(res)
	}
}

// dialogParent is the owner window, or 0: a task dialog's parent "should not be
// hidden or disabled" (TaskDialogIndirect, remarks).
func (w *Window) dialogParent() uintptr {
	if w.hwnd == 0 || !win32.IsWindowVisible(win32.HWND(w.hwnd)) {
		return 0
	}
	if en, _, _ := procIsWindowEnabled.Call(w.hwnd); en == 0 {
		return 0
	}
	return w.hwnd
}

func (w *Window) runDialog(req DialogRequest) DialogResult {
	parent := w.dialogParent()
	if req.Kind == DialogAlert {
		return w.runAlert(req, parent)
	}
	hr := uint32(callProc(procCoInitializeEx, 0, coinitApartment))
	if hr == 0 || hr == 1 { // S_OK, S_FALSE: both want a CoUninitialize
		defer procCoUninitialize.Call()
	} else if hr != rpcEChangedMode { // the thread is already an MTA: use it as it is
		return DialogResult{Err: hrError("CoInitializeEx", hr)}
	}
	return runFileDialog(req, parent)
}

// pins keeps Go memory addressed by a uintptr alive and where it is: the
// collector does not see a uintptr, and a stack variable may move.
type pins struct {
	keep []any
	out  [2]uintptr
}

var (
	pinMu  sync.Mutex
	pinned = map[*pins]struct{}{}
)

func newPins() *pins {
	p := &pins{}
	pinMu.Lock()
	pinned[p] = struct{}{}
	pinMu.Unlock()
	return p
}

func (p *pins) release() {
	pinMu.Lock()
	delete(pinned, p)
	pinMu.Unlock()
}

func (p *pins) str(s string) uintptr {
	u, err := syscall.UTF16PtrFromString(s)
	if err != nil { // the public package refused NULs; this is a guard
		u, _ = syscall.UTF16PtrFromString("")
	}
	p.keep = append(p.keep, u)
	return uintptr(unsafe.Pointer(u))
}

// caller is a lazily bound DLL export of either flavour (syscall or x/sys).
type caller interface {
	Call(a ...uintptr) (r1, r2 uintptr, err error)
}

func callProc(p caller, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

// vcall calls method idx of the COM object obj.
func vcall(obj uintptr, idx int, args ...uintptr) uint32 {
	vt := *(*uintptr)(ptr(obj))
	fn := *(*uintptr)(ptr(vt + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return uint32(r)
}

func release(obj uintptr) {
	if obj != 0 {
		vcall(obj, 2)
	}
}

func hrError(what string, hr uint32) error {
	return fmt.Errorf("win32: %s: HRESULT 0x%08X", what, hr)
}

// runFileDialog shows an open or save dialog and reads its answer.
func runFileDialog(req DialogRequest, parent uintptr) DialogResult {
	p := newPins()
	defer p.release()
	save := req.Kind == DialogSave
	clsid, iid := &clsidFileOpenDialog, &iidFileOpenDialog
	if save {
		clsid, iid = &clsidFileSaveDialog, &iidFileSaveDialog
	}
	var dlg uintptr
	if hr := uint32(callProc(procCoCreateInst, uintptr(unsafe.Pointer(clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&p.out[0])))); hr != 0 {
		return DialogResult{Err: hrError("CoCreateInstance", hr)}
	}
	dlg = p.out[0]
	defer release(dlg)

	if hr := vcall(dlg, 10, uintptr(unsafe.Pointer(&p.out[1]))); hr != 0 { // GetOptions
		return DialogResult{Err: hrError("GetOptions", hr)}
	}
	opts := uint32(p.out[1]) | FileDialogOptions(save, req.Multiple, req.Directories)
	if hr := vcall(dlg, 9, uintptr(opts)); hr != 0 { // SetOptions
		return DialogResult{Err: hrError("SetOptions", hr)}
	}
	if req.Title != "" {
		vcall(dlg, 17, p.str(req.Title)) // SetTitle
	}
	if save && req.Name != "" {
		vcall(dlg, 15, p.str(req.Name)) // SetFileName
	}
	if specs := FilterSpecs(req.Filters); len(specs) > 0 && !req.Directories {
		// COMDLG_FILTERSPEC is two pointers, name then spec.
		arr := make([]uintptr, 0, 2*len(specs))
		for _, s := range specs {
			arr = append(arr, p.str(s.Name), p.str(s.Spec))
		}
		p.keep = append(p.keep, arr)
		if hr := vcall(dlg, 4, uintptr(len(specs)), uintptr(unsafe.Pointer(&arr[0]))); hr != 0 { // SetFileTypes
			return DialogResult{Err: hrError("SetFileTypes", hr)}
		}
		if save {
			if ext := DefaultExt(req.Filters); ext != "" {
				vcall(dlg, 22, p.str(ext)) // SetDefaultExtension
			}
		}
	}

	hr := vcall(dlg, 3, parent) // Show
	if IsCancelledHR(hr) {
		return DialogResult{Cancelled: true}
	}
	if hr != 0 {
		return DialogResult{Err: hrError("Show", hr)}
	}

	if save || !req.Multiple {
		var item uintptr
		if hr := vcall(dlg, 20, uintptr(unsafe.Pointer(&p.out[0]))); hr != 0 { // GetResult
			return DialogResult{Err: hrError("GetResult", hr)}
		}
		item = p.out[0]
		defer release(item)
		path, err := itemPath(item, p)
		if err != nil {
			return DialogResult{Err: err}
		}
		return DialogResult{Paths: []string{path}}
	}
	if hr := vcall(dlg, 27, uintptr(unsafe.Pointer(&p.out[0]))); hr != 0 { // GetResults
		return DialogResult{Err: hrError("GetResults", hr)}
	}
	arr := p.out[0]
	defer release(arr)
	if hr := vcall(arr, 7, uintptr(unsafe.Pointer(&p.out[1]))); hr != 0 { // GetCount
		return DialogResult{Err: hrError("GetCount", hr)}
	}
	n := uint32(p.out[1])
	paths := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		if hr := vcall(arr, 8, uintptr(i), uintptr(unsafe.Pointer(&p.out[0]))); hr != 0 { // GetItemAt
			return DialogResult{Err: hrError("GetItemAt", hr)}
		}
		item := p.out[0]
		path, err := itemPath(item, p)
		release(item)
		if err != nil {
			return DialogResult{Err: err}
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return DialogResult{Cancelled: true}
	}
	return DialogResult{Paths: paths}
}

// itemPath is the item's file-system path, exactly as the shell gives it.
func itemPath(item uintptr, p *pins) (string, error) {
	p.out[1] = 0
	if hr := vcall(item, 5, sigdnFileSysPath, uintptr(unsafe.Pointer(&p.out[1]))); hr != 0 { // GetDisplayName
		return "", hrError("GetDisplayName", hr)
	}
	pw := p.out[1]
	defer procCoTaskMemFree.Call(pw)
	var u []uint16
	for off := uintptr(0); ; off += 2 {
		c := *(*uint16)(ptr(pw + off))
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return syscall.UTF16ToString(u), nil
}

// taskDialog resolves TaskDialogIndirect once, under the comctl32 v6 context.
func taskDialog() (*syscall.Proc, func()) {
	leave := enterLinkV6()
	tdOnce.Do(func() {
		if leave == nil {
			return
		}
		// A fresh handle, loaded while the v6 context is active, so that it is
		// the side-by-side comctl32 and not a v5 already in the process.
		dll, err := syscall.LoadDLL("comctl32.dll")
		if err != nil {
			return
		}
		if p, err := dll.FindProc("TaskDialogIndirect"); err == nil {
			tdProc = p
		}
	})
	if leave == nil {
		leave = func() {}
	}
	return tdProc, leave
}

func (w *Window) runAlert(req DialogRequest, parent uintptr) DialogResult {
	labels := AlertLabels(req.Buttons)
	ids := AlertButtonIDs(labels)
	proc, leave := taskDialog()
	defer leave()
	title := w.title
	if proc == nil {
		return alertMessageBox(w.title, req, labels, parent)
	}

	p := newPins()
	defer p.release()
	tb := make([]TaskButton, len(labels))
	for i, l := range labels {
		tb[i] = TaskButton{ID: ids[i], Text: p.str(l)}
	}
	btns := EncodeTaskDialogButtons(tb, int(unsafe.Sizeof(uintptr(0))))
	p.keep = append(p.keep, btns)
	f := TaskDialogFields{
		Parent: parent, Title: p.str(title), Instruction: p.str(req.Message),
		Buttons: uintptr(unsafe.Pointer(&btns[0])), ButtonCount: uint32(len(ids)),
		Flags: TDFAllowDialogCancellation, DefaultButton: ids[0],
	}
	if req.Detail != "" {
		f.Content = p.str(req.Detail)
	}
	if parent != 0 {
		f.Flags |= TDFPositionRelativeToWin
	}
	cfg := EncodeTaskDialogConfig(f, int(unsafe.Sizeof(uintptr(0))))
	p.keep = append(p.keep, cfg)
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&cfg[0])), uintptr(unsafe.Pointer(&p.out[0])), 0, 0)
	if hr := uint32(r); hr != 0 {
		return DialogResult{Err: hrError("TaskDialogIndirect", hr)}
	}
	return alertResult(int32(p.out[0]), ids)
}

func alertResult(id int32, ids []int32) DialogResult {
	b, cancelled, ok := AlertOutcome(id, ids)
	switch {
	case !ok:
		return DialogResult{Err: fmt.Errorf("win32: alert answered unknown button id %d", id)}
	case cancelled:
		return DialogResult{Cancelled: true}
	}
	return DialogResult{Button: b}
}

// alertMessageBox is the fallback when TaskDialogIndirect is not available.
func alertMessageBox(title string, req DialogRequest, labels []string, parent uintptr) DialogResult {
	flags, ids, ok := MessageBoxPlan(len(labels))
	if !ok {
		return DialogResult{Err: fmt.Errorf("win32: TaskDialogIndirect needs comctl32 v6, unavailable here, and MessageBoxW cannot show %d buttons", len(labels))}
	}
	p := newPins()
	defer p.release()
	text := req.Message
	if req.Detail != "" {
		text += "\n\n" + req.Detail
	}
	r := callProc(procMessageBoxW, parent, p.str(text), p.str(title), uintptr(flags))
	if r == 0 {
		return DialogResult{Err: errors.New("win32: MessageBoxW failed")}
	}
	return alertResult(int32(r), ids)
}
