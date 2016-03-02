// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// mustParseHexdumpFile returns a block of data generated by
// parsing the hex dump in the named file.
// If the file cannot be read or does not contain a valid hex dump,
// mustParseHexdumpFile calls t.Fatal.
func mustParseHexdumpFile(t *testing.T, file string) []byte {
	hex, err := ioutil.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data, err := parseHexdump(string(hex))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// parseHexdump parses the hex dump in text, which should be the
// output of "hexdump -C" or Plan 9's "xd -b",
// and returns the original data used to produce the dump.
// It is meant to enable storing golden binary files as text, so that
// changes to the golden files can be seen during code reviews.
func parseHexdump(text string) ([]byte, error) {
	var out []byte
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "|"); i >= 0 { // remove text dump
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) > 1+16 {
			return nil, fmt.Errorf("parsing hex dump: too many fields on line %q", line)
		}
		if len(f) == 0 || len(f) == 1 && f[0] == "*" { // all zeros block omitted
			continue
		}
		addr64, err := strconv.ParseUint(f[0], 16, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing hex dump: invalid address %q", f[0])
		}
		addr := int(addr64)
		if len(out) < addr {
			out = append(out, make([]byte, addr-len(out))...)
		}
		for _, x := range f[1:] {
			val, err := strconv.ParseUint(x, 16, 8)
			if err != nil {
				return nil, fmt.Errorf("parsing hexdump: invalid hex byte %q", x)
			}
			out = append(out, byte(val))
		}
	}
	return out, nil
}

func hexdump(data []byte) string {
	text := hex.Dump(data) + fmt.Sprintf("%08x\n", len(data))
	text = regexp.MustCompile(`\n([0-9a-f]+(\s+00){16}.*\n)+`).ReplaceAllString(text, "\n*\n")
	return text
}
