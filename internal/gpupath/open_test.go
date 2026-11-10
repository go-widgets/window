// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gpupath

import (
	"errors"
	"testing"

	"github.com/go-gfx/gpu2d/vulkan"
)

// A refusal comes with a nil Renderer, not an interface holding a nil pointer.
func TestWrapRefusesWithANilRenderer(t *testing.T) {
	refused := errors.New("no device")
	r, err := wrap(nil, refused)
	if r != nil || !errors.Is(err, refused) {
		t.Fatalf("wrap(nil, err) = %v, %v", r, err)
	}
	var one vulkan.Renderer
	if r, err := wrap(&one, nil); r == nil || err != nil {
		t.Fatalf("wrap(renderer, nil) = %v, %v", r, err)
	}
}

// The default Open is gpu2d's Vulkan renderer: either a device answers or the
// refusal says why (errors.Is(err, vulkan.ErrUnsupported) with no loader).
func TestTheDefaultOpenStartsVulkanOrSaysWhyNot(t *testing.T) {
	r, err := Open(32, 32)
	if err != nil {
		if r != nil {
			t.Fatalf("a refusal came with a renderer: %v", r)
		}
		t.Logf("no Vulkan here: %v", err)
		return
	}
	defer r.Close()
	if len(r.Pixels()) != 4*32*32 {
		t.Fatalf("%d pixel bytes for 32 × 32", len(r.Pixels()))
	}
}
