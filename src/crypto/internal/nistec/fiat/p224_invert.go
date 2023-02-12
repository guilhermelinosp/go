// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Code generated by addchain. DO NOT EDIT.

package fiat

// Invert sets e = 1/x, and returns e.
//
// If x == 0, Invert returns e = 0.
func (e *P224Element) Invert(x *P224Element) *P224Element {
	// Inversion is implemented as exponentiation with exponent p − 2.
	// The sequence of 11 multiplications and 223 squarings is derived from the
	// following addition chain generated with github.com/mmcloughlin/addchain v0.4.0.
	//
	//	_10     = 2*1
	//	_11     = 1 + _10
	//	_110    = 2*_11
	//	_111    = 1 + _110
	//	_111000 = _111 << 3
	//	_111111 = _111 + _111000
	//	x12     = _111111 << 6 + _111111
	//	x14     = x12 << 2 + _11
	//	x17     = x14 << 3 + _111
	//	x31     = x17 << 14 + x14
	//	x48     = x31 << 17 + x17
	//	x96     = x48 << 48 + x48
	//	x127    = x96 << 31 + x31
	//	return    x127 << 97 + x96
	//

	var z = new(P224Element).Set(e)
	var t0 = new(P224Element)
	var t1 = new(P224Element)
	var t2 = new(P224Element)

	z.Square(x)
	t0.Mul(x, z)
	z.Square(t0)
	z.Mul(x, z)
	t1.Square(z)
	for s := 1; s < 3; s++ {
		t1.Square(t1)
	}
	t1.Mul(z, t1)
	t2.Square(t1)
	for s := 1; s < 6; s++ {
		t2.Square(t2)
	}
	t1.Mul(t1, t2)
	for s := 0; s < 2; s++ {
		t1.Square(t1)
	}
	t0.Mul(t0, t1)
	t1.Square(t0)
	for s := 1; s < 3; s++ {
		t1.Square(t1)
	}
	z.Mul(z, t1)
	t1.Square(z)
	for s := 1; s < 14; s++ {
		t1.Square(t1)
	}
	t0.Mul(t0, t1)
	t1.Square(t0)
	for s := 1; s < 17; s++ {
		t1.Square(t1)
	}
	z.Mul(z, t1)
	t1.Square(z)
	for s := 1; s < 48; s++ {
		t1.Square(t1)
	}
	z.Mul(z, t1)
	t1.Square(z)
	for s := 1; s < 31; s++ {
		t1.Square(t1)
	}
	t0.Mul(t0, t1)
	for s := 0; s < 97; s++ {
		t0.Square(t0)
	}
	z.Mul(z, t0)

	return e.Set(z)
}
