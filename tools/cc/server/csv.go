package server

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Tables recognized by CSV file name, longest names first so "spell_proc_event" isn't read as "spell_proc".
var csvTables = []string{"spell_enchant_proc_data", "spell_cooldown_overrides", "spell_proc_event", "spell_bonus_data",
	"item_template", "spell_proc", "spell_dbc"}

// RawTable keeps a table's rows in column order (used for spell_dbc, whose columns follow the Spell.dbc layout).
type RawTable struct {
	Header []string
	Rows   [][]string
}

// LoadCSVFile loads an export with a header row. The table is chosen from the file name
// (e.g. "item_template.csv", "custom_item_template.tsv"); the delimiter (comma, tab or semicolon)
// is auto-detected. Returns false for files that aren't a table the importer uses.
func (d *Data) LoadCSVFile(path string) (bool, error) {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	table := ""
	for _, t := range csvTables {
		if strings.Contains(base, t) {
			table = t
			break
		}
	}
	if table == "" || strings.Contains(base, "locale") {
		return false, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	firstLine := string(data)
	if i := strings.IndexByte(firstLine, '\n'); i >= 0 {
		firstLine = firstLine[:i]
	}
	delim := ','
	if strings.Count(firstLine, "\t") > strings.Count(firstLine, string(delim)) {
		delim = '\t'
	}
	if strings.Count(firstLine, ";") > strings.Count(firstLine, string(delim)) {
		delim = ';'
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if len(records) < 2 {
		return true, nil
	}
	header := records[0]
	for _, rec := range records[1:] {
		for i, v := range rec {
			if strings.EqualFold(v, "NULL") || v == `\N` {
				rec[i] = ""
			}
		}
	}
	if table == "spell_dbc" {
		if d.SpellDBC == nil {
			d.SpellDBC = &RawTable{Header: header}
		}
		d.SpellDBC.Rows = append(d.SpellDBC.Rows, records[1:]...)
		return true, nil
	}
	for _, rec := range records[1:] {
		d.putRow(table, NewRow(header, rec))
	}
	return true, nil
}
