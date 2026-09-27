// Package convert maps 3.3.5a server/client data onto the sim's UI protos.
package convert

import (
	"fmt"
	"strings"

	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/core/stats"
	"github.com/wowsims/wotlk/tools/cc/dbc"
	"github.com/wowsims/wotlk/tools/cc/server"
)

// Source bundles everything the converter can look up.
type Source struct {
	Spells *dbc.SpellStore
	Tables *dbc.Tables
	Server *server.Data
}

// Issue describes an effect the importer could not express as plain stats; it needs Go code.
type Issue struct {
	Kind    string `json:"kind"` // "item-spell", "enchant", "gem", "set-bonus", "meta-gem", "warning"
	OwnerID int32  `json:"ownerId"`
	Owner   string `json:"owner"`
	SpellID uint32 `json:"spellId,omitempty"`
	Trigger string `json:"trigger,omitempty"`
	Detail  string `json:"detail"`
}

// ---------------------------------------------------------------------------
// Item stat mods (ITEM_MOD_* in 3.3.5a) -> sim stats
// ---------------------------------------------------------------------------

func addItemMod(s *stats.Stats, mod uint32, v float64) bool {
	switch mod {
	case 0:
		s[stats.Mana] += v
	case 1:
		s[stats.Health] += v
	case 3:
		s[stats.Agility] += v
	case 4:
		s[stats.Strength] += v
	case 5:
		s[stats.Intellect] += v
	case 6:
		s[stats.Spirit] += v
	case 7:
		s[stats.Stamina] += v
	case 12:
		s[stats.Defense] += v
	case 13:
		s[stats.Dodge] += v
	case 14:
		s[stats.Parry] += v
	case 15:
		s[stats.Block] += v
	case 16, 17: // melee / ranged hit rating (the sim uses melee stats for ranged)
		s[stats.MeleeHit] += v
	case 18:
		s[stats.SpellHit] += v
	case 19, 20:
		s[stats.MeleeCrit] += v
	case 21:
		s[stats.SpellCrit] += v
	case 28, 29:
		s[stats.MeleeHaste] += v
	case 30:
		s[stats.SpellHaste] += v
	case 31:
		s[stats.MeleeHit] += v
		s[stats.SpellHit] += v
	case 32:
		s[stats.MeleeCrit] += v
		s[stats.SpellCrit] += v
	case 35:
		s[stats.Resilience] += v
	case 36:
		s[stats.MeleeHaste] += v
		s[stats.SpellHaste] += v
	case 37:
		s[stats.Expertise] += v
	case 38:
		s[stats.AttackPower] += v
		s[stats.RangedAttackPower] += v
	case 39:
		s[stats.RangedAttackPower] += v
	case 41: // healing done: folded into spell power only when paired with 42 (see caller)
		return true
	case 42, 45:
		s[stats.SpellPower] += v
	case 43:
		s[stats.MP5] += v
	case 44:
		s[stats.ArmorPenetration] += v
	case 47:
		s[stats.SpellPenetration] += v
	case 48:
		s[stats.BlockValue] += v
	case 22, 23, 24, 25, 26, 27, 33, 34: // *_TAKEN ratings: irrelevant/unused in 3.3.5a
		return true
	case 40, 46: // feral AP (computed from weapon DPS in 3.3.5a), health regen
		return true
	default:
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Passive aura -> stats
// ---------------------------------------------------------------------------

const magicSchoolMask = 0x7E // holy..arcane

// SpellStats converts a passive spell (item equip spell, set bonus, enchant equip spell) into stats.
// ok=false means at least one effect needs custom code; reason explains why.
func SpellStats(sp *dbc.Spell) (s stats.Stats, ok bool, reason string) {
	if sp == nil {
		return s, false, "spell not found in Spell.dbc"
	}
	if sp.ProcFlags != 0 && hasAura(sp, dbc.AuraProcTriggerSpell, dbc.AuraProcTriggerDamage) {
		return s, false, "proc"
	}
	hasSpellDamage := false
	for _, e := range sp.Effects {
		if e.Effect == dbc.EffectApplyAura && e.Aura == dbc.AuraModDamageDone {
			hasSpellDamage = true
		}
	}
	for i, e := range sp.Effects {
		if e.Effect == 0 {
			continue
		}
		if e.Effect != dbc.EffectApplyAura {
			return s, false, fmt.Sprintf("effect %d is %s", i+1, e.EffectName)
		}
		if e.ImplicitTargetA != 0 && e.ImplicitTargetA != 1 { // TARGET_UNIT_CASTER
			return s, false, fmt.Sprintf("effect %d targets %d (not self)", i+1, e.ImplicitTargetA)
		}
		v := e.Avg()
		switch e.Aura {
		case dbc.AuraModDamageDone:
			if uint32(e.MiscValue)&magicSchoolMask != magicSchoolMask {
				return s, false, fmt.Sprintf("school-specific spell damage (mask 0x%x)", e.MiscValue)
			}
			s[stats.SpellPower] += v
		case dbc.AuraModHealingDone:
			if !hasSpellDamage {
				return s, false, "healing-only spell power"
			}
		case dbc.AuraModAttackPower: // melee only; "attack power" item spells carry a separate ranged aura
			s[stats.AttackPower] += v
		case dbc.AuraModRangedAttackPower:
			s[stats.RangedAttackPower] += v
		case dbc.AuraModPowerRegen:
			if e.MiscValue != 0 {
				return s, false, "non-mana power regen"
			}
			s[stats.MP5] += v
		case dbc.AuraModRating:
			if !addCombatRatingMask(&s, uint32(e.MiscValue), v) {
				return s, false, fmt.Sprintf("unsupported combat rating mask 0x%x", e.MiscValue)
			}
		case dbc.AuraModStat:
			switch e.MiscValue {
			case -1:
				for _, st := range []stats.Stat{stats.Strength, stats.Agility, stats.Stamina, stats.Intellect, stats.Spirit} {
					s[st] += v
				}
			case 0:
				s[stats.Strength] += v
			case 1:
				s[stats.Agility] += v
			case 2:
				s[stats.Stamina] += v
			case 3:
				s[stats.Intellect] += v
			case 4:
				s[stats.Spirit] += v
			default:
				return s, false, "unknown stat"
			}
		case dbc.AuraModResistance:
			addResistanceMask(&s, uint32(e.MiscValue), v)
		case dbc.AuraModTargetResistance:
			if uint32(e.MiscValue)&1 != 0 {
				return s, false, "flat armor penetration"
			}
			s[stats.SpellPenetration] += -v
		case dbc.AuraModShieldBlockValue:
			s[stats.BlockValue] += v
		case dbc.AuraModIncreaseHealth:
			s[stats.Health] += v
		case dbc.AuraModIncreaseEnergy:
			if e.MiscValue != 0 {
				return s, false, "non-mana max power"
			}
			s[stats.Mana] += v
		case dbc.AuraModExpertise:
			s[stats.Expertise] += v * core.ExpertisePerQuarterPercentReduction
		case 30: // MOD_SKILL
			if e.MiscValue != 95 { // defense
				return s, false, fmt.Sprintf("skill %d bonus", e.MiscValue)
			}
			s[stats.Defense] += v * core.DefenseRatingPerDefense
		default:
			return s, false, fmt.Sprintf("aura %s (%d)", e.AuraName, e.Aura)
		}
	}
	return s, true, ""
}

func hasAura(sp *dbc.Spell, auras ...uint32) bool {
	for _, e := range sp.Effects {
		for _, a := range auras {
			if e.Aura == a {
				return true
			}
		}
	}
	return false
}

// CombatRating bits (CR_*) used by SPELL_AURA_MOD_RATING.
func addCombatRatingMask(s *stats.Stats, mask uint32, v float64) bool {
	has := func(bits ...uint) bool {
		for _, b := range bits {
			if mask&(1<<b) != 0 {
				return true
			}
		}
		return false
	}
	handled := uint32(0)
	add := func(st stats.Stat, bits ...uint) {
		if has(bits...) {
			s[st] += v
		}
		for _, b := range bits {
			handled |= 1 << b
		}
	}
	add(stats.Defense, 1)
	add(stats.Dodge, 2)
	add(stats.Parry, 3)
	add(stats.Block, 4)
	add(stats.MeleeHit, 5, 6)
	add(stats.SpellHit, 7)
	add(stats.MeleeCrit, 8, 9)
	add(stats.SpellCrit, 10)
	add(stats.Resilience, 14, 15, 16)
	add(stats.MeleeHaste, 17, 18)
	add(stats.SpellHaste, 19)
	add(stats.Expertise, 23)
	add(stats.ArmorPenetration, 24)
	// Hit/crit taken ratings (11-13) are unused in 3.3.5a.
	handled |= 1<<11 | 1<<12 | 1<<13
	return mask&^handled == 0
}

// Resistance school mask: 1 armor, 2 holy, 4 fire, 8 nature, 16 frost, 32 shadow, 64 arcane.
func addResistanceMask(s *stats.Stats, mask uint32, v float64) {
	pairs := []struct {
		bit uint32
		st  stats.Stat
	}{{1, stats.BonusArmor}, {4, stats.FireResistance}, {8, stats.NatureResistance}, {16, stats.FrostResistance}, {32, stats.ShadowResistance}, {64, stats.ArcaneResistance}}
	for _, p := range pairs {
		if mask&p.bit != 0 {
			s[p.st] += v
		}
	}
}

// ---------------------------------------------------------------------------
// Enchantments (socket bonuses, gems, enchants)
// ---------------------------------------------------------------------------

// EnchantStats converts a SpellItemEnchantment into stats; unsupported parts are returned as reasons.
func (src *Source) EnchantStats(ench *dbc.SpellItemEnchantment) (s stats.Stats, problems []string) {
	for i := 0; i < 3; i++ {
		amount := float64(ench.Amount[i])
		if ench.AmountMax[i] > ench.Amount[i] {
			amount = float64(ench.Amount[i]+ench.AmountMax[i]) / 2
		}
		switch ench.Type[i] {
		case 0:
		case dbc.EnchantTypeStat:
			if !addItemMod(&s, ench.Arg[i], amount) {
				problems = append(problems, fmt.Sprintf("unknown stat mod %d", ench.Arg[i]))
			}
		case dbc.EnchantTypeResistance:
			addResistanceMask(&s, 1<<ench.Arg[i], amount)
		case dbc.EnchantTypeEquipSpell:
			sp := src.Spells.Spells[ench.Arg[i]]
			ss, ok, why := SpellStats(sp)
			if ok {
				s = s.Add(ss)
			} else {
				problems = append(problems, fmt.Sprintf("equip spell %d (%s): %s", ench.Arg[i], spellName(sp), why))
			}
		case dbc.EnchantTypeProcSpell:
			problems = append(problems, fmt.Sprintf("proc spell %d (%s)", ench.Arg[i], spellName(src.Spells.Spells[ench.Arg[i]])))
		case dbc.EnchantTypeUseSpell:
			problems = append(problems, fmt.Sprintf("use spell %d (%s)", ench.Arg[i], spellName(src.Spells.Spells[ench.Arg[i]])))
		case dbc.EnchantTypeDamage:
			problems = append(problems, fmt.Sprintf("+%d weapon damage", ench.Amount[i]))
		case dbc.EnchantTypePrismatic:
			problems = append(problems, "prismatic socket")
		default:
			problems = append(problems, fmt.Sprintf("enchant type %d", ench.Type[i]))
		}
	}
	return s, problems
}

func spellName(sp *dbc.Spell) string {
	if sp == nil {
		return "missing"
	}
	return sp.Name
}

// ---------------------------------------------------------------------------
// Items
// ---------------------------------------------------------------------------

func itemType(it *server.ItemTemplate) proto.ItemType {
	switch it.InventoryType {
	case 1:
		return proto.ItemType_ItemTypeHead
	case 2:
		return proto.ItemType_ItemTypeNeck
	case 3:
		return proto.ItemType_ItemTypeShoulder
	case 5, 20:
		return proto.ItemType_ItemTypeChest
	case 6:
		return proto.ItemType_ItemTypeWaist
	case 7:
		return proto.ItemType_ItemTypeLegs
	case 8:
		return proto.ItemType_ItemTypeFeet
	case 9:
		return proto.ItemType_ItemTypeWrist
	case 10:
		return proto.ItemType_ItemTypeHands
	case 11:
		return proto.ItemType_ItemTypeFinger
	case 12:
		return proto.ItemType_ItemTypeTrinket
	case 16:
		return proto.ItemType_ItemTypeBack
	case 13, 14, 17, 21, 22, 23:
		return proto.ItemType_ItemTypeWeapon
	case 15, 25, 26, 28:
		return proto.ItemType_ItemTypeRanged
	}
	return proto.ItemType_ItemTypeUnknown
}

func armorType(it *server.ItemTemplate) proto.ArmorType {
	if it.Class != 4 || it.InventoryType == 16 {
		return proto.ArmorType_ArmorTypeUnknown
	}
	switch it.SubClass {
	case 1:
		return proto.ArmorType_ArmorTypeCloth
	case 2:
		return proto.ArmorType_ArmorTypeLeather
	case 3:
		return proto.ArmorType_ArmorTypeMail
	case 4:
		return proto.ArmorType_ArmorTypePlate
	}
	return proto.ArmorType_ArmorTypeUnknown
}

func weaponType(it *server.ItemTemplate) proto.WeaponType {
	switch it.InventoryType {
	case 14:
		return proto.WeaponType_WeaponTypeShield
	case 23:
		return proto.WeaponType_WeaponTypeOffHand
	}
	if it.Class != 2 {
		return proto.WeaponType_WeaponTypeUnknown
	}
	switch it.SubClass {
	case 0, 1:
		return proto.WeaponType_WeaponTypeAxe
	case 4, 5:
		return proto.WeaponType_WeaponTypeMace
	case 6, 17:
		return proto.WeaponType_WeaponTypePolearm
	case 7, 8:
		return proto.WeaponType_WeaponTypeSword
	case 10:
		return proto.WeaponType_WeaponTypeStaff
	case 13:
		return proto.WeaponType_WeaponTypeFist
	case 15:
		return proto.WeaponType_WeaponTypeDagger
	}
	return proto.WeaponType_WeaponTypeUnknown
}

func handType(it *server.ItemTemplate) proto.HandType {
	switch it.InventoryType {
	case 13:
		return proto.HandType_HandTypeOneHand
	case 17:
		return proto.HandType_HandTypeTwoHand
	case 21:
		return proto.HandType_HandTypeMainHand
	case 14, 22, 23:
		return proto.HandType_HandTypeOffHand
	}
	return proto.HandType_HandTypeUnknown
}

func rangedWeaponType(it *server.ItemTemplate) proto.RangedWeaponType {
	if it.Class == 2 {
		switch it.SubClass {
		case 2:
			return proto.RangedWeaponType_RangedWeaponTypeBow
		case 3:
			return proto.RangedWeaponType_RangedWeaponTypeGun
		case 16:
			return proto.RangedWeaponType_RangedWeaponTypeThrown
		case 18:
			return proto.RangedWeaponType_RangedWeaponTypeCrossbow
		case 19:
			return proto.RangedWeaponType_RangedWeaponTypeWand
		}
	} else if it.Class == 4 {
		switch it.SubClass {
		case 7:
			return proto.RangedWeaponType_RangedWeaponTypeLibram
		case 8:
			return proto.RangedWeaponType_RangedWeaponTypeIdol
		case 9:
			return proto.RangedWeaponType_RangedWeaponTypeTotem
		case 10:
			return proto.RangedWeaponType_RangedWeaponTypeSigil
		}
	}
	return proto.RangedWeaponType_RangedWeaponTypeUnknown
}

func socketColor(c uint32) proto.GemColor {
	switch c {
	case 1:
		return proto.GemColor_GemColorMeta
	case 2:
		return proto.GemColor_GemColorRed
	case 4:
		return proto.GemColor_GemColorYellow
	case 8:
		return proto.GemColor_GemColorBlue
	case 14:
		return proto.GemColor_GemColorPrismatic
	}
	return proto.GemColor_GemColorUnknown
}

func gemColor(mask uint32) proto.GemColor {
	switch mask {
	case 1:
		return proto.GemColor_GemColorMeta
	case 2:
		return proto.GemColor_GemColorRed
	case 4:
		return proto.GemColor_GemColorYellow
	case 8:
		return proto.GemColor_GemColorBlue
	case 6:
		return proto.GemColor_GemColorOrange
	case 10:
		return proto.GemColor_GemColorPurple
	case 12:
		return proto.GemColor_GemColorGreen
	case 14:
		return proto.GemColor_GemColorPrismatic
	}
	return proto.GemColor_GemColorUnknown
}

// AllowableClass bits: 1 warrior, 2 paladin, 4 hunter, 8 rogue, 16 priest, 32 DK, 64 shaman, 128 mage, 256 warlock, 1024 druid.
var classBits = []struct {
	bit   int32
	class proto.Class
}{
	{1, proto.Class_ClassWarrior}, {2, proto.Class_ClassPaladin}, {4, proto.Class_ClassHunter}, {8, proto.Class_ClassRogue},
	{16, proto.Class_ClassPriest}, {32, proto.Class_ClassDeathknight}, {64, proto.Class_ClassShaman}, {128, proto.Class_ClassMage},
	{256, proto.Class_ClassWarlock}, {1024, proto.Class_ClassDruid},
}

func classAllowlist(mask int32) []proto.Class {
	const all = 1535
	if mask <= 0 || mask&all == all {
		return nil
	}
	var out []proto.Class
	for _, cb := range classBits {
		if mask&cb.bit != 0 {
			out = append(out, cb.class)
		}
	}
	return out
}

func factionRestriction(raceMask int32) proto.UIItem_FactionRestriction {
	const alliance, horde = 1 | 4 | 8 | 64 | 1024, 2 | 16 | 32 | 128 | 512
	if raceMask <= 0 {
		return proto.UIItem_FACTION_RESTRICTION_UNSPECIFIED
	}
	a, h := raceMask&alliance != 0, raceMask&horde != 0
	switch {
	case a && !h:
		return proto.UIItem_FACTION_RESTRICTION_ALLIANCE_ONLY
	case h && !a:
		return proto.UIItem_FACTION_RESTRICTION_HORDE_ONLY
	}
	return proto.UIItem_FACTION_RESTRICTION_UNSPECIFIED
}

var professionBySkill = map[uint32]proto.Profession{
	171: proto.Profession_Alchemy, 164: proto.Profession_Blacksmithing, 333: proto.Profession_Enchanting,
	202: proto.Profession_Engineering, 182: proto.Profession_Herbalism, 773: proto.Profession_Inscription,
	755: proto.Profession_Jewelcrafting, 165: proto.Profession_Leatherworking, 186: proto.Profession_Mining,
	393: proto.Profession_Skinning, 197: proto.Profession_Tailoring,
}

const (
	itemFlagHeroic           = 0x8
	itemFlagUniqueEquippable = 0x80000
)

func isUnique(it *server.ItemTemplate) bool {
	return it.MaxCount == 1 || it.Flags&itemFlagUniqueEquippable != 0 || it.ItemLimitCategory != 0
}

// IsEquippable reports whether the item can go in a sim gear slot. This is decided by the slot,
// not the class: e.g. the alchemist stones are class 7 (Trade Goods) trinkets.
func IsEquippable(it *server.ItemTemplate) bool {
	return itemType(it) != proto.ItemType_ItemTypeUnknown
}

func IsGem(it *server.ItemTemplate) bool {
	return it.Class == 3 && it.GemProperties != 0
}

func (src *Source) SetName(setID uint32) string {
	if setID == 0 {
		return ""
	}
	if set, ok := src.Tables.ItemSets[setID]; ok {
		return set.Name
	}
	return ""
}

// Icon is the item's real icon (ItemDisplayInfo.dbc), or a generic placeholder for its slot.
func (src *Source) Icon(it *server.ItemTemplate) string {
	if icon := src.Tables.DisplayIcons[it.DisplayID]; icon != "" {
		return icon
	}
	return PlaceholderIcon(it)
}

// ToUIItem converts an item_template entry, returning any effects that need custom code.
func (src *Source) ToUIItem(it *server.ItemTemplate, phase int32) (*proto.UIItem, []Issue) {
	var issues []Issue
	issue := func(kind string, spellID uint32, trigger, detail string) {
		issues = append(issues, Issue{Kind: kind, OwnerID: int32(it.Entry), Owner: it.Name, SpellID: spellID, Trigger: trigger, Detail: detail})
	}

	var s stats.Stats
	for _, st := range it.Stats {
		if !addItemMod(&s, st.Type, float64(st.Value)) {
			issue("warning", 0, "", fmt.Sprintf("unknown ITEM_MOD %d (value %d)", st.Type, st.Value))
		}
	}
	res := it.Resistances // holy, fire, nature, frost, shadow, arcane
	s[stats.FireResistance] += float64(res[1])
	s[stats.NatureResistance] += float64(res[2])
	s[stats.FrostResistance] += float64(res[3])
	s[stats.ShadowResistance] += float64(res[4])
	s[stats.ArcaneResistance] += float64(res[5])
	s[stats.BlockValue] += float64(it.Block)

	iType := itemType(it)
	wType := weaponType(it)
	scalableArmor := wType == proto.WeaponType_WeaponTypeShield || !(iType == proto.ItemType_ItemTypeNeck ||
		iType == proto.ItemType_ItemTypeFinger || iType == proto.ItemType_ItemTypeTrinket || iType == proto.ItemType_ItemTypeWeapon)
	// item_template.armor is the total; ArmorDamageModifier is the green "bonus armor" part of it.
	if scalableArmor {
		bonus := max(0, min(it.ArmorDamageModifier, float64(it.Armor)))
		s[stats.Armor] += float64(it.Armor) - bonus
		s[stats.BonusArmor] += bonus
	} else {
		s[stats.BonusArmor] += float64(it.Armor)
	}

	for _, spell := range it.Spells {
		sp := src.Spells.Spells[spell.SpellID]
		switch spell.Trigger {
		case server.SpellTriggerEquip:
			ss, ok, why := SpellStats(sp)
			if ok {
				s = s.Add(ss)
			} else {
				issue("item-spell", spell.SpellID, "equip", fmt.Sprintf("%s: %s", spellName(sp), why))
			}
		case server.SpellTriggerUse, server.SpellTriggerUseNoDelay:
			issue("item-spell", spell.SpellID, "use", spellName(sp))
		case server.SpellTriggerChanceHit:
			detail := spellName(sp)
			if spell.PPMRate > 0 {
				detail += fmt.Sprintf(" (%.2f PPM)", spell.PPMRate)
			}
			issue("item-spell", spell.SpellID, "chance-on-hit", detail)
		case 6: // learn spell (patterns etc.) - irrelevant
		default:
			issue("item-spell", spell.SpellID, fmt.Sprintf("trigger-%d", spell.Trigger), spellName(sp))
		}
	}

	var sockets []proto.GemColor
	for _, c := range it.SocketColors {
		if c != 0 {
			sockets = append(sockets, socketColor(c))
		}
	}
	var socketBonus stats.Stats
	if it.SocketBonus != 0 {
		if ench, ok := src.Tables.Enchants[it.SocketBonus]; ok {
			var problems []string
			socketBonus, problems = src.EnchantStats(ench)
			for _, p := range problems {
				issue("warning", 0, "", "socket bonus: "+p)
			}
		} else {
			issue("warning", 0, "", fmt.Sprintf("socket bonus enchant %d not in SpellItemEnchantment.dbc", it.SocketBonus))
		}
	}
	if it.ScalingStatDistribution != 0 {
		issue("warning", 0, "", "heirloom scaling stats (ScalingStatDistribution) are not imported; stats reflect item_template only")
	}
	if it.RandomProperty != 0 || it.RandomSuffix != 0 {
		issue("warning", 0, "", "random enchantment/suffix item; base stats only")
	}

	item := &proto.UIItem{
		Id:                 int32(it.Entry),
		Name:               it.Name,
		Icon:               src.Icon(it),
		Type:               iType,
		ArmorType:          armorType(it),
		WeaponType:         wType,
		HandType:           handType(it),
		RangedWeaponType:   rangedWeaponType(it),
		Stats:              s[:],
		GemSockets:         sockets,
		SocketBonus:        socketBonus[:],
		Ilvl:               int32(it.ItemLevel),
		Phase:              phase,
		Quality:            proto.ItemQuality(it.Quality),
		Unique:             isUnique(it),
		Heroic:             it.Flags&itemFlagHeroic != 0,
		ClassAllowlist:     classAllowlist(it.AllowableClass),
		RequiredProfession: professionBySkill[it.RequiredSkill],
		SetName:            src.SetName(it.ItemSet),
		Expansion:          proto.Expansion_ExpansionWotlk,
		FactionRestriction: factionRestriction(it.AllowableRace),
	}
	if it.Class == 2 && it.Delay > 0 {
		item.WeaponDamageMin = it.DamageMin[0]
		item.WeaponDamageMax = it.DamageMax[0]
		item.WeaponSpeed = float64(it.Delay) / 1000
		if it.DamageMax[1] > 0 {
			issue("warning", 0, "", fmt.Sprintf("secondary weapon damage %.0f-%.0f (school %d) is ignored by the sim", it.DamageMin[1], it.DamageMax[1], it.DamageType[1]))
		}
	}
	return item, issues
}

func (src *Source) ToUIGem(it *server.ItemTemplate, phase int32) (*proto.UIGem, []Issue) {
	var issues []Issue
	gp, ok := src.Tables.GemProperties[it.GemProperties]
	if !ok {
		return nil, []Issue{{Kind: "warning", OwnerID: int32(it.Entry), Owner: it.Name, Detail: fmt.Sprintf("GemProperties %d not in GemProperties.dbc", it.GemProperties)}}
	}
	gem := &proto.UIGem{
		Id:                 int32(it.Entry),
		Name:               it.Name,
		Icon:               src.Icon(it),
		Color:              gemColor(gp.Color),
		Phase:              phase,
		Quality:            proto.ItemQuality(it.Quality),
		Unique:             isUnique(it),
		RequiredProfession: professionBySkill[it.RequiredSkill],
	}
	var s stats.Stats
	if ench, ok := src.Tables.Enchants[gp.EnchantID]; ok {
		var problems []string
		s, problems = src.EnchantStats(ench)
		kind := "gem"
		if gem.Color == proto.GemColor_GemColorMeta {
			kind = "meta-gem"
			problems = append(problems, fmt.Sprintf("activation condition %d (SpellItemEnchantmentCondition.dbc) must be added to the UI", ench.ConditionID))
		}
		for _, p := range problems {
			issues = append(issues, Issue{Kind: kind, OwnerID: int32(it.Entry), Owner: it.Name, Detail: p})
		}
	} else {
		issues = append(issues, Issue{Kind: "warning", OwnerID: int32(it.Entry), Owner: it.Name, Detail: fmt.Sprintf("gem enchant %d not in SpellItemEnchantment.dbc", gp.EnchantID)})
	}
	gem.Stats = s[:]
	return gem, issues
}

// ---------------------------------------------------------------------------
// Enchants (from ENCHANT_ITEM spells)
// ---------------------------------------------------------------------------

// Inventory type bits used by Spell.EquippedItemInventoryTypeMask.
func invMask(types ...uint) int32 {
	m := int32(0)
	for _, t := range types {
		m |= 1 << t
	}
	return m
}

// enchantSlot works out which gear slot(s) an enchanting spell applies to.
func enchantSlot(sp *dbc.Spell) (proto.ItemType, []proto.ItemType, proto.EnchantType, bool) {
	switch sp.EquippedItemClass {
	case 2: // weapon
		sub := sp.EquippedItemSubClass
		rangedMask := int32(1<<2 | 1<<3 | 1<<18)
		twoHandMask := int32(1<<1 | 1<<5 | 1<<6 | 1<<8 | 1<<10 | 1<<17)
		if sub > 0 && sub&^rangedMask == 0 {
			return proto.ItemType_ItemTypeRanged, nil, proto.EnchantType_EnchantTypeNormal, true
		}
		if sub == 1<<10 {
			return proto.ItemType_ItemTypeWeapon, nil, proto.EnchantType_EnchantTypeStaff, true
		}
		if sub > 0 && sub&^twoHandMask == 0 {
			return proto.ItemType_ItemTypeWeapon, nil, proto.EnchantType_EnchantTypeTwoHand, true
		}
		return proto.ItemType_ItemTypeWeapon, nil, proto.EnchantType_EnchantTypeNormal, true
	case 4: // armor
		if sp.EquippedItemSubClass == 1<<6 { // shields
			return proto.ItemType_ItemTypeWeapon, nil, proto.EnchantType_EnchantTypeShield, true
		}
		inv := sp.EquippedItemInvType
		slots := []struct {
			mask int32
			t    proto.ItemType
		}{
			{invMask(1), proto.ItemType_ItemTypeHead}, {invMask(3), proto.ItemType_ItemTypeShoulder},
			{invMask(16), proto.ItemType_ItemTypeBack}, {invMask(5, 20), proto.ItemType_ItemTypeChest},
			{invMask(9), proto.ItemType_ItemTypeWrist}, {invMask(10), proto.ItemType_ItemTypeHands},
			{invMask(6), proto.ItemType_ItemTypeWaist}, {invMask(7), proto.ItemType_ItemTypeLegs},
			{invMask(8), proto.ItemType_ItemTypeFeet}, {invMask(11), proto.ItemType_ItemTypeFinger},
		}
		var types []proto.ItemType
		for _, sl := range slots {
			if inv&sl.mask != 0 {
				types = append(types, sl.t)
			}
		}
		if len(types) == 0 {
			return 0, nil, 0, false
		}
		if len(types) > 1 {
			return types[0], types[1:], proto.EnchantType_EnchantTypeKit, true
		}
		return types[0], nil, proto.EnchantType_EnchantTypeNormal, true
	}
	return 0, nil, 0, false
}

// EnchantSpellEffect returns the SpellItemEnchantment id applied by an enchanting spell, if any.
func EnchantSpellEffect(sp *dbc.Spell) uint32 {
	for _, e := range sp.Effects {
		if e.Effect == dbc.EffectEnchantItem && e.MiscValue > 0 {
			return uint32(e.MiscValue)
		}
	}
	return 0
}

// ToUIEnchant builds an enchant from its enchanting spell (and optional scroll/kit item id).
func (src *Source) ToUIEnchant(sp *dbc.Spell, itemID int32, phase int32) (*proto.UIEnchant, []Issue) {
	enchID := EnchantSpellEffect(sp)
	ench, ok := src.Tables.Enchants[enchID]
	if !ok {
		return nil, nil
	}
	t, extra, et, ok := enchantSlot(sp)
	if !ok {
		return nil, []Issue{{Kind: "warning", OwnerID: int32(sp.ID), Owner: sp.Name, Detail: "could not determine enchant slot from EquippedItemClass/InventoryTypeMask"}}
	}
	s, problems := src.EnchantStats(ench)
	var issues []Issue
	for _, p := range problems {
		issues = append(issues, Issue{Kind: "enchant", OwnerID: int32(enchID), Owner: ench.Name, SpellID: sp.ID, Detail: p})
	}
	name := ench.Name
	if name == "" || strings.HasPrefix(name, "+") {
		name = strings.TrimPrefix(sp.Name, "Enchant ")
	}
	return &proto.UIEnchant{
		EffectId:    int32(enchID),
		ItemId:      itemID,
		SpellId:     int32(sp.ID),
		Name:        name,
		Icon:        sp.Icon,
		Type:        t,
		ExtraTypes:  extra,
		EnchantType: et,
		Stats:       s[:],
		Quality:     proto.ItemQuality_ItemQualityCommon,
		Phase:       phase,
	}, issues
}
