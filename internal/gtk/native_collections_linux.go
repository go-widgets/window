// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// GTK4 peers of the three collection kinds, bound here over purego (the same way
// github.com/go-gtk/gtk4 binds the rest: libgtk-4 is dlopen'd, no cgo):
//
//   - NativeTable: a GtkColumnView in a GtkScrolledWindow, over a GtkSingleSelection
//     of a GtkStringList. The view binds only the rows on screen, so 10 000 rows
//     cost 10 000 small GObjects built by one gtk_string_list_splice, not 10 000
//     widgets. The rows are replaced only when the descriptor's Revision moves.
//   - NativeTree: a GtkListView over a GtkTreeListModel (GtkTreeExpander rows).
//     Children are made lazily, by the model's create_func, when a row expands.
//   - NativeTabs: a GtkNotebook whose pages are empty boxes: the strip only, the
//     toolkit lays the content out below it.
//
// Docs read (docs.gtk.org/gtk4): GtkColumnView, GtkColumnViewColumn,
// GtkTreeListModel, GtkTreeListRow, GtkTreeExpander, GtkSignalListItemFactory,
// GtkSingleSelection, GtkStringList, GtkNotebook.

var (
	collOnce sync.Once
	collErr  error

	gListModelNItems func(uintptr) uint32
	gObjectUnref     func(uintptr)
	gTypeNameOf      func(uintptr) string
	gConnect         func(uintptr, string, uintptr, uintptr, uintptr, int32) uint64

	gtkStrListNew    func(uintptr) uintptr
	gtkStrListSplice func(uintptr, uint32, uint32, uintptr)
	gtkStrObjString  func(uintptr) string

	gtkSelNew           func(uintptr) uintptr
	gtkSelAutoselect    func(uintptr, bool)
	gtkSelCanUnselect   func(uintptr, bool)
	gtkSelGetSelected   func(uintptr) uint32
	gtkSelSetSelected   func(uintptr, uint32)
	gtkSelSelectedItem  func(uintptr) uintptr
	gtkFactoryNew       func() uintptr
	gtkItemSetChild     func(uintptr, uintptr)
	gtkItemGetChild     func(uintptr) uintptr
	gtkItemGetItem      func(uintptr) uintptr
	gtkLabelNewC        func(string) uintptr
	gtkLabelSetTextC    func(uintptr, string)
	gtkWidgetHalign     func(uintptr, int32)
	gtkColViewNew       func(uintptr) uintptr
	gtkColViewAppend    func(uintptr, uintptr)
	gtkColNew           func(string, uintptr) uintptr
	gtkColResizable     func(uintptr, bool)
	gtkColExpand        func(uintptr, bool)
	gtkScrolledNew      func() uintptr
	gtkScrolledSetChild func(uintptr, uintptr)
	gtkListViewNew      func(uintptr, uintptr) uintptr
	gtkTreeModelNew     func(uintptr, bool, bool, uintptr, uintptr, uintptr) uintptr
	gtkTreeModelGetRow  func(uintptr, uint32) uintptr
	gtkRowSetExpanded   func(uintptr, bool)
	gtkRowGetExpanded   func(uintptr) bool
	gtkRowGetItem       func(uintptr) uintptr
	gtkExpanderNew      func() uintptr
	gtkExpanderSetChild func(uintptr, uintptr)
	gtkExpanderGetChild func(uintptr) uintptr
	gtkExpanderSetRow   func(uintptr, uintptr)
	gtkNotebookNew      func() uintptr
	gtkNotebookAppend   func(uintptr, uintptr, uintptr) int32
	gtkNotebookRemove   func(uintptr, int32)
	gtkNotebookNPages   func(uintptr) int32
	gtkNotebookCurrent  func(uintptr) int32
	gtkNotebookSetCur   func(uintptr, int32)
	gtkBoxNewC          func(int32, int32) uintptr

	cbSetupLabel    uintptr
	cbBindCell      uintptr
	cbSetupExpander uintptr
	cbBindNode      uintptr
	cbSelected      uintptr
	cbChildren      uintptr
	cbSwitchPage    uintptr

	regMu  sync.Mutex
	regSeq uintptr
	reg    = map[uintptr]any{}
)

const invalidPos = 0xffffffff // GTK_INVALID_LIST_POSITION

func loadCollections() error {
	collOnce.Do(func() {
		if _, err := gtk4.Init(); err != nil {
			collErr = err
			return
		}
		dl := func(names ...string) uintptr {
			for _, n := range names {
				if h, err := purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
					return h
				}
			}
			return 0
		}
		gio := dl("libgio-2.0.so.0", "libgio-2.0.so")
		gobj := dl("libgobject-2.0.so.0", "libgobject-2.0.so")
		gtk := dl("libgtk-4.so.1", "libgtk-4.so")
		if gio == 0 || gobj == 0 || gtk == 0 {
			collErr = errCollLibs
			return
		}
		r := func(p any, h uintptr, name string) { purego.RegisterLibFunc(p, h, name) }
		r(&gListModelNItems, gio, "g_list_model_get_n_items")
		r(&gObjectUnref, gobj, "g_object_unref")
		r(&gTypeNameOf, gobj, "g_type_name_from_instance")
		r(&gConnect, gobj, "g_signal_connect_data")
		r(&gtkStrListNew, gtk, "gtk_string_list_new")
		r(&gtkStrListSplice, gtk, "gtk_string_list_splice")
		r(&gtkStrObjString, gtk, "gtk_string_object_get_string")
		r(&gtkSelNew, gtk, "gtk_single_selection_new")
		r(&gtkSelAutoselect, gtk, "gtk_single_selection_set_autoselect")
		r(&gtkSelCanUnselect, gtk, "gtk_single_selection_set_can_unselect")
		r(&gtkSelGetSelected, gtk, "gtk_single_selection_get_selected")
		r(&gtkSelSetSelected, gtk, "gtk_single_selection_set_selected")
		r(&gtkSelSelectedItem, gtk, "gtk_single_selection_get_selected_item")
		r(&gtkFactoryNew, gtk, "gtk_signal_list_item_factory_new")
		r(&gtkItemSetChild, gtk, "gtk_list_item_set_child")
		r(&gtkItemGetChild, gtk, "gtk_list_item_get_child")
		r(&gtkItemGetItem, gtk, "gtk_list_item_get_item")
		r(&gtkLabelNewC, gtk, "gtk_label_new")
		r(&gtkLabelSetTextC, gtk, "gtk_label_set_text")
		r(&gtkWidgetHalign, gtk, "gtk_widget_set_halign")
		r(&gtkColViewNew, gtk, "gtk_column_view_new")
		r(&gtkColViewAppend, gtk, "gtk_column_view_append_column")
		r(&gtkColNew, gtk, "gtk_column_view_column_new")
		r(&gtkColResizable, gtk, "gtk_column_view_column_set_resizable")
		r(&gtkColExpand, gtk, "gtk_column_view_column_set_expand")
		r(&gtkScrolledNew, gtk, "gtk_scrolled_window_new")
		r(&gtkScrolledSetChild, gtk, "gtk_scrolled_window_set_child")
		r(&gtkListViewNew, gtk, "gtk_list_view_new")
		r(&gtkTreeModelNew, gtk, "gtk_tree_list_model_new")
		r(&gtkTreeModelGetRow, gtk, "gtk_tree_list_model_get_row")
		r(&gtkRowSetExpanded, gtk, "gtk_tree_list_row_set_expanded")
		r(&gtkRowGetExpanded, gtk, "gtk_tree_list_row_get_expanded")
		r(&gtkRowGetItem, gtk, "gtk_tree_list_row_get_item")
		r(&gtkExpanderNew, gtk, "gtk_tree_expander_new")
		r(&gtkExpanderSetChild, gtk, "gtk_tree_expander_set_child")
		r(&gtkExpanderGetChild, gtk, "gtk_tree_expander_get_child")
		r(&gtkExpanderSetRow, gtk, "gtk_tree_expander_set_list_row")
		r(&gtkNotebookNew, gtk, "gtk_notebook_new")
		r(&gtkNotebookAppend, gtk, "gtk_notebook_append_page")
		r(&gtkNotebookRemove, gtk, "gtk_notebook_remove_page")
		r(&gtkNotebookNPages, gtk, "gtk_notebook_get_n_pages")
		r(&gtkNotebookCurrent, gtk, "gtk_notebook_get_current_page")
		r(&gtkNotebookSetCur, gtk, "gtk_notebook_set_current_page")
		r(&gtkBoxNewC, gtk, "gtk_box_new")

		// One callback per signal shape for the whole process (purego never frees
		// a callback and caps their number); the user-data word says which control
		// or column a call is for.
		cbSetupLabel = purego.NewCallback(func(_, item, _ uintptr) {
			l := gtkLabelNewC("")
			gtkWidgetHalign(l, 1) // GTK_ALIGN_START
			gtkItemSetChild(item, l)
		})
		cbBindCell = purego.NewCallback(func(_, item, data uintptr) {
			col, _ := lookup(data).(*collColumn)
			if col == nil {
				return
			}
			obj := gtkItemGetItem(item)
			if obj == 0 {
				return
			}
			gtkLabelSetTextC(gtkItemGetChild(item), cellAt(gtkStrObjString(obj), col.idx))
		})
		cbSetupExpander = purego.NewCallback(func(_, item, _ uintptr) {
			e := gtkExpanderNew()
			l := gtkLabelNewC("")
			gtkWidgetHalign(l, 1)
			gtkExpanderSetChild(e, l)
			gtkItemSetChild(item, e)
		})
		cbBindNode = purego.NewCallback(func(_, item, _ uintptr) {
			row := gtkItemGetItem(item) // a GtkTreeListRow (not passed through)
			if row == 0 {
				return
			}
			e := gtkItemGetChild(item)
			gtkExpanderSetRow(e, row)
			obj := gtkRowGetItem(row)
			if obj == 0 {
				return
			}
			_, title := splitTreeItem(gtkStrObjString(obj))
			gObjectUnref(obj)
			gtkLabelSetTextC(gtkExpanderGetChild(e), title)
		})
		cbSelected = purego.NewCallback(func(_, _, data uintptr) {
			if c, _ := lookup(data).(*collection); c != nil {
				c.selectionChanged()
			}
		})
		cbChildren = purego.NewCallback(func(item, data uintptr) uintptr {
			c, _ := lookup(data).(*collection)
			if c == nil {
				return 0
			}
			return c.makeChildren(item)
		})
		cbSwitchPage = purego.NewCallback(func(_, _, page, data uintptr) {
			if c, _ := lookup(data).(*collection); c != nil {
				c.pageSwitched(int(int32(uint32(page))))
			}
		})
	})
	return collErr
}

var errCollLibs = errString("gtk: libgio/libgobject/libgtk-4 could not be loaded")

type errString string

func (e errString) Error() string { return string(e) }

func register(v any) uintptr {
	regMu.Lock()
	defer regMu.Unlock()
	regSeq++
	reg[regSeq] = v
	return regSeq
}

func lookup(id uintptr) any {
	regMu.Lock()
	defer regMu.Unlock()
	return reg[id]
}

func unregister(id uintptr) {
	regMu.Lock()
	delete(reg, id)
	regMu.Unlock()
}

// collColumn is the user data of one column's bind callback.
type collColumn struct{ idx int }

// collection is the GTK side of one hosted table, tree or tab strip.
type collection struct {
	id   uintptr
	lc   *liveControl
	kind toolkit.NativeKind

	root uintptr // the widget the control places: scroller, or the notebook
	view uintptr // the GtkColumnView / GtkListView / GtkNotebook itself
	list uintptr // the GtkStringList that holds the rows / top-level nodes
	sel  uintptr // the GtkSingleSelection
	tlm  uintptr // the GtkTreeListModel (tree)

	ncols int
	tree  []toolkit.NativeTreeNode
	items []string // the tab titles

	mute     int // >0 while the host itself changes the selection
	rev      uint64
	rebuilds int // how many times the data was rebuilt (tests read it)
	ids      []uintptr
}

// viewClass is the GTK class name of the widget that does the work.
func (c *collection) viewClass() string { return gTypeNameOf(c.view) }

func (c *collection) release() {
	unregister(c.id)
	for _, id := range c.ids {
		unregister(id)
	}
}

// newCollection builds the GTK widget for a NativeTable, NativeTree or NativeTabs
// descriptor and fills it. It returns nil when GTK4 cannot be bound.
func newCollection(lc *liveControl, spec toolkit.NativeControl) *collection {
	if loadCollections() != nil {
		return nil
	}
	c := &collection{lc: lc, kind: spec.Kind}
	c.id = register(c)
	switch spec.Kind {
	case toolkit.NativeTable:
		c.initTable(spec)
	case toolkit.NativeTree:
		c.initTree(spec)
	case toolkit.NativeTabs:
		c.initTabs(spec)
	}
	c.rev = spec.Revision
	return c
}

func (c *collection) widget() gtk4.Widget { return gtk4.Widget(c.root) }

func (c *collection) newSelection(model uintptr) {
	c.sel = gtkSelNew(model)
	gtkSelAutoselect(c.sel, false)
	gtkSelCanUnselect(c.sel, true)
	gConnect(c.sel, "notify::selected", cbSelected, c.id, 0, 0)
}

func (c *collection) scrolled(view uintptr) {
	c.view = view
	c.root = gtkScrolledNew()
	gtkScrolledSetChild(c.root, view)
}

// ---- table ----

func (c *collection) initTable(spec toolkit.NativeControl) {
	c.ncols = len(spec.Columns)
	c.list = gtkStrListNew(0)
	c.newSelection(c.list) // the selection owns the list from here
	view := gtkColViewNew(c.sel)
	for i, title := range spec.Columns {
		f := gtkFactoryNew()
		id := register(&collColumn{idx: i})
		c.ids = append(c.ids, id)
		gConnect(f, "setup", cbSetupLabel, 0, 0, 0)
		gConnect(f, "bind", cbBindCell, id, 0, 0)
		col := gtkColNew(title, f) // takes the factory
		gtkColResizable(col, true)
		gtkColExpand(col, true)
		gtkColViewAppend(view, col)
		gObjectUnref(col) // the view holds its own reference
	}
	c.scrolled(view)
	c.setRows(spec.Rows)
	if i := int(spec.Number); i >= 0 {
		c.selectIndex(i)
	}
}

// setRows replaces every row with one splice: a single items-changed.
func (c *collection) setRows(rows [][]string) {
	items := make([]string, len(rows))
	for i, r := range rows {
		items[i] = encodeRow(r, c.ncols)
	}
	c.spliceAll(c.list, items)
	c.rebuilds++
}

// spliceAll replaces the whole content of a GtkStringList with items, in one
// gtk_string_list_splice (the additions are a NULL-terminated char** built from a
// single buffer, kept alive across the call).
func (c *collection) spliceAll(list uintptr, items []string) {
	total := 0
	for _, s := range items {
		total += len(s) + 1
	}
	buf := make([]byte, 0, total)
	ptrs := make([]uintptr, len(items)+1)
	offs := make([]int, len(items))
	for i, s := range items {
		offs[i] = len(buf)
		buf = append(buf, s...)
		buf = append(buf, 0)
	}
	var add uintptr
	if len(items) > 0 {
		base := uintptr(unsafe.Pointer(&buf[0]))
		for i := range items {
			ptrs[i] = base + uintptr(offs[i])
		}
		add = uintptr(unsafe.Pointer(&ptrs[0]))
	}
	c.mute++
	gtkStrListSplice(list, 0, gListModelNItems(list), add)
	c.mute--
	runtime.KeepAlive(buf)
	runtime.KeepAlive(ptrs)
}

// selectIndex selects row i of a table, or clears the selection for i < 0.
func (c *collection) selectIndex(i int) {
	pos := uint32(invalidPos)
	if i >= 0 {
		pos = uint32(i)
	}
	c.mute++
	gtkSelSetSelected(c.sel, pos)
	c.mute--
}

// ---- tree ----

func (c *collection) initTree(spec toolkit.NativeControl) {
	c.tree = spec.Tree
	c.list = gtkStrListNew(0)
	c.tlm = gtkTreeModelNew(c.list, false, false, cbChildren, c.id, 0) // takes the list
	c.newSelection(c.tlm)
	f := gtkFactoryNew()
	gConnect(f, "setup", cbSetupExpander, 0, 0, 0)
	gConnect(f, "bind", cbBindNode, 0, 0, 0)
	c.scrolled(gtkListViewNew(c.sel, f))
	c.setTree(spec.Tree)
	c.selectPath(spec.Text)
}

// makeChildren is the model's create_func: the children of the node an item
// stands for, as a new model, or 0 for a leaf.
func (c *collection) makeChildren(item uintptr) uintptr {
	path, _ := splitTreeItem(gtkStrObjString(item))
	idx, ok := toolkit.ParseNativeTreePath(path)
	if !ok {
		return 0
	}
	kids := childItems(c.tree, idx)
	if len(kids) == 0 {
		return 0
	}
	l := gtkStrListNew(0)
	c.spliceAll(l, kids)
	return l
}

// setTree replaces the top level and re-expands the nodes the descriptor says
// start expanded. Children are made by makeChildren as each row expands.
func (c *collection) setTree(tree []toolkit.NativeTreeNode) {
	c.mute++
	defer func() { c.mute-- }()
	c.tree = tree
	c.spliceAll(c.list, childItems(tree, nil))
	// Expanding a row inserts its children right after it, so walking forward
	// reaches them next, and the walk is bounded by the clamped node count.
	for i := uint32(0); i < gListModelNItems(c.tlm); i++ {
		row := gtkTreeModelGetRow(c.tlm, i)
		if row == 0 {
			continue
		}
		if p, ok := c.rowPath(row); ok {
			if n := treeNodeAt(tree, p); n != nil && n.Expanded && len(n.Children) > 0 {
				gtkRowSetExpanded(row, true)
			}
		}
		gObjectUnref(row)
	}
	c.rebuilds++
}

// rowPath is the path of the node a GtkTreeListRow stands for.
func (c *collection) rowPath(row uintptr) ([]int, bool) {
	obj := gtkRowGetItem(row)
	if obj == 0 {
		return nil, false
	}
	path, _ := splitTreeItem(gtkStrObjString(obj))
	gObjectUnref(obj)
	return toolkit.ParseNativeTreePath(path)
}

// findPath returns the position of the visible row for a path, or -1.
func (c *collection) findPath(path string) int {
	n := gListModelNItems(c.tlm)
	for i := uint32(0); i < n; i++ {
		row := gtkTreeModelGetRow(c.tlm, i)
		if row == 0 {
			continue
		}
		obj := gtkRowGetItem(row)
		var p string
		if obj != 0 {
			p, _ = splitTreeItem(gtkStrObjString(obj))
			gObjectUnref(obj)
		}
		gObjectUnref(row)
		if p == path {
			return int(i)
		}
	}
	return -1
}

// selectPath selects the node at a path ("" clears), expanding its ancestors so
// the row exists.
func (c *collection) selectPath(path string) {
	c.mute++
	defer func() { c.mute-- }()
	idx, ok := toolkit.ParseNativeTreePath(path)
	if path == "" || !ok || treeNodeAt(c.tree, idx) == nil {
		gtkSelSetSelected(c.sel, invalidPos)
		return
	}
	for d := 1; d < len(idx); d++ {
		pos := c.findPath(treePathString(idx[:d]))
		if pos < 0 {
			return
		}
		if row := gtkTreeModelGetRow(c.tlm, uint32(pos)); row != 0 {
			gtkRowSetExpanded(row, true)
			gObjectUnref(row)
		}
	}
	if pos := c.findPath(path); pos >= 0 {
		gtkSelSetSelected(c.sel, uint32(pos))
	}
}

// ---- tabs ----

func (c *collection) initTabs(spec toolkit.NativeControl) {
	c.root = gtkNotebookNew()
	c.view = c.root
	gConnect(c.view, "switch-page", cbSwitchPage, c.id, 0, 0)
	c.setTabs(spec.Items)
	c.selectTab(int(spec.Number))
}

func (c *collection) setTabs(items []string) {
	c.mute++
	for n := gtkNotebookNPages(c.view); n > 0; n-- {
		gtkNotebookRemove(c.view, -1)
	}
	for _, t := range items {
		page := gtkBoxNewC(0, 0) // an empty page: the toolkit lays the content out
		gtkNotebookAppend(c.view, page, gtkLabelNewC(cleanCell(t)))
	}
	c.mute--
	c.items = items
	c.rebuilds++
}

func (c *collection) selectTab(i int) {
	if i < 0 || i >= len(c.items) {
		return
	}
	c.mute++
	gtkNotebookSetCur(c.view, int32(i))
	c.mute--
}

// ---- signals ----

func (c *collection) selectionChanged() {
	if c.mute > 0 {
		return
	}
	pos := gtkSelGetSelected(c.sel)
	switch c.kind {
	case toolkit.NativeTable:
		v := -1.0
		if pos != invalidPos {
			v = float64(pos)
		}
		c.lc.lastNum = v
		if c.lc.onNumber != nil {
			c.lc.onNumber(v)
		}
	case toolkit.NativeTree:
		path := ""
		if pos != invalidPos {
			if row := gtkSelSelectedItem(c.sel); row != 0 {
				if obj := gtkRowGetItem(row); obj != 0 {
					path, _ = splitTreeItem(gtkStrObjString(obj))
					gObjectUnref(obj)
				}
			}
		}
		c.lc.lastText = path
		if c.lc.onText != nil {
			c.lc.onText(path)
		}
	}
}

func (c *collection) pageSwitched(i int) {
	if c.mute > 0 {
		return
	}
	c.lc.lastNum = float64(i)
	if c.lc.onNumber != nil {
		c.lc.onNumber(c.lc.lastNum)
	}
}

// apply reconciles a frame's descriptor: the data is re-read only when the
// Revision moved (so an unchanged 10 000-row table costs a comparison), and a
// selection is pushed only when the app moved it.
func (c *collection) apply(lc *liveControl, spec toolkit.NativeControl) {
	rebuilt := false
	if spec.Revision != c.rev {
		switch c.kind {
		case toolkit.NativeTable:
			c.setRows(spec.Rows)
		case toolkit.NativeTree:
			c.setTree(spec.Tree)
		case toolkit.NativeTabs:
			c.setTabs(spec.Items)
		}
		c.rev = spec.Revision
		rebuilt = true
	}
	switch c.kind {
	case toolkit.NativeTable:
		if rebuilt || spec.Number != lc.lastNum {
			c.selectIndex(int(spec.Number))
			lc.lastNum = spec.Number
		}
	case toolkit.NativeTree:
		if rebuilt || spec.Text != lc.lastText {
			c.selectPath(spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativeTabs:
		if rebuilt || spec.Number != lc.lastNum {
			c.selectTab(int(spec.Number))
			lc.lastNum = spec.Number
		}
	}
}
