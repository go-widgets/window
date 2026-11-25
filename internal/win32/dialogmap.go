// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"encoding/binary"
	"errors"
	"strings"
)

// The OS-independent half of the Win32 native dialogs (dialog_windows.go): the
// request and result vocabulary, the file-type filter specs, the button-id
// scheme of an alert and the byte layout of TASKDIALOGCONFIG. Everything here is
// pure so that it is tested on every OS; only the COM and comctl32 calls need
// Windows.

// DialogKind says which dialog a request asks for.
type DialogKind int

// The dialog kinds.
const (
	DialogOpen DialogKind = iota
	DialogSave
	DialogAlert
)

// DialogFilter is one file-type filter: a display name and bare extensions
// ("png", not ".png" or "*.png").
type DialogFilter struct {
	Name string
	Exts []string
}

// DialogRequest is a dialog in primitive terms; the parent package fills it
// from its public options.
type DialogRequest struct {
	Kind        DialogKind
	Title       string
	Name        string // save: suggested file name
	Message     string // alert
	Detail      string // alert
	Buttons     []string
	Filters     []DialogFilter
	Multiple    bool
	Directories bool
	Done        func(DialogResult) // called once, on a goroutine of its own
}

// DialogResult is the outcome. Cancelled is the user's refusal; Paths is what
// the platform returned, untouched; Button indexes the alert's buttons; Err is a
// platform failure.
type DialogResult struct {
	Paths     []string
	Button    int
	Cancelled bool
	Err       error
}

// ErrDialogClosed is the Err of a request still queued when its window went
// away.
var ErrDialogClosed = errors.New("win32: the window closed before the dialog was shown")

// FilterSpec is a COMDLG_FILTERSPEC in Go strings: pszName and pszSpec.
type FilterSpec struct{ Name, Spec string }

// FilterSpecs turns filters into the "*.a;*.b" specs SetFileTypes takes
// (IFileDialog::SetFileTypes). The extensions were validated by the public
// package (no dot, glob or separator), so a spec is only ever a list of
// "*.ext" items. A filter without a name is shown as its spec.
func FilterSpecs(fs []DialogFilter) []FilterSpec {
	out := make([]FilterSpec, 0, len(fs))
	for _, f := range fs {
		items := make([]string, len(f.Exts))
		for i, e := range f.Exts {
			items[i] = "*." + e
		}
		spec := strings.Join(items, ";")
		name := f.Name
		if name == "" {
			name = spec
		}
		out = append(out, FilterSpec{Name: name, Spec: spec})
	}
	return out
}

// DefaultExt is the extension a saved name gets when it has none: the first
// extension of the first filter, the first filter being the default type.
func DefaultExt(fs []DialogFilter) string {
	// Ranges rather than indexes: nothing here can be out of range.
	ext := ""
	for _, f := range fs {
		for _, e := range f.Exts {
			ext = e
			break
		}
		break
	}
	return ext
}

// FOS_* option bits of IFileDialog::SetOptions.
const (
	fosOverwritePrompt = 0x00000002
	fosNoChangeDir     = 0x00000008
	fosPickFolders     = 0x00000020
	fosForceFileSystem = 0x00000040
	fosAllowMultiple   = 0x00000200
	fosPathMustExist   = 0x00000800
	fosFileMustExist   = 0x00001000
)

// FileDialogOptions is the FOS_* word for a request, added to whatever
// GetOptions returned. Paths always come from the file system (never a library
// or a virtual item), so SIGDN_FILESYSPATH has an answer.
func FileDialogOptions(save, multiple, directories bool) uint32 {
	o := uint32(fosForceFileSystem | fosNoChangeDir)
	if save {
		return o | fosOverwritePrompt | fosPathMustExist
	}
	o |= fosPathMustExist
	if directories {
		o |= fosPickFolders
	} else {
		o |= fosFileMustExist
	}
	if multiple {
		o |= fosAllowMultiple
	}
	return o
}

// hrCancelled is HRESULT_FROM_WIN32(ERROR_CANCELLED): what Show returns when
// the user dismissed the file dialog.
const hrCancelled = 0x800704C7

// IsCancelledHR reports whether hr is the user's cancel.
func IsCancelledHR(hr uint32) bool { return hr == hrCancelled }

// Button ids. A task dialog answers one of the ids it was given, or IDCANCEL
// for Cancel, Alt-F4, Escape and the close box (TaskDialogIndirect, pnButton).
const (
	idCancel     = 2
	idOK         = 1
	idYes        = 6
	idNo         = 7
	buttonIDBase = 100
)

// AlertLabels is the labels an alert shows: its own, or a single "OK".
func AlertLabels(buttons []string) []string {
	if len(buttons) == 0 {
		return []string{"OK"}
	}
	return buttons
}

// AlertButtonIDs gives each label the id the dialog reports for it. A label
// that reads "Cancel" (the first such) takes IDCANCEL, so that Escape and the
// close box press it, as they press the cancel button of any other dialog.
func AlertButtonIDs(labels []string) []int32 {
	ids := make([]int32, len(labels))
	cancel := -1
	for i, l := range labels {
		ids[i] = int32(buttonIDBase + i)
		if cancel < 0 && strings.EqualFold(strings.TrimSpace(l), "cancel") {
			cancel = i
		}
	}
	if cancel >= 0 {
		ids[cancel] = idCancel
	}
	return ids
}

// AlertOutcome reads the id a task dialog or message box answered: the index of
// the button it names, or cancelled when it is IDCANCEL and no button carries
// that id (Escape or the close box on an alert without a cancel button). An id
// that is neither is not ok.
func AlertOutcome(id int32, ids []int32) (button int, cancelled, ok bool) {
	for i, v := range ids {
		if v == id {
			return i, false, true
		}
	}
	if id == idCancel {
		return 0, true, true
	}
	return 0, false, false
}

// MessageBoxPlan is the MessageBoxW fallback for when comctl32 v6 (and so
// TaskDialogIndirect) is unavailable: one button is MB_OK, two are MB_YESNO
// (its Yes and No stand in for the labels), more cannot be shown.
func MessageBoxPlan(buttons int) (flags uint32, ids []int32, ok bool) {
	switch buttons {
	case 1:
		return 0x0, []int32{idOK}, true // MB_OK
	case 2:
		return 0x4, []int32{idYes, idNo}, true // MB_YESNO
	}
	return 0, nil, false
}

// TaskDialogFields is every TASKDIALOGCONFIG member this package sets; the
// pointers are addresses already held alive by the caller.
type TaskDialogFields struct {
	Parent, Title, Instruction, Content uintptr
	Buttons                             uintptr
	Flags, ButtonCount                  uint32
	DefaultButton                       int32
}

// Task dialog flags (TASKDIALOGCONFIG.dwFlags).
const (
	TDFAllowDialogCancellation = 0x0008
	TDFPositionRelativeToWin   = 0x1000
)

// EncodeTaskDialogConfig lays a TASKDIALOGCONFIG out as bytes. The structure is
// declared between pshpack1.h and poppack.h in commctrl.h, so its members are
// packed with no padding: a Go struct would insert the alignment holes the C
// one lacks and every member after the first would be read from the wrong
// offset. ptrSize is 8 (or 4 on a 32-bit build); the result is 160 (96) bytes.
func EncodeTaskDialogConfig(f TaskDialogFields, ptrSize int) []byte {
	var b []byte
	u32 := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	ptr := func(v uintptr) {
		if ptrSize == 8 {
			b = binary.LittleEndian.AppendUint64(b, uint64(v))
		} else {
			b = binary.LittleEndian.AppendUint32(b, uint32(v))
		}
	}
	u32(0) // cbSize, patched below
	ptr(f.Parent)
	ptr(0) // hInstance
	u32(f.Flags)
	u32(0) // dwCommonButtons: custom buttons only
	ptr(f.Title)
	ptr(0) // pszMainIcon
	ptr(f.Instruction)
	ptr(f.Content)
	u32(f.ButtonCount)
	ptr(f.Buttons)
	u32(uint32(f.DefaultButton))
	u32(0) // cRadioButtons
	ptr(0) // pRadioButtons
	u32(0) // nDefaultRadioButton
	ptr(0) // pszVerificationText
	ptr(0) // pszExpandedInformation
	ptr(0) // pszExpandedControlText
	ptr(0) // pszCollapsedControlText
	ptr(0) // pszFooterIcon
	ptr(0) // pszFooter
	ptr(0) // pfCallback
	ptr(0) // lpCallbackData
	u32(0) // cxWidth
	binary.LittleEndian.PutUint32(b, uint32(len(b)))
	return b
}

// EncodeTaskDialogButtons lays out a TASKDIALOG_BUTTON array (packed: an int id
// then a string pointer, 4+ptrSize bytes each).
func EncodeTaskDialogButtons(btns []TaskButton, ptrSize int) []byte {
	var b []byte
	for _, x := range btns {
		b = binary.LittleEndian.AppendUint32(b, uint32(x.ID))
		if ptrSize == 8 {
			b = binary.LittleEndian.AppendUint64(b, uint64(x.Text))
		} else {
			b = binary.LittleEndian.AppendUint32(b, uint32(x.Text))
		}
	}
	return b
}

// TaskButton is one TASKDIALOG_BUTTON: its id and the address of its label.
type TaskButton struct {
	ID   int32
	Text uintptr
}
