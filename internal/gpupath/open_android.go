// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build android

package gpupath

import "errors"

// ErrUnsupported is why the GPU path does not start on android. go-gfx/vulkan's
// build constraint is linux && (amd64 || arm64), which GOOS=android satisfies,
// so importing it there pulls in purego's android Dlopen, which needs cgo; this
// module links CGO-free on android (the `android` CI lane).
var ErrUnsupported = errors.New("gpupath: no Vulkan binding for android without cgo")

func open(int, int) (Renderer, error) { return nil, ErrUnsupported }
