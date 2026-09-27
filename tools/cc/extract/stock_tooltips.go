package main

// Tooltips for stock items. The UI shows wowhead tooltips for stock items, and wowhead only has
// Wrath Classic data, which retuned many items (e.g. the Ulduar trinket procs). Stock items whose
// 3.3.5a values (server item_template, Spell.dbc) differ from wowhead's get an in-game style tooltip
// in tooltips.json like custom items; everything else keeps wowhead's.

import (
	"fmt"
	"html"
	"log"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/tools"
	"github.com/wowsims/wotlk/tools/cc/server"
	"github.com/wowsims/wotlk/tools/database"
)

// stockCandidate is a stock item or gem the server has, with its server values converted.
type stockCandidate struct {
	it   *server.ItemTemplate
	item *proto.UIItem // nil for gems
	gem  *proto.UIGem  // nil for items
}

type stockTooltipChange struct {
	ID      int32
	Name    string
	Changes []string
	Skipped string // why the wowhead tooltip was kept anyway
}

// Spell lines in a wowhead item tooltip: `Equip: <a href="/wotlk/spell=64786/comets-trail" ...>text</a>`,
// also "Use:", "Chance on hit:" and set bonuses ("(2) Set : <a ...>").
var wowheadSpellLine = regexp.MustCompile(`(?:Equip|Use|Chance on hit|\(\d\) Set)\s*:\s*<a href="/wotlk/spell=(\d+)[^"]*"[^>]*>(.*?)</a>`)
var htmlTag = regexp.MustCompile(`<[^>]*>`)

// Annotations wowhead adds to spell text that aren't part of the in-game description.
var wowheadAnnotation = regexp.MustCompile(`\s*\((?:Proc chance|[\d.]+ procs? per minute)[^)]*\)`)
var number = regexp.MustCompile(`\d+(?:\.\d+)?`)

func wowheadSpellTexts(tooltip string) map[uint32]string {
	out := map[uint32]string{}
	for _, m := range wowheadSpellLine.FindAllStringSubmatch(tooltip, -1) {
		var id uint32
		fmt.Sscan(m[1], &id)
		text := html.UnescapeString(htmlTag.ReplaceAllString(m[2], ""))
		out[id] = oneLine(wowheadAnnotation.ReplaceAllString(text, ""))
	}
	return out
}

// tooltipSpellTexts returns the 3.3.5a texts of the spell lines ItemTooltipHTML renders.
func (ex *extraction) tooltipSpellTexts(it *server.ItemTemplate) map[uint32]string {
	out := map[uint32]string{}
	for _, s := range it.Spells {
		switch s.Trigger {
		case server.SpellTriggerUse, server.SpellTriggerUseNoDelay, server.SpellTriggerEquip, server.SpellTriggerChanceHit:
			out[s.SpellID] = oneLine(ex.spellText(s.SpellID))
		}
	}
	if set := ex.src.Tables.ItemSets[it.ItemSet]; set != nil && it.ItemSet != 0 {
		for _, b := range set.Bonuses {
			out[b.SpellID] = oneLine(ex.spellText(b.SpellID))
		}
	}
	return out
}

// sameValues compares the numbers in two spell texts, so rewording alone doesn't count as a change.
func sameValues(a, b string) bool {
	return slices.Equal(number.FindAllString(a, -1), number.FindAllString(b, -1))
}

func (ex *extraction) buildStockTooltips() {
	for _, path := range []string{ex.stock.tooltipsPath, ex.stock.plannerPath} {
		if _, err := os.Stat(path); err != nil {
			log.Printf("warning: %v (stock item tooltips are not compared)", err)
			return
		}
	}
	responses := database.NewWowheadItemTooltipManager(ex.stock.tooltipsPath).Read()
	planner := database.ParseWowheadDB(tools.ReadFile(ex.stock.plannerPath))
	for _, c := range ex.stockCandidates {
		id := int32(c.it.Entry)
		wh, ok := responses[id]
		if !ok {
			continue
		}
		// Same rule as gen_db: items not in the gear planner never make it into the sim's database.
		if _, listed := planner.Items[strconv.Itoa(int(id))]; c.item != nil && !listed {
			continue
		}
		ch := stockTooltipChange{ID: id, Name: c.it.Name}
		if c.item != nil {
			ch.Changes = database.DiffItem(slimStockItem(wh.ToItemProto()), slimStockItem(c.item))
		} else if d := database.DiffStats(wh.ToGemProto().Stats, c.gem.Stats); d != "" {
			ch.Changes = []string{d}
		}

		whTexts := wowheadSpellTexts(wh.Tooltip)
		ourTexts := ex.tooltipSpellTexts(c.it)
		spellIDs := make([]uint32, 0, len(ourTexts))
		for sid := range ourTexts {
			spellIDs = append(spellIDs, sid)
		}
		for sid := range whTexts {
			if _, ok := ourTexts[sid]; !ok {
				spellIDs = append(spellIDs, sid)
			}
		}
		slices.Sort(spellIDs)
		unresolved := uint32(0)
		for _, sid := range spellIDs {
			w, inW := whTexts[sid]
			o, inO := ourTexts[sid]
			switch {
			case !inO:
				ch.Changes = append(ch.Changes, fmt.Sprintf("spell %d only on wowhead: %q", sid, w))
			case !inW:
				ch.Changes = append(ch.Changes, fmt.Sprintf("spell %d only in 3.3.5a: %q", sid, o))
			case !sameValues(w, o):
				ch.Changes = append(ch.Changes, fmt.Sprintf("spell %d: %q -> %q", sid, w, o))
			}
			if inO && strings.Contains(o, "$") && unresolved == 0 {
				unresolved = sid
			}
		}
		if len(ch.Changes) == 0 {
			continue
		}

		switch {
		case c.it.RandomProperty != 0 || c.it.RandomSuffix != 0:
			ch.Skipped = "random enchantment (the tooltip renderer has no random suffix support)"
		case unresolved != 0:
			ch.Skipped = fmt.Sprintf("spell %d has description tokens the formatter can't resolve: %q", unresolved, ourTexts[unresolved])
		default:
			ex.tooltips.Items[id] = ex.src.ItemTooltipHTML(c.it)
		}
		ex.stockTooltips = append(ex.stockTooltips, ch)
	}
	sort.Slice(ex.stockTooltips, func(i, j int) bool { return ex.stockTooltips[i].ID < ex.stockTooltips[j].ID })
}

func (ex *extraction) stockTooltipReport() string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	rendered, skipped := 0, 0
	for _, c := range ex.stockTooltips {
		if c.Skipped == "" {
			rendered++
		} else {
			skipped++
		}
	}
	p("# Stock item tooltips (3.3.5a vs wowhead)")
	p("")
	p("Generated by `./build.ps1 cc` (tools/cc/extract). Stock items whose 3.3.5a values (server item_template, Spell.dbc) differ from wowhead's Wrath Classic tooltip. Rendered items show an in-game style tooltip built from the 3.3.5a data (`tooltips.json`); kept items still show wowhead's.")
	p("")
	p("- Rendered from 3.3.5a data: %d", rendered)
	p("- Kept at wowhead (can't render): %d", skipped)
	p("")
	p("| ID | Item | Difference (wowhead -> 3.3.5a) |")
	p("|---|---|---|")
	for _, c := range ex.stockTooltips {
		name := c.Name
		if c.Skipped != "" {
			name += " (kept: " + c.Skipped + ")"
		}
		p("| %d | %s | %s |", c.ID, mdCell(name), mdCell(strings.Join(c.Changes, "<br>")))
	}
	return b.String()
}

func mdCell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
