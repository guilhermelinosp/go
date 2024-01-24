// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.regabiargs

package abi

const (
	// See abi_generic.go.

	// R4 - R19
	IntArgRegs = 16

	// F0 - F15
	FloatArgRegs = 16

	EffectiveFloatRegSize = 8
)
