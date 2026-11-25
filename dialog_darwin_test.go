// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package window

import (
	"errors"
	"reflect"
	"testing"

	"github.com/go-widgets/window/internal/cocoa"
)

func TestDarwinDialogResultMapping(t *testing.T) {
	if r := openResult(cocoa.DialogResult{Cancelled: true, Paths: []string{"/x"}}); !errors.Is(r.Err, ErrCancelled) || r.Paths != nil {
		t.Errorf("cancelled open = %+v", r)
	}
	if r := openResult(cocoa.DialogResult{Paths: []string{"/a", "/b"}}); r.Err != nil || !reflect.DeepEqual(r.Paths, []string{"/a", "/b"}) {
		t.Errorf("open = %+v", r)
	}
	if r := saveResult(cocoa.DialogResult{Cancelled: true, Paths: []string{"/x"}}); !errors.Is(r.Err, ErrCancelled) || r.Path != "" {
		t.Errorf("cancelled save = %+v", r)
	}
	if r := saveResult(cocoa.DialogResult{}); !errors.Is(r.Err, ErrCancelled) {
		t.Errorf("save with no path = %+v", r)
	}
	if r := saveResult(cocoa.DialogResult{Paths: []string{"/s"}}); r.Err != nil || r.Path != "/s" {
		t.Errorf("save = %+v", r)
	}
	if got := flattenExts([]Filter{{Extensions: []string{"a", "b"}}, {Extensions: []string{"c"}}}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("flattenExts = %v", got)
	}
}
