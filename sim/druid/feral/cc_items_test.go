package feral

import (
	"testing"

	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/druid"
)

// Tests for custom 3.3.5a server content in sim/druid/cc_items.go.

const (
	prowlerNeck   = 901017
	prowlerRing   = 901018
	prowlerCloak  = 901019
	prowlerWeapon = 901020
)

// StandardTalents without Omen of Clarity, so every Clearcasting comes from the ring.
const talentsNoOoc = "-503202132322010053120230310511-205503002"

func catRequest(talents string, items map[proto.ItemSlot]int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../../ui/feral_druid/gear_sets", "p3").GearSet
	for slot, id := range items {
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	return &proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceTauren,
			Class:         proto.Class_ClassDruid,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          PlayerOptionsMonoCat,
			TalentsString: talents,
			Glyphs:        StandardGlyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../../ui/feral_druid/apls", "default").Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: 1, RandomSeed: 1, IsTest: true},
	}
}

// newCat builds a reset simulation whose spells can be driven by hand. Debuffs are disabled so no
// flat "bonus damage taken" muddies damage ratios.
func newCat(t *testing.T, talents string, items map[proto.ItemSlot]int32) (*core.Simulation, *FeralDruid, *core.Unit) {
	t.Helper()
	req := catRequest(talents, items)
	req.Raid.Debuffs = &proto.Debuffs{}
	sim := core.NewSim(req)
	sim.Reset()
	cat := sim.Raid.Parties[0].Players[0].(*FeralDruid)
	return sim, cat, sim.Encounter.TargetUnits[0]
}

func setComboPoints(sim *core.Simulation, cat *FeralDruid, cp int32) {
	metrics := cat.MangleCat.ComboPointMetrics()
	if cat.ComboPoints() > 0 {
		cat.SpendComboPoints(sim, metrics)
	}
	cat.AddComboPoints(sim, cp, metrics)
}

func TestRavagingClawBiteGrantsClearcasting(t *testing.T) {
	ring := map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: druid.MorgrathsRavagingClawItemID}
	sim, cat, target := newCat(t, talentsNoOoc, ring)
	if cat.ClearcastingAura == nil {
		t.Fatal("the ring must provide Clearcasting even without the Omen of Clarity talent")
	}

	for cp := int32(1); cp <= 5; cp++ {
		landed, procs := 0, 0
		for i := 0; i < 2000; i++ {
			cat.ClearcastingAura.Deactivate(sim)
			setComboPoints(sim, cat, cp)
			cat.FerociousBite.SkipCastAndApplyEffects(sim, target)
			if cat.ComboPoints() != 0 { // missed/dodged: combo points are kept and nothing procs
				if cat.ClearcastingAura.IsActive() {
					t.Fatal("Clearcasting procced from a Ferocious Bite that didn't land")
				}
				continue
			}
			landed++
			if cat.ClearcastingAura.IsActive() {
				procs++
			}
		}
		rate, want := float64(procs)/float64(landed), 0.2*float64(cp)
		if cp == 5 && procs != landed {
			t.Errorf("5 combo point bites must always proc: %d/%d", procs, landed)
		} else if rate < want-0.04 || rate > want+0.04 {
			t.Errorf("%d CP: Clearcasting rate %.3f, want %.2f", cp, rate, want)
		}
	}
}

func TestRavagingClawDoublesClearcastBuilders(t *testing.T) {
	ring := map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: druid.MorgrathsRavagingClawItemID}

	// Returns damage dealt and whether it crit.
	damage := func(sim *core.Simulation, spell *core.Spell, target *core.Unit, seed int64, clearcast *core.Aura, withCC bool) (float64, bool) {
		sim.Cleanup() // drop gear procs left over from the previous cast
		sim.Reset()
		sim.Reseed(seed)
		if withCC {
			clearcast.Activate(sim)
		} else {
			clearcast.Deactivate(sim)
		}
		m := &spell.SpellMetrics[target.UnitIndex]
		dmgBefore, critsBefore := m.TotalDamage, m.Crits
		spell.SkipCastAndApplyEffects(sim, target)
		return m.TotalDamage - dmgBefore, m.Crits > critsBefore
	}

	check := func(name string, items map[proto.ItemSlot]int32, wantRatio float64) {
		sim, cat, target := newCat(t, StandardTalents, items)
		for _, spell := range []*druid.DruidSpell{cat.Shred, cat.MangleCat} {
			compared := 0
			for seed := int64(1); seed <= 60; seed++ {
				plain, plainCrit := damage(sim, spell.Spell, target, seed, cat.ClearcastingAura, false)
				boosted, boostedCrit := damage(sim, spell.Spell, target, seed, cat.ClearcastingAura, true)
				// Only compare casts with the same outcome (both landed, same crit state).
				if plain == 0 || boosted == 0 || plainCrit != boostedCrit {
					continue
				}
				compared++
				if ratio := boosted / plain; ratio < wantRatio-1e-9 || ratio > wantRatio+1e-9 {
					t.Errorf("%s %s seed %d: clearcast/plain damage = %.4f, want %.1f", name, spell.ActionID, seed, ratio, wantRatio)
				}
			}
			if compared < 40 {
				t.Errorf("%s: only %d comparable casts", name, compared)
			}
		}
	}
	check("with ring", ring, 2)
	check("without ring", nil, 1)
}

func TestProwlerOfTheFeveredCanopy(t *testing.T) {
	set := func(n int) map[proto.ItemSlot]int32 {
		pieces := []struct {
			slot proto.ItemSlot
			id   int32
		}{{proto.ItemSlot_ItemSlotNeck, prowlerNeck}, {proto.ItemSlot_ItemSlotFinger2, prowlerRing},
			{proto.ItemSlot_ItemSlotBack, prowlerCloak}, {proto.ItemSlot_ItemSlotMainHand, prowlerWeapon}}
		m := map[proto.ItemSlot]int32{}
		for _, p := range pieces[:n] {
			m[p.slot] = p.id
		}
		return m
	}
	metricsFor := func(n int) map[int32]*proto.TargetedActionMetrics {
		req := catRequest(StandardTalents, set(n))
		req.SimOptions = &proto.SimOptions{Iterations: 200, RandomSeed: 5, IsTest: true}
		res := core.RunRaidSim(req)
		if res.ErrorResult != "" {
			t.Fatal(res.ErrorResult)
		}
		out := map[int32]*proto.TargetedActionMetrics{}
		for _, a := range res.RaidMetrics.Parties[0].Players[0].Actions {
			if a.Id.GetSpellId() != 0 && len(a.Targets) > 0 {
				out[a.Id.GetSpellId()] = a.Targets[0]
			}
		}
		return out
	}

	one := metricsFor(1)
	if one[900342] != nil {
		t.Error("Bloodseeker Vines active with only 1 set piece")
	}

	two := metricsFor(2)
	vines, rake, rip := two[900342], two[48574], two[49800]
	if vines == nil || vines.Damage <= 0 {
		t.Fatal("2pc: Bloodseeker Vines never applied")
	}
	if two[900343] != nil {
		t.Error("2pc: Bloodseeker Thorns must require the 4pc")
	}
	// Each Rake hit, Rake tick and Rip tick rolls 10%. Rake/Rip hit counts include their ticks.
	triggers := float64(rake.Hits + rake.Crits + rip.Hits + rip.Crits)
	if rate := float64(vines.Casts) / triggers; rate < 0.08 || rate > 0.12 {
		t.Errorf("2pc: vines applied on %.3f of Rip/Rake damage events, want ~0.10", rate)
	}

	four := metricsFor(4)
	thorns := four[900343]
	if thorns == nil || thorns.Damage <= 0 {
		t.Fatal("4pc: Bloodseeker Thorns never exploded")
	}
	// Every application either expires naturally, is consumed by Ferocious Bite, is refreshed, or
	// is still ticking at the end of the fight: explosions can't exceed applications.
	if thorns.Casts > four[900342].Casts || thorns.Casts < four[900342].Casts/3 {
		t.Errorf("4pc: %d explosions for %d vine applications", thorns.Casts, four[900342].Casts)
	}
}
