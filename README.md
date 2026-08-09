# go-widgets/window

A **pure-Go, CGO-free, zero-non-stdlib-dependency** X11 windowing backend for the
[go-widgets](https://github.com/go-widgets) toolkit.

It implements the **X11 core protocol (v11.0) from scratch over the unix
socket** — no Xlib, no XCB, no cgo — the same sovereign transport + wire-codec
approach used by [`go-freedesktop/dbus`](https://github.com/go-freedesktop/dbus).
It opens a **real window** on Linux, blits the toolkit's RGBA framebuffer into it
via the core-protocol `PutImage`, and routes X input into `toolkit.Event`.

```
┌──────────────────────────────────────────────────────────────┐
│  go-widgets/toolkit widget tree  (Button, Label, VBox, …)      │
├──────────────────────────────────────────────────────────────┤
│  window.Window   layout → painter.PixelPainter → RGBA buffer   │
│                  X events → toolkit.Event → root.OnEvent       │
├──────────────────────────────────────────────────────────────┤
│  internal/x11    sovereign X11 core protocol (from scratch)    │
│    · wire codec (both byte orders)  · setup handshake          │
│    · MIT-MAGIC-COOKIE-1 Xauthority  · keycode→keysym mapping   │
│    · request/reply/error/event demux                           │
│    · PutImage (RGBA→visual pixel packing, max-request tiling)  │
│    · MIT-SHM 1.2 fast path (shm fd over SCM_RIGHTS, ShmPutImage)│
├──────────────────────────────────────────────────────────────┤
│  unix socket  /tmp/.X11-unix/X<n>   →  X server                │
└──────────────────────────────────────────────────────────────┘
```

## Usage

```go
package main

import (
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window"
)

func main() {
	w, err := window.Open(window.Config{Title: "Demo", Width: 480, Height: 320})
	if err != nil {
		panic(err)
	}
	defer w.Close()

	box := toolkit.NewVBox()
	box.Append(toolkit.NewLabel("Hello from a pure-Go X11 window"))
	box.Append(toolkit.NewButton("Click me", func() { /* ... */ }))

	w.Run(box) // drives layout/draw/present + dispatches input until closed
}
```

Run the bundled example: `go run ./cmd/windowdemo`.

## Public API

- `window.Open(cfg Config) (*Window, error)` — dial `$DISPLAY`, authenticate,
  create and map the window. Linux only; returns `window.ErrUnsupported`
  elsewhere so cross-builds stay green.
- `(*Window).Run(root toolkit.Widget) error` — the host loop: initial
  layout/draw/present, then translate X events (`Expose`, `KeyPress/Release`,
  `ButtonPress/Release`, `MotionNotify`, `ConfigureNotify`, `ClientMessage`) into
  `toolkit.Event` and dispatch them, re-laying-out on resize.
- `(*Window).Close() error`, `(*Window).Size() (int, int)`.

## Design notes

- **Sovereign protocol.** `internal/x11` speaks the wire format byte-for-byte
  and is transport-agnostic (`io.ReadWriteCloser`), so the full
  request/reply/event machine is tested in-process against a scripted fake
  server — **100 % statement coverage** on the codec, Xauthority parser and
  keysym mapping, both byte orders, error branches included.
- **Present.** The toolkit's `painter.PixelPainter` renders into the backing
  RGBA buffer; the backend converts to the screen visual's pixel layout
  (channel masks + image byte order) and tiles `PutImage` under the server's
  maximum request length. A `presentRect` damage-region path is ready for when
  a scene damage list becomes available (`toolkit` exposes none today, so a
  full-surface present follows input).
- **Wayland** is a separate future backend, intentionally out of scope here.

## Verification

- Unit tests run on `amd64`, `arm64`, and under qemu on `riscv64`, `loong64`,
  `ppc64le`, `s390x` — the big-endian wire path exercised on real big-endian
  (`s390x`) models, all strictly `CGO=0`.
- A **live X11 proof** (`-tags=integration`, `WINDOW_X11_INTEGRATION=1`) runs
  under Xvfb: it opens a window, presents a known four-quadrant pattern,
  captures it with `import`, asserts the sampled pixels, then synthesises a
  click and a key with `xdotool` and asserts the dispatched `toolkit.Event`.

## License

BSD-3-Clause. Copyright (c) the go-widgets/window authors.
