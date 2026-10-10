# go-widgets/window

A **pure-Go, CGO-free** windowing backend for the
[go-widgets](https://github.com/go-widgets) toolkit, with eight interchangeable
backends behind one `Open`/`Run` API — **X11**, **Wayland**, **GTK4** (opt-in),
**macOS Cocoa/AppKit**, **Windows Win32/GDI**, **Android**, **wasmbox** (the
[wasmdesk/wasmbox](https://github.com/wasmdesk/wasmbox) browser compositor) and
a **browser tab** `<canvas>`.
`Open` auto-selects per environment: a real X11/Wayland window on Linux, a real
NSWindow on macOS, a real Win32 window on Windows, a real Activity surface on
Android, and — when built for `js/wasm` — a wasmbox external client, or a `<canvas>` in an ordinary browser tab.
**One go-widgets application runs unchanged natively AND inside wasmdesk.**

The macOS backend reaches AppKit through the fleet's shared purego Objective-C
bridge [`go-macos/objc`](https://github.com/go-macos/objc) — no cgo; the Windows
backend reaches Win32/GDI through the process' own user32/gdi32/kernel32 DLLs via
`syscall.NewLazyDLL` and a `syscall.NewCallback` WNDPROC — no cgo — so both link
with `CGO_ENABLED=0`.

It implements the **X11 core protocol (v11.0) from scratch over the unix
socket** — no Xlib, no XCB, no cgo — the same sovereign transport + wire-codec
approach used by [`godbus/dbus/v5`](https://github.com/godbus/dbus).
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

## Backends

`Open` returns a `Backend` (`Run`/`Close`/`Size`/`String`); the application is
backend-agnostic. The environment selects the implementation:

| GOOS/env | Backend | Transport |
| --- | --- | --- |
| Linux, `$GO_WIDGETS_GTK` set | **GTK4** (`internal/gtk`), opt-in | GTK owns the window; the framebuffer is a `GtkPicture`, and native controls (`toolkit.NativeControl`) are real GTK widgets above it. Needs the libgtk-4 runtime |
| Linux, `$WAYLAND_DISPLAY` set | Wayland (`internal/wayland`) | xdg-shell over the compositor unix socket |
| Linux, else `$DISPLAY` | X11 (`internal/x11`) | X11 core protocol over the unix socket (+ MIT-SHM) |
| macOS (`darwin`) | **Cocoa/AppKit** (`internal/cocoa`) | NSWindow + NSView via `go-macos/objc` (purego), NSBitmapImageRep present |
| Windows (`windows`) | **Win32/GDI** (`internal/win32`) | top-level HWND via user32/gdi32 syscalls + `NewCallback` WNDPROC, StretchDIBits BGRA present |
| Android, `$GW_ANDROID_SOCKET` set | **Android host** ([`go-widgets/android`](https://github.com/go-widgets/android)) | framed protocol over an abstract `LocalSocket` + a memfd surface shared with the Java host |
| Android, else | Wayland or X11, as on Linux | a shell under Termux still has a display server to dial |
| `js/wasm` | **wasmbox** (`internal/wasmbox`) | wasmbox client protocol over a `MessagePort` + a `SharedArrayBuffer` surface |
| `js/wasm`, an ordinary page | **browser tab** (`internal/tab`) | a `<canvas>` through [go-widgets/webcanvas](https://github.com/go-widgets/webcanvas): no compositor, no `SharedArrayBuffer`, no cross-origin isolation |
| other (BSD, …) | stub → `ErrUnsupported` | — |

### macOS Cocoa/AppKit backend (`darwin`)

On macOS `Open` creates a real **NSWindow** with a flipped content **NSView**,
presents the toolkit's RGBA framebuffer by wrapping it in an
**NSBitmapImageRep** drawn in `-drawRect:`, and decodes native
`NSEvent` mouse/scroll/key input into `toolkit.Event`. It honours the opt-in
`DamageRenderer` (only damaged rectangles are invalidated via
`-setNeedsDisplayInRect:` and re-blitted). Everything runs through
[`go-macos/objc`](https://github.com/go-macos/objc) over
[purego](https://github.com/ebitengine/purego) — **no cgo**. The OS-independent
`NSEvent`→`toolkit.Event` mapping, flipped-view coordinate maths and
damage→dirty-rect conversion live in a sovereign, 100%-covered codec
(`internal/cocoa/mapping.go`); the darwin-only AppKit glue
(`internal/cocoa/cocoa_darwin.go`) is proven live on-device by the
`darwin (cocoa)` CI lane (open a window, render it, assert sampled pixels,
synthesise a click + key and assert the dispatched event + the button counter).

### Windows Win32/GDI backend (`windows`)

On Windows `Open` declares **Per-Monitor-V2 DPI awareness**, registers a window
class and creates a real **titled, resizable top-level HWND**, presents the
toolkit's RGBA framebuffer by packing it **BGRA** into a top-down 32bpp DIB and
blitting it with **`StretchDIBits`** on `WM_PAINT`, and decodes native `WM_*`
mouse/wheel/key messages into `toolkit.Event`. It honours the opt-in
`DamageRenderer` (only damaged rectangles are re-packed and `InvalidateRect`'d,
so `WM_PAINT`'s update region blits just those). To stay readable on HiDPI it
renders the toolkit at **logical size** and lets the OS up-sample to the physical
client area (scale = `GetDpiForWindow`/96), rather than rendering at device
pixels and presenting into a smaller area. The whole path reaches Win32 through
the process' own **user32/gdi32/kernel32** DLLs via `syscall.NewLazyDLL` and a
`syscall.NewCallback` WNDPROC — **no cgo**. The OS-independent `WM_*`→
`toolkit.Event` mapping, RGBA→BGRA DIB packing, DPI/size maths and
damage→`InvalidateRect` conversion live in a sovereign, 100%-covered codec
(`internal/win32/mapping.go`); the windows-only Win32 glue
(`internal/win32/win32_windows.go`) is proven live **on-device** on a Windows 11
**arm64** QEMU VM — a real Win32 window rendering a `VBox`+`Label`+`Button`
([capture](internal/win32/win32-capture-2026-08-10.png)), with three injected
`WM_LBUTTONDOWN`/`UP` messages driving the button's counter `0 → 3` end to end
through the WNDPROC ([after](internal/win32/win32-input-2026-08-10.png)).

### Android backend (`android`)

Android is the one target where a CGO-free process **cannot own a window at
all**: the entire graphics and input API sits behind JNI, so there is no
syscall-level surface to claim the way X11, Wayland, Win32 and AppKit each
offer one. The backend answers that by not trying — the application runs as the
Go half of [`go-widgets/android`](https://github.com/go-widgets/android), where
a thin Java host owns the `Activity` and the `SurfaceView`, and the Go side owns
layout, widgets, theme and hit-testing. Pixels cross through a **memfd** both
processes map (Go writes RGBA\_8888, which is Android's ARGB\_8888 byte for
byte, so the blit is a plain copy); input, insets, IME text and the
accessibility tree cross a framed protocol over an abstract `LocalSocket`.

`GOOS=android` names **two** environments, and `Open` distinguishes them by the
one fact only a host can produce: it exports `$GW_ANDROID_SOCKET` when it spawns
the application. Set means an APK, and the host is dialled. Unset means a shell
— under Termux, against Termux:X11 or a Wayland compositor — where the ordinary
Linux path is both right and available, so `Open` falls through to it. One
binary serves both, chosen by what is actually there rather than by a build tag.

⚠ **`android/arm64` is the only Android target Go links CGO-free**; `arm`,
`amd64` and `386` all require external cgo linking. CI asserts both halves of
that — the one that works and the three that do not — so the day Go widens it,
the build says so.

### Browser tab backend (`js/wasm`, an ordinary page)

When the page is not a wasmbox client -- it has a `document` and the wasmbox
worker never installed its hook -- `Open` returns a backend that draws into a
`<canvas>` of the page (`Config.Canvas`, default `"screen"`) through
[go-widgets/webcanvas](https://github.com/go-widgets/webcanvas). The page needs
nothing special: no COOP/COEP headers, so it can be served as static files from
anywhere, behind any web tier. The same `w.Run(root)` drives the tree: pointer,
wheel and keyboard become toolkit events carrying Ctrl, Shift, Alt and Meta,
drag-and-drop goes through the same controller as the other backends, the
canvas follows the page's layout, and a root that renders incrementally repaints
only its damage.

```html
<canvas id="screen" style="width:100vw;height:100vh"></canvas>
<script src="wasm_exec.js"></script>
<script>
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("app.wasm"), go.importObject).then(r => go.run(r.instance));
</script>
```

### wasmbox client backend (`js/wasm`)

On `js/wasm` the environment *is* the [wasmdesk/wasmbox](https://github.com/wasmdesk/wasmbox)
browser compositor, so instead of dialling a display server the backend runs as
an **external client** of the compositor: it allocates the surface
`SharedArrayBuffer`, posts `hello` over its per-client `MessagePort`, awaits
`welcome`, paints the widget tree into the SAB and posts `commit` — whole-surface,
or (when the root implements `DamageRenderer`, e.g. `toolkit/scene.HostRoot`)
just the damaged rectangles. Incoming `input` messages map to `toolkit.Event`
exactly as the X11/Wayland backends do. The wire protocol
([wasmbox `docs/protocol.md`](https://github.com/wasmdesk/wasmbox/blob/main/docs/protocol.md))
is implemented in a sovereign, transport-agnostic codec (`internal/wasmbox/protocol.go`,
unit-tested to 100% on every GOOS); the `syscall/js` glue (`client_js.go`) only
carries the live JS handles. **The wasmbox repository is not modified** — this is
purely a client-side backend plus a worker shim.

Build the client and run it inside a compositor:

```sh
clients/gowidgets/build.sh          # → clients/gowidgets/{gowidgets.wasm,wasm_exec.js}
# a wasmbox compositor spawns it via:
#   wasmboxSpawnExternal("<origin>/clients/gowidgets/worker.js")
```

The live browser proof (headless Chromium via Playwright, served by wasmbox's
own COOP/COEP `cmd/serve`) lives in `test/`, in two tiers:

- **Real desktop** (`test/probe-wasmbox-real.mjs`) — drives the **actual
  wasmdesk/wasmbox Ruby compositor** (`compositor/*.rb` on the pure-Go rbgo
  interpreter, baked into `wasmbox.wasm`). It boots the real desktop, spawns
  this client with the documented
  `globalThis.wasmboxSpawnExternal("clients/gowidgets/worker.js")` hook (a real
  external Worker + wasm instance over the step-C.1 `MessagePort` + SAB), reads
  the compositor's own composited pixels (`__wasmboxReadRegion`) to assert the
  VBox+Label+Button rendered at the window's live focused rect, and injects a
  **real** `page.mouse.click` that the compositor routes to the focused window —
  asserting the counter goes `0→1` (input → `toolkit.Event` through the real
  input routing). Captured: `test/wasmbox-live-proof-real-desktop-2026-08-09.png`
  (the go-widgets window composited on the rbgo desktop, reading "Clicks: 1").
  The wasmbox repo is **unmodified**; the client is served same-origin via a
  symlink overlay — see [`test/README-real-desktop.md`](test/README-real-desktop.md).
- **Deterministic floor** (`test/probe-wasmbox.mjs`) — the same assertions
  against `test/harness.html`, a protocol-faithful compositor stand-in, so the
  wire + SAB + input round-trip are exercised even without building the ~80 MB
  Ruby compositor. Captured: `test/wasmbox-live-proof-2026-08-09.png`.

## Public API

- `window.Open(cfg Config) (Backend, error)` — open a window on the back-end
  the environment calls for (see [Backends](#backends)). Where there is none
  (the BSDs, …) it returns `window.ErrUnsupported`, so cross-builds stay green.
- `Backend.Run(root toolkit.Widget) error` — the host loop: initial
  layout/draw/present, then translate the platform's input (on X11: `Expose`,
  `KeyPress/Release`, `ButtonPress/Release`, `MotionNotify`, `ConfigureNotify`,
  `ClientMessage`) into `toolkit.Event` and dispatch it, re-laying-out on resize.
- `Backend.Close() error`, `Backend.Size() (int, int)`, `Backend.String() string`.
- `window.Screens() ([]Screen, error)` — enumerate the attached displays,
  primary first, in logical points with the desktop's panels excluded. Safe to
  call before `Open`, since picking an output is something an application does
  on the way in. **macOS** answers through Cocoa, **X11** through RANDR 1.5
  (shared with the screen capture in
  [`go-freedesktop/x11`](https://github.com/go-freedesktop/x11), not a second
  copy of it), **Wayland** through `wl_output`, and **Windows** through
  `EnumDisplayMonitors` (shared with `go-mswin/screencapture` through
  [`go-mswin/win32`](https://github.com/go-mswin/win32)). Only `js/wasm` still
  returns `window.ErrScreensUnsupported`.

  `Screen.Name` is the panel's **own** name where the platform offers one — the
  product string out of its EDID, `"DELL U2720Q"` — falling back to the
  connector (`"HDMI-1"`, `"DP-2"`, `\\.\DISPLAY1`) on a display that
  publishes none, **and also when two attached displays publish the same one**:
  two identical monitors say the identical thing about themselves, and a name
  that cannot tell them apart is not a name. That is what an application
  recognising a particular headset has to match on.

  On Windows the EDID lives in the registry under the monitor's device
  instance, and a panel with none falls back to the description its driver
  gives — which for the inbox monitor driver is `"Generic PnP Monitor"` for
  every panel attached, hence the rule above. Windows is also the one platform
  with **no single logical coordinate space**: each display's rectangle is in
  its own points, so on a mixed-DPI desktop they do not tile. Nothing depends
  on them tiling — `Config.Screen` takes the `Screen` value back and the
  back-end re-resolves it — but a caller doing arithmetic across two Windows
  displays should know.
- `window.VisibleScreenSize() (w, h int, ok bool)` — the usable area of the
  primary display, superseded by `Screens` for anything multi-display.
- `window.Show(b)`, `window.Hide(b)`, `window.Raise(b)` — the `Visibility`
  capability: take an open window off the screen **without closing it** (`Run`
  keeps running and the widget tree keeps its state), put it back, and bring it
  to the front with the keyboard focus. It is what a system-tray application
  needs: its "Open" item raises the window already there, and its window can
  hide while the application carries on in the tray. Safe from any goroutine;
  each returns once the request is on its way to the window's own thread.
  A back-end that cannot do one says so with `window.ErrNotSupported`, and a
  closed window answers `window.ErrClosed`.

  | back-end | Show | Hide | Raise |
  |---|---|---|---|
  | X11 | `MapWindow` | ICCCM withdraw (unmap + synthetic `UnmapNotify`), so it leaves the taskbar | map, stack above, EWMH `_NET_ACTIVE_WINDOW` |
  | Wayland | the initial commit again, then the next configure maps it | null buffer + commit (unmap) | `ErrNotSupported`: xdg-shell has no such request, and xdg-activation needs a token from the user's input to this window |
  | macOS (Cocoa) | `orderFront:` | `orderOut:` | out of the Dock, activate the app, `makeKeyAndOrderFront:` (a passive window is only ordered front) |
  | Windows (Win32) | `SW_SHOWNA` | `SW_HIDE` | `SW_RESTORE`/`SW_SHOW` + `SetForegroundWindow` |
  | GTK, Android, wasmbox | `ErrNotSupported` | `ErrNotSupported` | `ErrNotSupported` |

  Focus is the platform's to grant: under focus-stealing prevention a window
  manager, or Windows' foreground lock, may mark the window as wanting
  attention instead, and Raise cannot tell. Under an X11 window manager a
  window shown again is placed by the manager, as a new one would be.

## Design notes

- **Sovereign protocol.** `internal/x11` speaks the wire format byte-for-byte
  and is transport-agnostic (`io.ReadWriteCloser`), so the full
  request/reply/event machine is tested in-process against a scripted fake
  server — **100 % statement coverage** on the codec, Xauthority parser and
  keysym mapping, both byte orders, error branches included.
- **Present.** The toolkit's `painter.PixelPainter` renders into the backing
  RGBA buffer; the backend converts to the screen visual's pixel layout
  (channel masks + image byte order) and tiles `PutImage` under the server's
  maximum request length. A root that implements `DamageRenderer`
  (`toolkit/scene.HostRoot` is the reference) reports the rectangles it
  repainted, and X11 (MIT-SHM `ShmPutImage`), Wayland (`wl_shm` damage),
  Cocoa, Win32 and wasmbox present only those. The first frame, a resize and
  an X11 `Expose` still present the whole surface.

## Verification

- Unit tests run on `amd64`, `arm64`, and under qemu on `riscv64`, `loong64`,
  `ppc64le`, `s390x` — the big-endian wire path exercised on real big-endian
  (`s390x`) models, all strictly `CGO=0`.
- A **live X11 proof** (`-tags=integration`, `WINDOW_X11_INTEGRATION=1`) runs
  under Xvfb: it opens a window, presents a known four-quadrant pattern,
  captures it with `import`, asserts the sampled pixels, then synthesises a
  click and a key with `xdotool` and asserts the dispatched `toolkit.Event`.
  `TestLiveX11Visibility` hides, shows and raises real windows there and reads
  the result from the server (`xdotool search --onlyvisible`, and the pixel on
  top where two windows overlap); the same assertions are made against
  scripted fakes in-process. The Cocoa lane's `TestLiveShowHideRaise…` asks a
  real `NSWindow` from another goroutine and reads `-isVisible` /
  `-isMiniaturized` back from AppKit. The Wayland request order is proven
  against a fake compositor that enforces xdg-shell's map/unmap rules; the
  Win32 decision table is unit-tested, and its UI-thread calls are
  compile-verified only.

## License

BSD-3-Clause. Copyright (c) the go-widgets/window authors.
