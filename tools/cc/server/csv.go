package server

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadCSVFile loads an export of item_template / spell_proc / spell_proc_event / spell_bonus_data
// with a header row. The table is chosen from the file name (e.g. "item_template.csv",
// "custom_item_template.tsv"); the delimiter (comma, tab or semicolon) is auto-detected.
func (d *Data) LoadCSVFile(path string) error {
	base := strings.ToLower(filepath.Base(path))
	table := ""
	for _, t := range []string{"item_template", "spell_proc_event", "spell_proc", "spell_bonus_data"} {
		if strings.Contains(base, t) {
			table = t
			break
		}
	}
	if table == "" {
		return fmt.Errorf("%s: can't tell which table this is; include item_template, spell_proc, spell_proc_event or spell_bonus_data in the file name", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
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
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(records) < 2 {
		return nil
	}
	header := records[0]
	for _, rec := range records[1:] {
		for i, v := range rec {
			if strings.EqualFold(v, "NULL") || v == `\N` {
				rec[i] = ""
			}
		}
		d.putRow(table, NewRow(header, rec))
	}
	return nil
}
