// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// These tests exercise the sovereign wasmbox client codec on the host GOOS:
// message encode/decode, SAB damage-rectangle maths and the input→toolkit.Event
// mapping. They carry no syscall/js dependency and cover every statement +
// branch, so the codec is provably correct independently of the browser glue.
package wasmbox

import (
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestEncodeHello(t *testing.T) {
	got := EncodeHello(Hello{Title: "app", W: 200, H: 150, Stride: 800})
	want := map[string]any{
		"type": KindHello, "title": "app", "role": RoleWindow,
		"w": 200, "h": 150, "stride": 800,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EncodeHello = %v, want %v", got, want)
	}
}

func TestDecodeWelcome(t *testing.T) {
	// JS delivers numbers as float64; the codec must coerce them.
	w, err := DecodeWelcome(map[string]any{
		"type": KindWelcome, "window_id": float64(7),
		"granted_w": float64(200), "granted_h": float64(150),
	})
	if err != nil {
		t.Fatalf("DecodeWelcome err = %v", err)
	}
	if (w != Welcome{WindowID: 7, GrantedW: 200, GrantedH: 150}) {
		t.Fatalf("DecodeWelcome = %+v", w)
	}
	if _, err := DecodeWelcome(map[string]any{"type": KindCommit}); err == nil {
		t.Fatal("DecodeWelcome should reject a non-welcome message")
	}
}

func TestEncodeCommit(t *testing.T) {
	got := EncodeCommit(7, Rect{X: 1, Y: 2, W: 3, H: 4})
	want := map[string]any{
		"type": KindCommit, "window_id": 7,
		"damage": map[string]any{"x": 1, "y": 2, "w": 3, "h": 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EncodeCommit = %v, want %v", got, want)
	}
}

func TestEncodeSetTitleAndRequestClose(t *testing.T) {
	if got := EncodeSetTitle(7, "hi"); !reflect.DeepEqual(got, map[string]any{
		"type": KindSetTitle, "window_id": 7, "title": "hi",
	}) {
		t.Fatalf("EncodeSetTitle = %v", got)
	}
	if got := EncodeRequestClose(7); !reflect.DeepEqual(got, map[string]any{
		"type": KindRequestClose, "window_id": 7,
	}) {
		t.Fatalf("EncodeRequestClose = %v", got)
	}
}

func TestDecodeInput(t *testing.T) {
	wid, ev, err := DecodeInput(map[string]any{
		"type": KindInput, "window_id": float64(7),
		"event": map[string]any{
			"kind": "mousedown", "x": float64(42), "y": float64(17),
			"button": float64(2), "key": "", "code": "", "dx": float64(0), "dy": float64(0),
		},
	})
	if err != nil {
		t.Fatalf("DecodeInput err = %v", err)
	}
	if wid != 7 {
		t.Fatalf("windowID = %d, want 7", wid)
	}
	want := InputEvent{Kind: "mousedown", X: 42, Y: 17, Button: 2}
	if ev != want {
		t.Fatalf("DecodeInput ev = %+v, want %+v", ev, want)
	}
	// Wrong type is rejected.
	if _, _, err := DecodeInput(map[string]any{"type": KindWelcome}); err == nil {
		t.Fatal("DecodeInput should reject a non-input message")
	}
	// A missing/typeless event object decodes to a zero InputEvent, not a panic.
	_, ev2, err := DecodeInput(map[string]any{"type": KindInput, "window_id": 1})
	if err != nil || (ev2 != InputEvent{}) {
		t.Fatalf("DecodeInput with no event = %+v, %v", ev2, err)
	}
}

func TestDecodeClosed(t *testing.T) {
	wid, reason, err := DecodeClosed(map[string]any{
		"type": KindClosed, "window_id": float64(7), "reason": "user",
	})
	if err != nil || wid != 7 || reason != "user" {
		t.Fatalf("DecodeClosed = %d, %q, %v", wid, reason, err)
	}
	if _, _, err := DecodeClosed(map[string]any{"type": KindInput}); err == nil {
		t.Fatal("DecodeClosed should reject a non-closed message")
	}
}

func TestMessageKind(t *testing.T) {
	if got := MessageKind(map[string]any{"type": KindHello}); got != KindHello {
		t.Fatalf("MessageKind = %q", got)
	}
	if got := MessageKind(map[string]any{}); got != "" {
		t.Fatalf("MessageKind of typeless = %q, want empty", got)
	}
}

func TestFullRect(t *testing.T) {
	if got := FullRect(4, 3); (got != Rect{W: 4, H: 3}) {
		t.Fatalf("FullRect = %+v", got)
	}
}

func TestClampRect(t *testing.T) {
	cases := []struct {
		name string
		in   Rect
		w, h int
		want Rect
	}{
		{"inside", Rect{1, 1, 2, 2}, 10, 10, Rect{1, 1, 2, 2}},
		{"negX", Rect{-2, 1, 5, 2}, 10, 10, Rect{0, 1, 3, 2}},
		{"negY", Rect{1, -2, 2, 5}, 10, 10, Rect{1, 0, 2, 3}},
		{"overW", Rect{8, 1, 5, 2}, 10, 10, Rect{8, 1, 2, 2}},
		{"overH", Rect{1, 8, 2, 5}, 10, 10, Rect{1, 8, 2, 2}},
		{"offSurface", Rect{20, 20, 4, 4}, 10, 10, Rect{20, 20, -10, -10}},
	}
	for _, c := range cases {
		if got := ClampRect(c.in, c.w, c.h); got != c.want {
			t.Errorf("%s: ClampRect = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestRowSpan(t *testing.T) {
	start, rowBytes := RowSpan(Rect{X: 3, Y: 2, W: 5, H: 4}, 800)
	if start != 2*800+3*4 || rowBytes != 5*4 {
		t.Fatalf("RowSpan = %d, %d", start, rowBytes)
	}
}

func TestMapInputMouse(t *testing.T) {
	if got := MapInput(InputEvent{Kind: InMouseDown, X: 5, Y: 6, Button: 0}, false); !reflect.DeepEqual(
		got, []toolkit.Event{{Kind: toolkit.EventClick, X: 5, Y: 6}}) {
		t.Fatalf("mousedown = %+v", got)
	}
	if got := MapInput(InputEvent{Kind: InMouseUp, X: 5, Y: 6}, false); !reflect.DeepEqual(
		got, []toolkit.Event{{Kind: toolkit.EventMouseUp, X: 5, Y: 6}}) {
		t.Fatalf("mouseup = %+v", got)
	}
	if got := MapInput(InputEvent{Kind: InMouseMove, X: 5, Y: 6}, false); got[0].Kind != toolkit.EventMouseMove {
		t.Fatalf("mousemove (no button) = %+v", got)
	}
	if got := MapInput(InputEvent{Kind: InMouseMove, X: 5, Y: 6}, true); got[0].Kind != toolkit.EventMouseDrag {
		t.Fatalf("mousemove (button held) = %+v", got)
	}
}

func TestMapInputWheel(t *testing.T) {
	cases := []struct {
		dx, dy int
		delta  int
	}{
		{0, -3, -1}, // scroll up
		{0, 4, 1},   // scroll down
		{-2, 0, -1}, // horizontal fallback, left
		{5, 0, 1},   // horizontal fallback, right
		{0, 0, 0},   // no motion
	}
	for _, c := range cases {
		got := MapInput(InputEvent{Kind: InWheel, X: 1, Y: 2, DX: c.dx, DY: c.dy}, false)
		if len(got) != 1 || got[0].Kind != toolkit.EventScroll || got[0].Delta != c.delta {
			t.Fatalf("wheel dx=%d dy=%d = %+v, want Delta %d", c.dx, c.dy, got, c.delta)
		}
	}
}

func TestMapInputKeyboard(t *testing.T) {
	// Printable press → KeyDown + Char.
	got := MapInput(InputEvent{Kind: InKeyDown, Key: "a"}, false)
	want := []toolkit.Event{
		{Kind: toolkit.EventKeyDown, Code: "a"},
		{Kind: toolkit.EventChar, Code: "a"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keydown printable = %+v", got)
	}
	// A multi-byte single rune is still printable.
	if got := MapInput(InputEvent{Kind: InKeyDown, Key: "é"}, false); len(got) != 2 || got[1].Kind != toolkit.EventChar {
		t.Fatalf("keydown multibyte = %+v", got)
	}
	// Printable release → single KeyUp.
	if got := MapInput(InputEvent{Kind: InKeyUp, Key: "a"}, false); !reflect.DeepEqual(
		got, []toolkit.Event{{Kind: toolkit.EventKeyUp, Code: "a"}}) {
		t.Fatalf("keyup printable = %+v", got)
	}
	// Named key press → single KeyDown carrying the name.
	if got := MapInput(InputEvent{Kind: InKeyDown, Key: "ArrowLeft"}, false); !reflect.DeepEqual(
		got, []toolkit.Event{{Kind: toolkit.EventKeyDown, Code: "ArrowLeft"}}) {
		t.Fatalf("keydown named = %+v", got)
	}
	// Named key release → single KeyUp carrying the name.
	if got := MapInput(InputEvent{Kind: InKeyUp, Key: "Enter"}, false); !reflect.DeepEqual(
		got, []toolkit.Event{{Kind: toolkit.EventKeyUp, Code: "Enter"}}) {
		t.Fatalf("keyup named = %+v", got)
	}
	// Modifier keys deliver nothing.
	if got := MapInput(InputEvent{Kind: InKeyDown, Key: "Shift"}, false); got != nil {
		t.Fatalf("modifier keydown = %+v, want nil", got)
	}
	// An empty key delivers nothing.
	if got := MapInput(InputEvent{Kind: InKeyDown, Key: ""}, false); got != nil {
		t.Fatalf("empty keydown = %+v, want nil", got)
	}
}

func TestMapInputUnknownKind(t *testing.T) {
	if got := MapInput(InputEvent{Kind: "contextmenu"}, false); got != nil {
		t.Fatalf("unknown kind = %+v, want nil", got)
	}
}

func TestToInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{int(5), 5},
		{int64(6), 6},
		{float64(7.9), 7},
		{float32(8.9), 8},
		{"nope", 0},
		{nil, 0},
	}
	for _, c := range cases {
		if got := toInt(c.in); got != c.want {
			t.Errorf("toInt(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestToStr(t *testing.T) {
	if got := toStr("x"); got != "x" {
		t.Fatalf("toStr(string) = %q", got)
	}
	if got := toStr(42); got != "" {
		t.Fatalf("toStr(non-string) = %q, want empty", got)
	}
}
