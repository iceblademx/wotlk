// Command tuning fetches the custom content data the server publishes for simulators (tier sets and
// legendaries: stats, tooltips, script mechanics and live tuning), reports what changed since the copy
// stored in the repository, and stores the new copy.
//
//	go run ./tools/cc/tuning            # fetch, report, update assets/db_inputs/cc/server_sim_data.json
//	go run ./tools/cc/tuning -check     # fetch and report only; exit 1 if anything changed
//	go run ./tools/cc/tuning -file x    # use a local copy of data.json instead of fetching
//
// The report ends with a "Fingerprint:" line (a hash of the fetched content without its refresh
// timestamp, so it only changes when the content does) and "Status: changed" or "Status: unchanged".
// Callers such as the cc-server-check skill read those lines: "go run" turns every non-zero exit into
// 1, so the exit code can't tell a change from a failed fetch.
//
// The stored copy is what the sim's cc_tuning_test.go files check their constants against, so after an
// update `./build.ps1 test` names every constant that no longer matches. The report also compares item
// stats and set bonus text with the imported item data (assets/db_inputs/cc), which comes from the
// server database via `./build.ps1 dump` and `./build.ps1 cc`.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultURL = "http://209.38.90.151:8087/sim/data.json"

var (
	url          = flag.String("url", envOr("CC_SIM_DATA_URL", defaultURL), "URL of the server's data.json")
	file         = flag.String("file", "", "Read this local data.json instead of fetching")
	snapshotPath = flag.String("snapshot", "assets/db_inputs/cc/server_sim_data.json", "Stored copy")
	ccDBPath     = flag.String("db", "assets/db_inputs/cc/cc_db.json", "Imported custom items")
	setsPath     = flag.String("sets", "assets/db_inputs/cc/item_sets.json", "Imported custom item sets")
	simDir       = flag.String("sim", "sim", "Sim source directory (to mark simulated entries)")
	check        = flag.Bool("check", false, "Report only, without storing; exit 1 if anything changed")
)

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------------------------------
// data.json

type tuningValue struct {
	Value  json.Number     `json:"value"`
	Unit   string          `json:"unit"`
	Note   string          `json:"note"`
	Config json.RawMessage `json:"config"`
}

type weapon struct {
	Type      string  `json:"type"`
	MinDamage float64 `json:"min_damage"`
	MaxDamage float64 `json:"max_damage"`
	Speed     float64 `json:"speed"`
}

type item struct {
	Entry     int                `json:"entry"`
	Name      string             `json:"name"`
	ItemLevel int                `json:"item_level"`
	Slot      string             `json:"slot"`
	Stats     map[string]float64 `json:"stats"`
	Weapon    *weapon            `json:"weapon"`
}

type bonus struct {
	Pieces  int    `json:"pieces"`
	Name    string `json:"name"`
	SpellID int    `json:"spell_id"`
	Text    string `json:"text"`
}

type tierSet struct {
	Key       string                 `json:"key"`
	Name      string                 `json:"name"`
	Cls       string                 `json:"cls"`
	Spec      string                 `json:"spec"`
	ItemSetID int                    `json:"item_set_id"`
	Items     []item                 `json:"items"`
	Bonuses   []bonus                `json:"bonuses"`
	Tuning    map[string]tuningValue `json:"tuning"`
}

type spell struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Tooltip string `json:"tooltip"`
}

type legendary struct {
	item
	Classes   []string               `json:"classes"`
	Spec      string                 `json:"spec"`
	Spells    []spell                `json:"spells"`
	Mechanics []string               `json:"mechanics"`
	Tuning    map[string]tuningValue `json:"tuning"`
}

type simData struct {
	Server      string      `json:"server"`
	GeneratedAt string      `json:"generated_at"`
	Notes       []string    `json:"notes"`
	TierSets    []tierSet   `json:"tier_sets"`
	Legendaries []legendary `json:"legendaries"`
}

// entry is a tier set or legendary, keyed like the sim's cc_tuning_test.go files: set key or item ID.
type entry struct {
	key, title string
	tuning     map[string]tuningValue
	items      []item
	texts      map[string]string // label -> text, for spotting mechanic changes
}

func (d *simData) entries() map[string]*entry {
	out := map[string]*entry{}
	for _, s := range d.TierSets {
		e := &entry{key: s.Key, title: fmt.Sprintf("%s (%s %s set %d)", s.Name, s.Cls, s.Spec, s.ItemSetID),
			tuning: s.Tuning, items: s.Items, texts: map[string]string{}}
		for _, b := range s.Bonuses {
			e.texts[fmt.Sprintf("%dpc %s", b.Pieces, b.Name)] = b.Text
		}
		out[e.key] = e
	}
	for _, l := range d.Legendaries {
		e := &entry{key: strconv.Itoa(l.Entry), title: fmt.Sprintf("%s (%s %s ring)", l.Name, strings.Join(l.Classes, "/"), l.Spec),
			tuning: l.Tuning, items: []item{l.item}, texts: map[string]string{}}
		for _, sp := range l.Spells {
			if sp.Tooltip != "" {
				e.texts[fmt.Sprintf("tooltip %d", sp.ID)] = sp.Tooltip
			}
		}
		for i, m := range l.Mechanics {
			e.texts[fmt.Sprintf("mechanic %d", i+1)] = m
		}
		out[e.key] = e
	}
	return out
}

func parse(data []byte) (*simData, error) {
	var d simData
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

// normalize drops the refresh timestamp, so the stored copy only changes when the content does, and
// pretty-prints with sorted keys.
func normalize(data []byte) ([]byte, error) {
	var v map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	delete(v, "generated_at")
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fetch() ([]byte, error) {
	if *file != "" {
		return os.ReadFile(*file)
	}
	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Get(*url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", *url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// ---------------------------------------------------------------------------------------------------
// Report

type report struct {
	sections []section
}

type section struct {
	title, hint string
	lines       []string
}

func (r *report) add(title, hint string, lines []string) {
	if len(lines) > 0 {
		r.sections = append(r.sections, section{title, hint, lines})
	}
}

func (r *report) changed() bool { return len(r.sections) > 0 }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// simulatedEntries returns the entry keys that some sim cc_tuning_test.go checks.
func simulatedEntries(entries map[string]*entry) map[string]bool {
	var sources strings.Builder
	filepath.WalkDir(*simDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "cc_tuning_test.go" {
			if data, err := os.ReadFile(path); err == nil {
				sources.Write(data)
			}
		}
		return nil
	})
	out := map[string]bool{}
	for key := range entries {
		if strings.Contains(sources.String(), strconv.Quote(key)) {
			out[key] = true
		}
	}
	return out
}

func compareSnapshots(r *report, old, cur map[string]*entry, simulated map[string]bool) {
	label := func(key string) string {
		e := cur[key]
		if e == nil {
			e = old[key]
		}
		mark := ""
		if simulated[key] {
			mark = " [simulated]"
		}
		return fmt.Sprintf("`%s` %s%s", key, e.title, mark)
	}

	var added, removed []string
	for _, key := range sortedKeys(cur) {
		if old[key] == nil {
			added = append(added, "- "+label(key))
		}
	}
	for _, key := range sortedKeys(old) {
		if cur[key] == nil {
			removed = append(removed, "- "+label(key))
		}
	}
	r.add("New entries", "New sets or legendaries: candidates to implement.", added)
	r.add("Removed entries", "Simulated ones fail their cc_tuning_test.go until removed from it.", removed)

	var values, params, texts, stats []string
	for _, key := range sortedKeys(cur) {
		o, c := old[key], cur[key]
		if o == nil {
			continue
		}
		for _, name := range sortedKeys(c.tuning) {
			nv, ov := c.tuning[name], o.tuning[name]
			switch {
			case ov.Value == "":
				params = append(params, fmt.Sprintf("- %s: new `%s` = %s", label(key), name, strings.TrimSpace(fmt.Sprintf("%s %s %s", nv.Value, nv.Unit, nv.Note))))
			case ov.Value != nv.Value:
				values = append(values, fmt.Sprintf("| %s | `%s` | %s | %s | %s |", label(key), name, ov.Value, nv.Value,
					strings.TrimSpace(nv.Unit+" "+nv.Note)))
			}
		}
		for _, name := range sortedKeys(o.tuning) {
			if _, ok := c.tuning[name]; !ok {
				params = append(params, fmt.Sprintf("- %s: removed `%s` (was %s)", label(key), name, o.tuning[name].Value))
			}
		}
		for _, name := range sortedKeys(c.texts) {
			if ot := o.texts[name]; ot != c.texts[name] {
				texts = append(texts, fmt.Sprintf("- %s, %s:\n  - was: %s\n  - now: %s", label(key), name, orNone(ot), orNone(c.texts[name])))
			}
		}
		for _, name := range sortedKeys(o.texts) {
			if _, ok := c.texts[name]; !ok {
				texts = append(texts, fmt.Sprintf("- %s, %s removed (was: %s)", label(key), name, o.texts[name]))
			}
		}
		oldItems := map[int]item{}
		for _, it := range o.items {
			oldItems[it.Entry] = it
		}
		for _, it := range c.items {
			if diff := itemDiff(oldItems[it.Entry], it); diff != "" {
				stats = append(stats, fmt.Sprintf("- %s: item %d %s: %s", label(key), it.Entry, it.Name, diff))
			}
		}
	}
	if len(values) > 0 {
		values = append([]string{"| Entry | Parameter | Old | New | Meaning |", "|---|---|---|---|---|"}, values...)
	}
	r.add("Tuning values changed", "`./build.ps1 test` fails in cc_tuning_test.go for each simulated one, naming the constant to update.", values)
	r.add("Tuning parameters added or removed", "Simulated entries fail cc_tuning_test.go until each new parameter is mapped to a constant (or listed as not simulated).", params)
	r.add("Text changed", "Tooltips, set bonus text or documented mechanics: the behaviour may have changed, so review the code, not just the numbers.", texts)
	r.add("Item stats changed on the server", "Re-import with `./build.ps1 dump` and `./build.ps1 cc`.", stats)
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func itemDiff(o, c item) string {
	var diffs []string
	for _, k := range sortedKeys(c.Stats) {
		if o.Stats[k] != c.Stats[k] {
			diffs = append(diffs, fmt.Sprintf("%s %v -> %v", k, o.Stats[k], c.Stats[k]))
		}
	}
	for _, k := range sortedKeys(o.Stats) {
		if _, ok := c.Stats[k]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s %v -> 0", k, o.Stats[k]))
		}
	}
	if o.ItemLevel != c.ItemLevel {
		diffs = append(diffs, fmt.Sprintf("item level %d -> %d", o.ItemLevel, c.ItemLevel))
	}
	if (o.Weapon == nil) != (c.Weapon == nil) || (c.Weapon != nil && *o.Weapon != *c.Weapon) {
		diffs = append(diffs, fmt.Sprintf("weapon %+v -> %+v", o.Weapon, c.Weapon))
	}
	return strings.Join(diffs, ", ")
}

// ---------------------------------------------------------------------------------------------------
// Imported item data

// Stat indices of proto.Stat that the server's stat names map to (ratings count for melee and spell,
// attack power for melee and ranged, as tools/cc/convert imports them).
var statIndices = map[string][]int{
	"strength": {0}, "agility": {1}, "stamina": {2}, "intellect": {3}, "spirit": {4}, "spell_power": {5},
	"mp5": {6}, "hit_rating": {7, 12}, "crit_rating": {8, 13}, "haste_rating": {9, 14},
	"attack_power": {11, 21}, "armor_penetration_rating": {15}, "expertise_rating": {16}, "armor": {20},
	"defense_rating": {22}, "block_rating": {23}, "dodge_rating": {25}, "parry_rating": {26},
}

type dbItem struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Stats           []float64 `json:"stats"`
	WeaponDamageMin float64   `json:"weaponDamageMin"`
	WeaponDamageMax float64   `json:"weaponDamageMax"`
	WeaponSpeed     float64   `json:"weaponSpeed"`
	Ilvl            int       `json:"ilvl"`
}

type dbSet struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	ItemIDs []int  `json:"itemIds"`
	Bonuses []struct {
		Pieces  int    `json:"pieces"`
		SpellID int    `json:"spellId"`
		Text    string `json:"text"`
	} `json:"bonuses"`
}

func compareImported(r *report, d *simData) {
	var db struct {
		Items []dbItem `json:"items"`
	}
	var sets []dbSet
	if err := readJSON(*ccDBPath, &db); err != nil {
		r.add("Imported item data unreadable", "", []string{"- " + err.Error()})
		return
	}
	if err := readJSON(*setsPath, &sets); err != nil {
		r.add("Imported item data unreadable", "", []string{"- " + err.Error()})
		return
	}
	byID := map[int]dbItem{}
	for _, it := range db.Items {
		byID[it.ID] = it
	}

	var lines []string
	checkItem := func(it item) {
		got, ok := byID[it.Entry]
		if !ok {
			lines = append(lines, fmt.Sprintf("- item %d %s: not imported", it.Entry, it.Name))
			return
		}
		want := make([]float64, len(got.Stats))
		var diffs []string
		for name, v := range it.Stats {
			idx, ok := statIndices[name]
			if !ok {
				diffs = append(diffs, "unknown server stat "+name)
				continue
			}
			for _, i := range idx {
				if i < len(want) {
					want[i] += v
				}
			}
		}
		// The server lists one armor value; the import stores armor on jewelry as bonus armor.
		const armor, bonusArmor = 20, 34
		if len(want) > bonusArmor {
			got.Stats = append([]float64(nil), got.Stats...)
			got.Stats[armor] += got.Stats[bonusArmor]
			got.Stats[bonusArmor] = 0
		}
		for i := range want {
			if math.Abs(want[i]-got.Stats[i]) > 1e-6 {
				diffs = append(diffs, fmt.Sprintf("stat %d: imported %v, server %v", i, got.Stats[i], want[i]))
			}
		}
		if got.Name != it.Name {
			diffs = append(diffs, fmt.Sprintf("name %q, server %q", got.Name, it.Name))
		}
		if got.Ilvl != it.ItemLevel {
			diffs = append(diffs, fmt.Sprintf("item level %d, server %d", got.Ilvl, it.ItemLevel))
		}
		if w := it.Weapon; w != nil && (got.WeaponDamageMin != w.MinDamage || got.WeaponDamageMax != w.MaxDamage || math.Abs(got.WeaponSpeed-w.Speed) > 1e-6) {
			diffs = append(diffs, fmt.Sprintf("weapon %v-%v @%v, server %v-%v @%v", got.WeaponDamageMin, got.WeaponDamageMax, got.WeaponSpeed, w.MinDamage, w.MaxDamage, w.Speed))
		}
		if len(diffs) > 0 {
			sort.Strings(diffs)
			lines = append(lines, fmt.Sprintf("- item %d %s: %s", it.Entry, it.Name, strings.Join(diffs, "; ")))
		}
	}
	setsByID := map[int]dbSet{}
	for _, s := range sets {
		setsByID[s.ID] = s
	}
	for _, s := range d.TierSets {
		for _, it := range s.Items {
			checkItem(it)
		}
		got, ok := setsByID[s.ItemSetID]
		if !ok {
			lines = append(lines, fmt.Sprintf("- set %d %s: not imported", s.ItemSetID, s.Name))
			continue
		}
		if got.Name != s.Name {
			lines = append(lines, fmt.Sprintf("- set %d: imported name %q, server %q", s.ItemSetID, got.Name, s.Name))
		}
		for _, b := range s.Bonuses {
			found := false
			for _, gb := range got.Bonuses {
				if gb.Pieces == b.Pieces {
					found = true
					if gb.SpellID != b.SpellID || gb.Text != b.Text {
						lines = append(lines, fmt.Sprintf("- set %d %s (%dpc): imported spell %d %q, server spell %d %q",
							s.ItemSetID, s.Name, b.Pieces, gb.SpellID, gb.Text, b.SpellID, b.Text))
					}
				}
			}
			if !found {
				lines = append(lines, fmt.Sprintf("- set %d %s: %dpc bonus not imported", s.ItemSetID, s.Name, b.Pieces))
			}
		}
	}
	for _, l := range d.Legendaries {
		checkItem(l.item)
	}
	r.add("Imported item data differs from the server",
		"The sim's item stats and set text come from the server database: run `./build.ps1 dump` then `./build.ps1 cc` (needs .env), and review the result.", lines)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// ---------------------------------------------------------------------------------------------------

func main() {
	flag.Parse()
	log.SetFlags(0)

	raw, err := fetch()
	if err != nil {
		log.Fatal(err)
	}
	cur, err := parse(raw)
	if err != nil {
		log.Fatalf("data.json: %v", err)
	}
	normalized, err := normalize(raw)
	if err != nil {
		log.Fatal(err)
	}

	source := *url
	if *file != "" {
		source = *file
	}
	fmt.Printf("# Server tuning check\n\nSource: %s (generated %s): %d tier sets, %d legendaries.\n\n",
		source, cur.GeneratedAt, len(cur.TierSets), len(cur.Legendaries))

	r := &report{}
	curEntries := cur.entries()
	simulated := simulatedEntries(curEntries)

	storedRaw, err := os.ReadFile(*snapshotPath)
	switch {
	case os.IsNotExist(err):
		fmt.Printf("No stored copy at %s yet: nothing to compare with.\n\n", *snapshotPath)
	case err != nil:
		log.Fatal(err)
	case bytes.Equal(storedRaw, normalized):
		fmt.Printf("Identical to the stored copy (%s).\n\n", *snapshotPath)
	default:
		stored, err := parse(storedRaw)
		if err != nil {
			log.Fatalf("%s: %v", *snapshotPath, err)
		}
		compareSnapshots(r, stored.entries(), curEntries, simulated)
		if !r.changed() {
			r.add("Other changes", "Content outside tuning, text and stats changed (e.g. notes or related spells); see the git diff.", []string{"- " + *snapshotPath})
		}
	}
	compareImported(r, cur)

	for _, s := range r.sections {
		fmt.Printf("## %s\n\n", s.title)
		if s.hint != "" {
			fmt.Printf("%s\n\n", s.hint)
		}
		fmt.Printf("%s\n\n", strings.Join(s.lines, "\n"))
	}
	if !r.changed() {
		fmt.Println("No changes.")
	}

	var keys []string
	for key := range simulated {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Printf("Simulated entries (checked by sim/**/cc_tuning_test.go): %s\n", strings.Join(keys, ", "))

	sum := sha256.Sum256(normalized)
	fmt.Printf("\nFingerprint: %s\n", hex.EncodeToString(sum[:8]))
	if r.changed() {
		fmt.Println("Status: changed")
	} else {
		fmt.Println("Status: unchanged")
	}

	if *check {
		if r.changed() {
			os.Exit(1)
		}
		return
	}
	if !bytes.Equal(storedRaw, normalized) {
		if err := os.WriteFile(*snapshotPath, normalized, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nStored %s. Next: ./build.ps1 test\n", *snapshotPath)
	}
}
