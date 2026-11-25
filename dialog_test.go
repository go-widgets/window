// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"strings"
	"testing"
)

type noDlgBackend struct{ Backend }

func (noDlgBackend) String() string { return "nodialogs" }

type fakeDlg struct {
	noDlgBackend
	open  int
	save  int
	alert int
}

func (f *fakeDlg) OpenFile(OpenOptions, func(OpenResult)) error { f.open++; return nil }
func (f *fakeDlg) SaveFile(SaveOptions, func(SaveResult)) error { f.save++; return nil }
func (f *fakeDlg) Alert(AlertOptions, func(AlertResult)) error  { f.alert++; return nil }

func TestDialogValidation(t *testing.T) {
	long := strings.Repeat("a", MaxStringLen+1)
	manyF := make([]Filter, MaxFilters+1)
	for i := range manyF {
		manyF[i] = Filter{Name: "n", Extensions: []string{"x"}}
	}
	manyE := make([]string, MaxExtensions+1)
	for i := range manyE {
		manyE[i] = "x"
	}
	ok := []Filter{{Name: "Images", Extensions: []string{"png", "jpg"}}}
	for name, err := range map[string]error{
		"open title NUL":      OpenOptions{Title: "a\x00b"}.Validate(),
		"open title long":     OpenOptions{Title: long}.Validate(),
		"open title utf8":     OpenOptions{Title: "\xff"}.Validate(),
		"too many filters":    OpenOptions{Filters: manyF}.Validate(),
		"too many exts":       OpenOptions{Filters: []Filter{{Name: "n", Extensions: manyE}}}.Validate(),
		"no ext":              OpenOptions{Filters: []Filter{{Name: "n"}}}.Validate(),
		"empty ext":           OpenOptions{Filters: []Filter{{Name: "n", Extensions: []string{""}}}}.Validate(),
		"glob ext":            OpenOptions{Filters: []Filter{{Name: "n", Extensions: []string{"*"}}}}.Validate(),
		"dot ext":             OpenOptions{Filters: []Filter{{Name: "n", Extensions: []string{".png"}}}}.Validate(),
		"NUL ext":             OpenOptions{Filters: []Filter{{Name: "n", Extensions: []string{"a\x00"}}}}.Validate(),
		"NUL filter name":     OpenOptions{Filters: []Filter{{Name: "\x00", Extensions: []string{"a"}}}}.Validate(),
		"save name path":      SaveOptions{Name: "a/b.txt"}.Validate(),
		"save name backslash": SaveOptions{Name: `a\b`}.Validate(),
		"save name NUL":       SaveOptions{Name: "a\x00"}.Validate(),
		"alert no message":    AlertOptions{}.Validate(),
		"alert NUL":           AlertOptions{Message: "m\x00"}.Validate(),
		"alert detail long":   AlertOptions{Message: "m", Detail: strings.Repeat("a", MaxMessageLen+1)}.Validate(),
		"alert 5 buttons":     AlertOptions{Message: "m", Buttons: []string{"a", "b", "c", "d", "e"}}.Validate(),
		"alert empty button":  AlertOptions{Message: "m", Buttons: []string{""}}.Validate(),
		"alert NUL button":    AlertOptions{Message: "m", Buttons: []string{"\x00"}}.Validate(),
	} {
		if !errors.Is(err, ErrInvalidDialog) {
			t.Errorf("%s: err = %v, want ErrInvalidDialog", name, err)
		}
	}
	for name, err := range map[string]error{
		"open zero":  OpenOptions{}.Validate(),
		"open full":  OpenOptions{Title: "t", Filters: ok, Multiple: true}.Validate(),
		"save":       SaveOptions{Title: "t", Name: "a.png", Filters: ok}.Validate(),
		"alert":      AlertOptions{Message: "m"}.Validate(),
		"alert full": AlertOptions{Message: "m", Detail: "d", Buttons: []string{"Yes", "No", "Maybe", "Never"}}.Validate(),
	} {
		if err != nil {
			t.Errorf("%s: unexpected %v", name, err)
		}
	}
	// Boundaries: exactly at the limit is accepted.
	if err := (OpenOptions{Title: strings.Repeat("a", MaxStringLen)}).Validate(); err != nil {
		t.Errorf("title at the bound: %v", err)
	}
	if err := (OpenOptions{Filters: manyF[:MaxFilters]}).Validate(); err != nil {
		t.Errorf("filters at the bound: %v", err)
	}
	if err := (OpenOptions{Filters: []Filter{{Name: "n", Extensions: manyE[:MaxExtensions]}}}).Validate(); err != nil {
		t.Errorf("extensions at the bound: %v", err)
	}
}

func TestDialogDispatch(t *testing.T) {
	f := &fakeDlg{}
	noop := func(OpenResult) {}
	if err := OpenFile(f, OpenOptions{}, noop); err != nil || f.open != 1 {
		t.Fatalf("OpenFile: %v, %d", err, f.open)
	}
	if err := SaveFile(f, SaveOptions{}, func(SaveResult) {}); err != nil || f.save != 1 {
		t.Fatalf("SaveFile: %v, %d", err, f.save)
	}
	if err := Alert(f, AlertOptions{Message: "m"}, func(AlertResult) {}); err != nil || f.alert != 1 {
		t.Fatalf("Alert: %v, %d", err, f.alert)
	}
	if err := Confirm(f, "m", "", "Yes", "No", func(bool, error) {}); err != nil || f.alert != 2 {
		t.Fatalf("Confirm: %v, %d", err, f.alert)
	}
	// Invalid options never reach the back-end.
	if err := OpenFile(f, OpenOptions{Title: "\x00"}, noop); !errors.Is(err, ErrInvalidDialog) || f.open != 1 {
		t.Errorf("invalid reached the back-end: %v", err)
	}
	if err := SaveFile(f, SaveOptions{Title: "\x00"}, func(SaveResult) {}); !errors.Is(err, ErrInvalidDialog) || f.save != 1 {
		t.Errorf("invalid save reached the back-end: %v", err)
	}
	if err := Alert(f, AlertOptions{}, func(AlertResult) {}); !errors.Is(err, ErrInvalidDialog) || f.alert != 2 {
		t.Errorf("invalid alert reached the back-end: %v", err)
	}
	if err := (SaveOptions{Title: "\x00"}).Validate(); !errors.Is(err, ErrInvalidDialog) {
		t.Errorf("save title NUL: %v", err)
	}
	// Nil callbacks are refused.
	if err := OpenFile(f, OpenOptions{}, nil); !errors.Is(err, ErrInvalidDialog) {
		t.Error("nil open callback accepted")
	}
	if err := SaveFile(f, SaveOptions{}, nil); !errors.Is(err, ErrInvalidDialog) {
		t.Error("nil save callback accepted")
	}
	if err := Alert(f, AlertOptions{Message: "m"}, nil); !errors.Is(err, ErrInvalidDialog) {
		t.Error("nil alert callback accepted")
	}
	if err := Confirm(f, "m", "", "Y", "N", nil); !errors.Is(err, ErrInvalidDialog) {
		t.Error("nil confirm callback accepted")
	}
	// A back-end without the capability answers ErrNotSupported.
	n := noDlgBackend{}
	if err := OpenFile(n, OpenOptions{}, noop); !errors.Is(err, ErrNotSupported) {
		t.Errorf("open: %v", err)
	}
	if err := SaveFile(n, SaveOptions{}, func(SaveResult) {}); !errors.Is(err, ErrNotSupported) {
		t.Errorf("save: %v", err)
	}
	if err := Alert(n, AlertOptions{Message: "m"}, func(AlertResult) {}); !errors.Is(err, ErrNotSupported) {
		t.Errorf("alert: %v", err)
	}
}

type confirmBackend struct {
	noDlgBackend
	r AlertResult
}

func (c confirmBackend) Alert(_ AlertOptions, done func(AlertResult)) error { done(c.r); return nil }
func (confirmBackend) OpenFile(OpenOptions, func(OpenResult)) error         { return nil }
func (confirmBackend) SaveFile(SaveOptions, func(SaveResult)) error         { return nil }

func TestConfirmMapsButtons(t *testing.T) {
	for _, tc := range []struct {
		r    AlertResult
		want bool
	}{
		{AlertResult{Button: 0}, true},
		{AlertResult{Button: 1}, false},
		{AlertResult{Button: 0, Err: ErrCancelled}, false},
	} {
		var got bool
		var gerr error
		_ = Confirm(confirmBackend{r: tc.r}, "m", "", "Y", "N", func(y bool, err error) { got, gerr = y, err })
		if got != tc.want || gerr != tc.r.Err {
			t.Errorf("%+v: got (%v, %v), want %v", tc.r, got, gerr, tc.want)
		}
	}
}
