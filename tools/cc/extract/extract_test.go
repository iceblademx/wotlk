package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/core/stats"
	"github.com/wowsims/wotlk/tools"
	"github.com/wowsims/wotlk/tools/cc/dbc"
	"github.com/wowsims/wotlk/tools/database"
)

// Builds a small but complete cc_data/ fixture using the real 3.3.5a layouts.
func writeFixture(t *testing.T, root string) {
	t.Helper()
	dbcDir := filepath.Join(root, "dbc")
	serverDir := filepath.Join(root, "server")
	for _, d := range []string{dbcDir, serverDir} {
		if err := os.MkdirAll(d, 0777); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name string, b *dbc.Builder) {
		if err := os.WriteFile(filepath.Join(dbcDir, name), b.Bytes(), 0666); err != nil {
			t.Fatal(err)
		}
	}

	const (
		auraOnSelf = 1
	)
	spell := func(id uint32, name, desc string, fields map[int]any) map[int]any {
		m := map[int]any{dbc.SpellFieldID: id, dbc.SpellFieldName: name, dbc.SpellFieldDescription: desc}
		for k, v := range fields {
			m[k] = v
		}
		return m
	}
	aura := func(i int, auraID uint32, value int, misc int32) map[int]any {
		return map[int]any{
			dbc.SpellFieldEffect + i: uint32(dbc.EffectApplyAura), dbc.SpellFieldAura + i: auraID,
			dbc.SpellFieldBasePoints + i: value - 1, dbc.SpellFieldDieSides + i: 1,
			dbc.SpellFieldMiscValue + i: misc, dbc.SpellFieldTargetA + i: uint32(auraOnSelf),
		}
	}
	merge := func(ms ...map[int]any) map[int]any {
		out := map[int]any{}
		for _, m := range ms {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}

	spells := dbc.NewBuilder(dbc.SpellFieldCount)
	// Equip: +100 attack power (folds into item stats). Like real 3.3.5a data, melee and ranged AP are separate auras.
	spells.Add(spell(90001, "Attack Power 100", "Increases attack power by $s1.",
		merge(aura(0, dbc.AuraModAttackPower, 100, 0), aura(1, dbc.AuraModRangedAttackPower, 100, 0))))
	// Equip: proc -> 90003, 10% chance on melee.
	spells.Add(spell(90002, "Test Proc", "Chance on melee hit to gain haste.", merge(map[int]any{
		dbc.SpellFieldEffect: uint32(dbc.EffectApplyAura), dbc.SpellFieldAura: uint32(dbc.AuraProcTriggerSpell),
		dbc.SpellFieldTriggerSpell: uint32(90003), dbc.SpellFieldTargetA: uint32(auraOnSelf),
		dbc.SpellFieldProcFlags: uint32(0x14), dbc.SpellFieldProcChance: uint32(10),
	})))
	// Buff: +200 haste rating (melee, ranged, spell) for 10s, stacks to 5.
	spells.Add(spell(90003, "Test Haste", "Haste rating increased by $s1.", merge(aura(0, dbc.AuraModRating, 200, 1<<17|1<<18|1<<19),
		map[int]any{dbc.SpellFieldDurationIndex: uint32(1), dbc.SpellFieldStackAmount: uint32(5)})))
	// Set bonus: +50 spell power.
	spells.Add(spell(90004, "Set SP", "Increases spell power by $s1.", aura(0, dbc.AuraModDamageDone, 50, 126)))
	// Use: +300 crit rating for 20s, 2 min cooldown.
	spells.Add(spell(90005, "Test Crit", "Increases critical strike rating by $s1 for $d.", merge(aura(0, dbc.AuraModRating, 300, 1<<8|1<<9|1<<10),
		map[int]any{dbc.SpellFieldDurationIndex: uint32(2), dbc.SpellFieldRecoveryTime: uint32(120000)})))
	// Set bonus needing code: fire damage only.
	spells.Add(spell(90006, "Set Fire", "Increases fire damage by $s1.", aura(0, dbc.AuraModDamageDone, 40, 4)))
	// Enchant Cloak - Test Stamina (enchant 9001).
	spells.Add(spell(90010, "Enchant Cloak - Test Stamina", "", map[int]any{
		dbc.SpellFieldEffect: uint32(dbc.EffectEnchantItem), dbc.SpellFieldMiscValue: int32(9001),
		dbc.SpellFieldEquippedClass: int32(4), dbc.SpellFieldEquippedInv: int32(1 << 16), dbc.SpellFieldSpellIconID: uint32(7),
	}))
	write("Spell.dbc", spells)

	durations := dbc.NewBuilder(4)
	durations.Add(map[int]any{0: uint32(1), 1: int32(10000)})
	durations.Add(map[int]any{0: uint32(2), 1: int32(20000)})
	write("SpellDuration.dbc", durations)

	icons := dbc.NewBuilder(2)
	icons.Add(map[int]any{0: uint32(7), 1: `Interface\Icons\Spell_Holy_GreaterHeal`})
	write("SpellIcon.dbc", icons)

	sets := dbc.NewBuilder(53)
	sets.Add(map[int]any{0: uint32(900), 1: "Regalia of Testing", 18: uint32(900102), 19: uint32(900104), 35: uint32(90004), 43: uint32(2)})
	sets.Add(map[int]any{0: uint32(901), 1: "Flames of Testing", 18: uint32(900105), 35: uint32(90006), 43: uint32(2)})
	write("ItemSet.dbc", sets)

	enchants := dbc.NewBuilder(38)
	enchants.Add(map[int]any{0: uint32(9001), 2: uint32(dbc.EnchantTypeStat), 5: 22, 8: 22, 11: uint32(7), 14: "+22 Stamina"})
	enchants.Add(map[int]any{0: uint32(9002), 2: uint32(dbc.EnchantTypeStat), 5: 23, 8: 23, 11: uint32(45), 14: "+23 Spell Power"})
	enchants.Add(map[int]any{0: uint32(9003), 2: uint32(dbc.EnchantTypeStat), 5: 8, 8: 8, 11: uint32(32), 14: "+8 Critical Strike Rating"})
	write("SpellItemEnchantment.dbc", enchants)

	gems := dbc.NewBuilder(5)
	gems.Add(map[int]any{0: uint32(800), 1: uint32(9002), 4: uint32(2)})
	write("GemProperties.dbc", gems)

	display := dbc.NewBuilder(25)
	display.Add(map[int]any{0: uint32(5000), 5: "INV_Sword_39"})
	write("ItemDisplayInfo.dbc", display)

	sql := "CREATE TABLE `item_template` (`entry` int, `class` int, `subclass` int, `name` varchar(255), `displayid` int, `Quality` int, " +
		"`InventoryType` int, `ItemLevel` int, `stat_type1` int, `stat_value1` int, `stat_type2` int, `stat_value2` int, " +
		"`dmg_min1` float, `dmg_max1` float, `armor` int, `delay` int, `spellid_1` int, `spelltrigger_1` int, `spellcooldown_1` int, " +
		"`itemset` int, `socketColor_1` int, `socketColor_2` int, `socketBonus` int, `GemProperties` int, `AllowableClass` int, `AllowableRace` int);\n" +
		"INSERT INTO `item_template` VALUES " +
		"(900100,4,0,'Band of Testing',0,4,11,264,7,30,45,40,0,0,0,0,90001,1,-1,0,0,0,0,0,-1,-1)," +
		"(900101,4,0,'Trinket of Testing',0,4,12,264,0,0,0,0,0,0,0,0,90005,0,-1,0,0,0,0,0,-1,-1)," +
		"(900102,2,15,'Dagger of Testing',5000,4,13,264,3,50,0,0,300,500,0,1800,90002,1,-1,900,2,4,9003,0,8,-1)," +
		"(900103,3,0,'Test Ruby',0,4,0,80,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,800,-1,-1)," +
		"(900104,4,1,'Robe of Testing',0,4,20,264,5,60,0,0,0,0,150,0,0,0,0,900,0,0,0,0,-1,1101)," +
		"(900105,4,4,'Helm of Testing',0,4,1,264,4,70,0,0,0,0,2000,0,0,0,0,901,0,0,0,0,-1,-1);\n" +
		"INSERT INTO spell_proc (SpellId, ProcFlags, HitMask, ProcsPerMinute, Chance, Cooldown) VALUES (90002, 20, 0, 0, 0, 45000);\n"
	if err := os.WriteFile(filepath.Join(serverDir, "world.sql"), []byte(sql), 0666); err != nil {
		t.Fatal(err)
	}
}

func TestExtractEndToEnd(t *testing.T) {
	root := t.TempDir()
	inDir := filepath.Join(root, "cc_data")
	assets := filepath.Join(root, "assets")
	simDir := filepath.Join(root, "sim")
	genPath := filepath.Join(root, "zz_generated.go")
	writeFixture(t, inDir)
	os.MkdirAll(simDir, 0777)

	ex, outDir, err := runExtract(inDir, assets, simDir, genPath)
	if err != nil {
		t.Fatal(err)
	}

	db := database.ReadDatabaseFromJson(tools.ReadFile(filepath.Join(outDir, "cc_db.json")))
	if len(db.Items) != 5 || len(db.Gems) != 1 || len(db.Enchants) != 1 {
		t.Fatalf("got %d items, %d gems, %d enchants", len(db.Items), len(db.Gems), len(db.Enchants))
	}

	ring := db.Items[900100]
	if ring.Type != proto.ItemType_ItemTypeFinger || ring.Stats[stats.Stamina] != 30 || ring.Stats[stats.SpellPower] != 40 ||
		ring.Stats[stats.AttackPower] != 100 || ring.Stats[stats.RangedAttackPower] != 100 {
		t.Errorf("ring: %+v", ring)
	}

	dagger := db.Items[900102]
	if dagger.Type != proto.ItemType_ItemTypeWeapon || dagger.WeaponType != proto.WeaponType_WeaponTypeDagger ||
		dagger.HandType != proto.HandType_HandTypeOneHand || dagger.WeaponSpeed != 1.8 || dagger.WeaponDamageMax != 500 ||
		dagger.Icon != "inv_sword_39" || dagger.SetName != "Regalia of Testing" ||
		len(dagger.GemSockets) != 2 || dagger.GemSockets[1] != proto.GemColor_GemColorYellow ||
		dagger.SocketBonus[stats.MeleeCrit] != 8 || dagger.SocketBonus[stats.SpellCrit] != 8 ||
		len(dagger.ClassAllowlist) != 1 || dagger.ClassAllowlist[0] != proto.Class_ClassRogue {
		t.Errorf("dagger: %+v", dagger)
	}

	robe := db.Items[900104]
	if robe.Type != proto.ItemType_ItemTypeChest || robe.ArmorType != proto.ArmorType_ArmorTypeCloth || robe.Stats[stats.Armor] != 150 ||
		robe.FactionRestriction != proto.UIItem_FACTION_RESTRICTION_ALLIANCE_ONLY {
		t.Errorf("robe: %+v", robe)
	}

	gem := db.Gems[900103]
	if gem.Color != proto.GemColor_GemColorRed || gem.Stats[stats.SpellPower] != 23 {
		t.Errorf("gem: %+v", gem)
	}
	for _, e := range db.Enchants {
		if e.EffectId != 9001 || e.Type != proto.ItemType_ItemTypeBack || e.Stats[stats.Stamina] != 22 || e.Icon != "spell_holy_greaterheal" {
			t.Errorf("enchant: %+v", e)
		}
	}

	gen, _ := os.ReadFile(genPath)
	for _, want := range []string{
		`RegisterUseStat(900101, stats.Stats{stats.SpellCrit: 300, stats.MeleeCrit: 300}, 20*time.Second, 120*time.Second, true, false)`,
		`RegisterProcStat(ProcStat{ItemID: 900102, Name: "Dagger of Testing", AuraSpellID: 90003, Bonus: stats.Stats{stats.SpellHaste: 200, stats.MeleeHaste: 200}, Duration: 10 * time.Second, MaxStacks: 5, ProcFlags: 0x14, HitMask: 0x0, ProcChance: 0.1, PPM: 0, ICD: 45 * time.Second})`,
		`RegisterStatSet("Regalia of Testing", map[int32]stats.Stats{`,
		`2: stats.Stats{stats.SpellPower: 50},`,
	} {
		if !strings.Contains(string(gen), want) {
			t.Errorf("generated code missing %q\n%s", want, gen)
		}
	}
	if strings.Contains(string(gen), "Flames of Testing") {
		t.Error("set with a non-stat bonus must not be generated")
	}

	report, _ := os.ReadFile(filepath.Join(outDir, "REPORT.md"))
	if !strings.Contains(string(report), "Flames of Testing") || !strings.Contains(string(report), "school-specific spell damage") {
		t.Errorf("report should list the fire set bonus as needing code:\n%s", report)
	}
	if ex.generatedCount() != 3 || ex.todoCount() != 1 {
		t.Errorf("generated=%d todo=%d, want 3 and 1", ex.generatedCount(), ex.todoCount())
	}
	if _, err := os.Stat(filepath.Join(outDir, "server_item_ids.json")); err != nil {
		t.Error("complete item_template should produce server_item_ids.json")
	}
}

// Hand-written implementations must suppress generation.
func TestHandwrittenSuppressesGeneration(t *testing.T) {
	root := t.TempDir()
	inDir := filepath.Join(root, "cc_data")
	simDir := filepath.Join(root, "sim")
	genPath := filepath.Join(root, "zz_generated.go")
	writeFixture(t, inDir)
	os.MkdirAll(simDir, 0777)
	hand := "package cc\nfunc init() { core.NewItemEffect(900102, nil)\n core.NewItemSet(core.ItemSet{Name: \"Regalia of Testing\"}) }\n"
	os.WriteFile(filepath.Join(simDir, "custom.go"), []byte(hand), 0666)

	if _, _, err := runExtract(inDir, filepath.Join(root, "assets"), simDir, genPath); err != nil {
		t.Fatal(err)
	}
	gen, _ := os.ReadFile(genPath)
	if strings.Contains(string(gen), "900102") || strings.Contains(string(gen), "Regalia of Testing") {
		t.Errorf("hand-written item/set was regenerated:\n%s", gen)
	}
	if !strings.Contains(string(gen), "900101") {
		t.Errorf("unrelated item should still be generated:\n%s", gen)
	}
}
