package convert

// In-game style tooltips for custom content, rendered from the same data the 3.3.5a client uses:
// item_template fields, the client's stat/format strings (GlobalStrings.lua, enUS) and Spell.dbc
// descriptions. Shown by the UI instead of wowhead tooltips (ui/core/proto_utils/cc_tooltips.ts).

import (
	"fmt"
	"html"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/tools/cc/dbc"
	"github.com/wowsims/wotlk/tools/cc/server"
)

// PlaceholderIcon picks a generic stock icon for items without ItemDisplayInfo data.
func PlaceholderIcon(it *server.ItemTemplate) string {
	if it.Class == 3 {
		return "inv_misc_gem_01"
	}
	switch rangedWeaponType(it) {
	case proto.RangedWeaponType_RangedWeaponTypeBow:
		return "inv_weapon_bow_01"
	case proto.RangedWeaponType_RangedWeaponTypeCrossbow:
		return "inv_weapon_crossbow_01"
	case proto.RangedWeaponType_RangedWeaponTypeGun:
		return "inv_weapon_rifle_01"
	case proto.RangedWeaponType_RangedWeaponTypeWand:
		return "inv_wand_01"
	case proto.RangedWeaponType_RangedWeaponTypeThrown:
		return "inv_throwingknife_01"
	case proto.RangedWeaponType_RangedWeaponTypeIdol:
		return "inv_relics_idolofrejuvenation"
	case proto.RangedWeaponType_RangedWeaponTypeLibram:
		return "inv_relics_libramoftruth"
	case proto.RangedWeaponType_RangedWeaponTypeTotem:
		return "inv_relics_totemofrebirth"
	case proto.RangedWeaponType_RangedWeaponTypeSigil:
		return "inv_sigil_thorim"
	}
	switch weaponType(it) {
	case proto.WeaponType_WeaponTypeAxe:
		return "inv_axe_01"
	case proto.WeaponType_WeaponTypeDagger:
		return "inv_weapon_shortblade_01"
	case proto.WeaponType_WeaponTypeFist:
		return "inv_weapon_hand_01"
	case proto.WeaponType_WeaponTypeMace:
		return "inv_mace_01"
	case proto.WeaponType_WeaponTypeOffHand:
		return "inv_misc_book_09"
	case proto.WeaponType_WeaponTypePolearm:
		return "inv_spear_04"
	case proto.WeaponType_WeaponTypeShield:
		return "inv_shield_04"
	case proto.WeaponType_WeaponTypeStaff:
		return "inv_staff_08"
	case proto.WeaponType_WeaponTypeSword:
		return "inv_sword_04"
	}
	switch itemType(it) {
	case proto.ItemType_ItemTypeHead:
		return "inv_helmet_03"
	case proto.ItemType_ItemTypeNeck:
		return "inv_jewelry_necklace_07"
	case proto.ItemType_ItemTypeShoulder:
		return "inv_shoulder_02"
	case proto.ItemType_ItemTypeBack:
		return "inv_misc_cape_02"
	case proto.ItemType_ItemTypeChest:
		return "inv_chest_cloth_17"
	case proto.ItemType_ItemTypeWrist:
		return "inv_bracer_07"
	case proto.ItemType_ItemTypeHands:
		return "inv_gauntlets_04"
	case proto.ItemType_ItemTypeWaist:
		return "inv_belt_03"
	case proto.ItemType_ItemTypeLegs:
		return "inv_pants_02"
	case proto.ItemType_ItemTypeFeet:
		return "inv_boots_05"
	case proto.ItemType_ItemTypeFinger:
		return "inv_jewelry_ring_03"
	case proto.ItemType_ItemTypeTrinket:
		return "inv_jewelry_talisman_07"
	}
	return "inv_misc_questionmark"
}

// ITEM_MOD_* display strings (enUS GlobalStrings). Primary stats are white "+N Stat" lines; the rest
// are green "Equip:" lines.
var itemModPrimary = map[uint32]string{
	0: "+%d Mana", 1: "+%d Health", 3: "+%d Agility", 4: "+%d Strength", 5: "+%d Intellect", 6: "+%d Spirit", 7: "+%d Stamina",
}
var itemModEquip = map[uint32]string{
	12: "Increases defense rating by %d.",
	13: "Increases your dodge rating by %d.",
	14: "Increases your parry rating by %d.",
	15: "Increases your shield block rating by %d.",
	16: "Improves melee hit rating by %d.",
	17: "Improves ranged hit rating by %d.",
	18: "Improves spell hit rating by %d.",
	19: "Improves melee critical strike rating by %d.",
	20: "Improves ranged critical strike rating by %d.",
	21: "Improves spell critical strike rating by %d.",
	28: "Improves melee haste rating by %d.",
	29: "Improves ranged haste rating by %d.",
	30: "Improves spell haste rating by %d.",
	31: "Improves hit rating by %d.",
	32: "Improves critical strike rating by %d.",
	35: "Improves your resilience rating by %d.",
	36: "Improves haste rating by %d.",
	37: "Increases your expertise rating by %d.",
	38: "Increases attack power by %d.",
	39: "Increases ranged attack power by %d.",
	41: "Increases healing done by magical spells and effects by up to %d.",
	42: "Increases damage done by magical spells and effects by up to %d.",
	43: "Restores %d mana per 5 sec.",
	44: "Increases your armor penetration rating by %d.",
	45: "Increases spell power by %d.",
	46: "Restores %d health per 5 sec.",
	47: "Increases spell penetration by %d.",
	48: "Increases the block value of your shield by %d.",
}

var qualityColors = []string{"#9d9d9d", "#ffffff", "#1eff00", "#0070dd", "#a335ee", "#ff8000", "#e6cc80", "#e6cc80"}

var inventoryTypeNames = map[uint32]string{
	1: "Head", 2: "Neck", 3: "Shoulder", 4: "Shirt", 5: "Chest", 6: "Waist", 7: "Legs", 8: "Feet", 9: "Wrist", 10: "Hands",
	11: "Finger", 12: "Trinket", 13: "One-Hand", 14: "Off Hand", 15: "Ranged", 16: "Back", 17: "Two-Hand", 19: "Tabard",
	20: "Chest", 21: "Main Hand", 22: "Off Hand", 23: "Held In Off-hand", 25: "Thrown", 26: "Ranged", 28: "Relic",
}

var weaponSubclassNames = map[uint32]string{
	0: "Axe", 1: "Axe", 2: "Bow", 3: "Gun", 4: "Mace", 5: "Mace", 6: "Polearm", 7: "Sword", 8: "Sword", 10: "Staff",
	13: "Fist Weapon", 14: "Miscellaneous", 15: "Dagger", 16: "Thrown", 17: "Spear", 18: "Crossbow", 19: "Wand", 20: "Fishing Pole",
}

var armorSubclassNames = map[uint32]string{
	1: "Cloth", 2: "Leather", 3: "Mail", 4: "Plate", 6: "Shield", 7: "Libram", 8: "Idol", 9: "Totem", 10: "Sigil",
}

var classNamesColors = []struct {
	bit         int32
	name, color string
}{
	{1, "Warrior", "#c79c6e"}, {2, "Paladin", "#f58cba"}, {4, "Hunter", "#abd473"}, {8, "Rogue", "#fff569"},
	{16, "Priest", "#ffffff"}, {32, "Death Knight", "#c41f3b"}, {64, "Shaman", "#0070de"}, {128, "Mage", "#69ccf0"},
	{256, "Warlock", "#9482c9"}, {1024, "Druid", "#ff7d0a"},
}

var skillNames = map[uint32]string{
	171: "Alchemy", 164: "Blacksmithing", 333: "Enchanting", 202: "Engineering", 182: "Herbalism", 773: "Inscription",
	755: "Jewelcrafting", 165: "Leatherworking", 186: "Mining", 393: "Skinning", 197: "Tailoring",
}

var resistanceNames = []string{"Holy", "Fire", "Nature", "Frost", "Shadow", "Arcane"}

var socketNames = map[uint32]string{1: "Meta Socket", 2: "Red Socket", 4: "Yellow Socket", 8: "Blue Socket", 14: "Prismatic Socket"}

var damageSchoolNames = []string{"", "Holy", "Fire", "Nature", "Frost", "Shadow", "Arcane"}

const (
	colorGreen  = "#1eff00"
	colorGray   = "#9d9d9d"
	colorYellow = "#ffd100"
	colorWhite  = "#ffffff"
)

type tooltipWriter struct{ b strings.Builder }

func (w *tooltipWriter) line(color, text string) {
	fmt.Fprintf(&w.b, `<div style="color:%s">%s</div>`, color, text)
}

func (w *tooltipWriter) split(left, right string) {
	fmt.Fprintf(&w.b, `<div style="display:flex;justify-content:space-between;gap:2em"><span>%s</span><span>%s</span></div>`, left, right)
}

func (w *tooltipWriter) html() string {
	return `<div class="cc-tooltip">` + w.b.String() + `</div>`
}

func esc(s string) string { return html.EscapeString(s) }

func formatCooldown(ms int32) string {
	switch {
	case ms >= 3600000 && ms%3600000 == 0:
		return fmt.Sprintf("%d Hour", ms/3600000)
	case ms >= 60000 && ms%60000 == 0:
		return fmt.Sprintf("%d Min", ms/60000)
	default:
		return fmt.Sprintf("%g Sec", float64(ms)/1000)
	}
}

func (src *Source) spellText(id uint32) string {
	if sp := src.Spells.Spells[id]; sp != nil {
		if sp.Text != "" {
			return sp.Text
		}
		return sp.Name
	}
	return fmt.Sprintf("spell %d", id)
}

// ItemTooltipHTML renders an item tooltip laid out like the 3.3.5a client's.
func (src *Source) ItemTooltipHTML(it *server.ItemTemplate) string {
	w := &tooltipWriter{}
	q := qualityColors[min(int(it.Quality), len(qualityColors)-1)]
	fmt.Fprintf(&w.b, `<div style="color:%s;font-size:1.1em">%s</div>`, q, esc(it.Name))
	if it.Flags&itemFlagHeroic != 0 {
		w.line(colorGreen, "Heroic")
	}

	switch {
	case it.Flags&0x08000000 != 0:
		w.line(colorWhite, "Binds to account")
	case it.Bonding == 1:
		w.line(colorWhite, "Binds when picked up")
	case it.Bonding == 2:
		w.line(colorWhite, "Binds when equipped")
	case it.Bonding == 3:
		w.line(colorWhite, "Binds when used")
	}
	if it.MaxCount == 1 {
		w.line(colorWhite, "Unique")
	} else if it.Flags&itemFlagUniqueEquippable != 0 || it.ItemLimitCategory != 0 {
		w.line(colorWhite, "Unique-Equipped")
	}

	slot := inventoryTypeNames[it.InventoryType]
	sub := ""
	switch it.Class {
	case 2:
		sub = weaponSubclassNames[it.SubClass]
	case 4:
		if it.InventoryType != 16 { // cloaks don't show "Cloth"
			sub = armorSubclassNames[it.SubClass]
		}
	}
	if slot != "" || sub != "" {
		w.split(slot, sub)
	}

	if it.Class == 2 && it.Delay > 0 {
		speed := float64(it.Delay) / 1000
		w.split(fmt.Sprintf("%.0f - %.0f Damage", it.DamageMin[0], it.DamageMax[0]), fmt.Sprintf("Speed %.2f", speed))
		dps := (it.DamageMin[0] + it.DamageMax[0]) / 2
		if it.DamageMax[1] > 0 {
			school := ""
			if int(it.DamageType[1]) < len(damageSchoolNames) {
				school = damageSchoolNames[it.DamageType[1]] + " "
			}
			w.line(colorWhite, fmt.Sprintf("+%.0f - %.0f %sDamage", it.DamageMin[1], it.DamageMax[1], school))
			dps += (it.DamageMin[1] + it.DamageMax[1]) / 2
		}
		w.line(colorWhite, fmt.Sprintf("(%.1f damage per second)", dps/speed))
	}
	if it.Armor > 0 {
		c := colorWhite
		if it.ArmorDamageModifier > 0 {
			c = colorGreen
		}
		w.line(c, fmt.Sprintf("%d Armor", it.Armor))
	}
	if it.Block > 0 {
		w.line(colorWhite, fmt.Sprintf("%d Block", it.Block))
	}

	var equipLines []string
	for _, st := range it.Stats {
		if f, ok := itemModPrimary[st.Type]; ok {
			w.line(colorWhite, fmt.Sprintf(strings.Replace(f, "+", signOf(st.Value), 1), abs32(st.Value)))
		} else if f, ok := itemModEquip[st.Type]; ok {
			equipLines = append(equipLines, "Equip: "+fmt.Sprintf(f, st.Value))
		}
	}
	for i, r := range it.Resistances {
		if r != 0 {
			w.line(colorWhite, fmt.Sprintf("%s%d %s Resistance", signOf(r), abs32(r), resistanceNames[i]))
		}
	}

	// Dynamic parts are marked with data-cc-* attributes and filled in by the UI at hover time from
	// the equipped gear (ui/core/proto_utils/cc_tooltips.ts): enchant, socketed gems, socket bonus,
	// equipped set pieces and active set bonuses.
	w.b.WriteString(`<div data-cc-enchant style="color:` + colorGreen + `"></div>`)
	for _, c := range it.SocketColors {
		if name, ok := socketNames[c]; ok {
			fmt.Fprintf(&w.b, `<div data-cc-socket="%d" style="color:%s">%s</div>`, int32(socketColor(c)), colorGray, name)
		}
	}
	if it.SocketBonus != 0 {
		if e := src.Tables.Enchants[it.SocketBonus]; e != nil {
			fmt.Fprintf(&w.b, `<div data-cc-socket-bonus style="color:%s">Socket Bonus: %s</div>`, colorGray, esc(e.Name))
		}
	}

	if names := classList(it.AllowableClass); names != "" {
		w.line(colorWhite, "Classes: "+names)
	}
	if it.RequiredLevel > 0 {
		w.line(colorWhite, fmt.Sprintf("Requires Level %d", it.RequiredLevel))
	}
	if name, ok := skillNames[it.RequiredSkill]; ok {
		w.line(colorWhite, fmt.Sprintf("Requires %s (%d)", name, it.RequiredSkillRank))
	}
	if it.ItemLevel > 0 {
		w.line(colorYellow, fmt.Sprintf("Item Level %d", it.ItemLevel))
	}

	for _, l := range equipLines {
		w.line(colorGreen, esc(l))
	}
	for _, s := range it.Spells {
		prefix := ""
		switch s.Trigger {
		case server.SpellTriggerUse, server.SpellTriggerUseNoDelay:
			prefix = "Use: "
		case server.SpellTriggerEquip:
			prefix = "Equip: "
		case server.SpellTriggerChanceHit:
			prefix = "Chance on hit: "
		default:
			continue
		}
		text := prefix + src.spellText(s.SpellID)
		if prefix == "Use: " {
			cd := s.Cooldown
			if cd <= 0 {
				if sp := src.Spells.Spells[s.SpellID]; sp != nil {
					cd = int32(max(sp.RecoveryTime, sp.CategoryRecoveryTime))
				}
			}
			if cd > 0 {
				text += fmt.Sprintf(" (%s Cooldown)", formatCooldown(cd))
			}
		}
		w.line(colorGreen, esc(text))
	}
	if it.Description != "" {
		w.line(colorYellow, `"`+esc(it.Description)+`"`)
	}

	if set := src.Tables.ItemSets[it.ItemSet]; set != nil && it.ItemSet != 0 {
		w.b.WriteString(`<div style="margin-top:0.5em"></div>`)
		total := len(src.setPieces(it, set))
		fmt.Fprintf(&w.b, `<div data-cc-set-header data-name="%s" data-total="%d" style="color:%s">%s (0/%d)</div>`,
			esc(set.Name), total, colorYellow, esc(set.Name), total)
		for _, p := range src.setPieces(it, set) {
			fmt.Fprintf(&w.b, `<div data-cc-set-piece="%s" style="color:%s;padding-left:0.8em">%s</div>`, p.ids, colorGray, esc(p.name))
		}
		bonuses := append([]dbc.ItemSetBonus(nil), set.Bonuses...)
		sort.Slice(bonuses, func(i, j int) bool { return bonuses[i].Pieces < bonuses[j].Pieces })
		for _, b := range bonuses {
			fmt.Fprintf(&w.b, `<div data-cc-set-bonus="%d" style="color:%s">%s</div>`, b.Pieces, colorGray,
				esc(fmt.Sprintf("(%d) Set: %s", b.Pieces, src.spellText(b.SpellID))))
		}
	}
	return w.html()
}

type setPiece struct {
	name string
	ids  string // space-separated IDs of every version of the piece; wearing any of them counts
}

// setPieces lists the pieces of it's item set. ItemSet.dbc lists one version of each piece, but every
// server item with the set's ID counts toward its bonuses (10/25-man, heroic and faction versions of
// tier sets), so pieces are grouped by slot and named after the version closest to it. Sets that
// don't have exactly one piece per slot use ItemSet.dbc's list as-is.
func (src *Source) setPieces(it *server.ItemTemplate, set *dbc.ItemSet) []setPiece {
	if src.setItems == nil {
		src.setItems = map[uint32][]*server.ItemTemplate{}
		for _, item := range src.Server.Items {
			if item.ItemSet != 0 {
				src.setItems[item.ItemSet] = append(src.setItems[item.ItemSet], item)
			}
		}
		for _, items := range src.setItems {
			sort.Slice(items, func(i, j int) bool { return items[i].Entry < items[j].Entry })
		}
	}
	slotOf := func(item *server.ItemTemplate) uint32 {
		if item.InventoryType == 20 { // robes
			return 5
		}
		return item.InventoryType
	}
	bySlot := map[uint32][]*server.ItemTemplate{}
	for _, item := range src.setItems[it.ItemSet] {
		bySlot[slotOf(item)] = append(bySlot[slotOf(item)], item)
	}

	var slots []uint32
	for _, id := range set.ItemIDs {
		listed := src.Server.Items[id]
		if listed == nil || len(bySlot[slotOf(listed)]) == 0 || slices.Contains(slots, slotOf(listed)) {
			slots = nil
			break
		}
		slots = append(slots, slotOf(listed))
	}
	if len(slots) == 0 || len(slots) != len(bySlot) {
		pieces := make([]setPiece, 0, len(set.ItemIDs))
		for _, id := range set.ItemIDs {
			name := fmt.Sprintf("item %d", id)
			if piece := src.Server.Items[id]; piece != nil {
				name = piece.Name
			}
			pieces = append(pieces, setPiece{name: name, ids: fmt.Sprint(id)})
		}
		return pieces
	}

	pieces := make([]setPiece, 0, len(slots))
	for _, slot := range slots {
		var best *server.ItemTemplate
		score := func(item *server.ItemTemplate) int {
			s := 0
			if item.ItemLevel == it.ItemLevel {
				s += 2
			}
			if item.AllowableRace == it.AllowableRace {
				s++
			}
			return s
		}
		ids := make([]string, 0, len(bySlot[slot]))
		for _, item := range bySlot[slot] {
			if best == nil || score(item) > score(best) {
				best = item
			}
			ids = append(ids, fmt.Sprint(item.Entry))
		}
		pieces = append(pieces, setPiece{name: best.Name, ids: strings.Join(ids, " ")})
	}
	return pieces
}

// SpellTooltipHTML renders a spell tooltip: name, rank and description.
func (src *Source) SpellTooltipHTML(sp *dbc.Spell) string {
	w := &tooltipWriter{}
	if sp.Rank != "" {
		w.split(`<span style="font-size:1.1em">`+esc(sp.Name)+`</span>`, `<span style="color:`+colorGray+`">`+esc(sp.Rank)+`</span>`)
	} else {
		fmt.Fprintf(&w.b, `<div style="font-size:1.1em">%s</div>`, esc(sp.Name))
	}
	if sp.Text != "" {
		w.line(colorYellow, esc(sp.Text))
	}
	return w.html()
}

func classList(mask int32) string {
	const all = 1535
	if mask <= 0 || mask&all == all {
		return ""
	}
	var parts []string
	for _, c := range classNamesColors {
		if mask&c.bit != 0 {
			parts = append(parts, fmt.Sprintf(`<span style="color:%s">%s</span>`, c.color, c.name))
		}
	}
	return strings.Join(parts, ", ")
}

func signOf(v int32) string {
	if v < 0 {
		return "-"
	}
	return "+"
}

func abs32(v int32) int32 {
	return int32(math.Abs(float64(v)))
}
