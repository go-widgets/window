// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"reflect"
	"testing"

	"github.com/go-widgets/window/internal/win32"
)

func TestWin32Requests(t *testing.T) {
	var got OpenResult
	req := win32OpenRequest(OpenOptions{
		Title: "t", Multiple: true, Directories: true,
		Filters: []Filter{{Name: "Img", Extensions: []string{"png"}}},
	}, func(r OpenResult) { got = r })
	if req.Kind != win32.DialogOpen || req.Title != "t" || !req.Multiple || !req.Directories ||
		!reflect.DeepEqual(req.Filters, []win32.DialogFilter{{Name: "Img", Exts: []string{"png"}}}) {
		t.Errorf("open request = %+v", req)
	}
	req.Done(win32.DialogResult{Paths: []string{`C:\a`, `C:\b`}})
	if !reflect.DeepEqual(got.Paths, []string{`C:\a`, `C:\b`}) || got.Err != nil {
		t.Errorf("open result = %+v", got)
	}

	var sv SaveResult
	sreq := win32SaveRequest(SaveOptions{Title: "s", Name: "n.txt"}, func(r SaveResult) { sv = r })
	if sreq.Kind != win32.DialogSave || sreq.Name != "n.txt" || sreq.Title != "s" || sreq.Filters != nil {
		t.Errorf("save request = %+v", sreq)
	}
	sreq.Done(win32.DialogResult{Paths: []string{`C:\n.txt`}})
	if sv.Path != `C:\n.txt` || sv.Err != nil {
		t.Errorf("save result = %+v", sv)
	}

	var al AlertResult
	areq := win32AlertRequest(AlertOptions{Message: "m", Detail: "d", Buttons: []string{"a", "b"}}, func(r AlertResult) { al = r })
	if areq.Kind != win32.DialogAlert || areq.Message != "m" || areq.Detail != "d" || !reflect.DeepEqual(areq.Buttons, []string{"a", "b"}) {
		t.Errorf("alert request = %+v", areq)
	}
	areq.Done(win32.DialogResult{Button: 1})
	if al.Button != 1 || al.Err != nil {
		t.Errorf("alert result = %+v", al)
	}
}

func TestWin32Results(t *testing.T) {
	boom := errors.New("boom")
	if r := win32OpenResult(win32.DialogResult{Cancelled: true, Paths: []string{"x"}}); !errors.Is(r.Err, ErrCancelled) || r.Paths != nil {
		t.Errorf("cancelled open = %+v", r)
	}
	if r := win32OpenResult(win32.DialogResult{}); !errors.Is(r.Err, ErrCancelled) {
		t.Errorf("empty open = %+v", r)
	}
	if r := win32OpenResult(win32.DialogResult{Err: boom}); r.Err != boom {
		t.Errorf("failed open = %+v", r)
	}
	if r := win32SaveResult(win32.DialogResult{Cancelled: true, Paths: []string{"x"}}); !errors.Is(r.Err, ErrCancelled) || r.Path != "" {
		t.Errorf("cancelled save = %+v", r)
	}
	if r := win32SaveResult(win32.DialogResult{}); !errors.Is(r.Err, ErrCancelled) {
		t.Errorf("empty save = %+v", r)
	}
	if r := win32SaveResult(win32.DialogResult{Err: boom}); r.Err != boom {
		t.Errorf("failed save = %+v", r)
	}
	if r := win32AlertResult(win32.DialogResult{Cancelled: true, Button: 1}); !errors.Is(r.Err, ErrCancelled) {
		t.Errorf("cancelled alert = %+v", r)
	}
	if r := win32AlertResult(win32.DialogResult{Err: boom}); r.Err != boom {
		t.Errorf("failed alert = %+v", r)
	}
	if r := win32AlertResult(win32.DialogResult{Err: win32.ErrDialogClosed}); !errors.Is(r.Err, ErrClosed) {
		t.Errorf("closed alert = %+v", r)
	}
	if r := win32OpenResult(win32.DialogResult{Err: win32.ErrDialogClosed}); !errors.Is(r.Err, ErrClosed) {
		t.Errorf("closed open = %+v", r)
	}
	if r := win32SaveResult(win32.DialogResult{Err: win32.ErrDialogClosed}); !errors.Is(r.Err, ErrClosed) {
		t.Errorf("closed save = %+v", r)
	}
	if win32Filters(nil) != nil {
		t.Errorf("no filters is not nil")
	}
}
