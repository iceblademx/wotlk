// Package cc imports custom 3.3.5a content (client DBCs + server item data) into wowsims.
//
// Raw inputs live in cc_data/ (git-ignored):
//
//	cc_data/cc.json          optional config (see Config)
//	cc_data/dbc/             DBFilesClient/*.dbc from the custom client patch (Spell, ItemSet, ...)
//	cc_data/dbc_baseline/    optional stock 3.3.5a DBCs, to detect changed spells
//	cc_data/server/          item_template SQL dumps/patches, CSV/TSV exports, itemcache.wdb,
//	                         spell_proc(_event) and spell_bonus_data dumps
//
// `go run ./tools/cc/extract` turns these into committed, reviewable files under
// assets/db_inputs/cc/ plus sim/common/cc/zz_generated.go; gen_db merges them into the item database.
package cc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tailscale/hujson"
	"github.com/wowsims/wotlk/tools/cc/convert"
	"github.com/wowsims/wotlk/tools/cc/dbc"
	"github.com/wowsims/wotlk/tools/cc/server"
)

type Config struct {
	// Phase assigned to imported custom items/gems/enchants (UI phase filter). Default 1.
	Phase int32 `json:"phase"`
	// Items with ID >= this are always treated as custom, even if wowhead knows the ID. 0 = off.
	CustomItemIDMin uint32 `json:"customItemIdMin"`
	// Extra item IDs to import as custom / to never import.
	IncludeItems []uint32 `json:"includeItems"`
	ExcludeItems []uint32 `json:"excludeItems"`
	// Replace stock (wowhead-derived) item stats with the server's item_template values when they differ.
	// Only applied when the importer fully understood the item's equip effects.
	ApplyStockItemChanges *bool `json:"applyStockItemChanges"`
	// Hide stock items that don't exist in the server's item_template (e.g. Wrath Classic-only items).
	// Only applied when a complete item_template (SQL/CSV) was provided.
	RestrictToServerItems *bool `json:"restrictToServerItems"`
	// Additional spell IDs to decode into spells.json (e.g. modified class spells to implement).
	ExtraSpells []uint32 `json:"extraSpells"`
}

func (c *Config) ApplyStock() bool { return c.ApplyStockItemChanges == nil || *c.ApplyStockItemChanges }
func (c *Config) RestrictItems() bool {
	return c.RestrictToServerItems == nil || *c.RestrictToServerItems
}

func LoadConfig(inputDir string) (*Config, error) {
	cfg := &Config{Phase: 1}
	data, err := os.ReadFile(filepath.Join(inputDir, "cc.json"))
	if os.IsNotExist(err) {
		return cfg, nil
	} else if err != nil {
		return nil, err
	}
	std, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("cc.json: %w", err)
	}
	if err := json.Unmarshal(std, cfg); err != nil {
		return nil, fmt.Errorf("cc.json: %w", err)
	}
	if cfg.Phase == 0 {
		cfg.Phase = 1
	}
	return cfg, nil
}

// Inputs is everything loaded from cc_data/.
type Inputs struct {
	Dir      string
	Config   *Config
	Source   *convert.Source
	Baseline *dbc.SpellStore // nil unless cc_data/dbc_baseline/Spell.dbc exists
	Loaded   []string        // files read, for the report
}

func Load(inputDir string) (*Inputs, error) {
	in := &Inputs{Dir: inputDir}
	var err error
	if in.Config, err = LoadConfig(inputDir); err != nil {
		return nil, err
	}

	dbcDir, err := dbc.OpenDir(filepath.Join(inputDir, "dbc"))
	if os.IsNotExist(err) {
		dbcDir, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	src := &convert.Source{Server: server.NewData()}
	if dbcDir != nil && dbcDir.Has("Spell.dbc") {
		if src.Spells, err = dbc.LoadSpells(dbcDir); err != nil {
			return nil, err
		}
		in.Loaded = append(in.Loaded, fmt.Sprintf("dbc/Spell.dbc (%d spells)", len(src.Spells.Spells)))
	} else {
		src.Spells = &dbc.SpellStore{Spells: map[uint32]*dbc.Spell{}}
		src.Server.Warnings = append(src.Server.Warnings, "cc_data/dbc/Spell.dbc not found: equip/use/proc effects and set bonuses cannot be decoded")
	}
	if dbcDir != nil {
		if src.Tables, err = dbc.LoadTables(dbcDir); err != nil {
			return nil, err
		}
		in.Loaded = append(in.Loaded, fmt.Sprintf("dbc tables: %d item sets, %d enchantments, %d gem properties, %d display icons",
			len(src.Tables.ItemSets), len(src.Tables.Enchants), len(src.Tables.GemProperties), len(src.Tables.DisplayIcons)))
	} else {
		src.Tables = &dbc.Tables{ItemSets: map[uint32]*dbc.ItemSet{}, Enchants: map[uint32]*dbc.SpellItemEnchantment{},
			GemProperties: map[uint32]*dbc.GemProperties{}, DisplayIcons: map[uint32]string{}, ClientItems: map[uint32][4]uint32{}}
	}

	if baseDir, err := dbc.OpenDir(filepath.Join(inputDir, "dbc_baseline")); err == nil && baseDir.Has("Spell.dbc") {
		if in.Baseline, err = dbc.LoadSpells(baseDir); err != nil {
			return nil, fmt.Errorf("dbc_baseline: %w", err)
		}
		in.Loaded = append(in.Loaded, fmt.Sprintf("dbc_baseline/Spell.dbc (%d spells)", len(in.Baseline.Spells)))
	}

	// Server files load in name order so numbered patches (01_base.sql, 02_custom.sql) apply in sequence.
	serverDir := filepath.Join(inputDir, "server")
	var files, wdbFiles []string
	filepath.WalkDir(serverDir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".sql", ".csv", ".tsv":
			files = append(files, p)
		case ".wdb":
			wdbFiles = append(wdbFiles, p)
		}
		return nil
	})
	sort.Strings(files)
	for _, f := range files {
		var err error
		if strings.EqualFold(filepath.Ext(f), ".sql") {
			err = src.Server.LoadSQLFile(f)
		} else {
			err = src.Server.LoadCSVFile(f)
		}
		if err != nil {
			return nil, err
		}
		in.Loaded = append(in.Loaded, rel(inputDir, f))
	}
	src.Server.Finalize()
	for _, f := range wdbFiles {
		if err := src.Server.LoadItemCacheWDB(f); err != nil {
			return nil, err
		}
		in.Loaded = append(in.Loaded, rel(inputDir, f))
	}
	in.Source = src
	return in, nil
}

func rel(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

// WriteJSON writes indented JSON with a trailing newline.
func WriteJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0666)
}
