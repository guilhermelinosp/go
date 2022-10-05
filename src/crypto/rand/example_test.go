// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rand_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
)


// This example returns a different number each run.
func ExampleInt() {
	a, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	// The output is a signed multi-precision integer.
	fmt.Println(a.Int64())

	// Output:
	//
}

// This example reads 10 cryptographically secure pseudorandom numbers from
// rand.Reader and writes them to a byte slice.
func ExampleRead() {
	c := 10
	b := make([]byte, c)
	_, err := rand.Read(b)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	// The slice should now contain random bytes instead of only zeroes.
	fmt.Println(bytes.Equal(b, make([]byte, c)))

	// Output:
	// false
}
