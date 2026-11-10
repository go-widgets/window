// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gpupath

import "github.com/go-gfx/gpu2d/vulkan"

// open starts gpu2d's Vulkan renderer. Where Vulkan is absent (no loader, no
// driver behind it, a platform the binding does not support) the error
// satisfies errors.Is(err, vulkan.ErrUnsupported).
func open(w, h int) (Renderer, error) { return wrap(vulkan.New(w, h)) }

// wrap turns New's pair into a Renderer, refusing with a nil interface: a nil
// *vulkan.Renderer stored in one is not nil, and Start would call it.
func wrap(r *vulkan.Renderer, err error) (Renderer, error) {
	if err != nil {
		return nil, err
	}
	return r, nil
}
