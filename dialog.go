// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Dialogs is an optional [Backend] capability: the platform's own modal dialogs
// -- open-file, save-file and message -- as opposed to anything drawn by the
// toolkit. It is implemented by the macOS (NSOpenPanel, NSSavePanel, NSAlert)
// back-end; [OpenFile], [SaveFile] and [Alert] answer [ErrNotSupported] for a
// back-end without it (X11, Wayland, wasmbox, Android until their own
// implementations land).
//
// Threading. Every request is asynchronous and safe from ANY goroutine,
// including the UI goroutine and a tray callback: it validates its options,
// hands the request to the platform's UI thread, and returns at once. The
// dialog is then shown there, and the callback is invoked exactly once, on a
// goroutine of its own, never on the UI thread -- so a callback may block, call
// back into the window or open the next dialog without freezing the loop. If
// the request cannot even be queued the error is returned and the callback is
// never called.
//
// Paths are returned exactly as the platform hands them over. Nothing is
// resolved, cleaned, made absolute or checked for existence, and symbolic
// links are not followed: a caller that is about to open the path owns that
// decision.
type Dialogs interface {
	OpenFile(OpenOptions, func(OpenResult)) error
	SaveFile(SaveOptions, func(SaveResult)) error
	Alert(AlertOptions, func(AlertResult)) error
}

// ErrCancelled is the Err of a result the user dismissed without choosing:
// Cancel on a file dialog, Escape or the close box on an alert that has no
// cancel button of its own.
var ErrCancelled = errors.New("window: dialog cancelled")

// ErrInvalidDialog wraps every rejection of a dialog's options.
var ErrInvalidDialog = errors.New("window: invalid dialog options")

// Bounds on what a dialog request may carry. They exist so that a caller
// forwarding untrusted strings cannot make the platform allocate without limit.
const (
	MaxFilters    = 32   // file-type filters per dialog
	MaxExtensions = 64   // extensions per filter
	MaxButtons    = 4    // buttons per alert
	MaxStringLen  = 1024 // bytes: title, suggested name, filter name, extension, button
	MaxMessageLen = 4096 // bytes: an alert's message and detail
)

// Filter is one file-type filter: a display name and the extensions it admits,
// without the leading dot ("png", not ".png"). An extension is not a glob;
// "*" and "?" are rejected.
type Filter struct {
	Name       string
	Extensions []string
}

// OpenOptions configures [OpenFile]. The zero value asks for one existing file.
type OpenOptions struct {
	Title       string   // window title or prompt; empty means the platform's own
	Filters     []Filter // what may be chosen; empty means anything
	Multiple    bool     // allow more than one choice
	Directories bool     // choose directories instead of files
}

// OpenResult is what an [OpenFile] callback receives. On success Paths holds at
// least one path and Err is nil; on cancel Err is [ErrCancelled]; on a platform
// failure Err says so.
type OpenResult struct {
	Paths []string
	Err   error
}

// SaveOptions configures [SaveFile].
type SaveOptions struct {
	Title   string   // window title or prompt
	Name    string   // suggested file name, never a path
	Filters []Filter // types the file may be saved as; the first is the default
}

// SaveResult is what a [SaveFile] callback receives. Path is the platform's
// answer, which may name a file that does not exist yet.
type SaveResult struct {
	Path string
	Err  error
}

// AlertOptions configures [Alert] and [Confirm].
type AlertOptions struct {
	Message string   // the headline; required
	Detail  string   // informative text under it
	Buttons []string // one to MaxButtons labels, the first being the default; none means a single "OK"
}

// AlertResult is what an [Alert] callback receives: Button is the index into
// AlertOptions.Buttons of the button that was pressed (0 when none were given).
type AlertResult struct {
	Button int
	Err    error
}

// OpenFile shows the platform's open-file dialog on b; see [Dialogs] for the
// threading and path rules. The options are validated first.
func OpenFile(b Backend, o OpenOptions, done func(OpenResult)) error {
	if done == nil {
		return fmt.Errorf("%w: nil callback", ErrInvalidDialog)
	}
	if err := o.Validate(); err != nil {
		return err
	}
	if d, ok := b.(Dialogs); ok {
		return d.OpenFile(o, done)
	}
	return noDialogs(b)
}

// SaveFile shows the platform's save-file dialog on b; see [Dialogs].
func SaveFile(b Backend, o SaveOptions, done func(SaveResult)) error {
	if done == nil {
		return fmt.Errorf("%w: nil callback", ErrInvalidDialog)
	}
	if err := o.Validate(); err != nil {
		return err
	}
	if d, ok := b.(Dialogs); ok {
		return d.SaveFile(o, done)
	}
	return noDialogs(b)
}

// Alert shows the platform's message dialog on b; see [Dialogs]. With one
// button it is an alert, with two or more it is a choice -- see [Confirm].
func Alert(b Backend, o AlertOptions, done func(AlertResult)) error {
	if done == nil {
		return fmt.Errorf("%w: nil callback", ErrInvalidDialog)
	}
	if err := o.Validate(); err != nil {
		return err
	}
	if d, ok := b.(Dialogs); ok {
		return d.Alert(o, done)
	}
	return noDialogs(b)
}

// Confirm is [Alert] with two buttons, yes first (the default) and no second.
func Confirm(b Backend, message, detail, yes, no string, done func(yes bool, err error)) error {
	if done == nil {
		return fmt.Errorf("%w: nil callback", ErrInvalidDialog)
	}
	return Alert(b, AlertOptions{Message: message, Detail: detail, Buttons: []string{yes, no}}, func(r AlertResult) {
		done(r.Err == nil && r.Button == 0, r.Err)
	})
}

func noDialogs(b Backend) error {
	return fmt.Errorf("%w: %v has no native dialogs", ErrNotSupported, b)
}

// Validate checks the options against the package bounds. Every error wraps
// [ErrInvalidDialog].
func (o OpenOptions) Validate() error {
	if err := checkString("title", o.Title, MaxStringLen); err != nil {
		return err
	}
	return checkFilters(o.Filters)
}

// Validate checks the options against the package bounds.
func (o SaveOptions) Validate() error {
	if err := checkString("title", o.Title, MaxStringLen); err != nil {
		return err
	}
	if err := checkString("name", o.Name, MaxStringLen); err != nil {
		return err
	}
	if strings.ContainsAny(o.Name, `/\`) {
		return invalid("name %q is a path, not a file name", o.Name)
	}
	return checkFilters(o.Filters)
}

// Validate checks the options against the package bounds.
func (o AlertOptions) Validate() error {
	if o.Message == "" {
		return invalid("message is empty")
	}
	if err := checkString("message", o.Message, MaxMessageLen); err != nil {
		return err
	}
	if err := checkString("detail", o.Detail, MaxMessageLen); err != nil {
		return err
	}
	if len(o.Buttons) > MaxButtons {
		return invalid("%d buttons, at most %d", len(o.Buttons), MaxButtons)
	}
	for i, b := range o.Buttons {
		if b == "" {
			return invalid("button %d has an empty label", i)
		}
		if err := checkString(fmt.Sprintf("button %d", i), b, MaxStringLen); err != nil {
			return err
		}
	}
	return nil
}

func checkFilters(fs []Filter) error {
	if len(fs) > MaxFilters {
		return invalid("%d filters, at most %d", len(fs), MaxFilters)
	}
	for i, f := range fs {
		if err := checkString(fmt.Sprintf("filter %d name", i), f.Name, MaxStringLen); err != nil {
			return err
		}
		if len(f.Extensions) == 0 {
			return invalid("filter %d has no extension", i)
		}
		if len(f.Extensions) > MaxExtensions {
			return invalid("filter %d has %d extensions, at most %d", i, len(f.Extensions), MaxExtensions)
		}
		for j, e := range f.Extensions {
			if e == "" {
				return invalid("filter %d extension %d is empty", i, j)
			}
			if err := checkString(fmt.Sprintf("filter %d extension %d", i, j), e, MaxStringLen); err != nil {
				return err
			}
			if strings.ContainsAny(e, "*?./\\") {
				return invalid("filter %d extension %q: no dot, glob or separator", i, e)
			}
		}
	}
	return nil
}

func checkString(what, s string, max int) error {
	if len(s) > max {
		return invalid("%s is %d bytes, at most %d", what, len(s), max)
	}
	if strings.IndexByte(s, 0) >= 0 {
		return invalid("%s contains NUL", what)
	}
	if !utf8.ValidString(s) {
		return invalid("%s is not valid UTF-8", what)
	}
	return nil
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDialog, fmt.Sprintf(format, a...))
}
