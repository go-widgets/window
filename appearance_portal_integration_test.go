// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// The live proof for the desktop-appearance path, over a REAL D-Bus session.
//
// The unit tests answer Settings.Read from a hand-made object, which never goes
// near serialisation -- and serialisation is exactly where this can lie. The
// portal returns a variant wrapping a variant; a fake can hand back whatever
// shape the reader happens to expect, while the wire cannot. So this starts a
// session bus, serves a stub portal on it, and reads through the same code an
// application uses.
//
// It is a stub portal rather than GNOME's because CI has no desktop. That is
// enough for what only a real bus can settle: the connection, the call, the
// signature, and the double unwrap.
package window

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// stubPortal serves org.freedesktop.portal.Settings.Read.
type stubPortal struct{ scheme uint32 }

func (s stubPortal) Read(ns, key string) (dbus.Variant, *dbus.Error) {
	if ns != appearanceNS {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.portal.Error.NotFound", nil)
	}
	switch key {
	case keyColorScheme:
		// The wrapping the real portal does, and the reason this test exists.
		return dbus.MakeVariant(dbus.MakeVariant(s.scheme)), nil
	case keyAccentColour:
		return dbus.MakeVariant(dbus.MakeVariant([]interface{}{1.0, 0.5, 0.0})), nil
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.portal.Error.NotFound", nil)
}

// sessionBus starts a private dbus-daemon and returns a connection to it.
func sessionBus(t *testing.T) *dbus.Conn {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("no dbus-daemon to start a session bus with")
	}
	out, err := exec.Command("dbus-daemon", "--session", "--print-address", "--fork",
		"--print-pid").Output()
	if err != nil {
		t.Skipf("dbus-daemon: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 1 {
		t.Skip("dbus-daemon printed no address")
	}
	addr := fields[0]
	conn, err := dbus.Dial(addr)
	if err != nil {
		t.Fatalf("dial %q: %v", addr, err)
	}
	if err := conn.Auth(nil); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := conn.Hello(); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// So the code under test finds the same bus.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	return conn
}

func servePortal(t *testing.T, conn *dbus.Conn, scheme uint32) {
	t.Helper()
	s := stubPortal{scheme: scheme}
	if err := conn.Export(s, dbus.ObjectPath(portalPath), portalSettings); err != nil {
		t.Fatalf("export: %v", err)
	}
	node := &introspect.Node{
		Name: portalPath,
		Interfaces: []introspect.Interface{{
			Name:    portalSettings,
			Methods: introspect.Methods(s),
		}},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), dbus.ObjectPath(portalPath),
		"org.freedesktop.DBus.Introspectable"); err != nil {
		t.Fatalf("export introspection: %v", err)
	}
	reply, err := conn.RequestName(portalService, dbus.NameFlagDoNotQueue)
	if err != nil {
		t.Fatalf("request name: %v", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("could not own %s", portalService)
	}
}

// The whole path over a real bus: connect, call, and unwrap what actually came
// back rather than what a fake was told to return.
func TestLiveX11PortalAppearanceOverARealBus(t *testing.T) {
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 to run the live portal proof")
	}
	conn := sessionBus(t)
	servePortal(t, conn, 1) // 1 = prefer dark

	w := &Window{}
	ap := w.Appearance()

	if !ap.Dark {
		t.Error("the portal said prefer-dark and the reading came back light")
	}
	if !ap.HasAccent {
		t.Fatal("the portal published an accent and the reading found none")
	}
	if ap.Accent.R != 255 || ap.Accent.G != 128 || ap.Accent.B != 0 || ap.Accent.A != 255 {
		t.Errorf("accent = %+v, want opaque 255,128,0", ap.Accent)
	}
	t.Logf("over a real bus: dark=%v accent=#%02X%02X%02X", ap.Dark, ap.Accent.R, ap.Accent.G, ap.Accent.B)
}

// A bus with nobody serving the portal is the common case on a minimal
// desktop, and must read as no preference rather than as an error or a hang.
func TestLiveX11PortalAbsent(t *testing.T) {
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 to run the live portal proof")
	}
	sessionBus(t) // a bus, but nothing serving org.freedesktop.portal.Desktop

	w := &Window{}
	if ap := w.Appearance(); ap.Dark || ap.HasAccent {
		t.Errorf("with no portal on the bus the reading was %+v", ap)
	}
}
