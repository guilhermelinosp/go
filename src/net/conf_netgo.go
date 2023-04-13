// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Set the defaultResolver to resolverGo when the netgo build tag is being used.

//go:build netgo

package net

func init() { defaultResolver = resolverGo }
