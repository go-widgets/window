// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package webmirror

import (
	"reflect"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"
)

// fake is a container with a chosen A11yInfo, so the table can say exactly what
// each field of the toolkit's node becomes.
type fake struct {
	toolkit.Base
	info toolkit.A11yInfo
	kids []toolkit.Widget
}

func (f *fake) A11y() toolkit.A11yInfo               { return f.info }
func (f *fake) Children() []toolkit.Widget           { return f.kids }
func (f *fake) Draw(painter.Painter, *toolkit.Theme) {}
func (f *fake) OnEvent(toolkit.Event)                {}

func leaf(info toolkit.A11yInfo, r toolkit.Rect) *fake {
	f := &fake{info: info}
	f.SetBounds(r)
	return f
}

func area(x int) toolkit.Rect { return toolkit.Rect{X: x, Y: 2 * x, W: 30, H: 10} }

func TestConvertEveryField(t *testing.T) {
	in := toolkit.A11yNode{
		A11yInfo: toolkit.A11yInfo{
			Role: toolkit.RoleSlider, Name: "Volume", Value: "7 of 10",
			Focused: true, Disabled: true, ReadOnly: true, Multiline: true,
			HasRange: true, Min: 0, Max: 10, Now: 7,
		},
		Rect: toolkit.Rect{X: 1, Y: 2, W: 3, H: 4},
		Key:  "VBox#0/Slider#1",
	}
	want := webcanvas.A11yNode{
		Key: "VBox#0/Slider#1", Role: "slider", Name: "Volume", Value: "7 of 10",
		X: 1, Y: 2, W: 3, H: 4,
		Focused: true, Disabled: true, ReadOnly: true, Multiline: true,
		HasRange: true, Min: 0, Max: 10, Now: 7,
	}
	if got := Convert(in, ""); got != want {
		t.Errorf("Convert =\n%+v\nwant\n%+v", got, want)
	}
	want.Key = "col|VBox#0/Slider#1"
	if got := Convert(in, "col"); got != want {
		t.Errorf("Convert with prefix = %q, want %q", got.Key, want.Key)
	}
}

func TestNodesTable(t *testing.T) {
	cases := []struct {
		name string
		info toolkit.A11yInfo
		rect toolkit.Rect
		want []webcanvas.A11yNode // nil: dropped
	}{
		{"button", toolkit.A11yInfo{Role: toolkit.RoleButton, Name: "Save"}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "button", Name: "Save", X: 1, Y: 2, W: 30, H: 10}}},
		{"pressed button keeps its value", toolkit.A11yInfo{Role: toolkit.RoleButton, Name: "Bold", Value: "pressed"}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "button", Name: "Bold", Value: "pressed", X: 1, Y: 2, W: 30, H: 10}}},
		{"checked checkbox", toolkit.A11yInfo{Role: toolkit.RoleCheckbox, Name: "Agree", Value: "checked"}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "checkbox", Name: "Agree", Value: "checked", X: 1, Y: 2, W: 30, H: 10}}},
		{"disabled", toolkit.A11yInfo{Role: toolkit.RoleButton, Name: "Off", Disabled: true}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "button", Name: "Off", Disabled: true, X: 1, Y: 2, W: 30, H: 10}}},
		{"focused multiline textbox", toolkit.A11yInfo{Role: toolkit.RoleTextbox, Name: "Notes", Value: "a\nb", Focused: true, Multiline: true}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "textbox", Name: "Notes", Value: "a\nb", Focused: true, Multiline: true, X: 1, Y: 2, W: 30, H: 10}}},
		{"range", toolkit.A11yInfo{Role: toolkit.RoleSlider, Name: "Vol", HasRange: true, Min: 1, Max: 9, Now: 4}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "slider", Name: "Vol", HasRange: true, Min: 1, Max: 9, Now: 4, X: 1, Y: 2, W: 30, H: 10}}},
		// window#153/#154: a nameless field is still published, and its value
		// stays the value.
		{"nameless textbox is kept", toolkit.A11yInfo{Role: toolkit.RoleTextbox, Value: "hello"}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "textbox", Value: "hello", X: 1, Y: 2, W: 30, H: 10}}},
		{"nameless button is kept", toolkit.A11yInfo{Role: toolkit.RoleButton}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "button", X: 1, Y: 2, W: 30, H: 10}}},
		{"nameless text is dropped", toolkit.A11yInfo{Role: toolkit.RoleText}, area(1), nil},
		{"nameless group is dropped", toolkit.A11yInfo{Role: toolkit.RoleGroup}, area(1), nil},
		{"zero area is dropped", toolkit.A11yInfo{Role: toolkit.RoleButton, Name: "Hidden"}, toolkit.Rect{}, nil},
		{"markup stays text", toolkit.A11yInfo{Role: toolkit.RoleButton, Name: `<img src=x onerror=alert(1)>`}, area(1),
			[]webcanvas.A11yNode{{Key: "fake#0", Role: "button", Name: `<img src=x onerror=alert(1)>`, X: 1, Y: 2, W: 30, H: 10}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Nodes(Single(leaf(c.info, c.rect))...)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Nodes =\n%+v\nwant\n%+v", got, c.want)
			}
		})
	}
}

func TestNodesPrefixKeepsTwoTreesApart(t *testing.T) {
	mk := func(name string) toolkit.Widget {
		return leaf(toolkit.A11yInfo{Role: toolkit.RoleButton, Name: name}, area(1))
	}
	got := Nodes(Source{Prefix: "a", Root: mk("one")}, Source{Prefix: "b", Root: mk("two")}, Source{Prefix: "c"})
	if len(got) != 2 || got[0].Key != "a|fake#0" || got[1].Key != "b|fake#0" {
		t.Fatalf("Nodes = %+v, want keys a|fake#0 and b|fake#0 (a nil root publishes nothing)", got)
	}
	if got[0].Key == got[1].Key {
		t.Fatal("two trees share a key")
	}
}

func TestNodesKeepReadingOrderAndKeys(t *testing.T) {
	root := &fake{info: toolkit.A11yInfo{Role: toolkit.RoleGroup, Name: "Form"}}
	root.SetBounds(toolkit.Rect{W: 100, H: 100})
	root.kids = []toolkit.Widget{
		leaf(toolkit.A11yInfo{Role: toolkit.RoleButton, Name: "A"}, area(1)),
		leaf(toolkit.A11yInfo{Role: toolkit.RoleButton, Name: "B"}, area(2)),
	}
	got := Nodes(Single(root)...)
	var keys []string
	for _, n := range got {
		keys = append(keys, n.Key+"="+n.Name)
	}
	want := []string{"fake#0=Form", "fake#0/fake#0=A", "fake#0/fake#1=B"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

func TestNilHostAndNilRoot(t *testing.T) {
	var h Host
	if h.A11yNodes() != nil || h.TextInputFor("x") != nil {
		t.Error("a Host with no Sources answered")
	}
	if Nodes() != nil || TextInput("x", Source{}) != nil {
		t.Error("nothing to read must give nothing")
	}
}

func TestRealWidgets(t *testing.T) {
	btn := toolkit.NewButton("Save", func() {})
	chk := toolkit.NewCheckButton("Agree", true)
	ent := toolkit.NewEntry("a@b.c")
	ent.AccessibleName = "Email"
	root := toolkit.NewVBox()
	root.AddFixed(btn, 20)
	root.AddFixed(chk, 20)
	root.AddFixed(ent, 20)
	root.SetBounds(toolkit.Rect{W: 200, H: 60})
	byName := map[string]webcanvas.A11yNode{}
	for _, n := range Nodes(Single(root)...) {
		byName[n.Role+":"+n.Name] = n
	}
	for _, k := range []string{"button:Save", "checkbox:Agree", "textbox:Email"} {
		if _, ok := byName[k]; !ok {
			t.Errorf("%s not published; got %v", k, byName)
		}
	}
	if c := byName["checkbox:Agree"]; c.Value != "checked" {
		t.Errorf("checkbox value = %q, want checked", c.Value)
	}
	if e := byName["textbox:Email"]; e.Value != "a@b.c" || e.Multiline {
		t.Errorf("entry = %+v", e)
	}
}

func TestTextInputForTextView(t *testing.T) {
	tv := toolkit.NewTextView("hello\nworld")
	tv.AccessibleName = "Notes"
	ent := toolkit.NewEntry("x")
	ent.AccessibleName = "One line"
	root := toolkit.NewVBox()
	root.AddFixed(ent, 20)
	root.AddFixed(tv, 80)
	root.SetBounds(toolkit.Rect{W: 200, H: 120})

	var tvKey, entKey string
	for _, n := range Nodes(Single(root)...) {
		switch n.Name {
		case "Notes":
			tvKey = n.Key
			if !n.Multiline || n.Role != "textbox" {
				t.Errorf("text view node = %+v", n)
			}
		case "One line":
			entKey = n.Key
		}
	}
	if tvKey == "" || entKey == "" {
		t.Fatalf("keys not found: %q %q", tvKey, entKey)
	}
	h := Host{Sources: func() []Source { return Single(root) }}
	c := h.TextInputFor(tvKey)
	if c == nil {
		t.Fatal("the TextView has no text-input client")
	}
	if got := c.(*toolkit.TextView); got != tv {
		t.Error("the client is not the TextView that was published")
	}
	if h.TextInputFor(entKey) != nil {
		t.Error("an Entry is not a text-input client yet")
	}
	if h.TextInputFor("no/such#0") != nil {
		t.Error("an unknown key answered")
	}
	if got := len(h.A11yNodes()); got != len(Nodes(Single(root)...)) {
		t.Errorf("Host.A11yNodes = %d nodes", got)
	}
}

func TestTextInputForPrefixAndMismatch(t *testing.T) {
	tv := toolkit.NewTextView("x")
	tv.AccessibleName = "N"
	tv.SetBounds(toolkit.Rect{W: 50, H: 50})
	srcs := []Source{{Prefix: "p", Root: tv}}
	var key string
	for _, n := range Nodes(srcs...) {
		key = n.Key
	}
	if TextInput(key, srcs...) == nil {
		t.Errorf("prefixed key %q gave no client", key)
	}
	if TextInput("x|"+key, srcs...) != nil || TextInput(toolkit.WalkA11y(tv)[0].Key, srcs...) != nil {
		t.Error("a key without the right prefix answered")
	}
}

// A presentation widget is looked through, as WalkA11y does: the widgets and
// the nodes must still pair up.
func TestPairingSurvivesPresentationAndNil(t *testing.T) {
	tv := toolkit.NewTextView("x")
	tv.AccessibleName = "N"
	tv.SetBounds(toolkit.Rect{Y: 10, W: 50, H: 50})
	box := &fake{info: toolkit.A11yInfo{Role: toolkit.RolePresentation}, kids: []toolkit.Widget{nil, tv}}
	box.SetBounds(toolkit.Rect{W: 60, H: 60})
	var key string
	for _, n := range Nodes(Single(box)...) {
		key = n.Key
	}
	if c := TextInput(key, Single(box)...); c == nil || c.(*toolkit.TextView) != tv {
		t.Errorf("key %q did not reach the TextView", key)
	}
}

// Widgets that report an A11yInfo whose role changes between the two walks
// must not hand back the wrong client.
type shifty struct {
	fake
	calls int
}

func (s *shifty) A11y() toolkit.A11yInfo {
	s.calls++
	info := s.info
	if s.calls > 2 {
		info.Role = toolkit.RoleButton
	}
	return info
}

func TestPairingRefusesAChangedWidget(t *testing.T) {
	s := &shifty{fake: fake{info: toolkit.A11yInfo{Role: toolkit.RoleTextbox, Name: "N"}}}
	s.SetBounds(toolkit.Rect{W: 5, H: 5})
	if c := TextInput("shifty#0", Single(s)...); c != nil {
		t.Error("a widget that changed role was answered")
	}
}

// A count that cannot pair gives no client.
func TestUnpairableTreeGivesNil(t *testing.T) {
	// The root reports one node on the first walk, none on the second.
	l := &flicker{}
	l.SetBounds(toolkit.Rect{W: 5, H: 5})
	if c := TextInput("flicker#0", Single(l)...); c != nil {
		t.Error("unpairable tree answered")
	}
}

type flicker struct {
	fake
	calls int
}

func (f *flicker) A11y() toolkit.A11yInfo {
	f.calls++
	if f.calls == 1 {
		return toolkit.A11yInfo{Role: toolkit.RoleTextbox, Name: "N"}
	}
	return toolkit.A11yInfo{Role: toolkit.RolePresentation}
}
