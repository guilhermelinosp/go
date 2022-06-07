// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package flate implements the DEFLATE compressed data format, described in
// RFC 1951.  The gzip and zlib packages implement access to DEFLATE-based file
// formats.

//go:build generate
// +build generate

//go:generate go run $GOFILE
//go:generate go fmt inflate_gen.go

package main

import (
	"os"
	"strings"
)

func main() {
	f, err := os.Create("inflate_gen.go")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	types := []string{"*bytes.Buffer", "*bytes.Reader", "*bufio.Reader", "*strings.Reader", "Reader"}
	names := []string{"BytesBuffer", "BytesReader", "BufioReader", "StringsReader", "GenericReader"}
	imports := []string{"bytes", "bufio", "strings", "math/bits"}
	f.WriteString(`// Code generated by go generate gen_inflate.go. DO NOT EDIT.

package flate

import (
`)

	for _, imp := range imports {
		f.WriteString("\t\"" + imp + "\"\n")
	}
	f.WriteString(")\n\n")

	template := `

// $FUNCNAME$ decodes a single Huffman block from f.
// f.r must be a $TYPE$.
// hl and hd are the Huffman states for the lit/length values
// and the distance values, respectively. If hd == nil, using the
// fixed distance encoding associated with fixed Huffman blocks.
func (f *decompressor) $FUNCNAME$() {
	const (
		stateInit = iota // Zero value must be stateInit
		stateDict
	)
	fr := f.r.($TYPE$)

	// Optimization. Compiler isn't smart enough to keep f.b, f.nb in registers,
	// but is smart enough to keep local variables in registers, so use nb and b,
	// inline call to moreBits and reassign b, nb back to f on return.
	fnb, fb, dict := f.nb, f.b, &f.dict
	switch f.stepState {
	case stateInit:
		goto readLiteral
	case stateDict:
		goto copyHistory
	}

readLiteral:
	// Read literal and/or (length, distance) according to RFC section 3.2.3.
	{
		var v int
		{
			// Inlined v, err := f.huffSym(f.hl)
			// Since a huffmanDecoder can be empty or be composed of a degenerate tree
			// with single element, huffSym must error on these two edge cases. In both
			// cases, the chunks slice will be 0 for the invalid sequence, leading it
			// satisfy the n == 0 check below.
			n := uint(f.hl.maxRead)
			for {
				for fnb < n {
					c, err := fr.ReadByte()
					if err != nil {
						f.b, f.nb = fb, fnb
						f.err = noEOF(err)
						return
					}
					f.roffset++
					fb |= uint32(c) << (fnb & 31)
					fnb += 8
				}
				chunk := f.hl.chunks[fb&(huffmanNumChunks-1)]
				n = uint(chunk & huffmanCountMask)
				if n > huffmanChunkBits {
					chunk = f.hl.links[chunk>>huffmanValueShift][(fb>>huffmanChunkBits)&f.hl.linkMask]
					n = uint(chunk & huffmanCountMask)
				}
				if n <= fnb {
					if n == 0 {
						f.b, f.nb = fb, fnb
						f.err = CorruptInputError(f.roffset)
						return
					}
					fb = fb >> (n & 31)
					fnb = fnb - n
					v = int(chunk >> huffmanValueShift)
					break
				}
			}
		}
		var length int
		switch {
		case v < 256:
			dict.writeByte(byte(v))
			if dict.availWrite() == 0 {
				f.toRead = dict.readFlush()
				f.step = (*decompressor).$FUNCNAME$
				f.stepState = stateInit
				f.b, f.nb = fb, fnb
				return
			}
			goto readLiteral
		case v == 256:
			f.b, f.nb = fb, fnb
			f.finishBlock()
			return
		// otherwise, reference to older data
		case v < 265:
			length = v - (257 - 3)
		case v < maxNumLit:
			val := decCodeToLen[(v - 257)]
			length = int(val.length) + 3
			n := uint(val.extra)
			for fnb < n {
				c, err := fr.ReadByte()
				if err != nil {
					f.b, f.nb = fb, fnb
					f.err = err
					return
				}
				f.roffset++
				fb |= uint32(c) << (fnb&31)
				fnb += 8	
			}
			length += int(fb & bitMask32[n])
			fb >>= n & 31
			fnb -= n
		default:
			f.err = CorruptInputError(f.roffset)
			f.b, f.nb = fb, fnb
			return
		}
		var dist uint32
		if f.hd == nil {
			for fnb < 5 {
				c, err := fr.ReadByte()
				if err != nil {
					f.b, f.nb = fb, fnb
					f.err = err
					return
				}
				f.roffset++
				fb |= uint32(c) << (fnb&31)
				fnb += 8
			}
			dist = uint32(bits.Reverse8(uint8(fb & 0x1F << 3)))
			fb >>= 5
			fnb -= 5
		} else {
			// Since a huffmanDecoder can be empty or be composed of a degenerate tree
			// with single element, huffSym must error on these two edge cases. In both
			// cases, the chunks slice will be 0 for the invalid sequence, leading it
			// satisfy the n == 0 check below.
			n := uint(f.hd.maxRead)
			// Optimization. Compiler isn't smart enough to keep f.b,f.nb in registers,
			// but is smart enough to keep local variables in registers, so use nb and b,
			// inline call to moreBits and reassign b,nb back to f on return.
			for {
				for fnb < n {
					c, err := fr.ReadByte()
					if err != nil {
						f.b, f.nb = fb, fnb
						f.err = noEOF(err)
						return
					}
					f.roffset++
					fb |= uint32(c) << (fnb & 31)
					fnb += 8
				}
				chunk := f.hd.chunks[fb&(huffmanNumChunks-1)]
				n = uint(chunk & huffmanCountMask)
				if n > huffmanChunkBits {
					chunk = f.hd.links[chunk>>huffmanValueShift][(fb>>huffmanChunkBits)&f.hd.linkMask]
					n = uint(chunk & huffmanCountMask)
				}
				if n <= fnb {
					if n == 0 {
						f.b, f.nb = fb, fnb
						f.err = CorruptInputError(f.roffset)
						return
					}
					fb = fb >> (n & 31)
					fnb = fnb - n
					dist = uint32(chunk >> huffmanValueShift)
					break
				}
			}
		}
		switch {
		case dist < 4:
			dist++
		case dist < maxNumDist:
			nb := uint(dist-2) >> 1
			// have 1 bit in bottom of dist, need nb more.
			extra := (dist & 1) << (nb & 31)
			for fnb < nb {
				c, err := fr.ReadByte()
				if err != nil {
					f.b, f.nb = fb, fnb
					f.err = err
					return
				}
				f.roffset++
				fb |= uint32(c) << (fnb&31)
				fnb += 8
			}
			extra |= fb & bitMask32[nb]
			fb >>= nb & 31
			fnb -= nb
			dist = 1<<((nb+1)&31) + 1 + extra
		default:
			f.b, f.nb = fb, fnb
			f.err = CorruptInputError(f.roffset)
			return
		}
		// No check on length; encoding can be prescient.
		if dist > uint32(dict.histSize()) {
			f.b, f.nb = fb, fnb
			f.err = CorruptInputError(f.roffset)
			return
		}
		f.copyLen, f.copyDist = length, int(dist)
		goto copyHistory
	}
copyHistory:
	// Perform a backwards copy according to RFC section 3.2.3.
	{
		cnt := dict.tryWriteCopy(f.copyDist, f.copyLen)
		if cnt == 0 {
			cnt = dict.writeCopy(f.copyDist, f.copyLen)
		}
		f.copyLen -= cnt
		if dict.availWrite() == 0 || f.copyLen > 0 {
			f.toRead = dict.readFlush()
			f.step = (*decompressor).$FUNCNAME$ // We need to continue this work
			f.stepState = stateDict
			f.b, f.nb = fb, fnb
			return
		}
		goto readLiteral
	}
	// Not reached
}

`
	for i, t := range types {
		s := strings.Replace(template, "$FUNCNAME$", "huffman"+names[i], -1)
		s = strings.Replace(s, "$TYPE$", t, -1)
		f.WriteString(s)
	}
	f.WriteString("func (f *decompressor) huffmanBlockDecoder() func() {\n")
	f.WriteString("\tswitch f.r.(type) {\n")
	for i, t := range types {
		f.WriteString("\t\tcase " + t + ":\n")
		f.WriteString("\t\t\treturn f.huffman" + names[i] + "\n")
	}
	f.WriteString("\t\tdefault:\n")
	f.WriteString("\t\t\treturn f.huffmanGenericReader")
	f.WriteString("\t}\n}\n")
}
