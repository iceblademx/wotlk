package dbc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnumTables(t *testing.T) {
	// 3.3.5a has effects 0..164 and auras 0..316.
	if len(effectNames) != 165 {
		t.Errorf("effect names: got %d, want 165", len(effectNames))
	}
	if len(auraNames) != 317 {
		t.Errorf("aura names: got %d, want 317", len(auraNames))
	}
	checks := map[uint32]string{AuraProcTriggerSpell: "PROC_TRIGGER_SPELL", AuraModRating: "MOD_RATING", AuraModAttackPower: "MOD_ATTACK_POWER",
		AuraModExpertise: "MOD_EXPERTISE", AuraModTargetResistance: "MOD_TARGET_RESISTANCE", 316: "PERIODIC_HASTE"}
	for id, want := range checks {
		if got := AuraName(id); got != want {
			t.Errorf("AuraName(%d) = %s, want %s", id, got, want)
		}
	}
	if got := EffectName(EffectEnchantItem); got != "ENCHANT_ITEM" {
		t.Errorf("EffectName(53) = %s", got)
	}
	if got := EffectName(EffectEnchantPrismatic); got != "ENCHANT_ITEM_PRISMATIC" {
		t.Errorf("EffectName(156) = %s", got)
	}
}

func TestSpellDecodeAndFormat(t *testing.T) {
	dir := t.TempDir()

	spells := NewBuilder(SpellFieldCount)
	spells.Add(map[int]any{
		spId: uint32(90003), spName: "Test Haste", spDescription: "Increases haste rating by $s1 for $d.",
		spDurationIndex: uint32(1), spSpellIconID: uint32(7), spProcChance: uint32(10),
		spEffect: uint32(EffectApplyAura), spEffectApplyAuraName: uint32(AuraModRating), spEffectBasePoints: 199, spEffectDieSides: 1,
		spEffectMiscValue: int32(1<<17 | 1<<18 | 1<<19), spEffectImplicitTargetA: uint32(1),
		spEffect + 1: uint32(EffectApplyAura), spEffectApplyAuraName + 1: uint32(AuraModStat), spEffectBasePoints + 1: 9, spEffectDieSides + 1: 11,
		spEffectSpellClassMaskA + 3: uint32(0xAB), spSpellFamilyFlags + 2: uint32(0x55), spEffectBonusMultiplier + 2: float32(0.5),
	})
	durations := NewBuilder(4)
	durations.Add(map[int]any{0: uint32(1), 1: int32(10000)})
	icons := NewBuilder(2)
	icons.Add(map[int]any{0: uint32(7), 1: `Interface\Icons\Spell_Nature_Bloodlust`})

	write := func(name string, b *Builder) {
		if err := os.WriteFile(filepath.Join(dir, name), b.Bytes(), 0666); err != nil {
			t.Fatal(err)
		}
	}
	write("Spell.dbc", spells)
	write("spellduration.DBC", durations) // lookups are case-insensitive
	write("SpellIcon.dbc", icons)

	d, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := LoadSpells(d)
	if err != nil {
		t.Fatal(err)
	}
	sp := store.Spells[90003]
	if sp == nil {
		t.Fatal("spell not decoded")
	}
	if sp.Name != "Test Haste" || sp.DurationMs != 10000 || sp.Icon != "spell_nature_bloodlust" || sp.ProcChance != 10 {
		t.Errorf("unexpected spell: %+v", sp)
	}
	if e := sp.Effects[0]; e.Aura != AuraModRating || e.Avg() != 200 || e.AuraName != "MOD_RATING" {
		t.Errorf("unexpected effect 1: %+v (avg %v)", e, e.Avg())
	}
	if e := sp.Effects[1]; e.Min() != 10 || e.Max() != 20 || e.Avg() != 15 {
		t.Errorf("effect 2 range: %d-%d avg %v", e.Min(), e.Max(), e.Avg())
	}
	if sp.Effects[1].SpellClassMask[0] != 0xAB || sp.SpellFamilyFlags[2] != 0x55 || sp.Effects[2].BonusMultiplier != 0.5 {
		t.Errorf("class mask / family flags / bonus multiplier misdecoded: %+v", sp)
	}
	if want := "Increases haste rating by 200 for 10 sec."; sp.Text != want {
		t.Errorf("text = %q, want %q", sp.Text, want)
	}
}

func TestLayoutMismatchIsRejected(t *testing.T) {
	f, err := Parse("Spell.dbc", NewBuilder(233).Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Expect(SpellFieldCount); err == nil {
		t.Error("expected a layout error for a 233-field Spell.dbc")
	}
}
