// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func cookie16(b byte) []byte { return bytes.Repeat([]byte{b}, 16) }

func TestParseXauthorityRoundTrip(t *testing.T) {
	entries := []AuthEntry{
		{Family: familyLocal, Address: []byte("host1"), Number: "0", Name: authMITCookie, Data: cookie16(0xAA)},
		{Family: familyWild, Address: nil, Number: "", Name: authMITCookie, Data: cookie16(0xBB)},
	}
	var buf bytes.Buffer
	for _, e := range entries {
		buf.Write(encodeXauthEntry(e))
	}
	got, err := parseXauthority(&buf)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Name != authMITCookie || !bytes.Equal(got[0].Data, cookie16(0xAA)) {
		t.Fatalf("entry0 wrong: %+v", got[0])
	}
	if got[1].Family != familyWild {
		t.Fatalf("entry1 family wrong")
	}
}

func TestParseXauthorityErrors(t *testing.T) {
	// Truncated in the family field.
	if _, err := parseXauthority(bytes.NewReader([]byte{0x01})); err == nil {
		t.Fatal("truncated family should error")
	}
	// Truncated in a field length.
	if _, err := parseXauthority(bytes.NewReader([]byte{0x00, 0x00, 0x00})); err == nil {
		t.Fatal("truncated field length should error")
	}
	// Truncated in each successive field body: address, number, name, data.
	// Each prefix is valid up to a length that promises 5 missing bytes.
	truncAt := [][]byte{
		{0x00, 0x00, 0x00, 0x05},                                     // address body
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x05},                         // number body
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05},             // name body
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05}, // data body
	}
	for i, b := range truncAt {
		if _, err := parseXauthority(bytes.NewReader(b)); err == nil {
			t.Fatalf("truncated field %d should error", i)
		}
	}
}

func TestMatchCookie(t *testing.T) {
	local := AuthEntry{Family: familyLocal, Address: []byte("myhost"), Number: "0", Name: authMITCookie, Data: cookie16(1)}
	wild := AuthEntry{Family: familyWild, Number: "0", Name: authMITCookie, Data: cookie16(2)}
	inet := AuthEntry{Family: familyInternet, Address: []byte{127, 0, 0, 1}, Number: "0", Name: authMITCookie, Data: cookie16(3)}
	other := AuthEntry{Family: familyLocal, Address: []byte("elsewhere"), Number: "1", Name: "XDM-AUTHORIZATION-1", Data: cookie16(9)}

	// Local exact match wins.
	if e, ok := matchCookie([]AuthEntry{wild, local}, "myhost", "0"); !ok || e.Data[0] != 1 {
		t.Fatalf("local match failed: %+v ok=%v", e, ok)
	}
	// Wild wins when no local match.
	if e, ok := matchCookie([]AuthEntry{wild, inet}, "nohost", "0"); !ok || e.Data[0] != 2 {
		t.Fatalf("wild match failed: %+v ok=%v", e, ok)
	}
	// Falls back to any name-matching entry with a matching number.
	if e, ok := matchCookie([]AuthEntry{inet}, "nohost", "0"); !ok || e.Data[0] != 3 {
		t.Fatalf("anyNum match failed: %+v ok=%v", e, ok)
	}
	// Number mismatch + wrong name => no match.
	if _, ok := matchCookie([]AuthEntry{other}, "myhost", "0"); ok {
		t.Fatal("should not match")
	}
	// Blank number is a wildcard on the number.
	blank := AuthEntry{Family: familyInternet, Number: "", Name: authMITCookie, Data: cookie16(7)}
	if e, ok := matchCookie([]AuthEntry{blank}, "h", "5"); !ok || e.Data[0] != 7 {
		t.Fatalf("blank-number wildcard failed")
	}
}

func TestLoadAuthCookie(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Xauthority")
	host, _ := os.Hostname()
	entry := AuthEntry{Family: familyLocal, Address: []byte(host), Number: "0", Name: authMITCookie, Data: cookie16(0x5A)}
	if err := os.WriteFile(path, encodeXauthEntry(entry), 0o600); err != nil {
		t.Fatal(err)
	}

	name, data, err := LoadAuthCookie(path, "", "0")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if name != authMITCookie || !bytes.Equal(data, cookie16(0x5A)) {
		t.Fatalf("loaded wrong cookie: %q %x", name, data)
	}

	// Explicit host argument path.
	if name, data, err = LoadAuthCookie(path, host, "0"); err != nil || name != authMITCookie || len(data) != 16 {
		t.Fatalf("explicit host load failed: %v", err)
	}

	// No match (different display) -> empty, no error.
	if name, _, err := LoadAuthCookie(path, host, "9"); err != nil || name != "" {
		t.Fatalf("no-match should be empty, no error: %q %v", name, err)
	}

	// Empty path -> empty, no error.
	if name, _, err := LoadAuthCookie("", "", "0"); err != nil || name != "" {
		t.Fatalf("empty path should be empty: %v", err)
	}

	// Missing file -> empty, no error.
	if name, _, err := LoadAuthCookie(filepath.Join(dir, "nope"), "", "0"); err != nil || name != "" {
		t.Fatalf("missing file should be empty: %v", err)
	}

	// A non-NotExist open error (a path whose parent is a regular file, so
	// the lookup fails with ENOTDIR) surfaces as an error.
	if _, _, err := LoadAuthCookie(filepath.Join(path, "sub"), "", "0"); err == nil {
		t.Fatal("ENOTDIR open error should surface")
	}

	// Corrupt file -> error.
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte{0x01}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadAuthCookie(bad, "", "0"); err == nil {
		t.Fatal("corrupt file should error")
	}
}

func TestAuthFilePath(t *testing.T) {
	t.Setenv("XAUTHORITY", "/custom/authority")
	if got := authFilePath(); got != "/custom/authority" {
		t.Fatalf("XAUTHORITY path = %q", got)
	}
	t.Setenv("XAUTHORITY", "")
	home, err := os.UserHomeDir()
	if err == nil {
		if got := authFilePath(); got != home+"/.Xauthority" {
			t.Fatalf("home path = %q want %q", got, home+"/.Xauthority")
		}
	}
	// With no home resolvable, authFilePath returns "".
	t.Setenv("HOME", "")
	if _, err := os.UserHomeDir(); err != nil {
		if got := authFilePath(); got != "" {
			t.Fatalf("no-home path = %q want empty", got)
		}
	}
}
