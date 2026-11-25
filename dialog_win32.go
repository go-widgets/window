// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"

	"github.com/go-widgets/window/internal/win32"
)

// The pure half of the Win32 Dialogs back-end, free of build tags so that it is
// tested on every OS: how the options become the back-end's request and how its
// result becomes the public one. The windows-only methods are in open_windows.go.

func win32Filters(fs []Filter) []win32.DialogFilter {
	if len(fs) == 0 {
		return nil
	}
	out := make([]win32.DialogFilter, len(fs))
	for i, f := range fs {
		out[i] = win32.DialogFilter{Name: f.Name, Exts: f.Extensions}
	}
	return out
}

func win32OpenRequest(o OpenOptions, done func(OpenResult)) win32.DialogRequest {
	return win32.DialogRequest{
		Kind: win32.DialogOpen, Title: o.Title, Filters: win32Filters(o.Filters),
		Multiple: o.Multiple, Directories: o.Directories,
		Done: func(r win32.DialogResult) { done(win32OpenResult(r)) },
	}
}

func win32SaveRequest(o SaveOptions, done func(SaveResult)) win32.DialogRequest {
	return win32.DialogRequest{
		Kind: win32.DialogSave, Title: o.Title, Name: o.Name, Filters: win32Filters(o.Filters),
		Done: func(r win32.DialogResult) { done(win32SaveResult(r)) },
	}
}

func win32AlertRequest(o AlertOptions, done func(AlertResult)) win32.DialogRequest {
	return win32.DialogRequest{
		Kind: win32.DialogAlert, Message: o.Message, Detail: o.Detail, Buttons: o.Buttons,
		Done: func(r win32.DialogResult) { done(win32AlertResult(r)) },
	}
}

func win32OpenResult(r win32.DialogResult) OpenResult {
	switch {
	case r.Err != nil:
		return OpenResult{Err: win32DialogErr(r.Err)}
	case r.Cancelled || len(r.Paths) == 0:
		return OpenResult{Err: ErrCancelled}
	}
	return OpenResult{Paths: r.Paths}
}

func win32SaveResult(r win32.DialogResult) SaveResult {
	switch {
	case r.Err != nil:
		return SaveResult{Err: win32DialogErr(r.Err)}
	case r.Cancelled || len(r.Paths) == 0:
		return SaveResult{Err: ErrCancelled}
	}
	return SaveResult{Path: r.Paths[0]}
}

func win32AlertResult(r win32.DialogResult) AlertResult {
	switch {
	case r.Err != nil:
		return AlertResult{Err: win32DialogErr(r.Err)}
	case r.Cancelled:
		return AlertResult{Err: ErrCancelled}
	}
	return AlertResult{Button: r.Button}
}

// win32DialogErr puts the back-end's closed-window error in this package's
// vocabulary and leaves a platform failure as it is.
func win32DialogErr(err error) error {
	if errors.Is(err, win32.ErrDialogClosed) {
		return ErrClosed
	}
	return err
}
