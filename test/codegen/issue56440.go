// asmcheck

// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Check to make sure that we recognize when the length of an append
// is constant. We check this by making sure that the constant length
// is folded into a load offset.

package p

func f(x []int) int {
	s := make([]int, 3)
	s = append(s, 4, 5)
	// amd64:`MOVQ\t40\(.*\),`
	return x[len(s)]
}
