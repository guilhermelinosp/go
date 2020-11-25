// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Indexed package export.
//
// The indexed export data format is an evolution of the previous
// binary export data format. Its chief contribution is introducing an
// index table, which allows efficient random access of individual
// declarations and inline function bodies. In turn, this allows
// avoiding unnecessary work for compilation units that import large
// packages.
//
//
// The top-level data format is structured as:
//
//     Header struct {
//         Tag        byte   // 'i'
//         Version    uvarint
//         StringSize uvarint
//         DataSize   uvarint
//     }
//
//     Strings [StringSize]byte
//     Data    [DataSize]byte
//
//     MainIndex []struct{
//         PkgPath   stringOff
//         PkgName   stringOff
//         PkgHeight uvarint
//
//         Decls []struct{
//             Name   stringOff
//             Offset declOff
//         }
//     }
//
//     Fingerprint [8]byte
//
// uvarint means a uint64 written out using uvarint encoding.
//
// []T means a uvarint followed by that many T objects. In other
// words:
//
//     Len   uvarint
//     Elems [Len]T
//
// stringOff means a uvarint that indicates an offset within the
// Strings section. At that offset is another uvarint, followed by
// that many bytes, which form the string value.
//
// declOff means a uvarint that indicates an offset within the Data
// section where the associated declaration can be found.
//
//
// There are five kinds of declarations, distinguished by their first
// byte:
//
//     type Var struct {
//         Tag  byte // 'V'
//         Pos  Pos
//         Type typeOff
//     }
//
//     type Func struct {
//         Tag       byte // 'F'
//         Pos       Pos
//         Signature Signature
//     }
//
//     type Const struct {
//         Tag   byte // 'C'
//         Pos   Pos
//         Value Value
//     }
//
//     type Type struct {
//         Tag        byte // 'T'
//         Pos        Pos
//         Underlying typeOff
//
//         Methods []struct{  // omitted if Underlying is an interface type
//             Pos       Pos
//             Name      stringOff
//             Recv      Param
//             Signature Signature
//         }
//     }
//
//     type Alias struct {
//         Tag  byte // 'A'
//         Pos  Pos
//         Type typeOff
//     }
//
//
// typeOff means a uvarint that either indicates a predeclared type,
// or an offset into the Data section. If the uvarint is less than
// predeclReserved, then it indicates the index into the predeclared
// types list (see predeclared in bexport.go for order). Otherwise,
// subtracting predeclReserved yields the offset of a type descriptor.
//
// Value means a type and type-specific value. See
// (*exportWriter).value for details.
//
//
// There are nine kinds of type descriptors, distinguished by an itag:
//
//     type DefinedType struct {
//         Tag     itag // definedType
//         Name    stringOff
//         PkgPath stringOff
//     }
//
//     type PointerType struct {
//         Tag  itag // pointerType
//         Elem typeOff
//     }
//
//     type SliceType struct {
//         Tag  itag // sliceType
//         Elem typeOff
//     }
//
//     type ArrayType struct {
//         Tag  itag // arrayType
//         Len  uint64
//         Elem typeOff
//     }
//
//     type ChanType struct {
//         Tag  itag   // chanType
//         Dir  uint64 // 1 RecvOnly; 2 SendOnly; 3 SendRecv
//         Elem typeOff
//     }
//
//     type MapType struct {
//         Tag  itag // mapType
//         Key  typeOff
//         Elem typeOff
//     }
//
//     type FuncType struct {
//         Tag       itag // signatureType
//         PkgPath   stringOff
//         Signature Signature
//     }
//
//     type StructType struct {
//         Tag     itag // structType
//         PkgPath stringOff
//         Fields []struct {
//             Pos      Pos
//             Name     stringOff
//             Type     typeOff
//             Embedded bool
//             Note     stringOff
//         }
//     }
//
//     type InterfaceType struct {
//         Tag     itag // interfaceType
//         PkgPath stringOff
//         Embeddeds []struct {
//             Pos  Pos
//             Type typeOff
//         }
//         Methods []struct {
//             Pos       Pos
//             Name      stringOff
//             Signature Signature
//         }
//     }
//
//
//     type Signature struct {
//         Params   []Param
//         Results  []Param
//         Variadic bool  // omitted if Results is empty
//     }
//
//     type Param struct {
//         Pos  Pos
//         Name stringOff
//         Type typOff
//     }
//
//
// Pos encodes a file:line:column triple, incorporating a simple delta
// encoding scheme within a data object. See exportWriter.pos for
// details.
//
//
// Compiler-specific details.
//
// cmd/compile writes out a second index for inline bodies and also
// appends additional compiler-specific details after declarations.
// Third-party tools are not expected to depend on these details and
// they're expected to change much more rapidly, so they're omitted
// here. See exportWriter's varExt/funcExt/etc methods for details.

package gc

import (
	"bufio"
	"bytes"
	"cmd/compile/internal/base"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/types"
	"cmd/internal/goobj"
	"cmd/internal/src"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"go/constant"
	"io"
	"math/big"
	"sort"
	"strings"
)

// Current indexed export format version. Increase with each format change.
// 1: added column details to Pos
// 0: Go1.11 encoding
const iexportVersion = 1

// predeclReserved is the number of type offsets reserved for types
// implicitly declared in the universe block.
const predeclReserved = 32

// An itag distinguishes the kind of type that was written into the
// indexed export format.
type itag uint64

const (
	// Types
	definedType itag = iota
	pointerType
	sliceType
	arrayType
	chanType
	mapType
	signatureType
	structType
	interfaceType
)

func iexport(out *bufio.Writer) {
	// Mark inline bodies that are reachable through exported objects.
	// (Phase 0 of bexport.go.)
	{
		// TODO(mdempsky): Separate from bexport logic.
		p := &exporter{marked: make(map[*types.Type]bool)}
		for _, n := range exportlist {
			p.markObject(n)
		}
	}

	p := iexporter{
		allPkgs:     map[*types.Pkg]bool{},
		stringIndex: map[string]uint64{},
		declIndex:   map[ir.Node]uint64{},
		inlineIndex: map[ir.Node]uint64{},
		typIndex:    map[*types.Type]uint64{},
	}

	for i, pt := range predeclared() {
		p.typIndex[pt] = uint64(i)
	}
	if len(p.typIndex) > predeclReserved {
		base.Fatalf("too many predeclared types: %d > %d", len(p.typIndex), predeclReserved)
	}

	// Initialize work queue with exported declarations.
	for _, n := range exportlist {
		p.pushDecl(n)
	}

	// Loop until no more work. We use a queue because while
	// writing out inline bodies, we may discover additional
	// declarations that are needed.
	for !p.declTodo.Empty() {
		p.doDecl(p.declTodo.PopLeft())
	}

	// Append indices to data0 section.
	dataLen := uint64(p.data0.Len())
	w := p.newWriter()
	w.writeIndex(p.declIndex, true)
	w.writeIndex(p.inlineIndex, false)
	w.flush()

	// Assemble header.
	var hdr intWriter
	hdr.WriteByte('i')
	hdr.uint64(iexportVersion)
	hdr.uint64(uint64(p.strings.Len()))
	hdr.uint64(dataLen)

	// Flush output.
	h := md5.New()
	wr := io.MultiWriter(out, h)
	io.Copy(wr, &hdr)
	io.Copy(wr, &p.strings)
	io.Copy(wr, &p.data0)

	// Add fingerprint (used by linker object file).
	// Attach this to the end, so tools (e.g. gcimporter) don't care.
	copy(base.Ctxt.Fingerprint[:], h.Sum(nil)[:])
	out.Write(base.Ctxt.Fingerprint[:])
}

// writeIndex writes out an object index. mainIndex indicates whether
// we're writing out the main index, which is also read by
// non-compiler tools and includes a complete package description
// (i.e., name and height).
func (w *exportWriter) writeIndex(index map[ir.Node]uint64, mainIndex bool) {
	// Build a map from packages to objects from that package.
	pkgObjs := map[*types.Pkg][]ir.Node{}

	// For the main index, make sure to include every package that
	// we reference, even if we're not exporting (or reexporting)
	// any symbols from it.
	if mainIndex {
		pkgObjs[ir.LocalPkg] = nil
		for pkg := range w.p.allPkgs {
			pkgObjs[pkg] = nil
		}
	}

	for n := range index {
		pkgObjs[n.Sym().Pkg] = append(pkgObjs[n.Sym().Pkg], n)
	}

	var pkgs []*types.Pkg
	for pkg, objs := range pkgObjs {
		pkgs = append(pkgs, pkg)

		sort.Slice(objs, func(i, j int) bool {
			return objs[i].Sym().Name < objs[j].Sym().Name
		})
	}

	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i].Path < pkgs[j].Path
	})

	w.uint64(uint64(len(pkgs)))
	for _, pkg := range pkgs {
		w.string(pkg.Path)
		if mainIndex {
			w.string(pkg.Name)
			w.uint64(uint64(pkg.Height))
		}

		objs := pkgObjs[pkg]
		w.uint64(uint64(len(objs)))
		for _, n := range objs {
			w.string(n.Sym().Name)
			w.uint64(index[n])
		}
	}
}

type iexporter struct {
	// allPkgs tracks all packages that have been referenced by
	// the export data, so we can ensure to include them in the
	// main index.
	allPkgs map[*types.Pkg]bool

	declTodo ir.NodeQueue

	strings     intWriter
	stringIndex map[string]uint64

	data0       intWriter
	declIndex   map[ir.Node]uint64
	inlineIndex map[ir.Node]uint64
	typIndex    map[*types.Type]uint64
}

// stringOff returns the offset of s within the string section.
// If not already present, it's added to the end.
func (p *iexporter) stringOff(s string) uint64 {
	off, ok := p.stringIndex[s]
	if !ok {
		off = uint64(p.strings.Len())
		p.stringIndex[s] = off

		p.strings.uint64(uint64(len(s)))
		p.strings.WriteString(s)
	}
	return off
}

// pushDecl adds n to the declaration work queue, if not already present.
func (p *iexporter) pushDecl(n ir.Node) {
	if n.Sym() == nil || ir.AsNode(n.Sym().Def) != n && n.Op() != ir.OTYPE {
		base.Fatalf("weird Sym: %v, %v", n, n.Sym())
	}

	// Don't export predeclared declarations.
	if n.Sym().Pkg == ir.BuiltinPkg || n.Sym().Pkg == unsafepkg {
		return
	}

	if _, ok := p.declIndex[n]; ok {
		return
	}

	p.declIndex[n] = ^uint64(0) // mark n present in work queue
	p.declTodo.PushRight(n)
}

// exportWriter handles writing out individual data section chunks.
type exportWriter struct {
	p *iexporter

	data       intWriter
	currPkg    *types.Pkg
	prevFile   string
	prevLine   int64
	prevColumn int64
}

func (p *iexporter) doDecl(n ir.Node) {
	w := p.newWriter()
	w.setPkg(n.Sym().Pkg, false)

	switch n.Op() {
	case ir.ONAME:
		switch n.Class() {
		case ir.PEXTERN:
			// Variable.
			w.tag('V')
			w.pos(n.Pos())
			w.typ(n.Type())
			w.varExt(n)

		case ir.PFUNC:
			if ir.IsMethod(n) {
				base.Fatalf("unexpected method: %v", n)
			}

			// Function.
			w.tag('F')
			w.pos(n.Pos())
			w.signature(n.Type())
			w.funcExt(n)

		default:
			base.Fatalf("unexpected class: %v, %v", n, n.Class())
		}

	case ir.OLITERAL:
		// Constant.
		n = typecheck(n, ctxExpr)
		w.tag('C')
		w.pos(n.Pos())
		w.value(n.Type(), n.Val())

	case ir.OTYPE:
		if IsAlias(n.Sym()) {
			// Alias.
			w.tag('A')
			w.pos(n.Pos())
			w.typ(n.Type())
			break
		}

		// Defined type.
		w.tag('T')
		w.pos(n.Pos())

		underlying := n.Type().Orig
		if underlying == types.Errortype.Orig {
			// For "type T error", use error as the
			// underlying type instead of error's own
			// underlying anonymous interface. This
			// ensures consistency with how importers may
			// declare error (e.g., go/types uses nil Pkg
			// for predeclared objects).
			underlying = types.Errortype
		}
		w.typ(underlying)

		t := n.Type()
		if t.IsInterface() {
			w.typeExt(t)
			break
		}

		ms := t.Methods()
		w.uint64(uint64(ms.Len()))
		for _, m := range ms.Slice() {
			w.pos(m.Pos)
			w.selector(m.Sym)
			w.param(m.Type.Recv())
			w.signature(m.Type)
		}

		w.typeExt(t)
		for _, m := range ms.Slice() {
			w.methExt(m)
		}

	default:
		base.Fatalf("unexpected node: %v", n)
	}

	p.declIndex[n] = w.flush()
}

func (w *exportWriter) tag(tag byte) {
	w.data.WriteByte(tag)
}

func (p *iexporter) doInline(f ir.Node) {
	w := p.newWriter()
	w.setPkg(fnpkg(f), false)

	w.stmtList(ir.AsNodes(f.Func().Inl.Body))

	p.inlineIndex[f] = w.flush()
}

func (w *exportWriter) pos(pos src.XPos) {
	p := base.Ctxt.PosTable.Pos(pos)
	file := p.Base().AbsFilename()
	line := int64(p.RelLine())
	column := int64(p.RelCol())

	// Encode position relative to the last position: column
	// delta, then line delta, then file name. We reserve the
	// bottom bit of the column and line deltas to encode whether
	// the remaining fields are present.
	//
	// Note: Because data objects may be read out of order (or not
	// at all), we can only apply delta encoding within a single
	// object. This is handled implicitly by tracking prevFile,
	// prevLine, and prevColumn as fields of exportWriter.

	deltaColumn := (column - w.prevColumn) << 1
	deltaLine := (line - w.prevLine) << 1

	if file != w.prevFile {
		deltaLine |= 1
	}
	if deltaLine != 0 {
		deltaColumn |= 1
	}

	w.int64(deltaColumn)
	if deltaColumn&1 != 0 {
		w.int64(deltaLine)
		if deltaLine&1 != 0 {
			w.string(file)
		}
	}

	w.prevFile = file
	w.prevLine = line
	w.prevColumn = column
}

func (w *exportWriter) pkg(pkg *types.Pkg) {
	// Ensure any referenced packages are declared in the main index.
	w.p.allPkgs[pkg] = true

	w.string(pkg.Path)
}

func (w *exportWriter) qualifiedIdent(n ir.Node) {
	// Ensure any referenced declarations are written out too.
	w.p.pushDecl(n)

	s := n.Sym()
	w.string(s.Name)
	w.pkg(s.Pkg)
}

func (w *exportWriter) selector(s *types.Sym) {
	if w.currPkg == nil {
		base.Fatalf("missing currPkg")
	}

	// Method selectors are rewritten into method symbols (of the
	// form T.M) during typechecking, but we want to write out
	// just the bare method name.
	name := s.Name
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	} else {
		pkg := w.currPkg
		if types.IsExported(name) {
			pkg = ir.LocalPkg
		}
		if s.Pkg != pkg {
			base.Fatalf("package mismatch in selector: %v in package %q, but want %q", s, s.Pkg.Path, pkg.Path)
		}
	}

	w.string(name)
}

func (w *exportWriter) typ(t *types.Type) {
	w.data.uint64(w.p.typOff(t))
}

func (p *iexporter) newWriter() *exportWriter {
	return &exportWriter{p: p}
}

func (w *exportWriter) flush() uint64 {
	off := uint64(w.p.data0.Len())
	io.Copy(&w.p.data0, &w.data)
	return off
}

func (p *iexporter) typOff(t *types.Type) uint64 {
	off, ok := p.typIndex[t]
	if !ok {
		w := p.newWriter()
		w.doTyp(t)
		off = predeclReserved + w.flush()
		p.typIndex[t] = off
	}
	return off
}

func (w *exportWriter) startType(k itag) {
	w.data.uint64(uint64(k))
}

func (w *exportWriter) doTyp(t *types.Type) {
	if t.Sym != nil {
		if t.Sym.Pkg == ir.BuiltinPkg || t.Sym.Pkg == unsafepkg {
			base.Fatalf("builtin type missing from typIndex: %v", t)
		}

		w.startType(definedType)
		w.qualifiedIdent(typenod(t))
		return
	}

	switch t.Etype {
	case types.TPTR:
		w.startType(pointerType)
		w.typ(t.Elem())

	case types.TSLICE:
		w.startType(sliceType)
		w.typ(t.Elem())

	case types.TARRAY:
		w.startType(arrayType)
		w.uint64(uint64(t.NumElem()))
		w.typ(t.Elem())

	case types.TCHAN:
		w.startType(chanType)
		w.uint64(uint64(t.ChanDir()))
		w.typ(t.Elem())

	case types.TMAP:
		w.startType(mapType)
		w.typ(t.Key())
		w.typ(t.Elem())

	case types.TFUNC:
		w.startType(signatureType)
		w.setPkg(t.Pkg(), true)
		w.signature(t)

	case types.TSTRUCT:
		w.startType(structType)
		w.setPkg(t.Pkg(), true)

		w.uint64(uint64(t.NumFields()))
		for _, f := range t.FieldSlice() {
			w.pos(f.Pos)
			w.selector(f.Sym)
			w.typ(f.Type)
			w.bool(f.Embedded != 0)
			w.string(f.Note)
		}

	case types.TINTER:
		var embeddeds, methods []*types.Field
		for _, m := range t.Methods().Slice() {
			if m.Sym != nil {
				methods = append(methods, m)
			} else {
				embeddeds = append(embeddeds, m)
			}
		}

		w.startType(interfaceType)
		w.setPkg(t.Pkg(), true)

		w.uint64(uint64(len(embeddeds)))
		for _, f := range embeddeds {
			w.pos(f.Pos)
			w.typ(f.Type)
		}

		w.uint64(uint64(len(methods)))
		for _, f := range methods {
			w.pos(f.Pos)
			w.selector(f.Sym)
			w.signature(f.Type)
		}

	default:
		base.Fatalf("unexpected type: %v", t)
	}
}

func (w *exportWriter) setPkg(pkg *types.Pkg, write bool) {
	if pkg == nil {
		// TODO(mdempsky): Proactively set Pkg for types and
		// remove this fallback logic.
		pkg = ir.LocalPkg
	}

	if write {
		w.pkg(pkg)
	}

	w.currPkg = pkg
}

func (w *exportWriter) signature(t *types.Type) {
	w.paramList(t.Params().FieldSlice())
	w.paramList(t.Results().FieldSlice())
	if n := t.Params().NumFields(); n > 0 {
		w.bool(t.Params().Field(n - 1).IsDDD())
	}
}

func (w *exportWriter) paramList(fs []*types.Field) {
	w.uint64(uint64(len(fs)))
	for _, f := range fs {
		w.param(f)
	}
}

func (w *exportWriter) param(f *types.Field) {
	w.pos(f.Pos)
	w.localIdent(ir.OrigSym(f.Sym), 0)
	w.typ(f.Type)
}

func constTypeOf(typ *types.Type) constant.Kind {
	switch typ {
	case types.UntypedInt, types.UntypedRune:
		return constant.Int
	case types.UntypedFloat:
		return constant.Float
	case types.UntypedComplex:
		return constant.Complex
	}

	switch typ.Etype {
	case types.TBOOL:
		return constant.Bool
	case types.TSTRING:
		return constant.String
	case types.TINT, types.TINT8, types.TINT16, types.TINT32, types.TINT64,
		types.TUINT, types.TUINT8, types.TUINT16, types.TUINT32, types.TUINT64, types.TUINTPTR:
		return constant.Int
	case types.TFLOAT32, types.TFLOAT64:
		return constant.Float
	case types.TCOMPLEX64, types.TCOMPLEX128:
		return constant.Complex
	}

	base.Fatalf("unexpected constant type: %v", typ)
	return 0
}

func (w *exportWriter) value(typ *types.Type, v constant.Value) {
	ir.AssertValidTypeForConst(typ, v)
	w.typ(typ)

	// Each type has only one admissible constant representation,
	// so we could type switch directly on v.U here. However,
	// switching on the type increases symmetry with import logic
	// and provides a useful consistency check.

	switch constTypeOf(typ) {
	case constant.Bool:
		w.bool(constant.BoolVal(v))
	case constant.String:
		w.string(constant.StringVal(v))
	case constant.Int:
		w.mpint(v, typ)
	case constant.Float:
		w.mpfloat(v, typ)
	case constant.Complex:
		w.mpfloat(constant.Real(v), typ)
		w.mpfloat(constant.Imag(v), typ)
	}
}

func intSize(typ *types.Type) (signed bool, maxBytes uint) {
	if typ.IsUntyped() {
		return true, Mpprec / 8
	}

	switch typ.Etype {
	case types.TFLOAT32, types.TCOMPLEX64:
		return true, 3
	case types.TFLOAT64, types.TCOMPLEX128:
		return true, 7
	}

	signed = typ.IsSigned()
	maxBytes = uint(typ.Size())

	// The go/types API doesn't expose sizes to importers, so they
	// don't know how big these types are.
	switch typ.Etype {
	case types.TINT, types.TUINT, types.TUINTPTR:
		maxBytes = 8
	}

	return
}

// mpint exports a multi-precision integer.
//
// For unsigned types, small values are written out as a single
// byte. Larger values are written out as a length-prefixed big-endian
// byte string, where the length prefix is encoded as its complement.
// For example, bytes 0, 1, and 2 directly represent the integer
// values 0, 1, and 2; while bytes 255, 254, and 253 indicate a 1-,
// 2-, and 3-byte big-endian string follow.
//
// Encoding for signed types use the same general approach as for
// unsigned types, except small values use zig-zag encoding and the
// bottom bit of length prefix byte for large values is reserved as a
// sign bit.
//
// The exact boundary between small and large encodings varies
// according to the maximum number of bytes needed to encode a value
// of type typ. As a special case, 8-bit types are always encoded as a
// single byte.
//
// TODO(mdempsky): Is this level of complexity really worthwhile?
func (w *exportWriter) mpint(x constant.Value, typ *types.Type) {
	signed, maxBytes := intSize(typ)

	negative := constant.Sign(x) < 0
	if !signed && negative {
		base.Fatalf("negative unsigned integer; type %v, value %v", typ, x)
	}

	b := constant.Bytes(x) // little endian
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	if len(b) > 0 && b[0] == 0 {
		base.Fatalf("leading zeros")
	}
	if uint(len(b)) > maxBytes {
		base.Fatalf("bad mpint length: %d > %d (type %v, value %v)", len(b), maxBytes, typ, x)
	}

	maxSmall := 256 - maxBytes
	if signed {
		maxSmall = 256 - 2*maxBytes
	}
	if maxBytes == 1 {
		maxSmall = 256
	}

	// Check if x can use small value encoding.
	if len(b) <= 1 {
		var ux uint
		if len(b) == 1 {
			ux = uint(b[0])
		}
		if signed {
			ux <<= 1
			if negative {
				ux--
			}
		}
		if ux < maxSmall {
			w.data.WriteByte(byte(ux))
			return
		}
	}

	n := 256 - uint(len(b))
	if signed {
		n = 256 - 2*uint(len(b))
		if negative {
			n |= 1
		}
	}
	if n < maxSmall || n >= 256 {
		base.Fatalf("encoding mistake: %d, %v, %v => %d", len(b), signed, negative, n)
	}

	w.data.WriteByte(byte(n))
	w.data.Write(b)
}

// mpfloat exports a multi-precision floating point number.
//
// The number's value is decomposed into mantissa × 2**exponent, where
// mantissa is an integer. The value is written out as mantissa (as a
// multi-precision integer) and then the exponent, except exponent is
// omitted if mantissa is zero.
func (w *exportWriter) mpfloat(v constant.Value, typ *types.Type) {
	f := bigFloatVal(v)
	if f.IsInf() {
		base.Fatalf("infinite constant")
	}

	// Break into f = mant × 2**exp, with 0.5 <= mant < 1.
	var mant big.Float
	exp := int64(f.MantExp(&mant))

	// Scale so that mant is an integer.
	prec := mant.MinPrec()
	mant.SetMantExp(&mant, int(prec))
	exp -= int64(prec)

	manti, acc := mant.Int(nil)
	if acc != big.Exact {
		base.Fatalf("mantissa scaling failed for %f (%s)", f, acc)
	}
	w.mpint(makeInt(manti), typ)
	if manti.Sign() != 0 {
		w.int64(exp)
	}
}

func (w *exportWriter) bool(b bool) bool {
	var x uint64
	if b {
		x = 1
	}
	w.uint64(x)
	return b
}

func (w *exportWriter) int64(x int64)   { w.data.int64(x) }
func (w *exportWriter) uint64(x uint64) { w.data.uint64(x) }
func (w *exportWriter) string(s string) { w.uint64(w.p.stringOff(s)) }

// Compiler-specific extensions.

func (w *exportWriter) varExt(n ir.Node) {
	w.linkname(n.Sym())
	w.symIdx(n.Sym())
}

func (w *exportWriter) funcExt(n ir.Node) {
	w.linkname(n.Sym())
	w.symIdx(n.Sym())

	// Escape analysis.
	for _, fs := range &types.RecvsParams {
		for _, f := range fs(n.Type()).FieldSlice() {
			w.string(f.Note)
		}
	}

	// Inline body.
	if n.Func().Inl != nil {
		w.uint64(1 + uint64(n.Func().Inl.Cost))
		if n.Func().ExportInline() {
			w.p.doInline(n)
		}

		// Endlineno for inlined function.
		if n.Name().Defn != nil {
			w.pos(n.Name().Defn.Func().Endlineno)
		} else {
			// When the exported node was defined externally,
			// e.g. io exports atomic.(*Value).Load or bytes exports errors.New.
			// Keep it as we don't distinguish this case in iimport.go.
			w.pos(n.Func().Endlineno)
		}
	} else {
		w.uint64(0)
	}
}

func (w *exportWriter) methExt(m *types.Field) {
	w.bool(m.Nointerface())
	w.funcExt(ir.AsNode(m.Nname))
}

func (w *exportWriter) linkname(s *types.Sym) {
	w.string(s.Linkname)
}

func (w *exportWriter) symIdx(s *types.Sym) {
	lsym := s.Linksym()
	if lsym.PkgIdx > goobj.PkgIdxSelf || (lsym.PkgIdx == goobj.PkgIdxInvalid && !lsym.Indexed()) || s.Linkname != "" {
		// Don't export index for non-package symbols, linkname'd symbols,
		// and symbols without an index. They can only be referenced by
		// name.
		w.int64(-1)
	} else {
		// For a defined symbol, export its index.
		// For re-exporting an imported symbol, pass its index through.
		w.int64(int64(lsym.SymIdx))
	}
}

func (w *exportWriter) typeExt(t *types.Type) {
	// Export whether this type is marked notinheap.
	w.bool(t.NotInHeap())
	// For type T, export the index of type descriptor symbols of T and *T.
	if i, ok := typeSymIdx[t]; ok {
		w.int64(i[0])
		w.int64(i[1])
		return
	}
	w.symIdx(typesym(t))
	w.symIdx(typesym(t.PtrTo()))
}

// Inline bodies.

func (w *exportWriter) stmtList(list ir.Nodes) {
	for _, n := range list.Slice() {
		w.node(n)
	}
	w.op(ir.OEND)
}

func (w *exportWriter) node(n ir.Node) {
	if ir.OpPrec[n.Op()] < 0 {
		w.stmt(n)
	} else {
		w.expr(n)
	}
}

// Caution: stmt will emit more than one node for statement nodes n that have a non-empty
// n.Ninit and where n cannot have a natural init section (such as in "if", "for", etc.).
func (w *exportWriter) stmt(n ir.Node) {
	if n.Init().Len() > 0 && !ir.StmtWithInit(n.Op()) {
		// can't use stmtList here since we don't want the final OEND
		for _, n := range n.Init().Slice() {
			w.stmt(n)
		}
	}

	switch op := n.Op(); op {
	case ir.ODCL:
		w.op(ir.ODCL)
		w.pos(n.Left().Pos())
		w.localName(n.Left())
		w.typ(n.Left().Type())

	// case ODCLFIELD:
	//	unimplemented - handled by default case

	case ir.OAS:
		// Don't export "v = <N>" initializing statements, hope they're always
		// preceded by the DCL which will be re-parsed and typecheck to reproduce
		// the "v = <N>" again.
		if n.Right() != nil {
			w.op(ir.OAS)
			w.pos(n.Pos())
			w.expr(n.Left())
			w.expr(n.Right())
		}

	case ir.OASOP:
		w.op(ir.OASOP)
		w.pos(n.Pos())
		w.op(n.SubOp())
		w.expr(n.Left())
		if w.bool(!n.Implicit()) {
			w.expr(n.Right())
		}

	case ir.OAS2:
		w.op(ir.OAS2)
		w.pos(n.Pos())
		w.exprList(n.List())
		w.exprList(n.Rlist())

	case ir.OAS2DOTTYPE, ir.OAS2FUNC, ir.OAS2MAPR, ir.OAS2RECV:
		w.op(ir.OAS2)
		w.pos(n.Pos())
		w.exprList(n.List())
		w.exprList(ir.AsNodes([]ir.Node{n.Right()}))

	case ir.ORETURN:
		w.op(ir.ORETURN)
		w.pos(n.Pos())
		w.exprList(n.List())

	// case ORETJMP:
	// 	unreachable - generated by compiler for trampolin routines

	case ir.OGO, ir.ODEFER:
		w.op(op)
		w.pos(n.Pos())
		w.expr(n.Left())

	case ir.OIF:
		w.op(ir.OIF)
		w.pos(n.Pos())
		w.stmtList(n.Init())
		w.expr(n.Left())
		w.stmtList(n.Body())
		w.stmtList(n.Rlist())

	case ir.OFOR:
		w.op(ir.OFOR)
		w.pos(n.Pos())
		w.stmtList(n.Init())
		w.exprsOrNil(n.Left(), n.Right())
		w.stmtList(n.Body())

	case ir.ORANGE:
		w.op(ir.ORANGE)
		w.pos(n.Pos())
		w.stmtList(n.List())
		w.expr(n.Right())
		w.stmtList(n.Body())

	case ir.OSELECT, ir.OSWITCH:
		w.op(op)
		w.pos(n.Pos())
		w.stmtList(n.Init())
		w.exprsOrNil(n.Left(), nil)
		w.caseList(n)

	// case OCASE:
	//	handled by caseList

	case ir.OFALL:
		w.op(ir.OFALL)
		w.pos(n.Pos())

	case ir.OBREAK, ir.OCONTINUE:
		w.op(op)
		w.pos(n.Pos())
		w.exprsOrNil(n.Left(), nil)

	case ir.OEMPTY:
		// nothing to emit

	case ir.OGOTO, ir.OLABEL:
		w.op(op)
		w.pos(n.Pos())
		w.string(n.Sym().Name)

	default:
		base.Fatalf("exporter: CANNOT EXPORT: %v\nPlease notify gri@\n", n.Op())
	}
}

func (w *exportWriter) caseList(sw ir.Node) {
	namedTypeSwitch := sw.Op() == ir.OSWITCH && sw.Left() != nil && sw.Left().Op() == ir.OTYPESW && sw.Left().Left() != nil

	cases := sw.List().Slice()
	w.uint64(uint64(len(cases)))
	for _, cas := range cases {
		if cas.Op() != ir.OCASE {
			base.Fatalf("expected OCASE, got %v", cas)
		}
		w.pos(cas.Pos())
		w.stmtList(cas.List())
		if namedTypeSwitch {
			w.localName(cas.Rlist().First())
		}
		w.stmtList(cas.Body())
	}
}

func (w *exportWriter) exprList(list ir.Nodes) {
	for _, n := range list.Slice() {
		w.expr(n)
	}
	w.op(ir.OEND)
}

func (w *exportWriter) expr(n ir.Node) {
	// from nodefmt (fmt.go)
	//
	// nodefmt reverts nodes back to their original - we don't need to do
	// it because we are not bound to produce valid Go syntax when exporting
	//
	// if (fmtmode != FExp || n.Op != OLITERAL) && n.Orig != nil {
	// 	n = n.Orig
	// }

	// from exprfmt (fmt.go)
	for n.Op() == ir.OPAREN || n.Implicit() && (n.Op() == ir.ODEREF || n.Op() == ir.OADDR || n.Op() == ir.ODOT || n.Op() == ir.ODOTPTR) {
		n = n.Left()
	}

	switch op := n.Op(); op {
	// expressions
	// (somewhat closely following the structure of exprfmt in fmt.go)
	case ir.ONIL:
		if !n.Type().HasNil() {
			base.Fatalf("unexpected type for nil: %v", n.Type())
		}
		if n.Orig() != nil && n.Orig() != n {
			w.expr(n.Orig())
			break
		}
		w.op(ir.OLITERAL)
		w.pos(n.Pos())
		w.typ(n.Type())

	case ir.OLITERAL:
		w.op(ir.OLITERAL)
		w.pos(n.Pos())
		w.value(n.Type(), n.Val())

	case ir.OMETHEXPR:
		// Special case: explicit name of func (*T) method(...) is turned into pkg.(*T).method,
		// but for export, this should be rendered as (*pkg.T).meth.
		// These nodes have the special property that they are names with a left OTYPE and a right ONAME.
		w.op(ir.OXDOT)
		w.pos(n.Pos())
		w.expr(n.Left()) // n.Left.Op == OTYPE
		w.selector(n.Right().Sym())

	case ir.ONAME:
		// Package scope name.
		if (n.Class() == ir.PEXTERN || n.Class() == ir.PFUNC) && !ir.IsBlank(n) {
			w.op(ir.ONONAME)
			w.qualifiedIdent(n)
			break
		}

		// Function scope name.
		w.op(ir.ONAME)
		w.localName(n)

	// case OPACK, ONONAME:
	// 	should have been resolved by typechecking - handled by default case

	case ir.OTYPE:
		w.op(ir.OTYPE)
		w.typ(n.Type())

	case ir.OTYPESW:
		w.op(ir.OTYPESW)
		w.pos(n.Pos())
		var s *types.Sym
		if n.Left() != nil {
			if n.Left().Op() != ir.ONONAME {
				base.Fatalf("expected ONONAME, got %v", n.Left())
			}
			s = n.Left().Sym()
		}
		w.localIdent(s, 0) // declared pseudo-variable, if any
		w.exprsOrNil(n.Right(), nil)

	// case OTARRAY, OTMAP, OTCHAN, OTSTRUCT, OTINTER, OTFUNC:
	// 	should have been resolved by typechecking - handled by default case

	// case OCLOSURE:
	//	unimplemented - handled by default case

	// case OCOMPLIT:
	// 	should have been resolved by typechecking - handled by default case

	case ir.OPTRLIT:
		w.op(ir.OADDR)
		w.pos(n.Pos())
		w.expr(n.Left())

	case ir.OSTRUCTLIT:
		w.op(ir.OSTRUCTLIT)
		w.pos(n.Pos())
		w.typ(n.Type())
		w.elemList(n.List()) // special handling of field names

	case ir.OARRAYLIT, ir.OSLICELIT, ir.OMAPLIT:
		w.op(ir.OCOMPLIT)
		w.pos(n.Pos())
		w.typ(n.Type())
		w.exprList(n.List())

	case ir.OKEY:
		w.op(ir.OKEY)
		w.pos(n.Pos())
		w.exprsOrNil(n.Left(), n.Right())

	// case OSTRUCTKEY:
	//	unreachable - handled in case OSTRUCTLIT by elemList

	case ir.OCALLPART:
		// An OCALLPART is an OXDOT before type checking.
		w.op(ir.OXDOT)
		w.pos(n.Pos())
		w.expr(n.Left())
		// Right node should be ONAME
		w.selector(n.Right().Sym())

	case ir.OXDOT, ir.ODOT, ir.ODOTPTR, ir.ODOTINTER, ir.ODOTMETH:
		w.op(ir.OXDOT)
		w.pos(n.Pos())
		w.expr(n.Left())
		w.selector(n.Sym())

	case ir.ODOTTYPE, ir.ODOTTYPE2:
		w.op(ir.ODOTTYPE)
		w.pos(n.Pos())
		w.expr(n.Left())
		w.typ(n.Type())

	case ir.OINDEX, ir.OINDEXMAP:
		w.op(ir.OINDEX)
		w.pos(n.Pos())
		w.expr(n.Left())
		w.expr(n.Right())

	case ir.OSLICE, ir.OSLICESTR, ir.OSLICEARR:
		w.op(ir.OSLICE)
		w.pos(n.Pos())
		w.expr(n.Left())
		low, high, _ := n.SliceBounds()
		w.exprsOrNil(low, high)

	case ir.OSLICE3, ir.OSLICE3ARR:
		w.op(ir.OSLICE3)
		w.pos(n.Pos())
		w.expr(n.Left())
		low, high, max := n.SliceBounds()
		w.exprsOrNil(low, high)
		w.expr(max)

	case ir.OCOPY, ir.OCOMPLEX:
		// treated like other builtin calls (see e.g., OREAL)
		w.op(op)
		w.pos(n.Pos())
		w.expr(n.Left())
		w.expr(n.Right())
		w.op(ir.OEND)

	case ir.OCONV, ir.OCONVIFACE, ir.OCONVNOP, ir.OBYTES2STR, ir.ORUNES2STR, ir.OSTR2BYTES, ir.OSTR2RUNES, ir.ORUNESTR:
		w.op(ir.OCONV)
		w.pos(n.Pos())
		w.expr(n.Left())
		w.typ(n.Type())

	case ir.OREAL, ir.OIMAG, ir.OAPPEND, ir.OCAP, ir.OCLOSE, ir.ODELETE, ir.OLEN, ir.OMAKE, ir.ONEW, ir.OPANIC, ir.ORECOVER, ir.OPRINT, ir.OPRINTN:
		w.op(op)
		w.pos(n.Pos())
		if n.Left() != nil {
			w.expr(n.Left())
			w.op(ir.OEND)
		} else {
			w.exprList(n.List()) // emits terminating OEND
		}
		// only append() calls may contain '...' arguments
		if op == ir.OAPPEND {
			w.bool(n.IsDDD())
		} else if n.IsDDD() {
			base.Fatalf("exporter: unexpected '...' with %v call", op)
		}

	case ir.OCALL, ir.OCALLFUNC, ir.OCALLMETH, ir.OCALLINTER, ir.OGETG:
		w.op(ir.OCALL)
		w.pos(n.Pos())
		w.stmtList(n.Init())
		w.expr(n.Left())
		w.exprList(n.List())
		w.bool(n.IsDDD())

	case ir.OMAKEMAP, ir.OMAKECHAN, ir.OMAKESLICE:
		w.op(op) // must keep separate from OMAKE for importer
		w.pos(n.Pos())
		w.typ(n.Type())
		switch {
		default:
			// empty list
			w.op(ir.OEND)
		case n.List().Len() != 0: // pre-typecheck
			w.exprList(n.List()) // emits terminating OEND
		case n.Right() != nil:
			w.expr(n.Left())
			w.expr(n.Right())
			w.op(ir.OEND)
		case n.Left() != nil && (n.Op() == ir.OMAKESLICE || !n.Left().Type().IsUntyped()):
			w.expr(n.Left())
			w.op(ir.OEND)
		}

	// unary expressions
	case ir.OPLUS, ir.ONEG, ir.OADDR, ir.OBITNOT, ir.ODEREF, ir.ONOT, ir.ORECV:
		w.op(op)
		w.pos(n.Pos())
		w.expr(n.Left())

	// binary expressions
	case ir.OADD, ir.OAND, ir.OANDAND, ir.OANDNOT, ir.ODIV, ir.OEQ, ir.OGE, ir.OGT, ir.OLE, ir.OLT,
		ir.OLSH, ir.OMOD, ir.OMUL, ir.ONE, ir.OOR, ir.OOROR, ir.ORSH, ir.OSEND, ir.OSUB, ir.OXOR:
		w.op(op)
		w.pos(n.Pos())
		w.expr(n.Left())
		w.expr(n.Right())

	case ir.OADDSTR:
		w.op(ir.OADDSTR)
		w.pos(n.Pos())
		w.exprList(n.List())

	case ir.ODCLCONST:
		// if exporting, DCLCONST should just be removed as its usage
		// has already been replaced with literals

	default:
		base.Fatalf("cannot export %v (%d) node\n"+
			"\t==> please file an issue and assign to gri@", n.Op(), int(n.Op()))
	}
}

func (w *exportWriter) op(op ir.Op) {
	w.uint64(uint64(op))
}

func (w *exportWriter) exprsOrNil(a, b ir.Node) {
	ab := 0
	if a != nil {
		ab |= 1
	}
	if b != nil {
		ab |= 2
	}
	w.uint64(uint64(ab))
	if ab&1 != 0 {
		w.expr(a)
	}
	if ab&2 != 0 {
		w.node(b)
	}
}

func (w *exportWriter) elemList(list ir.Nodes) {
	w.uint64(uint64(list.Len()))
	for _, n := range list.Slice() {
		w.selector(n.Sym())
		w.expr(n.Left())
	}
}

func (w *exportWriter) localName(n ir.Node) {
	// Escape analysis happens after inline bodies are saved, but
	// we're using the same ONAME nodes, so we might still see
	// PAUTOHEAP here.
	//
	// Check for Stackcopy to identify PAUTOHEAP that came from
	// PPARAM/PPARAMOUT, because we only want to include vargen in
	// non-param names.
	var v int32
	if n.Class() == ir.PAUTO || (n.Class() == ir.PAUTOHEAP && n.Name().Param.Stackcopy == nil) {
		v = n.Name().Vargen
	}

	w.localIdent(n.Sym(), v)
}

func (w *exportWriter) localIdent(s *types.Sym, v int32) {
	// Anonymous parameters.
	if s == nil {
		w.string("")
		return
	}

	name := s.Name
	if name == "_" {
		w.string("_")
		return
	}

	// TODO(mdempsky): Fix autotmp hack.
	if i := strings.LastIndex(name, "."); i >= 0 && !strings.HasPrefix(name, ".autotmp_") {
		base.Fatalf("unexpected dot in identifier: %v", name)
	}

	if v > 0 {
		if strings.Contains(name, "·") {
			base.Fatalf("exporter: unexpected · in symbol name")
		}
		name = fmt.Sprintf("%s·%d", name, v)
	}

	if !types.IsExported(name) && s.Pkg != w.currPkg {
		base.Fatalf("weird package in name: %v => %v, not %q", s, name, w.currPkg.Path)
	}

	w.string(name)
}

type intWriter struct {
	bytes.Buffer
}

func (w *intWriter) int64(x int64) {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutVarint(buf[:], x)
	w.Write(buf[:n])
}

func (w *intWriter) uint64(x uint64) {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], x)
	w.Write(buf[:n])
}
