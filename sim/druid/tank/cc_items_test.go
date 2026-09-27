package tank

import (
	"math"
	"testing"

	"github.com/wowsims/wotlk/sim/common/cc"
	"github.com/wowsims/wotlk/sim/common/cc/ccfamily"
	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	googleProto "google.golang.org/protobuf/proto"
)

// Tests for custom 3.3.5a server content in sim/druid/cc_items.go.

var ursocsClawPieces = [4]int32{901021, 901022, 901023, 901024}

// ursocsClaw equips the first n pieces (neck, ring, cloak, staff).
func ursocsClaw(n int) map[proto.ItemSlot]int32 {
	slots := [4]proto.ItemSlot{proto.ItemSlot_ItemSlotNeck, proto.ItemSlot_ItemSlotFinger2, proto.ItemSlot_ItemSlotBack, proto.ItemSlot_ItemSlotMainHand}
	m := map[proto.ItemSlot]int32{}
	for i := 0; i < n; i++ {
		m[slots[i]] = ursocsClawPieces[i]
	}
	return m
}

func bearRequest(items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../../ui/feral_tank_druid/gear_sets", "p4").GearSet
	for slot, id := range items {
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	// A deep copy: sims write their class buffs into the raid buffs, which are shared globals.
	return googleProto.Clone(&proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceTauren,
			Class:         proto.Class_ClassDruid,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          PlayerOptionsDefault,
			TalentsString: StandardTalents,
			Glyphs:        StandardGlyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../../ui/feral_tank_druid/apls", "default").Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 9, IsTest: true},
	}).(*proto.RaidSimRequest)
}

func newBear(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *FeralTankDruid, *core.Unit) {
	t.Helper()
	req := bearRequest(items, 1)
	req.Raid.Debuffs = &proto.Debuffs{}
	sim := core.NewSim(req)
	sim.Reset()
	return sim, sim.Raid.Parties[0].Players[0].(*FeralTankDruid), sim.Encounter.TargetUnits[0]
}

// maulUntilLanded casts Maul until it lands and returns the bonus damage the set dealt with it.
func maulUntilLanded(t *testing.T, sim *core.Simulation, bear *FeralTankDruid, target *core.Unit, before func()) float64 {
	t.Helper()
	strike := bear.GetSpell(core.ActionID{SpellID: 900353})
	maul := bear.Maul.SpellMetrics[target.UnitIndex]
	for i := 0; i < 100; i++ {
		before()
		landed := maul.Hits + maul.Crits
		damage := strike.SpellMetrics[target.UnitIndex].TotalDamage
		bear.Maul.SkipCastAndApplyEffects(sim, target)
		maul = bear.Maul.SpellMetrics[target.UnitIndex]
		if maul.Hits+maul.Crits > landed {
			return strike.SpellMetrics[target.UnitIndex].TotalDamage - damage
		}
	}
	t.Fatal("Maul never landed")
	return 0
}

func TestUrsocsTaintedClaw(t *testing.T) {
	sim, bear, target := newBear(t, ursocsClaw(4))
	empowered, ravage := bear.GetAura("Empowered Maul"), bear.GetAura("Ravage")
	cost := bear.Maul.DefaultCast.Cost

	empowered.Activate(sim)
	if bear.Maul.DefaultCast.Cost != 0 {
		t.Error("an empowered Maul must be free")
	}
	single := maulUntilLanded(t, sim, bear, target, func() { empowered.Activate(sim) })
	if empowered.IsActive() || bear.Maul.DefaultCast.Cost != cost {
		t.Error("Maul must consume the empowerment and restore its cost")
	}
	if none := maulUntilLanded(t, sim, bear, target, func() {}); none != 0 || single <= 0 {
		t.Errorf("empowered Maul bonus %.1f, plain Maul bonus %.1f", single, none)
	}

	// Ravage deals 250% of an empowered Maul (either may crit).
	ravaged := maulUntilLanded(t, sim, bear, target, func() { ravage.Activate(sim) })
	crit := bear.GetSpell(core.ActionID{SpellID: 900353}).CritMultiplier
	ok := false
	for _, ratio := range []float64{2.5, 2.5 * crit, 2.5 / crit} {
		ok = ok || math.Abs(ravaged/single-ratio) < 1e-6
	}
	if !ok {
		t.Errorf("Ravage dealt %.1f, empowered Maul %.1f: want 250%%", ravaged, single)
	}

	// Ravage only procs during Berserk.
	ravage.Deactivate(sim)
	for i := 0; i < 50; i++ {
		bear.BerserkAura.Deactivate(sim)
		bear.MangleBear.SkipCastAndApplyEffects(sim, target)
		if ravage.IsActive() {
			t.Fatal("Ravage procced outside Berserk")
		}
	}
	procs := 0
	for i := 0; i < 1000; i++ {
		ravage.Deactivate(sim)
		bear.BerserkAura.Activate(sim)
		bear.Lacerate.SkipCastAndApplyEffects(sim, target)
		if ravage.IsActive() {
			procs++
		}
	}
	// Lacerate can miss, so a little under 20% of casts.
	if rate := float64(procs) / 1000; rate < 0.15 || rate > 0.24 {
		t.Errorf("Ravage on %.1f%% of Lacerates during Berserk, want 20%% of hits", 100*rate)
	}

	m := core.RunRaidSim(bearRequest(ursocsClaw(4), 5))
	if m.ErrorResult != "" {
		t.Fatal(m.ErrorResult)
	}
	for _, a := range m.RaidMetrics.Parties[0].Players[0].Actions {
		if a.Id.GetSpellId() == 900353 && a.Targets[0].Damage > 0 {
			return
		}
	}
	t.Error("the rotation never used an empowered Maul")
}

// Logs the DPS impact of each custom item (go test -run CCDps -v).
func TestCCDpsImpactBear(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	run := func(items map[proto.ItemSlot]int32) float64 {
		res := core.RunRaidSim(bearRequest(items, iterations))
		if res.ErrorResult != "" {
			t.Fatal(res.ErrorResult)
		}
		return res.RaidMetrics.Parties[0].Players[0].Dps.Avg
	}
	base := run(nil)
	for _, n := range []int{2, 4} {
		dps, statsOnly := run(ursocsClaw(n)), run(cc.StatOnlyCopies(ursocsClaw(n)))
		t.Logf("Ursoc's Tainted Claw %dpc %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f)",
			n, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base)
	}
}

// Every custom spell gets only the class modifiers its 3.3.5a family flags allow.
func TestClassModifiers(t *testing.T) {
	sim, _, _ := newBear(t, ursocsClaw(4))
	ccfamily.Check(t, nil, sim)
}
