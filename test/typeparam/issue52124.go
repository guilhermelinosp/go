// compile

// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package p

type Any any
type IntOrBool interface{ int | bool }

type I interface{ Any | IntOrBool }

var (
	X I = 42
	Y I = "xxx"
	Z I = true
)

type A interface{ *B | int }
type B interface{ A | any }
