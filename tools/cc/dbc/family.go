package dbc

import "sort"

// In 3.3.5a, class talents, glyphs, set bonuses, buffs and debuffs select the spells they modify by
// spell family: an effect with a SpellClassMask applies only to spells of the same SpellFamilyName
// whose SpellFamilyFlags share a bit with the mask. A custom spell with no family flags is therefore
// untouched by all of them, whatever the sim's class code does for real spells.
//
// familyRestricted lists the effects the server matches this way (AzerothCore's IsAffectedOnSpell,
// proc family masks and class scripts). Other auras with a mask, such as MOD_DAMAGE_PERCENT_DONE
// (Murder, Hunger for Blood, Titan's Grip), are applied by school and ignore it.
var familyRestricted = map[string]string{
	"ADD_FLAT_MODIFIER":             "spell modifier",
	"ADD_PCT_MODIFIER":              "spell modifier",
	"MOD_DAMAGE_FROM_CASTER":        "damage taken from the caster",
	"ABILITY_IGNORE_AURASTATE":      "spell modifier",
	"ADD_TARGET_TRIGGER":            "class proc",
	"PROC_TRIGGER_SPELL":            "class proc",
	"PROC_TRIGGER_DAMAGE":           "class proc",
	"PROC_TRIGGER_SPELL_WITH_VALUE": "class proc",
	"DUMMY":                         "class script",
	"PERIODIC_DUMMY":                "class script",
	"OVERRIDE_CLASS_SCRIPTS":        "class script",
	"SCRIPT_EFFECT":                 "class script",
}

// MechanicBleed is the Mechanic of bleeds, which Mangle, Trauma and similar debuffs amplify
// (MOD_MECHANIC_DAMAGE_TAKEN_PERCENT) and which Bleed-based talents check.
const MechanicBleed = 15

// ClassModifier is an effect of another spell that applies to a spell because of its family flags.
type ClassModifier struct {
	SpellID     uint32 `json:"spellId"`
	Name        string `json:"name"`
	Kind        string `json:"kind"` // spell modifier, damage taken from the caster, class proc, class script
	EffectIndex int    `json:"effectIndex"`
	Effect      string `json:"effect"`
	Aura        uint32 `json:"aura,omitempty"`
	AuraName    string `json:"auraName,omitempty"`
	MiscValue   int32  `json:"miscValue,omitempty"`
	BasePoints  int32  `json:"basePoints"`
}

// ClassModifiers returns the family-restricted effects of spells in the same family whose class mask
// matches sp's family flags, sorted by spell ID. It is empty when sp has no family flags.
func ClassModifiers(all map[uint32]*Spell, sp *Spell) []ClassModifier {
	if sp.SpellFamilyName == 0 || sp.SpellFamilyFlags == [3]uint32{} {
		return nil
	}
	var out []ClassModifier
	for id, other := range all {
		if id == sp.ID || other.SpellFamilyName != sp.SpellFamilyName {
			continue
		}
		for i, e := range other.Effects {
			if e.Effect == 0 || !masksOverlap(e.SpellClassMask, sp.SpellFamilyFlags) {
				continue
			}
			kind := familyRestricted[e.AuraName]
			if e.AuraName == "" {
				kind = familyRestricted[e.EffectName]
			}
			if kind == "" {
				continue
			}
			out = append(out, ClassModifier{SpellID: id, Name: other.Name, Kind: kind, EffectIndex: i, Effect: e.EffectName,
				Aura: e.Aura, AuraName: e.AuraName, MiscValue: e.MiscValue, BasePoints: e.BasePoints})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SpellID != out[j].SpellID {
			return out[i].SpellID < out[j].SpellID
		}
		return out[i].EffectIndex < out[j].EffectIndex
	})
	return out
}

// FamilyMaskMatches reports whether a proc or modifier restricted to family/mask selects sp.
func FamilyMaskMatches(family uint32, mask [3]uint32, sp *Spell) bool {
	return family != 0 && family == sp.SpellFamilyName && masksOverlap(mask, sp.SpellFamilyFlags)
}

// IsBleed reports whether the spell or one of its effects has the Bleed mechanic.
func (sp *Spell) IsBleed() bool {
	if sp.Mechanic == MechanicBleed {
		return true
	}
	for _, e := range sp.Effects {
		if e.Effect != 0 && e.Mechanic == MechanicBleed {
			return true
		}
	}
	return false
}

func masksOverlap(a, b [3]uint32) bool {
	return a[0]&b[0] != 0 || a[1]&b[1] != 0 || a[2]&b[2] != 0
}
