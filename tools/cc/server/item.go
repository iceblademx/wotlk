// Package server reads 3.3.5a item data, which (unlike spells) is NOT in the client DBCs.
// Sources, in order of completeness:
//   - item_template SQL dumps / patches (TrinityCore, AzerothCore, MaNGOS-style column names)
//   - item_template CSV/TSV exports with a header row
//   - the client cache itemcache.wdb (only contains items the client has seen; no proc PPM)
//
// It also reads the proc/coefficient tables custom item effects usually depend on:
// spell_proc (TC), spell_proc_event (AC/older TC) and spell_bonus_data.
package server

import (
	"strconv"
	"strings"
)

type ItemStat struct {
	Type  uint32 `json:"type"`
	Value int32  `json:"value"`
}

type ItemSpell struct {
	SpellID          uint32  `json:"spellId"`
	Trigger          uint32  `json:"trigger"` // 0 use, 1 equip, 2 chance on hit, 4 soulstone, 5 use (no delay), 6 learn
	Charges          int32   `json:"charges,omitempty"`
	PPMRate          float64 `json:"ppm,omitempty"`
	Cooldown         int32   `json:"cooldownMs,omitempty"`
	Category         uint32  `json:"category,omitempty"`
	CategoryCooldown int32   `json:"categoryCooldownMs,omitempty"`
}

const (
	SpellTriggerUse        = 0
	SpellTriggerEquip      = 1
	SpellTriggerChanceHit  = 2
	SpellTriggerUseNoDelay = 5
)

type ItemTemplate struct {
	Entry             uint32 `json:"entry"`
	Class             uint32 `json:"class"`
	SubClass          uint32 `json:"subclass"`
	Name              string `json:"name"`
	DisplayID         uint32 `json:"displayId"`
	Quality           uint32 `json:"quality"`
	Flags             uint32 `json:"flags,omitempty"`
	InventoryType     uint32 `json:"inventoryType"`
	AllowableClass    int32  `json:"allowableClass"`
	AllowableRace     int32  `json:"allowableRace"`
	ItemLevel         uint32 `json:"itemLevel"`
	RequiredLevel     uint32 `json:"requiredLevel,omitempty"`
	RequiredSkill     uint32 `json:"requiredSkill,omitempty"`
	RequiredSkillRank uint32 `json:"requiredSkillRank,omitempty"`
	RequiredSpell     uint32 `json:"requiredSpell,omitempty"`
	MaxCount          int32  `json:"maxCount,omitempty"`

	Stats                   []ItemStat `json:"stats,omitempty"`
	ScalingStatDistribution uint32     `json:"scalingStatDistribution,omitempty"`
	ScalingStatValue        uint32     `json:"scalingStatValue,omitempty"`

	DamageMin  [2]float64 `json:"damageMin"`
	DamageMax  [2]float64 `json:"damageMax"`
	DamageType [2]uint32  `json:"damageType"`
	Armor      uint32     `json:"armor,omitempty"`
	// holy, fire, nature, frost, shadow, arcane
	Resistances [6]int32 `json:"resistances"`
	Delay       uint32   `json:"delayMs,omitempty"`

	Spells []ItemSpell `json:"spells,omitempty"`

	Bonding             uint32    `json:"bonding,omitempty"`
	Description         string    `json:"description,omitempty"`
	Block               uint32    `json:"block,omitempty"`
	ItemSet             uint32    `json:"itemSet,omitempty"`
	SocketColors        [3]uint32 `json:"socketColors"`
	SocketBonus         uint32    `json:"socketBonus,omitempty"`
	GemProperties       uint32    `json:"gemProperties,omitempty"`
	ArmorDamageModifier float64   `json:"armorDamageModifier,omitempty"`
	ItemLimitCategory   uint32    `json:"itemLimitCategory,omitempty"`
	RandomProperty      int32     `json:"randomProperty,omitempty"`
	RandomSuffix        uint32    `json:"randomSuffix,omitempty"`
	TotemCategory       uint32    `json:"totemCategory,omitempty"`
}

// Normalizes a column name: lowercase, no underscores/backticks. "spellppmRate_1" -> "spellppmrate1".
func normalizeColumn(name string) string {
	name = strings.Trim(strings.TrimSpace(name), "`\"[]")
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

// Column aliases across TrinityCore / AzerothCore / MaNGOS-family schemas.
var columnAliases = map[string]string{
	"id":                    "entry",
	"itemid":                "entry",
	"soundoverridesubclass": "soundoverridesubclass",
	"displayinfoid":         "displayid",
	"itemlevel":             "itemlevel",
	"ilevel":                "itemlevel",
	"requiredlevel":         "requiredlevel",
	"reqlevel":              "requiredlevel",
	"allowableclass":        "allowableclass",
	"allowablerace":         "allowablerace",
	"armordamagemodifier":   "armordamagemodifier",
}

// Row is one item_template row keyed by normalized column name. Values are raw strings (NULL -> "").
type Row map[string]string

func (r Row) str(col string) string { return r[col] }

func (r Row) u32(col string) uint32 {
	v := strings.TrimSpace(r[col])
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return uint32(n)
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return uint32(int64(f))
	}
	return 0
}

func (r Row) i32(col string) int32 { return int32(r.u32(col)) }

func (r Row) i64(col string) int64 {
	v := strings.TrimSpace(r[col])
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	f, _ := strconv.ParseFloat(v, 64)
	return int64(f)
}

func (r Row) f64(col string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(r[col]), 64)
	return f
}

func (r Row) has(col string) bool {
	_, ok := r[col]
	return ok
}

// NewRow builds a Row from parallel column/value slices, applying aliases.
func NewRow(columns []string, values []string) Row {
	row := make(Row, len(columns))
	for i, c := range columns {
		key := normalizeColumn(c)
		if alias, ok := columnAliases[key]; ok {
			key = alias
		}
		if i < len(values) {
			row[key] = values[i]
		}
	}
	return row
}

func (r Row) ToItemTemplate() *ItemTemplate {
	it := &ItemTemplate{
		Entry:                   r.u32("entry"),
		Class:                   r.u32("class"),
		SubClass:                r.u32("subclass"),
		Name:                    r.str("name"),
		DisplayID:               r.u32("displayid"),
		Quality:                 r.u32("quality"),
		Flags:                   r.u32("flags"),
		InventoryType:           r.u32("inventorytype"),
		AllowableClass:          r.i32("allowableclass"),
		AllowableRace:           r.i32("allowablerace"),
		ItemLevel:               r.u32("itemlevel"),
		RequiredLevel:           r.u32("requiredlevel"),
		RequiredSkill:           r.u32("requiredskill"),
		RequiredSkillRank:       r.u32("requiredskillrank"),
		RequiredSpell:           r.u32("requiredspell"),
		MaxCount:                r.i32("maxcount"),
		ScalingStatDistribution: r.u32("scalingstatdistribution"),
		ScalingStatValue:        r.u32("scalingstatvalue"),
		Armor:                   r.u32("armor"),
		Delay:                   r.u32("delay"),
		Bonding:                 r.u32("bonding"),
		Description:             r.str("description"),
		Block:                   r.u32("block"),
		ItemSet:                 r.u32("itemset"),
		SocketBonus:             r.u32("socketbonus"),
		GemProperties:           r.u32("gemproperties"),
		ArmorDamageModifier:     r.f64("armordamagemodifier"),
		ItemLimitCategory:       r.u32("itemlimitcategory"),
		RandomProperty:          r.i32("randomproperty"),
		RandomSuffix:            r.u32("randomsuffix"),
		TotemCategory:           r.u32("totemcategory"),
	}
	if !r.has("allowableclass") {
		it.AllowableClass = -1
	}
	if !r.has("allowablerace") {
		it.AllowableRace = -1
	}

	for i := 1; i <= 10; i++ {
		n := strconv.Itoa(i)
		t, v := r.u32("stattype"+n), r.i32("statvalue"+n)
		if v != 0 {
			it.Stats = append(it.Stats, ItemStat{Type: t, Value: v})
		}
	}
	for i := 0; i < 2; i++ {
		n := strconv.Itoa(i + 1)
		it.DamageMin[i] = r.f64("dmgmin" + n)
		it.DamageMax[i] = r.f64("dmgmax" + n)
		it.DamageType[i] = r.u32("dmgtype" + n)
	}
	for i, school := range []string{"holy", "fire", "nature", "frost", "shadow", "arcane"} {
		it.Resistances[i] = r.i32(school + "res")
	}
	for i := 1; i <= 5; i++ {
		n := strconv.Itoa(i)
		id := r.u32("spellid" + n)
		if id == 0 {
			continue
		}
		it.Spells = append(it.Spells, ItemSpell{
			SpellID:          id,
			Trigger:          r.u32("spelltrigger" + n),
			Charges:          r.i32("spellcharges" + n),
			PPMRate:          r.f64("spellppmrate" + n),
			Cooldown:         r.i32("spellcooldown" + n),
			Category:         r.u32("spellcategory" + n),
			CategoryCooldown: r.i32("spellcategorycooldown" + n),
		})
	}
	for i := 0; i < 3; i++ {
		it.SocketColors[i] = r.u32("socketcolor" + strconv.Itoa(i+1))
	}
	return it
}

// SpellProc is a server-side proc definition (spell_proc / spell_proc_event).
type SpellProc struct {
	SpellID         uint32    `json:"spellId"`
	SpellFamilyName uint32    `json:"spellFamilyName,omitempty"`
	SpellFamilyMask [3]uint32 `json:"spellFamilyMask"` // non-zero: only procs from specific class spells
	ProcFlags       uint32    `json:"procFlags,omitempty"`
	HitMask         uint32    `json:"hitMask,omitempty"` // TC HitMask / AC procEx: 0x1 normal hit, 0x2 crit, ...
	PPM             float64   `json:"ppm,omitempty"`
	Chance          float64   `json:"chance,omitempty"`
	CooldownMs      int32     `json:"cooldownMs,omitempty"`
	Charges         uint32    `json:"charges,omitempty"`
	Source          string    `json:"source"`
}

// SpellBonus is a spell_bonus_data row: spell power / attack power coefficients.
type SpellBonus struct {
	SpellID uint32  `json:"spellId"`
	Direct  float64 `json:"direct"`
	Dot     float64 `json:"dot"`
	AP      float64 `json:"ap"`
	APDot   float64 `json:"apDot"`
}

func rowToSpellProc(table string, r Row) *SpellProc {
	sp := &SpellProc{Source: table}
	switch table {
	case "spell_proc": // TrinityCore 3.3.5: Cooldown in ms
		sp.SpellID = r.u32("spellid")
		sp.ProcFlags = r.u32("procflags")
		sp.HitMask = r.u32("hitmask")
		sp.PPM = r.f64("procsperminute")
		sp.Chance = r.f64("chance")
		sp.CooldownMs = r.i32("cooldown")
		sp.Charges = r.u32("charges")
	default: // spell_proc_event: entry, procFlags, ppmRate, CustomChance, Cooldown
		sp.SpellID = r.u32("entry")
		sp.ProcFlags = r.u32("procflags")
		sp.HitMask = r.u32("procex")
		sp.PPM = r.f64("ppmrate")
		sp.Chance = r.f64("customchance")
		sp.CooldownMs = r.i32("cooldown")
		// Older schemas store the cooldown in seconds; no real ICD is below 1s, so small values are seconds.
		if sp.CooldownMs > 0 && sp.CooldownMs < 1000 {
			sp.CooldownMs *= 1000
		}
	}
	if sp.SpellID == 0 {
		sp.SpellID = r.u32("entry")
	}
	sp.SpellID = uint32(abs64(int64(int32(sp.SpellID))))
	sp.SpellFamilyName = r.u32("spellfamilyname")
	for i := 0; i < 3; i++ {
		sp.SpellFamilyMask[i] = r.u32("spellfamilymask" + strconv.Itoa(i))
	}
	return sp
}

func rowToSpellBonus(r Row) *SpellBonus {
	return &SpellBonus{
		SpellID: r.u32("entry"),
		Direct:  r.f64("directbonus"),
		Dot:     r.f64("dotbonus"),
		AP:      r.f64("apbonus"),
		APDot:   r.f64("apdotbonus"),
	}
}
