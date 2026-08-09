// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

// This is the syscall/js transport that binds the sovereign codec (protocol.go)
// to a live wasmbox compositor. It runs inside a dedicated Web Worker (see
// clients/gowidgets/worker.js): it allocates the surface SharedArrayBuffer,
// posts `hello` over the per-client MessagePort the compositor hands it, awaits
// `welcome`, then paints the go-widgets root into the SAB and posts `commit`
// (whole-surface, or — when the root implements RenderDamaged, e.g.
// scene.HostRoot — only the damaged rectangles). Incoming `input` messages are
// mapped to toolkit.Event via the codec and delivered to the widget tree;
// `closed` ends Run. All wire (de)serialisation and the input mapping go
// through protocol.go, which is unit-tested to 100% on the host GOOS — this file
// is the (browser-only, hence untestable off js) glue that carries JS handles.
package wasmbox

import (
	"fmt"
	"sync"
	"syscall/js"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// portHandoff is the one-shot message the compositor posts to a freshly-spawned
// client worker, carrying the private MessagePort for all further traffic
// (docs/protocol.md, step C.1). The SDK's `__wasmbox_port` handshake is
// reproduced here so this backend needs no JS SDK.
const portHandoff = "__wasmbox_port"

// installHook is the global function clients/gowidgets/worker.js installs so the
// Go runtime can drain messages the worker buffered before wasm booted (the
// compositor's port handoff can arrive before go.run starts). Passing a handler
// to it replays the buffer in order and routes every later `self` message.
const installHook = "__gowidgetsInstall"

// damageRenderer is the OPT-IN incremental-present capability, declared
// structurally so this backend needs no import of window (which imports this
// package) nor of the scene layer. A root that also implements it (e.g.
// toolkit/scene.HostRoot) drives damage-rectangle commits; a plain root uses
// whole-surface commits. It mirrors window.DamageRenderer exactly.
type damageRenderer interface {
	RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect
}

// Client is an open wasmbox external-client surface bound to a go-widgets scene.
// It satisfies window.Backend (Run/Close/Size/String), so a go-widgets app runs
// through Open→Run unchanged whether the backend is X11, Wayland or wasmbox.
type Client struct {
	title string
	w, h  int
	theme *toolkit.Theme

	buf   []byte   // RGBA framebuffer, 4*w*h bytes
	sab   js.Value // SharedArrayBuffer shared with the compositor
	sabU8 js.Value // Uint8Array view over sab, for js.CopyBytesToJS

	port     js.Value // per-client MessagePort (compositor step C.1)
	windowID int

	root       toolkit.Widget
	dmg        damageRenderer
	buttonHeld bool // pointer-button state, so mousemove picks drag vs move

	welcomeCh chan Welcome
	done      chan struct{}
	closeOnce sync.Once

	funcs []js.Func // retained callbacks to Release on shutdown
}

// Compile-time shape check against the window.Backend method set (Run/Close/
// Size/String); the interface itself lives in package window.
var _ interface {
	Run(root toolkit.Widget) error
	Close() error
	Size() (int, int)
	String() string
} = (*Client)(nil)

// Dial performs the wasmbox client handshake and returns a Client ready for
// Run: it allocates the w×h×4 SAB, installs the worker message pump, and — once
// the compositor hands over the MessagePort — posts `hello` (with the SAB handle
// + stride) and blocks until `welcome` grants a window id and size. It is the
// browser-environment analogue of openX11/openWayland.
func Dial(title string, w, h int, theme *toolkit.Theme) (*Client, error) {
	if w <= 0 {
		w = 640
	}
	if h <= 0 {
		h = 480
	}
	if theme == nil {
		theme = toolkit.DefaultDark()
	}
	sab := js.Global().Get("SharedArrayBuffer")
	if !sab.Truthy() {
		return nil, fmt.Errorf("wasmbox: SharedArrayBuffer unavailable (page needs COOP/COEP cross-origin isolation)")
	}
	c := &Client{
		title:     title,
		w:         w,
		h:         h,
		theme:     theme,
		buf:       make([]byte, 4*w*h),
		welcomeCh: make(chan Welcome, 1),
		done:      make(chan struct{}),
	}
	c.sab = sab.New(4 * w * h)
	c.sabU8 = js.Global().Get("Uint8Array").New(c.sab)

	// The worker buffers early `self` messages and replays them here. The port
	// handoff routes through swapPort; any other typed message (a harness that
	// talks directly on `self` without a port) routes straight to dispatch.
	onSelf := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := args[0]
		if MessageKindJS(data) == portHandoff {
			c.swapPort(data.Get("port"))
		} else {
			c.dispatch(data)
		}
		return nil
	})
	c.funcs = append(c.funcs, onSelf)
	js.Global().Call(installHook, onSelf)

	welcome := <-c.welcomeCh
	c.windowID = welcome.WindowID
	if welcome.GrantedW > 0 && welcome.GrantedH > 0 &&
		(welcome.GrantedW != c.w || welcome.GrantedH != c.h) {
		c.w, c.h = welcome.GrantedW, welcome.GrantedH
		c.buf = make([]byte, 4*c.w*c.h)
		c.sab = sab.New(4 * c.w * c.h)
		c.sabU8 = js.Global().Get("Uint8Array").New(c.sab)
	}
	return c, nil
}

// swapPort adopts the compositor's per-client MessagePort, routes its messages
// to dispatch, and posts the opening `hello` (SAB handle + stride) over it.
func (c *Client) swapPort(port js.Value) {
	c.port = port
	onPort := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		c.dispatch(args[0].Get("data"))
		return nil
	})
	c.funcs = append(c.funcs, onPort)
	port.Set("onmessage", onPort) // setting onmessage implicitly starts the port
	c.postHello()
}

// postHello sends the opening handshake with the shared surface attached. The
// SAB is shared by reference (not transferred), so no transfer list is used.
func (c *Client) postHello() {
	msg := objectFromMap(EncodeHello(Hello{Title: c.title, W: c.w, H: c.h, Stride: 4 * c.w}))
	msg.Set("sab", c.sab)
	c.send(msg)
}

// send posts a message over the active channel (the per-client port, or `self`
// as a fallback for a portless harness).
func (c *Client) send(msg js.Value) {
	if c.port.Truthy() {
		c.port.Call("postMessage", msg)
		return
	}
	js.Global().Call("postMessage", msg)
}

// dispatch routes one decoded compositor message (welcome/input/closed) through
// the codec into backend action.
func (c *Client) dispatch(data js.Value) {
	switch MessageKindJS(data) {
	case KindWelcome:
		w, err := DecodeWelcome(welcomeMap(data))
		if err != nil {
			return
		}
		select {
		case c.welcomeCh <- w:
		default:
		}
	case KindInput:
		_, ev, err := DecodeInput(inputMap(data))
		if err != nil {
			return
		}
		c.handleInput(ev)
	case KindClosed:
		c.shutdown(false)
	}
}

// handleInput maps a wasmbox input event to toolkit events, delivers them to the
// widget tree, tracks the pointer-button state (so a later mousemove is a drag),
// then repaints + commits the frame.
func (c *Client) handleInput(ev InputEvent) {
	if c.root != nil {
		for _, te := range MapInput(ev, c.buttonHeld) {
			c.root.OnEvent(te)
		}
	}
	switch ev.Kind {
	case InMouseDown:
		c.buttonHeld = true
	case InMouseUp:
		c.buttonHeld = false
	}
	c.frame()
}

// Run binds root, performs the initial full-surface paint + commit, then blocks
// while compositor input callbacks drive the widget tree until `closed` (or
// Close) ends the session. The Go runtime yields to the JS event loop while
// blocked, so the js.FuncOf callbacks fire and repaint between frames — the
// browser analogue of the X11/Wayland event loops.
func (c *Client) Run(root toolkit.Widget) error {
	c.root = root
	c.dmg, _ = root.(damageRenderer)
	c.frame() // seed: full-surface for a plain root, full seed-damage for a scene root
	<-c.done
	return nil
}

// frame repaints and commits. A plain root repaints the whole surface and posts
// one full-surface commit; an incremental (DamageRenderer) root repaints only
// the reported rectangles and posts one commit per (clamped, non-empty) rect.
// The whole SAB is refreshed each frame (a single CopyBytesToJS), so damage only
// narrows the compositor's blit region, never risks a stale copy.
func (c *Client) frame() {
	if c.dmg != nil {
		rects := c.drawIncremental()
		c.copyToSAB()
		for _, r := range rects {
			cr := ClampRect(Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, c.w, c.h)
			if cr.W > 0 && cr.H > 0 {
				c.commit(cr)
			}
		}
		return
	}
	c.draw()
	c.copyToSAB()
	c.commit(FullRect(c.w, c.h))
}

// draw repaints the whole framebuffer: background fill then the root laid out to
// fill the surface (the full-surface path, mirroring the X11/Wayland backends).
func (c *Client) draw() {
	p := painter.NewPixelPainter(c.buf, c.w, c.h)
	full := toolkit.Rect{X: 0, Y: 0, W: c.w, H: c.h}
	p.FillRect(full, c.theme.Background)
	if c.root != nil {
		c.root.SetBounds(full)
		c.root.Draw(p, c.theme)
	}
}

// drawIncremental lays the root out to the full surface and repaints only the
// damage the root reports, returning those rectangles for commit.
func (c *Client) drawIncremental() []toolkit.Rect {
	p := painter.NewPixelPainter(c.buf, c.w, c.h)
	c.root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: c.w, H: c.h})
	return c.dmg.RenderDamaged(p, c.theme)
}

// copyToSAB streams the whole RGBA framebuffer into the shared surface in one
// copy (the compositor reads damage sub-rectangles out of it on each commit).
func (c *Client) copyToSAB() { js.CopyBytesToJS(c.sabU8, c.buf) }

// commit tells the compositor which surface-local rectangle changed.
func (c *Client) commit(damage Rect) {
	if c.windowID == 0 {
		return
	}
	c.send(objectFromMap(EncodeCommit(c.windowID, damage)))
}

// SetTitle updates the window title (a `set_title` message). Exposed for apps
// that retitle at runtime; unused by the core Run loop.
func (c *Client) SetTitle(title string) {
	c.title = title
	if c.windowID == 0 {
		return
	}
	c.send(objectFromMap(EncodeSetTitle(c.windowID, title)))
}

// Size returns the current surface size in pixels.
func (c *Client) Size() (int, int) { return c.w, c.h }

// String identifies the client for debugging.
func (c *Client) String() string {
	return fmt.Sprintf("wasmbox-client(%dx%d id=%d)", c.w, c.h, c.windowID)
}

// Close releases the surface: it asks the compositor to close the window
// (`request_close`), releases the JS callbacks and ends Run. Idempotent.
func (c *Client) Close() error {
	c.shutdown(true)
	return nil
}

// shutdown ends the session once. sendClose posts `request_close` first (Close);
// the compositor-initiated `closed` path passes false (the window is already
// gone).
func (c *Client) shutdown(sendClose bool) {
	c.closeOnce.Do(func() {
		if sendClose && c.windowID != 0 {
			c.send(objectFromMap(EncodeRequestClose(c.windowID)))
		}
		for _, f := range c.funcs {
			f.Release()
		}
		close(c.done)
	})
}

// ---- JS ↔ codec adapters --------------------------------------------------

// MessageKindJS reads the `type` discriminant of a JS message object, or ""
// when absent — the JS twin of MessageKind.
func MessageKindJS(o js.Value) string {
	t := o.Get("type")
	if t.Type() == js.TypeString {
		return t.String()
	}
	return ""
}

// welcomeMap lifts a JS welcome object into the codec's map representation.
func welcomeMap(o js.Value) map[string]any {
	return map[string]any{
		"type":      MessageKindJS(o),
		"window_id": optInt(o, "window_id"),
		"granted_w": optInt(o, "granted_w"),
		"granted_h": optInt(o, "granted_h"),
	}
}

// inputMap lifts a JS input object (and its nested event) into the codec's map
// representation.
func inputMap(o js.Value) map[string]any {
	m := map[string]any{"type": MessageKindJS(o), "window_id": optInt(o, "window_id")}
	if e := o.Get("event"); e.Type() == js.TypeObject {
		m["event"] = map[string]any{
			"kind":   optStr(e, "kind"),
			"x":      optInt(e, "x"),
			"y":      optInt(e, "y"),
			"button": optInt(e, "button"),
			"key":    optStr(e, "key"),
			"code":   optStr(e, "code"),
			"dx":     optInt(e, "dx"),
			"dy":     optInt(e, "dy"),
		}
	}
	return m
}

// optInt reads a numeric field, returning nil (→ 0 in the codec) when the field
// is absent or non-numeric, so a mouse-only or key-only payload never panics.
func optInt(o js.Value, key string) any {
	v := o.Get(key)
	if v.Type() == js.TypeNumber {
		return v.Int()
	}
	return nil
}

// optStr reads a string field, returning nil (→ "" in the codec) when absent.
func optStr(o js.Value, key string) any {
	v := o.Get(key)
	if v.Type() == js.TypeString {
		return v.String()
	}
	return nil
}

// objectFromMap builds a plain JS object from a JSON-cloneable map produced by
// the codec's encoders (string/int/bool scalars, nested maps for damage).
func objectFromMap(m map[string]any) js.Value {
	o := js.Global().Get("Object").New()
	for k, v := range m {
		switch t := v.(type) {
		case string:
			o.Set(k, t)
		case int:
			o.Set(k, t)
		case bool:
			o.Set(k, t)
		case map[string]any:
			o.Set(k, objectFromMap(t))
		}
	}
	return o
}
