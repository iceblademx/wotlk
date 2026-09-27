// Package dbc reads World of Warcraft 3.3.5a (build 12340) client database files (WDBC format).
//
// Only the tables needed to import custom items, gems, enchants, item sets and spells are
// decoded. Every table layout is validated against the field count in the file header, so a
// mismatched (e.g. non-3.3.5a) file fails loudly instead of producing garbage.
package dbc

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// File is a raw WDBC file: fixed-size records of 4-byte fields plus a string block.
type File struct {
	Name        string
	RecordCount int
	FieldCount  int
	RecordSize  int

	records     []byte
	stringBlock []byte
}

func Open(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(filepath.Base(path), data)
}

func Parse(name string, data []byte) (*File, error) {
	if len(data) < 20 || string(data[:4]) != "WDBC" {
		return nil, fmt.Errorf("%s: not a WDBC file (3.3.5a client DBCs start with 'WDBC')", name)
	}
	f := &File{
		Name:        name,
		RecordCount: int(binary.LittleEndian.Uint32(data[4:])),
		FieldCount:  int(binary.LittleEndian.Uint32(data[8:])),
		RecordSize:  int(binary.LittleEndian.Uint32(data[12:])),
	}
	stringSize := int(binary.LittleEndian.Uint32(data[16:]))
	recordsEnd := 20 + f.RecordCount*f.RecordSize
	if recordsEnd+stringSize > len(data) {
		return nil, fmt.Errorf("%s: truncated file (header claims %d bytes, have %d)", name, recordsEnd+stringSize, len(data))
	}
	f.records = data[20:recordsEnd]
	f.stringBlock = data[recordsEnd : recordsEnd+stringSize]
	return f, nil
}

// Expect verifies the file has the layout a decoder was written for.
func (f *File) Expect(fieldCount int) error {
	if f.FieldCount != fieldCount || f.RecordSize != fieldCount*4 {
		return fmt.Errorf("%s: expected %d fields / %d byte records (3.3.5a build 12340), got %d fields / %d bytes",
			f.Name, fieldCount, fieldCount*4, f.FieldCount, f.RecordSize)
	}
	return nil
}

func (f *File) Record(i int) Record {
	return Record{f: f, data: f.records[i*f.RecordSize : (i+1)*f.RecordSize]}
}

type Record struct {
	f    *File
	data []byte
}

func (r Record) Uint32(field int) uint32 { return binary.LittleEndian.Uint32(r.data[field*4:]) }
func (r Record) Int32(field int) int32   { return int32(r.Uint32(field)) }
func (r Record) Float(field int) float32 { return math.Float32frombits(r.Uint32(field)) }

func (r Record) String(field int) string {
	off := int(r.Uint32(field))
	if off <= 0 || off >= len(r.f.stringBlock) {
		return ""
	}
	end := off
	for end < len(r.f.stringBlock) && r.f.stringBlock[end] != 0 {
		end++
	}
	return string(r.f.stringBlock[off:end])
}

// LocString reads a 3.3.5a localized string (16 locale slots + 1 flags field = 17 fields),
// returning the first non-empty locale. enUS is slot 0 but custom servers sometimes only fill
// the locale their client uses.
func (r Record) LocString(field int) string {
	for i := 0; i < 16; i++ {
		if s := r.String(field + i); s != "" {
			return s
		}
	}
	return ""
}

const locStringFields = 17

// Dir locates DBC files case-insensitively inside a directory (DBFilesClient extracted from MPQs).
type Dir struct {
	path  string
	files map[string]string
}

func OpenDir(path string) (*Dir, error) {
	d := &Dir{path: path, files: map[string]string{}}
	err := filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && strings.EqualFold(filepath.Ext(p), ".dbc") {
			key := strings.ToLower(e.Name())
			if _, exists := d.files[key]; !exists {
				d.files[key] = p
			}
		}
		return nil
	})
	return d, err
}

func (d *Dir) Has(name string) bool {
	_, ok := d.files[strings.ToLower(name)]
	return ok
}

// Open returns nil (and no error) when the file isn't present, since most tables are optional.
func (d *Dir) Open(name string) (*File, error) {
	p, ok := d.files[strings.ToLower(name)]
	if !ok {
		return nil, nil
	}
	return Open(p)
}
