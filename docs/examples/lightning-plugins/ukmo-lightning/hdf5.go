// A small HDF5 reader, written for one file layout and nothing else.
//
// The Met Office lightning grids are NetCDF-4 files, and NetCDF-4 is HDF5. A
// general NetCDF library reads them, but the ones in pure Go lean on
// reflect.SliceOf, which TinyGo does not implement, so they panic when the
// plugin runs (ADR 0284). This reader uses no reflection and handles exactly
// what the Met Office files contain:
//
//   - superblock version 2 with 8-byte offsets and lengths;
//   - version 2 object headers, with the root group's links and a dataset's
//     attributes kept either in the header or in dense storage (a fractal
//     heap with a version 2 B-tree name index, single direct block);
//   - datasets in contiguous layout or in one chunk (layout version 3, a
//     version 1 B-tree chunk index), filtered by shuffle and deflate.
//
// Anything else is an error that names what it found, so a change at the Met
// Office surfaces as a failed fetch and never as wrong numbers. Checksums
// inside the HDF5 structures are not verified; the deflate stream carries its
// own, and the end-of-file address is checked against the length downloaded.
//
// This file has no dependency on the Extism PDK so it builds and tests under
// the plain Go toolchain as well as TinyGo.
package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const h5Undefined = ^uint64(0)

// Object header message types this reader looks at.
const (
	h5MsgDataspace    = 0x01
	h5MsgLinkInfo     = 0x02
	h5MsgDatatype     = 0x03
	h5MsgLayout       = 0x08
	h5MsgFilters      = 0x0B
	h5MsgAttribute    = 0x0C
	h5MsgContinuation = 0x10
	h5MsgAttrInfo     = 0x15
	h5MsgLink         = 0x06
)

// h5Cursor reads little-endian fields from a byte slice. The first failure is
// remembered and every later read returns zero, so a parser can read a whole
// structure and check err once.
type h5Cursor struct {
	b   []byte
	p   int
	err error
}

func (c *h5Cursor) take(n int) []byte {
	if c.err != nil {
		return nil
	}
	if n < 0 || c.p+n > len(c.b) {
		c.err = fmt.Errorf("hdf5: read of %d bytes at offset %d runs past the end of the data (%d bytes)", n, c.p, len(c.b))
		return nil
	}
	out := c.b[c.p : c.p+n]
	c.p += n
	return out
}

func (c *h5Cursor) skip(n int) { c.take(n) }

func (c *h5Cursor) uN(n int) uint64 {
	raw := c.take(n)
	if raw == nil {
		return 0
	}
	var v uint64
	for i := n - 1; i >= 0; i-- {
		v = v<<8 | uint64(raw[i])
	}
	return v
}

func (c *h5Cursor) u8() uint8   { return uint8(c.uN(1)) }
func (c *h5Cursor) u16() uint16 { return uint16(c.uN(2)) }
func (c *h5Cursor) u32() uint32 { return uint32(c.uN(4)) }
func (c *h5Cursor) u64() uint64 { return c.uN(8) }

// signature reads len(want) bytes and fails unless they match.
func (c *h5Cursor) signature(want string) {
	got := c.take(len(want))
	if c.err == nil && string(got) != want {
		c.err = fmt.Errorf("hdf5: expected %q signature at offset %d, found %q", want, c.p-len(want), got)
	}
}

// h5File is a whole HDF5 file held in memory.
type h5File struct {
	b []byte
}

// at returns a cursor positioned at the absolute file offset addr.
func (f *h5File) at(addr uint64) (*h5Cursor, error) {
	if addr == h5Undefined || addr >= uint64(len(f.b)) {
		return nil, fmt.Errorf("hdf5: address 0x%x is outside the file (%d bytes)", addr, len(f.b))
	}
	return &h5Cursor{b: f.b, p: int(addr)}, nil
}

// openH5 checks the superblock and returns the file with its root group's
// object header address.
func openH5(b []byte) (*h5File, uint64, error) {
	c := &h5Cursor{b: b}
	if len(b) < 8 || string(b[:8]) != "\x89HDF\r\n\x1a\n" {
		return nil, 0, fmt.Errorf("hdf5: not an HDF5 file (bad signature)")
	}
	c.skip(8)
	version := c.u8()
	if c.err == nil && version != 2 {
		return nil, 0, fmt.Errorf("hdf5: superblock version %d, only version 2 is supported", version)
	}
	offsetSize, lengthSize := c.u8(), c.u8()
	if c.err == nil && (offsetSize != 8 || lengthSize != 8) {
		return nil, 0, fmt.Errorf("hdf5: offset size %d and length size %d, only 8 and 8 are supported", offsetSize, lengthSize)
	}
	c.skip(1) // file consistency flags
	base := c.u64()
	c.skip(8) // superblock extension address
	eof := c.u64()
	root := c.u64()
	if c.err != nil {
		return nil, 0, c.err
	}
	if base != 0 {
		return nil, 0, fmt.Errorf("hdf5: base address %d, only 0 is supported", base)
	}
	if eof != uint64(len(b)) {
		return nil, 0, fmt.Errorf("hdf5: file says it is %d bytes but %d were read (truncated download?)", eof, len(b))
	}
	return &h5File{b: b}, root, nil
}

type h5Message struct {
	typ  uint8
	data []byte
}

// readObject returns every message of the version 2 object header at addr,
// following continuation blocks.
func (f *h5File) readObject(addr uint64) ([]h5Message, error) {
	c, err := f.at(addr)
	if err != nil {
		return nil, err
	}
	c.signature("OHDR")
	version := c.u8()
	flags := c.u8()
	if c.err == nil && version != 2 {
		return nil, fmt.Errorf("hdf5: object header at 0x%x is version %d, only version 2 is supported", addr, version)
	}
	if flags&0x20 != 0 {
		c.skip(16) // access, modification, change and birth times
	}
	if flags&0x10 != 0 {
		c.skip(4) // attribute phase-change thresholds
	}
	size := int(c.uN(1 << (flags & 3)))
	if c.err != nil {
		return nil, c.err
	}
	creationOrder := flags&0x04 != 0
	headerLen := 4 // message type, size, flags
	if creationOrder {
		headerLen = 6
	}

	var msgs []h5Message
	type block struct{ start, end int }
	pending := []block{{c.p, c.p + size}}
	for len(pending) > 0 {
		blk := pending[0]
		pending = pending[1:]
		if blk.end > len(f.b) || blk.start > blk.end {
			return nil, fmt.Errorf("hdf5: object header block 0x%x..0x%x is outside the file", blk.start, blk.end)
		}
		mc := &h5Cursor{b: f.b[:blk.end], p: blk.start}
		for mc.p+headerLen <= blk.end {
			typ := mc.u8()
			msize := int(mc.u16())
			mflags := mc.u8()
			if creationOrder {
				mc.skip(2)
			}
			data := mc.take(msize)
			if mc.err != nil {
				return nil, mc.err
			}
			if mflags&0x02 != 0 {
				return nil, fmt.Errorf("hdf5: shared object header message (type 0x%02x) is not supported", typ)
			}
			if typ == h5MsgContinuation {
				cc := &h5Cursor{b: data}
				off, length := cc.u64(), cc.u64()
				if cc.err != nil {
					return nil, cc.err
				}
				// The continuation block is "OCHK", messages, then a checksum.
				chunk, cerr := f.at(off)
				if cerr != nil {
					return nil, cerr
				}
				chunk.signature("OCHK")
				if chunk.err != nil {
					return nil, chunk.err
				}
				pending = append(pending, block{int(off) + 4, int(off) + int(length) - 4})
				continue
			}
			if typ != 0 {
				msgs = append(msgs, h5Message{typ: typ, data: data})
			}
		}
	}
	return msgs, nil
}

// --- dense storage: fractal heap and version 2 B-tree ---

// h5Block is one direct block of a fractal heap: the heap offsets it covers
// and where it sits in the file.
type h5Block struct {
	heapOffset, size, addr uint64
}

type h5Heap struct {
	f        *h5File
	idLen    int
	offBytes int
	blocks   []h5Block
}

func (f *h5File) openHeap(addr uint64) (*h5Heap, error) {
	c, err := f.at(addr)
	if err != nil {
		return nil, err
	}
	c.signature("FRHP")
	version := c.u8()
	idLen := int(c.u16())
	filterLen := c.u16()
	c.skip(1 + 4 + 8*12) // flags, max managed size, then twelve counters and addresses
	width := uint64(c.u16())
	startBlock := c.u64()
	maxDirect := c.u64()
	maxHeapBits := int(c.u16())
	c.skip(2) // starting rows in root indirect block
	root := c.u64()
	rows := int(c.u16())
	if c.err != nil {
		return nil, c.err
	}
	if version != 0 {
		return nil, fmt.Errorf("hdf5: fractal heap at 0x%x is version %d, only version 0 is supported", addr, version)
	}
	if filterLen != 0 {
		return nil, fmt.Errorf("hdf5: filtered fractal heap at 0x%x is not supported", addr)
	}
	if root == h5Undefined {
		return nil, fmt.Errorf("hdf5: fractal heap at 0x%x is empty", addr)
	}
	offBytes := (maxHeapBits + 7) / 8
	if idLen < 1+offBytes+1 {
		return nil, fmt.Errorf("hdf5: fractal heap at 0x%x has an implausible heap ID length %d", addr, idLen)
	}
	heap := &h5Heap{f: f, idLen: idLen, offBytes: offBytes}
	if rows == 0 {
		heap.blocks = []h5Block{{0, startBlock, root}}
		return heap, nil
	}

	// The root is an indirect block: rows of width direct blocks. The first two
	// rows hold blocks of the starting size and each row after doubles it, up
	// to the maximum direct block size. Rows beyond that point to further
	// indirect blocks, which this reader does not follow.
	if startBlock == 0 || width == 0 {
		return nil, fmt.Errorf("hdf5: fractal heap at 0x%x has a zero block size or table width", addr)
	}
	ic, err := f.at(root)
	if err != nil {
		return nil, err
	}
	ic.signature("FHIB")
	ic.skip(1 + 8 + offBytes) // version, heap header address, block offset
	var heapOffset uint64
	for r := 0; r < rows; r++ {
		size := startBlock
		if r >= 2 {
			size = startBlock << uint(r-1)
		}
		if size > maxDirect {
			return nil, fmt.Errorf("hdf5: fractal heap at 0x%x has indirect blocks below its root, which are not supported", addr)
		}
		for k := uint64(0); k < width; k++ {
			blockAddr := ic.u64()
			if ic.err != nil {
				return nil, ic.err
			}
			if blockAddr != h5Undefined {
				heap.blocks = append(heap.blocks, h5Block{heapOffset, size, blockAddr})
			}
			heapOffset += size
		}
	}
	return heap, nil
}

// object returns the bytes a managed heap ID points at. A heap offset counts
// from the start of the heap's address space, including each direct block's
// own header.
func (h *h5Heap) object(id []byte) ([]byte, error) {
	if len(id) != h.idLen {
		return nil, fmt.Errorf("hdf5: heap ID is %d bytes, expected %d", len(id), h.idLen)
	}
	if id[0]&0xF0 != 0 {
		return nil, fmt.Errorf("hdf5: heap ID 0x%02x is not a managed object (huge and tiny objects are not supported)", id[0])
	}
	c := &h5Cursor{b: id, p: 1}
	off := c.uN(h.offBytes)
	length := c.uN(h.idLen - 1 - h.offBytes)
	if c.err != nil {
		return nil, c.err
	}
	for _, blk := range h.blocks {
		if off < blk.heapOffset || off >= blk.heapOffset+blk.size {
			continue
		}
		if off+length > blk.heapOffset+blk.size {
			return nil, fmt.Errorf("hdf5: heap object at %d length %d runs past its direct block", off, length)
		}
		head, err := h.f.at(blk.addr)
		if err != nil {
			return nil, err
		}
		head.signature("FHDB")
		if head.err != nil {
			return nil, head.err
		}
		start := blk.addr + (off - blk.heapOffset)
		end := start + length
		if end > uint64(len(h.f.b)) {
			return nil, fmt.Errorf("hdf5: heap object at 0x%x runs past the end of the file", start)
		}
		return h.f.b[start:end], nil
	}
	return nil, fmt.Errorf("hdf5: heap offset %d is in no allocated direct block", off)
}

// btreeHeapIDs lists the heap IDs held in a version 2 B-tree's records.
// skip is how many bytes of each record come before the heap ID: the name
// hash for link records (type 5), nothing for attribute records (type 8).
func (f *h5File) btreeHeapIDs(addr uint64, wantType uint8, skip, idLen int) ([][]byte, error) {
	c, err := f.at(addr)
	if err != nil {
		return nil, err
	}
	c.signature("BTHD")
	version := c.u8()
	typ := c.u8()
	c.skip(4) // node size
	recordSize := int(c.u16())
	depth := c.u16()
	c.skip(2) // split and merge percentages
	root := c.u64()
	nrec := int(c.u16())
	if c.err != nil {
		return nil, c.err
	}
	if version != 0 || typ != wantType {
		return nil, fmt.Errorf("hdf5: B-tree at 0x%x is version %d type %d, expected version 0 type %d", addr, version, typ, wantType)
	}
	if depth != 0 {
		return nil, fmt.Errorf("hdf5: B-tree at 0x%x has depth %d, only a single leaf is supported", addr, depth)
	}
	if recordSize < skip+idLen {
		return nil, fmt.Errorf("hdf5: B-tree record size %d is too small for a %d-byte heap ID", recordSize, idLen)
	}
	if nrec == 0 {
		return nil, nil
	}
	leaf, err := f.at(root)
	if err != nil {
		return nil, err
	}
	leaf.signature("BTLF")
	lv, lt := leaf.u8(), leaf.u8()
	if leaf.err != nil {
		return nil, leaf.err
	}
	if lv != 0 || lt != wantType {
		return nil, fmt.Errorf("hdf5: B-tree leaf at 0x%x is version %d type %d, expected version 0 type %d", root, lv, lt, wantType)
	}
	ids := make([][]byte, 0, nrec)
	for i := 0; i < nrec; i++ {
		rec := leaf.take(recordSize)
		if leaf.err != nil {
			return nil, leaf.err
		}
		ids = append(ids, rec[skip:skip+idLen])
	}
	return ids, nil
}

// --- links and attributes ---

// denseObjects returns the objects a link info or attribute info message
// points at in dense storage: the raw bytes of each entry in the fractal heap
// its version 2 B-tree name index lists. what names the message for errors,
// orderLen is the size of its optional maximum creation order index, and
// btType and recSkip are the B-tree record type and the bytes before the heap
// ID in each record (see btreeHeapIDs). Nothing is returned when the object
// keeps everything in its header.
func (f *h5File) denseObjects(info []byte, what string, orderLen int, btType uint8, recSkip int) ([][]byte, error) {
	c := &h5Cursor{b: info}
	version, flags := c.u8(), c.u8()
	if flags&0x01 != 0 {
		c.skip(orderLen) // maximum creation order index
	}
	heapAddr, nameIndex := c.u64(), c.u64()
	if c.err != nil {
		return nil, c.err
	}
	if version != 0 {
		return nil, fmt.Errorf("hdf5: %s info message version %d is not supported", what, version)
	}
	if heapAddr == h5Undefined {
		return nil, nil
	}
	heap, err := f.openHeap(heapAddr)
	if err != nil {
		return nil, err
	}
	ids, err := f.btreeHeapIDs(nameIndex, btType, recSkip, heap.idLen)
	if err != nil {
		return nil, err
	}
	objects := make([][]byte, 0, len(ids))
	for _, id := range ids {
		raw, err := heap.object(id)
		if err != nil {
			return nil, err
		}
		objects = append(objects, raw)
	}
	return objects, nil
}

// groupLinks returns the hard links of the group whose object header is at
// addr, as name to object header address.
func (f *h5File) groupLinks(addr uint64) (map[string]uint64, error) {
	msgs, err := f.readObject(addr)
	if err != nil {
		return nil, err
	}
	links := map[string]uint64{}
	var linkInfo []byte
	for _, m := range msgs {
		switch m.typ {
		case h5MsgLink:
			name, target, err := parseLink(m.data)
			if err != nil {
				return nil, err
			}
			links[name] = target
		case h5MsgLinkInfo:
			linkInfo = m.data
		}
	}
	if linkInfo == nil {
		return links, nil
	}
	dense, err := f.denseObjects(linkInfo, "link", 8, 5, 4)
	if err != nil {
		return nil, err
	}
	for _, raw := range dense {
		name, target, err := parseLink(raw)
		if err != nil {
			return nil, err
		}
		links[name] = target
	}
	return links, nil
}

func parseLink(b []byte) (name string, target uint64, err error) {
	c := &h5Cursor{b: b}
	version, flags := c.u8(), c.u8()
	if c.err == nil && version != 1 {
		return "", 0, fmt.Errorf("hdf5: link message version %d is not supported", version)
	}
	linkType := uint8(0)
	if flags&0x08 != 0 {
		linkType = c.u8()
	}
	if flags&0x04 != 0 {
		c.skip(8) // creation order
	}
	if flags&0x10 != 0 {
		c.skip(1) // name character set
	}
	nameLen := int(c.uN(1 << (flags & 3)))
	raw := c.take(nameLen)
	if c.err != nil {
		return "", 0, c.err
	}
	if linkType != 0 {
		// Soft, external and user-defined links carry no object header address.
		return string(raw), h5Undefined, nil
	}
	target = c.u64()
	if c.err != nil {
		return "", 0, c.err
	}
	return string(raw), target, nil
}

type h5Attribute struct {
	dtype h5Datatype
	data  []byte
}

// attributes returns the attributes of the object with the given header
// messages, whether compact (in the header) or dense (in a fractal heap).
func (f *h5File) attributes(msgs []h5Message) (map[string]h5Attribute, error) {
	attrs := map[string]h5Attribute{}
	var info []byte
	for _, m := range msgs {
		switch m.typ {
		case h5MsgAttribute:
			name, a, err := parseAttribute(m.data)
			if err != nil {
				return nil, err
			}
			attrs[name] = a
		case h5MsgAttrInfo:
			info = m.data
		}
	}
	if info == nil {
		return attrs, nil
	}
	dense, err := f.denseObjects(info, "attribute", 2, 8, 0)
	if err != nil {
		return nil, err
	}
	for _, raw := range dense {
		name, a, err := parseAttribute(raw)
		if err != nil {
			return nil, err
		}
		attrs[name] = a
	}
	return attrs, nil
}

func parseAttribute(b []byte) (string, h5Attribute, error) {
	c := &h5Cursor{b: b}
	version, flags := c.u8(), c.u8()
	nameSize, dtSize, dsSize := int(c.u16()), int(c.u16()), int(c.u16())
	c.skip(1) // name character set
	if c.err == nil && version != 3 {
		return "", h5Attribute{}, fmt.Errorf("hdf5: attribute message version %d is not supported", version)
	}
	if c.err == nil && flags&0x03 != 0 {
		return "", h5Attribute{}, fmt.Errorf("hdf5: shared attribute datatype or dataspace is not supported")
	}
	rawName := c.take(nameSize)
	dt := c.take(dtSize)
	c.skip(dsSize)
	if c.err != nil {
		return "", h5Attribute{}, c.err
	}
	dtype, err := parseDatatype(dt)
	if err != nil {
		return "", h5Attribute{}, err
	}
	name := string(bytes.TrimRight(rawName, "\x00"))
	return name, h5Attribute{dtype: dtype, data: c.b[c.p:]}, nil
}

// stringValue returns a fixed-length string attribute's text.
func (a h5Attribute) stringValue() (string, error) {
	if a.dtype.class != 3 {
		return "", fmt.Errorf("hdf5: attribute is datatype class %d, not a fixed-length string", a.dtype.class)
	}
	if len(a.data) < a.dtype.size {
		return "", fmt.Errorf("hdf5: string attribute holds %d bytes, datatype says %d", len(a.data), a.dtype.size)
	}
	return string(bytes.TrimRight(a.data[:a.dtype.size], "\x00")), nil
}

// --- datasets ---

type h5Datatype struct {
	class     int // 0 fixed-point, 1 floating-point, 3 string
	size      int
	bigEndian bool
	signed    bool
}

func parseDatatype(b []byte) (h5Datatype, error) {
	c := &h5Cursor{b: b}
	head := c.u8()
	bits := c.take(3)
	size := int(c.u32())
	if c.err != nil {
		return h5Datatype{}, c.err
	}
	dt := h5Datatype{class: int(head & 0x0F), size: size, bigEndian: bits[0]&0x01 != 0, signed: bits[0]&0x08 != 0}
	if dt.class == 1 {
		// A floating-point datatype is only read as IEEE 754 binary32 or binary64.
		c.skip(2 + 2) // bit offset, precision
		expLoc, expSize := c.u8(), c.u8()
		mantLoc, mantSize := c.u8(), c.u8()
		bias := c.u32()
		if c.err != nil {
			return h5Datatype{}, c.err
		}
		single := size == 4 && expLoc == 23 && expSize == 8 && mantLoc == 0 && mantSize == 23 && bias == 127
		double := size == 8 && expLoc == 52 && expSize == 11 && mantLoc == 0 && mantSize == 52 && bias == 1023
		if !single && !double {
			return h5Datatype{}, fmt.Errorf("hdf5: floating-point datatype of %d bytes is not IEEE 754 single or double", size)
		}
		if bits[0]&0x40 != 0 {
			return h5Datatype{}, fmt.Errorf("hdf5: VAX byte order is not supported")
		}
	}
	return dt, nil
}

type h5Filter struct {
	id     uint16
	values []uint32
}

type h5Dataset struct {
	dims     []uint64
	dtype    h5Datatype
	filters  []h5Filter
	layout   uint8 // 1 contiguous, 2 chunked
	addr     uint64
	size     uint64   // contiguous: byte length
	chunkDim []uint32 // chunked: chunk shape plus the element size
	attrs    map[string]h5Attribute
}

func (f *h5File) openDataset(addr uint64) (*h5Dataset, error) {
	msgs, err := f.readObject(addr)
	if err != nil {
		return nil, err
	}
	ds := &h5Dataset{}
	var haveSpace, haveType, haveLayout bool
	for _, m := range msgs {
		switch m.typ {
		case h5MsgDataspace:
			c := &h5Cursor{b: m.data}
			version, rank := c.u8(), int(c.u8())
			c.skip(1) // flags
			kind := c.u8()
			if c.err != nil {
				return nil, c.err
			}
			if version != 2 {
				return nil, fmt.Errorf("hdf5: dataspace version %d is not supported", version)
			}
			if kind > 1 {
				return nil, fmt.Errorf("hdf5: dataspace type %d is not supported", kind)
			}
			for i := 0; i < rank; i++ {
				ds.dims = append(ds.dims, c.u64())
			}
			if c.err != nil {
				return nil, c.err
			}
			haveSpace = true
		case h5MsgDatatype:
			dt, err := parseDatatype(m.data)
			if err != nil {
				return nil, err
			}
			ds.dtype = dt
			haveType = true
		case h5MsgLayout:
			c := &h5Cursor{b: m.data}
			version, class := c.u8(), c.u8()
			if c.err != nil {
				return nil, c.err
			}
			if version != 3 {
				return nil, fmt.Errorf("hdf5: data layout version %d is not supported", version)
			}
			ds.layout = class
			switch class {
			case 1:
				ds.addr = c.u64()
				ds.size = c.u64()
			case 2:
				n := int(c.u8())
				ds.addr = c.u64()
				for i := 0; i < n; i++ {
					ds.chunkDim = append(ds.chunkDim, c.u32())
				}
			default:
				return nil, fmt.Errorf("hdf5: data layout class %d is not supported (only contiguous and chunked)", class)
			}
			if c.err != nil {
				return nil, c.err
			}
			haveLayout = true
		case h5MsgFilters:
			c := &h5Cursor{b: m.data}
			version, n := c.u8(), int(c.u8())
			if c.err == nil && version != 2 {
				return nil, fmt.Errorf("hdf5: filter pipeline version %d is not supported", version)
			}
			for i := 0; i < n; i++ {
				id := c.u16()
				nameLen := 0
				if id >= 256 {
					nameLen = int(c.u16())
				}
				c.skip(2) // flags
				nvalues := int(c.u16())
				c.skip(nameLen)
				flt := h5Filter{id: id}
				for v := 0; v < nvalues; v++ {
					flt.values = append(flt.values, c.u32())
				}
				ds.filters = append(ds.filters, flt)
			}
			if c.err != nil {
				return nil, c.err
			}
		}
	}
	if !haveSpace || !haveType || !haveLayout {
		return nil, fmt.Errorf("hdf5: object at 0x%x is missing a dataspace, datatype or layout, so it is not a dataset", addr)
	}
	attrs, err := f.attributes(msgs)
	if err != nil {
		return nil, err
	}
	ds.attrs = attrs
	return ds, nil
}

func (ds *h5Dataset) elements() uint64 {
	n := uint64(1)
	for _, d := range ds.dims {
		n *= d
	}
	return n
}

// read returns the dataset's bytes as stored in the file's own byte order,
// after undoing the filters.
func (f *h5File) read(ds *h5Dataset) ([]byte, error) {
	data, shuffled, err := f.readStored(ds)
	if err != nil {
		return nil, err
	}
	if shuffled {
		data = unshuffle(data, ds.dtype.size)
	}
	return data, nil
}

// readStored is read except that a shuffle filter, when it is the last one to
// undo, is left undone: shuffled reports that the bytes are still stored byte
// plane by byte plane. The big grid is read in place that way instead of
// being copied element by element into a second 4.9 MB buffer.
func (f *h5File) readStored(ds *h5Dataset) (data []byte, shuffled bool, err error) {
	want := ds.elements() * uint64(ds.dtype.size)
	switch ds.layout {
	case 1:
		if len(ds.filters) != 0 {
			return nil, false, fmt.Errorf("hdf5: filtered contiguous data is not supported")
		}
		if ds.size != want {
			return nil, false, fmt.Errorf("hdf5: contiguous data is %d bytes, the dataspace needs %d", ds.size, want)
		}
		if ds.addr == h5Undefined || ds.addr+ds.size > uint64(len(f.b)) {
			return nil, false, fmt.Errorf("hdf5: contiguous data at 0x%x runs outside the file", ds.addr)
		}
		return f.b[ds.addr : ds.addr+ds.size], false, nil
	case 2:
		return f.readSingleChunk(ds, want)
	}
	return nil, false, fmt.Errorf("hdf5: data layout class %d is not supported", ds.layout)
}

func (f *h5File) readSingleChunk(ds *h5Dataset, want uint64) ([]byte, bool, error) {
	rank := len(ds.dims)
	if len(ds.chunkDim) != rank+1 || int(ds.chunkDim[rank]) != ds.dtype.size {
		return nil, false, fmt.Errorf("hdf5: chunk shape %v does not match a rank %d dataset of %d-byte elements", ds.chunkDim, rank, ds.dtype.size)
	}
	for i, d := range ds.dims {
		if uint64(ds.chunkDim[i]) != d {
			return nil, false, fmt.Errorf("hdf5: the dataset is split into several chunks (chunk %v, dataset %v), only a single chunk is supported", ds.chunkDim[:rank], ds.dims)
		}
	}
	c, err := f.at(ds.addr)
	if err != nil {
		return nil, false, err
	}
	c.signature("TREE")
	nodeType, level := c.u8(), c.u8()
	entries := c.u16()
	c.skip(16) // sibling addresses
	chunkSize := uint64(c.u32())
	mask := c.u32()
	for i := 0; i <= rank; i++ {
		if off := c.u64(); c.err == nil && off != 0 {
			return nil, false, fmt.Errorf("hdf5: the only chunk starts at a non-zero offset")
		}
	}
	chunkAddr := c.u64()
	if c.err != nil {
		return nil, false, c.err
	}
	if nodeType != 1 || level != 0 || entries != 1 {
		return nil, false, fmt.Errorf("hdf5: chunk index is node type %d level %d with %d entries, expected a single leaf entry", nodeType, level, entries)
	}
	if mask != 0 {
		return nil, false, fmt.Errorf("hdf5: chunk filter mask 0x%x skips a filter, which is not supported", mask)
	}
	if chunkAddr == h5Undefined || chunkAddr+chunkSize > uint64(len(f.b)) {
		return nil, false, fmt.Errorf("hdf5: chunk at 0x%x (%d bytes) runs outside the file", chunkAddr, chunkSize)
	}
	data := f.b[chunkAddr : chunkAddr+chunkSize]

	// Filters were applied in pipeline order on write, so undo them in reverse.
	shuffled := false
	for i := len(ds.filters) - 1; i >= 0; i-- {
		flt := ds.filters[i]
		switch flt.id {
		case 1: // deflate
			zr, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, false, fmt.Errorf("hdf5: deflate stream: %w", err)
			}
			out := make([]byte, want)
			if _, err := io.ReadFull(zr, out); err != nil {
				return nil, false, fmt.Errorf("hdf5: deflate stream is shorter than the %d bytes expected: %w", want, err)
			}
			// Reading to the end makes zlib check its checksum.
			if n, err := zr.Read(make([]byte, 1)); n != 0 || err != io.EOF {
				return nil, false, fmt.Errorf("hdf5: deflate stream is longer than the %d bytes expected or failed its checksum: %v", want, err)
			}
			data = out
		case 2: // shuffle
			if len(flt.values) != 1 || int(flt.values[0]) != ds.dtype.size {
				return nil, false, fmt.Errorf("hdf5: shuffle filter parameters %v do not match %d-byte elements", flt.values, ds.dtype.size)
			}
			if i == 0 {
				// Last filter to undo: leave the bytes shuffled for the caller.
				shuffled = true
			} else {
				data = unshuffle(data, ds.dtype.size)
			}
		default:
			return nil, false, fmt.Errorf("hdf5: filter %d is not supported (only deflate and shuffle)", flt.id)
		}
	}
	if uint64(len(data)) != want {
		return nil, false, fmt.Errorf("hdf5: decoded chunk is %d bytes, the dataset needs %d", len(data), want)
	}
	return data, shuffled, nil
}

// unshuffle undoes HDF5's shuffle filter, which stores byte k of every element
// together: all the first bytes, then all the second bytes, and so on.
func unshuffle(b []byte, elemSize int) []byte {
	n := len(b) / elemSize
	out := make([]byte, len(b))
	for k := 0; k < elemSize; k++ {
		plane := b[k*n : (k+1)*n]
		for e, v := range plane {
			out[e*elemSize+k] = v
		}
	}
	return out
}

// --- the lightning grid ---

const (
	lightningVariable = "probability_of_number_of_lightning_flashes_per_unit_area_above_threshold"

	gridRows    = 960
	gridCols    = 1280
	gridLatStep = 0.1875
	gridLonStep = 0.28125
	gridLat0    = -89.90625   // centre of row 0
	gridLon0    = -179.859375 // centre of column 0
)

// lightningGrid is one decoded window file: the probability grid and the
// window it covers.
type lightningGrid struct {
	// values holds rows x cols little-endian float32s, row 0 southernmost. When
	// shuffled, they are still in the file's shuffle layout (byte k of every
	// element together) and at() picks each element's four bytes out of the
	// planes, so no unshuffled copy is made.
	values   []byte
	shuffled bool
	// start and end are time_bnds, seconds since the Unix epoch.
	start, end int64
}

func (g *lightningGrid) at(row, col int) float32 {
	i := row*gridCols + col
	if !g.shuffled {
		return math.Float32frombits(binary.LittleEndian.Uint32(g.values[i*4:]))
	}
	const n = gridRows * gridCols
	v := g.values
	return math.Float32frombits(uint32(v[i]) | uint32(v[n+i])<<8 | uint32(v[2*n+i])<<16 | uint32(v[3*n+i])<<24)
}

// parseLightningGrid decodes a Met Office lightning_flash_accumulation file.
// It finds the probability variable by name and by its long_name attribute,
// and checks the latitude and longitude variables against the fixed grid the
// cell lookup assumes, so a regridded file fails here rather than reading the
// wrong cell.
func parseLightningGrid(file []byte) (*lightningGrid, error) {
	f, root, err := openH5(file)
	if err != nil {
		return nil, err
	}
	links, err := f.groupLinks(root)
	if err != nil {
		return nil, err
	}
	open := func(name string) (*h5Dataset, error) {
		addr, ok := links[name]
		if !ok || addr == h5Undefined {
			return nil, fmt.Errorf("hdf5: the file has no variable %q", name)
		}
		return f.openDataset(addr)
	}

	prob, err := open(lightningVariable)
	if err != nil {
		return nil, err
	}
	longName, ok := prob.attrs["long_name"]
	if !ok {
		return nil, fmt.Errorf("hdf5: %s has no long_name attribute", lightningVariable)
	}
	if text, err := longName.stringValue(); err != nil || text != lightningVariable {
		return nil, fmt.Errorf("hdf5: %s has long_name %q (%v)", lightningVariable, text, err)
	}
	if len(prob.dims) != 2 || prob.dims[0] != gridRows || prob.dims[1] != gridCols {
		return nil, fmt.Errorf("hdf5: grid shape is %v, expected [%d %d]", prob.dims, gridRows, gridCols)
	}
	if prob.dtype.class != 1 || prob.dtype.size != 4 || prob.dtype.bigEndian {
		return nil, fmt.Errorf("hdf5: grid datatype is %+v, expected little-endian float32", prob.dtype)
	}
	values, shuffled, err := f.readStored(prob)
	if err != nil {
		return nil, err
	}

	if err := checkAxis(f, open, "latitude", gridRows, gridLat0, gridLatStep); err != nil {
		return nil, err
	}
	if err := checkAxis(f, open, "longitude", gridCols, gridLon0, gridLonStep); err != nil {
		return nil, err
	}

	tb, err := open("time_bnds")
	if err != nil {
		return nil, err
	}
	if len(tb.dims) != 1 || tb.dims[0] != 2 || tb.dtype.class != 0 || tb.dtype.size != 8 || tb.dtype.bigEndian || !tb.dtype.signed {
		return nil, fmt.Errorf("hdf5: time_bnds is shape %v datatype %+v, expected two little-endian int64s", tb.dims, tb.dtype)
	}
	raw, err := f.read(tb)
	if err != nil {
		return nil, err
	}
	return &lightningGrid{
		values:   values,
		shuffled: shuffled,
		start:    int64(binary.LittleEndian.Uint64(raw[0:8])),
		end:      int64(binary.LittleEndian.Uint64(raw[8:16])),
	}, nil
}

func checkAxis(f *h5File, open func(string) (*h5Dataset, error), name string, n int, first, step float64) error {
	ds, err := open(name)
	if err != nil {
		return err
	}
	if len(ds.dims) != 1 || ds.dims[0] != uint64(n) || ds.dtype.class != 1 || ds.dtype.size != 4 || ds.dtype.bigEndian {
		return fmt.Errorf("hdf5: %s is shape %v datatype %+v, expected %d little-endian float32s", name, ds.dims, ds.dtype, n)
	}
	raw, err := f.read(ds)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		got := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:])))
		want := first + step*float64(i)
		if math.Abs(got-want) > 1e-4 {
			return fmt.Errorf("hdf5: %s[%d] is %v, the grid this plugin reads has %v", name, i, got, want)
		}
	}
	return nil
}
