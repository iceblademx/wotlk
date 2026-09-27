// Command extract imports custom 3.3.5a content from cc_data/ into the repository.
//
//	go run ./tools/cc/extract            (or: ./build.ps1 cc)
//
// Outputs (all committed):
//
//	assets/db_inputs/cc/cc_db.json              custom items, gems and enchants (merged by gen_db)
//	assets/db_inputs/cc/stock_overrides.json    server values for stock items/gems (gen_db applies differences)
//	assets/db_inputs/cc/server_item_ids.json    every item ID on the server (gen_db hides the rest)
//	assets/db_inputs/cc/settings.json           options gen_db needs
//	assets/db_inputs/cc/spells.json             decoded spells referenced by custom content
//	assets/db_inputs/cc/item_sets.json          custom item sets and their bonus spells
//	assets/db_inputs/cc/effects.json            effects needing (or given) Go implementations
//	assets/db_inputs/cc/custom_spells.json      index of every spell not present in stock data
//	assets/db_inputs/cc/spell_changes.json      spells that differ from cc_data/dbc_baseline (if provided)
//	assets/db_inputs/cc/sim_spells.json         family data of the custom spells hand-written sim code uses
//	                                            (checked by sim/common/cc/ccfamily)
//	assets/db_inputs/cc/REPORT.md               human-readable summary / TODO list
//	sim/common/cc/zz_generated.go               effects expressible from data alone
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/core/stats"
	"github.com/wowsims/wotlk/tools/cc"
	"github.com/wowsims/wotlk/tools/cc/convert"
	"github.com/wowsims/wotlk/tools/cc/dbc"
	"github.com/wowsims/wotlk/tools/cc/server"
	"github.com/wowsims/wotlk/tools/database"
)

var inDir = flag.String("in", "cc_data", "Directory with raw 3.3.5a client/server files")
var assetsDir = flag.String("assets", "assets", "Assets directory (reads stock data, writes db_inputs/cc)")
var simDir = flag.String("sim", "sim", "Sim source directory (scanned for hand-written effects)")
var genFile = flag.String("gen", "sim/common/cc/zz_generated.go", "Generated Go effects file")

func main() {
	flag.Parse()
	ex, outDir, err := runExtract(*inDir, *assetsDir, *simDir, *genFile)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("cc: %d custom items, %d gems, %d enchants, %d sets; %d effects generated, %d need code. See %s\n",
		len(ex.customDB.Items), len(ex.customDB.Gems), len(ex.customDB.Enchants), len(ex.sets),
		ex.generatedCount(), ex.todoCount(), filepath.ToSlash(filepath.Join(outDir, "REPORT.md")))
}

func runExtract(inDir, assetsDir, simDir, genFile string) (*extraction, string, error) {
	in, err := cc.Load(inDir)
	if err != nil {
		return nil, "", err
	}
	if len(in.Loaded) == 0 {
		return nil, "", fmt.Errorf("nothing to import: %s has no dbc/*.dbc or server/ files (see cc_data/README.md)", inDir)
	}
	outDir := filepath.Join(assetsDir, "db_inputs", "cc")
	if err := os.MkdirAll(outDir, 0777); err != nil {
		return nil, "", err
	}
	ex := &extraction{
		in:          in,
		src:         in.Source,
		cfg:         in.Config,
		stock:       loadStock(assetsDir, in.Config),
		handwritten: scanHandwritten(simDir, genFile),
		customDB:    database.NewWowDatabase(),
		stockDB:     database.NewWowDatabase(),
		spellRefs:   map[uint32]bool{},
	}
	ex.run()
	if err := ex.write(outDir, genFile); err != nil {
		return nil, "", err
	}
	return ex, outDir, nil
}

// ---------------------------------------------------------------------------
// Stock data (what upstream wowsims already knows about)
// ---------------------------------------------------------------------------

// Only stock *inputs* are used here, never the generated database: reading gen_db's output would
// make each import depend on the previous one.
type stockData struct {
	itemIDs    map[int32]bool // wowhead item tooltip IDs: anything else is custom
	spellIDs   map[int32]bool // wowhead spell tooltip IDs: anything else is custom
	enchantIDs map[int32]bool // stock enchant effect IDs
}

func csvIDs(path string) map[int32]bool {
	ids := map[int32]bool{}
	f, err := os.Open(path)
	if err != nil {
		log.Printf("warning: %v (every ID will be treated as custom)", err)
		return ids
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, ','); i > 0 {
			if id, err := strconv.Atoi(line[:i]); err == nil {
				ids[int32(id)] = true
			}
		}
	}
	return ids
}

func loadStock(assets string, cfg *cc.Config) *stockData {
	s := &stockData{
		itemIDs:    csvIDs(filepath.Join(assets, "db_inputs", "wowhead_item_tooltips.csv")),
		spellIDs:   csvIDs(filepath.Join(assets, "db_inputs", "wowhead_spell_tooltips.csv")),
		enchantIDs: map[int32]bool{},
	}
	for _, e := range database.EnchantOverrides {
		s.enchantIDs[e.EffectId] = true
	}
	return s
}

func isCustomID(cfg *cc.Config, s *stockData, id uint32) bool {
	for _, x := range cfg.IncludeItems {
		if x == id {
			return true
		}
	}
	// With an explicit custom ID range, only that range is custom: stock 3.3.5a items that wowhead's
	// Classic data lacks (test/unused items) must not be mistaken for custom content.
	if cfg.CustomItemIDMin > 0 {
		return id >= cfg.CustomItemIDMin
	}
	return !s.itemIDs[int32(id)]
}

// scanHandwritten finds item IDs and set names that hand-written sim code already implements,
// so generated code never duplicates them.
type handwritten struct {
	itemIDs  map[int32]bool
	literals map[string]bool
}

// Stock item IDs are < 100000, so any larger integer literal in hand-written sim code refers to
// custom content (e.g. `const MorgrathsRavagingClawItemID = 900115`).
var itemIDPatterns = regexp.MustCompile(`(?:NewItemEffect|NewSimpleStat\w*Effect\w*|NewItemEffectWithHeroic|NewSimpleStatItemActiveEffect)\(\s*(\d+)|\b(?:ID|ItemID):\s*(\d+)|\b([1-9]\d{5,6})\b`)
var stringLiteral = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

func scanHandwritten(simDir, genFile string) *handwritten {
	h := &handwritten{itemIDs: map[int32]bool{}, literals: map[string]bool{}}
	genAbs, _ := filepath.Abs(genFile)
	filepath.WalkDir(simDir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".pb.go") {
			return nil
		}
		if abs, _ := filepath.Abs(p); abs == genAbs {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, m := range itemIDPatterns.FindAllStringSubmatch(string(data), -1) {
			idStr := m[1] + m[2] + m[3]
			if id, err := strconv.Atoi(idStr); err == nil {
				h.itemIDs[int32(id)] = true
			}
		}
		for _, m := range stringLiteral.FindAllStringSubmatch(string(data), -1) {
			h.literals[m[1]] = true
		}
		return nil
	})
	return h
}

// ---------------------------------------------------------------------------
// Extraction
// ---------------------------------------------------------------------------

type effectEntry struct {
	convert.Issue
	Generated bool   `json:"generated,omitempty"`
	Handled   bool   `json:"handledByHandwrittenCode,omitempty"`
	Text      string `json:"text,omitempty"`
}

type setEntry struct {
	ID          uint32     `json:"id"`
	Name        string     `json:"name"`
	ItemIDs     []uint32   `json:"itemIds"`
	Bonuses     []setBonus `json:"bonuses"`
	Stock       bool       `json:"stockSet,omitempty"`
	Generated   bool       `json:"generated,omitempty"`
	Handwritten bool       `json:"handwritten,omitempty"`
}

type setBonus struct {
	Pieces  uint32    `json:"pieces"`
	SpellID uint32    `json:"spellId"`
	Name    string    `json:"name"`
	Text    string    `json:"text"`
	Stats   []float64 `json:"stats,omitempty"` // set when the bonus is plain stats
	Reason  string    `json:"needsCode,omitempty"`
}

type spellExport struct {
	*dbc.Spell
	ServerProc  *server.SpellProc  `json:"serverProc,omitempty"`
	ServerBonus *server.SpellBonus `json:"serverBonus,omitempty"`
}

type stockReview struct {
	ID     int32  `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type extraction struct {
	in          *cc.Inputs
	src         *convert.Source
	cfg         *cc.Config
	stock       *stockData
	handwritten *handwritten

	customDB     *database.WowDatabase
	stockDB      *database.WowDatabase // server values for stock items (only fully-understood ones)
	stockReview  []stockReview         // stock items whose server values couldn't be fully converted
	serverIDs    []int32
	effects      []effectEntry
	sets         []*setEntry
	spellRefs    map[uint32]bool
	gen          []string // generated Go statements
	tooltips     Tooltips
	customSpells []map[string]any
	spellChanges []map[string]any
}

func (ex *extraction) excluded(id uint32) bool {
	for _, x := range ex.cfg.ExcludeItems {
		if x == id {
			return true
		}
	}
	return false
}

func (ex *extraction) refSpell(id uint32, depth int) {
	if id == 0 || depth > 3 || ex.spellRefs[id] {
		return
	}
	ex.spellRefs[id] = true
	if sp := ex.src.Spells.Spells[id]; sp != nil {
		for _, e := range sp.Effects {
			ex.refSpell(e.TriggerSpell, depth+1)
		}
	}
}

func (ex *extraction) spellText(id uint32) string {
	sp := ex.src.Spells.Spells[id]
	if sp == nil {
		return ""
	}
	return sp.Text
}

func (ex *extraction) run() {
	items := ex.src.Server.Items
	ids := make([]uint32, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	setIDs := map[uint32]bool{}
	scrollItems := map[uint32]int32{} // enchant spell -> scroll/kit item id
	for _, id := range ids {
		it := items[id]
		if ex.excluded(id) {
			continue
		}
		custom := isCustomID(ex.cfg, ex.stock, id)
		switch {
		case convert.IsEquippable(it):
			ex.serverIDs = append(ex.serverIDs, int32(id))
			item, issues := ex.src.ToUIItem(it, ex.cfg.Phase)
			if custom {
				ex.customDB.MergeItem(item)
				ex.addItemEffects(it, issues)
				if it.ItemSet != 0 {
					setIDs[it.ItemSet] = true
				}
			} else if ex.stock.itemIDs[int32(id)] {
				ex.considerStock(item.Id, item.Name, issues, func() { ex.stockDB.MergeItem(slimStockItem(item)) })
			}
		case convert.IsGem(it):
			ex.serverIDs = append(ex.serverIDs, int32(id))
			gem, issues := ex.src.ToUIGem(it, ex.cfg.Phase)
			if gem == nil {
				ex.addIssues(issues)
				continue
			}
			if custom {
				ex.customDB.MergeGem(gem)
				ex.addIssues(issues)
				if gp := ex.src.Tables.GemProperties[it.GemProperties]; gp != nil {
					if ench := ex.src.Tables.Enchants[gp.EnchantID]; ench != nil {
						for i := 0; i < 3; i++ {
							if ench.Type[i] == dbc.EnchantTypeEquipSpell || ench.Type[i] == dbc.EnchantTypeProcSpell || ench.Type[i] == dbc.EnchantTypeUseSpell {
								ex.refSpell(ench.Arg[i], 0)
							}
						}
					}
				}
			} else if ex.stock.itemIDs[int32(id)] {
				ex.considerStock(gem.Id, gem.Name, issues, func() {
					ex.stockDB.MergeGem(&proto.UIGem{Id: gem.Id, Name: gem.Name, Color: gem.Color, Stats: gem.Stats})
				})
			}
		case it.Class == 0 && it.SubClass == 6: // item enhancements (scrolls, armor kits, spellthreads)
			for _, sp := range it.Spells {
				if custom || !ex.stock.spellIDs[int32(sp.SpellID)] {
					scrollItems[sp.SpellID] = int32(id)
				}
			}
		}
	}

	ex.extractEnchants(scrollItems)
	ex.extractSets(setIDs)
	for _, id := range ex.cfg.ExtraSpells {
		ex.refSpell(id, 0)
	}
	ex.indexCustomSpells()
	ex.diffBaseline()
	ex.buildTooltips()
}

// Tooltips holds in-game style tooltip HTML for custom items and spells, keyed by ID.
type Tooltips struct {
	Items  map[int32]string `json:"items"`
	Spells map[int32]string `json:"spells"`
	// In-game texts (SpellItemEnchantment names) for socketed gems (by gem item ID) and enchants (by
	// effect ID), shown inside custom item tooltips.
	Gems     map[int32]string `json:"gems"`
	Enchants map[int32]string `json:"enchants"`
}

// buildTooltips renders tooltips for custom items/gems and for custom spells the UI can show:
// spells referenced by custom content, plus custom spell IDs used in hand-written sim code
// (e.g. proc spells that appear in the results tables).
func (ex *extraction) buildTooltips() {
	ex.tooltips = Tooltips{Items: map[int32]string{}, Spells: map[int32]string{}, Gems: map[int32]string{}, Enchants: map[int32]string{}}
	for id, it := range ex.src.Server.Items {
		if !convert.IsGem(it) {
			continue
		}
		if gp := ex.src.Tables.GemProperties[it.GemProperties]; gp != nil {
			if e := ex.src.Tables.Enchants[gp.EnchantID]; e != nil && e.Name != "" {
				ex.tooltips.Gems[int32(id)] = e.Name
			}
		}
	}
	enchantIDs := map[int32]bool{}
	for id := range ex.stock.enchantIDs {
		enchantIDs[id] = true
	}
	for key := range ex.customDB.Enchants {
		enchantIDs[key.EffectID] = true
	}
	for id := range enchantIDs {
		if e := ex.src.Tables.Enchants[uint32(id)]; e != nil && e.Name != "" {
			ex.tooltips.Enchants[id] = e.Name
		}
	}

	for id := range ex.customDB.Items {
		if it := ex.src.Server.Items[uint32(id)]; it != nil {
			ex.tooltips.Items[id] = ex.src.ItemTooltipHTML(it)
		}
	}
	for id := range ex.customDB.Gems {
		if it := ex.src.Server.Items[uint32(id)]; it != nil {
			ex.tooltips.Items[id] = ex.src.ItemTooltipHTML(it)
		}
	}

	spellIDs := map[uint32]bool{}
	for id := range ex.spellRefs {
		spellIDs[id] = true
	}
	for id := range ex.handwritten.itemIDs {
		spellIDs[uint32(id)] = true
	}
	for id := range spellIDs {
		sp := ex.src.Spells.Spells[id]
		if sp == nil || ex.stock.spellIDs[int32(id)] {
			continue
		}
		ex.tooltips.Spells[int32(id)] = ex.src.SpellTooltipHTML(sp)
		icon := sp.Icon
		if icon == "" {
			icon = "inv_misc_questionmark"
		}
		ex.customDB.SpellIcons[int32(id)] = &proto.IconData{Id: int32(id), Name: sp.Name, Icon: icon}
	}
}

func (ex *extraction) addIssues(issues []convert.Issue) {
	for _, is := range issues {
		ex.effects = append(ex.effects, effectEntry{Issue: is, Text: ex.spellText(is.SpellID)})
	}
}

// considerStock queues server values for a stock item unless some effect wasn't understood,
// in which case the stats would be incomplete and the item is listed for review instead.
func (ex *extraction) considerStock(id int32, name string, issues []convert.Issue, accept func()) {
	// Procs, dummies and spell modifiers never count as item stats (not in wowhead's values either),
	// so only unresolvable data makes the server values untrustworthy.
	for _, is := range issues {
		if strings.Contains(is.Detail, "spell not found") || strings.Contains(is.Detail, "unknown ITEM_MOD") ||
			strings.Contains(is.Detail, "not in SpellItemEnchantment.dbc") || strings.Contains(is.Detail, "socket bonus:") {
			ex.stockReview = append(ex.stockReview, stockReview{ID: id, Name: name, Reason: is.Detail})
			return
		}
	}
	accept()
}

// slimStockItem keeps only the fields gen_db applies to stock items (see CustomContent.Apply).
func slimStockItem(item *proto.UIItem) *proto.UIItem {
	return &proto.UIItem{
		Id: item.Id, Name: item.Name, Stats: item.Stats, GemSockets: item.GemSockets, SocketBonus: item.SocketBonus,
		WeaponDamageMin: item.WeaponDamageMin, WeaponDamageMax: item.WeaponDamageMax, WeaponSpeed: item.WeaponSpeed,
		Ilvl: item.Ilvl, Quality: item.Quality,
	}
}

// isStockSet reports whether an item set contains any stock item (its bonuses are upstream code).
func (ex *extraction) isStockSet(set *dbc.ItemSet) bool {
	for _, id := range set.ItemIDs {
		if !isCustomID(ex.cfg, ex.stock, id) {
			return true
		}
	}
	return false
}

// addItemEffects records effects needing code and generates the ones expressible from data.
func (ex *extraction) addItemEffects(it *server.ItemTemplate, issues []convert.Issue) {
	var codeIssues []int
	for i, is := range issues {
		if is.Kind == "item-spell" {
			ex.refSpell(is.SpellID, 0)
			codeIssues = append(codeIssues, i)
		}
	}
	handled := ex.handwritten.itemIDs[int32(it.Entry)]
	generated := false
	// One NewItemEffect per item: only auto-generate when there is exactly one effect to implement.
	if len(codeIssues) == 1 && !handled {
		generated = ex.generateItemEffect(it, issues[codeIssues[0]])
	}
	for _, is := range issues {
		e := effectEntry{Issue: is, Text: ex.spellText(is.SpellID)}
		if is.Kind == "item-spell" {
			e.Generated = generated
			e.Handled = handled
		}
		ex.effects = append(ex.effects, e)
	}
}

func (ex *extraction) itemSpell(it *server.ItemTemplate, spellID uint32) server.ItemSpell {
	for _, s := range it.Spells {
		if s.SpellID == spellID {
			return s
		}
	}
	return server.ItemSpell{}
}

var defensiveStats = []stats.Stat{stats.Armor, stats.BonusArmor, stats.Defense, stats.Dodge, stats.Parry, stats.Block,
	stats.BlockValue, stats.Health, stats.Resilience}

func isDefensive(s stats.Stats) bool {
	def, off := false, false
	for i, v := range s {
		if v == 0 {
			continue
		}
		isDef := false
		for _, d := range defensiveStats {
			if stats.Stat(i) == d {
				isDef = true
			}
		}
		if isDef || stats.Stat(i) == stats.Stamina {
			def = true
		} else {
			off = true
		}
	}
	return def && !off
}

func (ex *extraction) generateItemEffect(it *server.ItemTemplate, is convert.Issue) bool {
	sp := ex.src.Spells.Spells[is.SpellID]
	if sp == nil {
		return false
	}
	isp := ex.itemSpell(it, is.SpellID)
	comment := fmt.Sprintf("// %d %s: %s", it.Entry, it.Name, oneLine(sp.Text))

	switch is.Trigger {
	case "use":
		bonus, ok, _ := convert.SpellStats(sp)
		if !ok || sp.DurationMs <= 0 {
			return false
		}
		cd := int32(sp.RecoveryTime)
		if isp.Cooldown > 0 {
			cd = isp.Cooldown
		}
		trinket := it.InventoryType == 12
		ex.gen = append(ex.gen, comment, fmt.Sprintf("RegisterUseStat(%d, %s, %s, %s, %t, %t)",
			it.Entry, statsLiteral(bonus), durationLiteral(sp.DurationMs), durationLiteral(cd), trinket, isDefensive(bonus)))
		return true

	case "equip", "chance-on-hit":
		var buff *dbc.Spell
		var procFlags, chance uint32
		if is.Trigger == "equip" {
			// Expect exactly one PROC_TRIGGER_SPELL effect pointing at a stat buff.
			var trig uint32
			for _, e := range sp.Effects {
				if e.Effect == 0 {
					continue
				}
				if e.Effect != dbc.EffectApplyAura || e.Aura != dbc.AuraProcTriggerSpell || trig != 0 {
					return false
				}
				// Procs limited to specific class spells ("each time you cast Holy Shield") need code.
				if e.SpellClassMask != [3]uint32{} {
					return false
				}
				trig = e.TriggerSpell
			}
			buff = ex.src.Spells.Spells[trig]
			procFlags, chance = sp.ProcFlags, sp.ProcChance
		} else {
			buff = sp              // chance-on-hit item spells are applied directly
			procFlags = 0x4 | 0x10 // melee auto attacks + melee specials
			chance = sp.ProcChance
		}
		if buff == nil || buff.DurationMs <= 0 {
			return false
		}
		bonus, ok, _ := convert.SpellStats(buff)
		if !ok || bonus == (stats.Stats{}) {
			return false
		}
		ppm := isp.PPMRate
		var icd int32
		var hitMask uint32
		if proc := ex.src.Server.SpellProcs[sp.ID]; proc != nil {
			if proc.SpellFamilyMask != [3]uint32{} {
				return false
			}
			if proc.ProcFlags != 0 {
				procFlags = proc.ProcFlags
			}
			if proc.Chance > 0 {
				chance = uint32(proc.Chance)
			}
			if proc.PPM > 0 {
				ppm = proc.PPM
			}
			icd = proc.CooldownMs
			hitMask = proc.HitMask
		}
		if procFlags == 0 {
			return false
		}
		procChance := float64(chance) / 100
		if ppm > 0 {
			procChance = 0
		}
		stacks := int32(1)
		if buff.StackAmount > 1 {
			stacks = int32(buff.StackAmount)
		}
		ex.gen = append(ex.gen, comment, fmt.Sprintf(`RegisterProcStat(ProcStat{ItemID: %d, Name: %q, AuraSpellID: %d, Bonus: %s, Duration: %s, MaxStacks: %d, ProcFlags: 0x%x, HitMask: 0x%x, ProcChance: %s, PPM: %s, ICD: %s})`,
			it.Entry, it.Name, buff.ID, statsLiteral(bonus), durationLiteral(buff.DurationMs), stacks, procFlags, hitMask,
			strconv.FormatFloat(procChance, 'f', -1, 64), strconv.FormatFloat(ppm, 'f', -1, 64), durationLiteral(icd)))
		if icd == 0 {
			ex.gen = append(ex.gen, "// NOTE: no internal cooldown found in spell_proc/spell_proc_event for this proc.")
		}
		return true
	}
	return false
}

func (ex *extraction) extractEnchants(scrollItems map[uint32]int32) {
	ids := make([]uint32, 0)
	for id, sp := range ex.src.Spells.Spells {
		if convert.EnchantSpellEffect(sp) == 0 {
			continue
		}
		_, fromScroll := scrollItems[id]
		if !fromScroll && ex.stock.spellIDs[int32(id)] {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		sp := ex.src.Spells.Spells[id]
		effectID := int32(convert.EnchantSpellEffect(sp))
		if ex.stock.enchantIDs[effectID] {
			continue
		}
		ench, issues := ex.src.ToUIEnchant(sp, scrollItems[id], ex.cfg.Phase)
		if ench == nil {
			ex.addIssues(issues)
			continue
		}
		if e := ex.src.Tables.Enchants[uint32(effectID)]; e != nil {
			if e.RequiredSkill == 333 {
				ench.RequiredProfession = proto.Profession_Enchanting
			}
			for i := 0; i < 3; i++ {
				if e.Type[i] == dbc.EnchantTypeEquipSpell || e.Type[i] == dbc.EnchantTypeProcSpell || e.Type[i] == dbc.EnchantTypeUseSpell {
					ex.refSpell(e.Arg[i], 0)
				}
			}
		}
		ex.customDB.MergeEnchant(ench)
		ex.addIssues(issues)
	}
}

func (ex *extraction) extractSets(setIDs map[uint32]bool) {
	ids := make([]uint32, 0, len(setIDs))
	for id := range setIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		set := ex.src.Tables.ItemSets[id]
		if set == nil {
			ex.effects = append(ex.effects, effectEntry{Issue: convert.Issue{Kind: "warning", OwnerID: int32(id), Detail: fmt.Sprintf("item set %d not in ItemSet.dbc", id)}})
			continue
		}
		entry := &setEntry{ID: id, Name: set.Name, ItemIDs: set.ItemIDs, Stock: ex.isStockSet(set),
			Handwritten: ex.handwritten.literals[set.Name]}
		allStats := true
		statBonuses := map[uint32]stats.Stats{}
		for _, b := range set.Bonuses {
			ex.refSpell(b.SpellID, 0)
			sp := ex.src.Spells.Spells[b.SpellID]
			bonus := setBonus{Pieces: b.Pieces, SpellID: b.SpellID, Name: spellNameOf(sp), Text: ex.spellText(b.SpellID)}
			s, ok, why := convert.SpellStats(sp)
			if ok {
				bonus.Stats = s[:]
				statBonuses[b.Pieces] = statBonuses[b.Pieces].Add(s)
			} else {
				bonus.Reason = why
				allStats = false
			}
			entry.Bonuses = append(entry.Bonuses, bonus)
		}
		if allStats && !entry.Stock && !entry.Handwritten && len(statBonuses) > 0 {
			entry.Generated = true
			pieces := make([]int, 0, len(statBonuses))
			for p := range statBonuses {
				pieces = append(pieces, int(p))
			}
			sort.Ints(pieces)
			var sb strings.Builder
			fmt.Fprintf(&sb, "// Item set %d\nRegisterStatSet(%q, map[int32]stats.Stats{\n", id, set.Name)
			for _, p := range pieces {
				fmt.Fprintf(&sb, "%d: %s,\n", p, statsLiteral(statBonuses[uint32(p)]))
			}
			sb.WriteString("})")
			ex.gen = append(ex.gen, sb.String())
		}
		ex.sets = append(ex.sets, entry)
	}
}

func spellNameOf(sp *dbc.Spell) string {
	if sp == nil {
		return "(missing)"
	}
	return sp.Name
}

func (ex *extraction) indexCustomSpells() {
	ids := make([]uint32, 0)
	for id := range ex.src.Spells.Spells {
		if !ex.stock.spellIDs[int32(id)] {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		sp := ex.src.Spells.Spells[id]
		entry := map[string]any{"id": id, "name": sp.Name}
		if sp.Rank != "" {
			entry["rank"] = sp.Rank
		}
		if sp.SpellFamilyName != 0 {
			entry["family"] = sp.SpellFamilyName
		}
		if sp.Text != "" {
			entry["text"] = sp.Text
		}
		ex.customSpells = append(ex.customSpells, entry)
	}
}

// diffBaseline compares the custom Spell.dbc against stock 3.3.5a DBCs to surface tuned spells.
func (ex *extraction) diffBaseline() {
	base := ex.in.Baseline
	if base == nil {
		return
	}
	ids := make([]uint32, 0)
	for id := range ex.src.Spells.Spells {
		if _, ok := base.Spells[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		a, b := base.Spells[id], ex.src.Spells.Spells[id]
		changes := diffSpell(a, b)
		if len(changes) == 0 {
			continue
		}
		ex.spellChanges = append(ex.spellChanges, map[string]any{
			"id": id, "name": b.Name, "rank": b.Rank, "family": b.SpellFamilyName, "changes": changes,
			"oldText": a.Text, "newText": b.Text,
		})
		if b.SpellFamilyName != 0 {
			ex.refSpell(id, 3)
		}
	}
}

func diffSpell(a, b *dbc.Spell) []string {
	var out []string
	cmp := func(name string, x, y any) {
		if fmt.Sprint(x) != fmt.Sprint(y) {
			out = append(out, fmt.Sprintf("%s: %v -> %v", name, x, y))
		}
	}
	cmp("name", a.Name, b.Name)
	cmp("description", a.Description, b.Description)
	cmp("attributes", a.Attributes, b.Attributes)
	cmp("castTimeIndex", a.CastingTimeIndex, b.CastingTimeIndex)
	cmp("recoveryMs", a.RecoveryTime, b.RecoveryTime)
	cmp("categoryRecoveryMs", a.CategoryRecoveryTime, b.CategoryRecoveryTime)
	cmp("gcdMs", a.StartRecoveryTime, b.StartRecoveryTime)
	cmp("durationMs", a.DurationMs, b.DurationMs)
	cmp("procFlags", a.ProcFlags, b.ProcFlags)
	cmp("procChance", a.ProcChance, b.ProcChance)
	cmp("procCharges", a.ProcCharges, b.ProcCharges)
	cmp("manaCost", a.ManaCost, b.ManaCost)
	cmp("manaCostPct", a.ManaCostPercentage, b.ManaCostPercentage)
	cmp("stackAmount", a.StackAmount, b.StackAmount)
	cmp("rangeIndex", a.RangeIndex, b.RangeIndex)
	cmp("schoolMask", a.SchoolMask, b.SchoolMask)
	cmp("spellFamilyFlags", a.SpellFamilyFlags, b.SpellFamilyFlags)
	cmp("maxAffectedTargets", a.MaxAffectedTargets, b.MaxAffectedTargets)
	for i := 0; i < 3; i++ {
		ea, eb := a.Effects[i], b.Effects[i]
		p := fmt.Sprintf("effect%d.", i+1)
		cmp(p+"effect", ea.EffectName, eb.EffectName)
		cmp(p+"aura", ea.AuraName, eb.AuraName)
		cmp(p+"basePoints", ea.BasePoints, eb.BasePoints)
		cmp(p+"dieSides", ea.DieSides, eb.DieSides)
		cmp(p+"pointsPerComboPoint", ea.PointsPerComboPoint, eb.PointsPerComboPoint)
		cmp(p+"amplitude", ea.Amplitude, eb.Amplitude)
		cmp(p+"miscValue", ea.MiscValue, eb.MiscValue)
		cmp(p+"triggerSpell", ea.TriggerSpell, eb.TriggerSpell)
		cmp(p+"spellClassMask", ea.SpellClassMask, eb.SpellClassMask)
		cmp(p+"damageMultiplier", ea.DamageMultiplier, eb.DamageMultiplier)
		cmp(p+"bonusMultiplier", ea.BonusMultiplier, eb.BonusMultiplier)
		cmp(p+"valueMultiplier", ea.ValueMultiplier, eb.ValueMultiplier)
		cmp(p+"chainTarget", ea.ChainTarget, eb.ChainTarget)
		cmp(p+"radiusIndex", ea.RadiusIndex, eb.RadiusIndex)
	}
	return out
}

func (ex *extraction) generatedCount() int {
	n := 0
	for _, e := range ex.effects {
		if e.Generated {
			n++
		}
	}
	for _, s := range ex.sets {
		if s.Generated {
			n++
		}
	}
	return n
}

func (ex *extraction) todoCount() int {
	n := 0
	for _, e := range ex.effects {
		if e.Kind != "warning" && !e.Generated && !e.Handled {
			n++
		}
	}
	for _, s := range ex.sets {
		if !s.Generated && !s.Stock && !s.Handwritten {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

// simSpell is the family data of a custom spell that hand-written sim code uses. Class talents,
// glyphs, buffs, debuffs and procs reach a spell only through its family flags, so this is what the
// sim may apply to it; sim/common/cc/ccfamily checks the registered spells against it.
type simSpell struct {
	ID               uint32              `json:"id"`
	Name             string              `json:"name"`
	SpellFamilyName  uint32              `json:"spellFamilyName"`
	SpellFamilyFlags [3]uint32           `json:"spellFamilyFlags"`
	SchoolMask       uint32              `json:"schoolMask"`
	DmgClass         uint32              `json:"dmgClass"`
	Bleed            bool                `json:"bleed"`
	ClassModifiers   []dbc.ClassModifier `json:"classModifiers"`
}

// customSpellIDMin: stock 3.3.5a spell IDs stay below 100000; the server's custom spells use 200000+.
const customSpellIDMin = 200000

func (ex *extraction) simSpells() []simSpell {
	out := []simSpell{}
	for id := range ex.handwritten.itemIDs {
		sp := ex.src.Spells.Spells[uint32(id)]
		if id < customSpellIDMin || sp == nil {
			continue
		}
		mods := dbc.ClassModifiers(ex.src.Spells.Spells, sp)
		for procID, p := range ex.src.Server.SpellProcs {
			if dbc.FamilyMaskMatches(p.SpellFamilyName, p.SpellFamilyMask, sp) {
				name := ""
				if other := ex.src.Spells.Spells[procID]; other != nil {
					name = other.Name
				}
				mods = append(mods, dbc.ClassModifier{SpellID: procID, Name: name, Kind: "class proc", Effect: p.Source})
			}
		}
		sort.SliceStable(mods, func(i, j int) bool { return mods[i].SpellID < mods[j].SpellID })
		if mods == nil {
			mods = []dbc.ClassModifier{}
		}
		out = append(out, simSpell{ID: sp.ID, Name: sp.Name, SpellFamilyName: sp.SpellFamilyName,
			SpellFamilyFlags: sp.SpellFamilyFlags, SchoolMask: sp.SchoolMask, DmgClass: sp.DmgClass,
			Bleed: sp.IsBleed(), ClassModifiers: mods})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (ex *extraction) write(outDir, genFile string) (err error) {
	must := func(e error) {
		if e != nil && err == nil {
			err = e
		}
	}
	ex.customDB.WriteJson(filepath.Join(outDir, "cc_db.json"))
	ex.stockDB.WriteJson(filepath.Join(outDir, "stock_overrides.json"))

	serverIDsPath := filepath.Join(outDir, "server_item_ids.json")
	if ex.src.Server.CompleteItemList && ex.cfg.RestrictItems() {
		sort.Slice(ex.serverIDs, func(i, j int) bool { return ex.serverIDs[i] < ex.serverIDs[j] })
		must(cc.WriteJSON(serverIDsPath, ex.serverIDs))
	} else {
		os.Remove(serverIDsPath)
	}
	must(cc.WriteJSON(filepath.Join(outDir, "settings.json"), map[string]any{
		"applyStockItemChanges": ex.cfg.ApplyStock(),
	}))

	spellIDs := make([]uint32, 0, len(ex.spellRefs))
	for id := range ex.spellRefs {
		spellIDs = append(spellIDs, id)
	}
	sort.Slice(spellIDs, func(i, j int) bool { return spellIDs[i] < spellIDs[j] })
	spells := make([]spellExport, 0, len(spellIDs))
	for _, id := range spellIDs {
		if sp := ex.src.Spells.Spells[id]; sp != nil {
			spells = append(spells, spellExport{Spell: sp, ServerProc: ex.src.Server.SpellProcs[id], ServerBonus: ex.src.Server.SpellBonus[id]})
		}
	}
	must(cc.WriteJSON(filepath.Join(outDir, "spells.json"), spells))
	must(cc.WriteJSON(filepath.Join(outDir, "sim_spells.json"), ex.simSpells()))
	must(cc.WriteJSON(filepath.Join(outDir, "item_sets.json"), ex.sets))
	must(cc.WriteJSON(filepath.Join(outDir, "effects.json"), ex.effects))
	must(cc.WriteJSON(filepath.Join(outDir, "custom_spells.json"), ex.customSpells))
	must(cc.WriteJSON(filepath.Join(outDir, "tooltips.json"), ex.tooltips))
	if ex.in.Baseline != nil {
		must(cc.WriteJSON(filepath.Join(outDir, "spell_changes.json"), ex.spellChanges))
	}
	must(os.WriteFile(filepath.Join(outDir, "REPORT.md"), []byte(ex.report()), 0666))
	must(writeGenerated(genFile, ex.gen))
	return err
}

func (ex *extraction) report() string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	p("# Custom content import report")
	p("")
	p("Generated by `./build.ps1 cc` (tools/cc/extract). Do not edit; re-run the import instead.")
	p("")
	p("## Inputs")
	if len(ex.in.Loaded) == 0 {
		p("- (nothing found in `%s/`)", filepath.ToSlash(ex.in.Dir))
	}
	for _, l := range ex.in.Loaded {
		p("- %s", l)
	}
	p("")
	p("## Summary")
	p("| | Count |")
	p("|---|---|")
	p("| Custom items | %d |", len(ex.customDB.Items))
	p("| Custom gems | %d |", len(ex.customDB.Gems))
	p("| Custom enchants | %d |", len(ex.customDB.Enchants))
	p("| Custom item sets | %d |", len(ex.sets))
	p("| Stock items/gems with server values | %d / %d |", len(ex.stockDB.Items), len(ex.stockDB.Gems))
	p("| Effects generated automatically | %d |", ex.generatedCount())
	p("| Effects needing hand-written code | %d |", ex.todoCount())
	if ex.src.Server.CompleteItemList && ex.cfg.RestrictItems() {
		p("| Server item IDs (stock items not in this list are hidden) | %d |", len(ex.serverIDs))
	}
	p("")

	p("## Needs implementation")
	p("Implement these in `sim/common/cc/` (hand-written files; see `sim/common/cc/cc.go`). Decoded spell data is in `spells.json`; inspect with `go run ./tools/cc/inspect spell <id>`.")
	p("")
	found := false
	for _, e := range ex.effects {
		if e.Kind == "warning" || e.Generated || e.Handled {
			continue
		}
		found = true
		trigger := ""
		if e.Trigger != "" {
			trigger = " [" + e.Trigger + "]"
		}
		spell := ""
		if e.SpellID != 0 {
			spell = fmt.Sprintf(" spell %d", e.SpellID)
		}
		p("- **%s** %d %s%s%s: %s", e.Kind, e.OwnerID, e.Owner, trigger, spell, e.Detail)
		if e.Text != "" {
			p("  - _%s_", oneLine(e.Text))
		}
		if proc := ex.src.Server.SpellProcs[e.SpellID]; proc != nil {
			p("  - server proc: flags 0x%x, hitMask 0x%x, chance %.1f, ppm %.2f, cooldown %dms", proc.ProcFlags, proc.HitMask, proc.Chance, proc.PPM, proc.CooldownMs)
		}
	}
	for _, s := range ex.sets {
		if s.Generated || s.Stock || s.Handwritten {
			continue
		}
		found = true
		p("- **set-bonus** %d %s", s.ID, s.Name)
		for _, bn := range s.Bonuses {
			status := "stats (auto)"
			if bn.Reason != "" {
				status = "needs code: " + bn.Reason
			}
			p("  - %dpc: spell %d %s: _%s_ (%s)", bn.Pieces, bn.SpellID, bn.Name, oneLine(bn.Text), status)
		}
	}
	if !found {
		p("Nothing.")
	}
	p("")

	p("## Generated automatically")
	p("See `sim/common/cc/zz_generated.go`.")
	for _, s := range ex.gen {
		if strings.HasPrefix(s, "// ") && !strings.HasPrefix(s, "// NOTE") && !strings.HasPrefix(s, "// Item set") {
			p("- %s", strings.TrimPrefix(s, "// "))
		}
	}
	for _, s := range ex.sets {
		if s.Generated {
			p("- set %s (%d)", s.Name, s.ID)
		}
	}
	p("")

	if len(ex.stockReview) > 0 {
		p("## Stock items kept at wowhead values")
		p("The server has these stock items, but some of their stats couldn't be converted, so the wowhead values are kept. Check them by hand.")
		for _, r := range ex.stockReview {
			p("- %d %s: %s", r.ID, r.Name, r.Reason)
		}
		p("")
	}

	if ex.in.Baseline != nil {
		p("## Spells changed vs. stock 3.3.5a (%d)", len(ex.spellChanges))
		p("Full details in `spell_changes.json`. Class spells (spell family ≠ 0) are listed here.")
		for _, c := range ex.spellChanges {
			if c["family"].(uint32) == 0 {
				continue
			}
			p("- %d %s %s: %s", c["id"], c["name"], c["rank"], strings.Join(c["changes"].([]string), "; "))
		}
		p("")
	}

	var warnings []string
	warnings = append(warnings, ex.src.Server.Warnings...)
	for _, e := range ex.effects {
		if e.Kind == "warning" {
			warnings = append(warnings, fmt.Sprintf("%d %s: %s", e.OwnerID, e.Owner, e.Detail))
		}
	}
	if len(warnings) > 0 {
		p("## Warnings")
		for _, w := range warnings {
			p("- %s", w)
		}
	}
	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
