// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestFilterSpecs(t *testing.T) {
	got := FilterSpecs([]DialogFilter{
		{Name: "Images", Exts: []string{"png", "jpg"}},
		{Name: "", Exts: []string{"txt"}},
	})
	want := []FilterSpec{{"Images", "*.png;*.jpg"}, {"*.txt", "*.txt"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterSpecs = %+v, want %+v", got, want)
	}
	if got := FilterSpecs(nil); len(got) != 0 {
		t.Fatalf("FilterSpecs(nil) = %+v", got)
	}
}

func TestDefaultExt(t *testing.T) {
	if got := DefaultExt([]DialogFilter{{Exts: []string{"md", "txt"}}, {Exts: []string{"go"}}}); got != "md" {
		t.Errorf("DefaultExt = %q, want md", got)
	}
	if DefaultExt(nil) != "" || DefaultExt([]DialogFilter{{}}) != "" {
		t.Errorf("DefaultExt of nothing is not empty")
	}
}

func TestFileDialogOptions(t *testing.T) {
	has := func(o, bit uint32) bool { return o&bit != 0 }
	open := FileDialogOptions(false, false, false)
	if !has(open, fosFileMustExist) || !has(open, fosForceFileSystem) || has(open, fosPickFolders) || has(open, fosAllowMultiple) || has(open, fosOverwritePrompt) {
		t.Errorf("open = %#x", open)
	}
	multi := FileDialogOptions(false, true, false)
	if !has(multi, fosAllowMultiple) {
		t.Errorf("multiple = %#x", multi)
	}
	dirs := FileDialogOptions(false, false, true)
	if !has(dirs, fosPickFolders) || has(dirs, fosFileMustExist) {
		t.Errorf("directories = %#x", dirs)
	}
	save := FileDialogOptions(true, true, true) // multiple and directories mean nothing to a save
	if !has(save, fosOverwritePrompt) || has(save, fosAllowMultiple) || has(save, fosPickFolders) || has(save, fosFileMustExist) {
		t.Errorf("save = %#x", save)
	}
}

func TestIsCancelledHR(t *testing.T) {
	if !IsCancelledHR(0x800704C7) {
		t.Error("HRESULT_FROM_WIN32(ERROR_CANCELLED) is not a cancel")
	}
	for _, hr := range []uint32{0, 0x80004005, 0x800704C6, 0x800704C8} {
		if IsCancelledHR(hr) {
			t.Errorf("%#x is a cancel", hr)
		}
	}
}

func TestAlertIDsAndOutcome(t *testing.T) {
	if got := AlertLabels(nil); !reflect.DeepEqual(got, []string{"OK"}) {
		t.Errorf("AlertLabels(nil) = %v", got)
	}
	labels := []string{"Save", "Don't save", "Cancel"}
	if got := AlertLabels(labels); !reflect.DeepEqual(got, labels) {
		t.Errorf("AlertLabels(own) = %v", got)
	}
	ids := AlertButtonIDs(labels)
	if want := []int32{100, 101, idCancel}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i, id := range ids {
		if b, c, ok := AlertOutcome(id, ids); b != i || c || !ok {
			t.Errorf("outcome(%d) = %d %v %v, want %d false true", id, b, c, ok, i)
		}
	}
	// No cancel button: IDCANCEL is the close box.
	plain := AlertButtonIDs([]string{"Yes", "No"})
	if want := []int32{100, 101}; !reflect.DeepEqual(plain, want) {
		t.Fatalf("plain ids = %v", plain)
	}
	if _, c, ok := AlertOutcome(idCancel, plain); !c || !ok {
		t.Errorf("IDCANCEL without a cancel button: cancelled=%v ok=%v", c, ok)
	}
	if _, _, ok := AlertOutcome(77, plain); ok {
		t.Errorf("an id nobody owns was accepted")
	}
	// Only the first "cancel" takes the id, whatever its case or padding.
	if got := AlertButtonIDs([]string{" cANCEL ", "Cancel"}); !reflect.DeepEqual(got, []int32{idCancel, 101}) {
		t.Errorf("two cancels = %v", got)
	}
}

func TestMessageBoxPlan(t *testing.T) {
	if f, ids, ok := MessageBoxPlan(1); !ok || f != 0 || !reflect.DeepEqual(ids, []int32{idOK}) {
		t.Errorf("1 = %#x %v %v", f, ids, ok)
	}
	if f, ids, ok := MessageBoxPlan(2); !ok || f != 4 || !reflect.DeepEqual(ids, []int32{idYes, idNo}) {
		t.Errorf("2 = %#x %v %v", f, ids, ok)
	}
	for _, n := range []int{0, 3, 4} {
		if _, _, ok := MessageBoxPlan(n); ok {
			t.Errorf("%d buttons accepted", n)
		}
	}
}

func TestEncodeTaskDialogConfig(t *testing.T) {
	f := TaskDialogFields{
		Parent: 0x1111, Title: 0x2222, Instruction: 0x3333, Content: 0x4444,
		Buttons: 0x5555, Flags: TDFAllowDialogCancellation | TDFPositionRelativeToWin,
		ButtonCount: 3, DefaultButton: 100,
	}
	for _, c := range []struct{ ptr, size int }{{8, 160}, {4, 96}} {
		b := EncodeTaskDialogConfig(f, c.ptr)
		if len(b) != c.size {
			t.Fatalf("ptr %d: %d bytes, want %d", c.ptr, len(b), c.size)
		}
		le := binary.LittleEndian
		if le.Uint32(b[0:]) != uint32(c.size) {
			t.Errorf("ptr %d: cbSize = %d", c.ptr, le.Uint32(b[0:]))
		}
		rd := func(off int) uint64 {
			if c.ptr == 8 {
				return le.Uint64(b[off:])
			}
			return uint64(le.Uint32(b[off:]))
		}
		p := c.ptr
		// cbSize(4) hwndParent hInstance dwFlags(4) dwCommonButtons(4) title icon
		// instruction content cButtons(4) pButtons nDefaultButton(4)
		offParent := 4
		offFlags := offParent + 2*p
		offTitle := offFlags + 8
		offInstr := offTitle + 2*p
		offContent := offInstr + p
		offCount := offContent + p
		offButtons := offCount + 4
		offDefault := offButtons + p
		if rd(offParent) != 0x1111 || le.Uint32(b[offFlags:]) != 0x1008 || rd(offTitle) != 0x2222 ||
			rd(offInstr) != 0x3333 || rd(offContent) != 0x4444 || le.Uint32(b[offCount:]) != 3 ||
			rd(offButtons) != 0x5555 || int32(le.Uint32(b[offDefault:])) != 100 {
			t.Errorf("ptr %d: members at the wrong offsets: % x", c.ptr, b)
		}
	}
}

func TestEncodeTaskDialogButtons(t *testing.T) {
	b := EncodeTaskDialogButtons([]TaskButton{{100, 0xAAAA}, {2, 0xBBBB}}, 8)
	if len(b) != 24 {
		t.Fatalf("len %d, want 24 (two packed 12-byte records)", len(b))
	}
	le := binary.LittleEndian
	if le.Uint32(b[0:]) != 100 || le.Uint64(b[4:]) != 0xAAAA || le.Uint32(b[12:]) != 2 || le.Uint64(b[16:]) != 0xBBBB {
		t.Errorf("records: % x", b)
	}
	if b := EncodeTaskDialogButtons([]TaskButton{{1, 5}}, 4); len(b) != 8 {
		t.Errorf("32-bit record is %d bytes, want 8", len(b))
	}
}
