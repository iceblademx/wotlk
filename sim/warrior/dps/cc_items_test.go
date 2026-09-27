package dps

import (
	"math"
	"testing"

	"github.com/wowsims/wotlk/sim/common/cc"
	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/warrior"
)

// Tests for custom 3.3.5a server content in sim/warrior/cc_items.go.

var berserkerPieces = []struct {
	slot proto.ItemSlot
	id   int32
}{{proto.ItemSlot_ItemSlotNeck, 901117}, {proto.ItemSlot_ItemSlotFinger2, 901118},
	{proto.ItemSlot_ItemSlotBack, 901119}, {proto.ItemSlot_ItemSlotMainHand, 901120}}

func berserkerSet(n int) map[proto.ItemSlot]int32 {
	m := map[proto.ItemSlot]int32{}
	for _, p := range berserkerPieces[:n] {
		m[p.slot] = p.id
	}
	return m
}

func ring(id int32) map[proto.ItemSlot]int32 {
	return map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: id}
}

func furyRequest(items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../../ui/warrior/gear_sets", "p4_fury_horde").GearSet
	for slot, id := range items {
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	return &proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceOrc,
			Class:         proto.Class_ClassWarrior,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          PlayerOptionsFury,
			TalentsString: FuryTalents,
			Glyphs:        FuryGlyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../../ui/warrior/apls", "fury").Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 3, IsTest: true},
	}
}

func newFury(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *warrior.Warrior, *core.Unit) {
	t.Helper()
	req := furyRequest(items, 1)
	req.Raid.Debuffs = &proto.Debuffs{}
	sim := core.NewSim(req)
	sim.Reset()
	return sim, sim.Raid.Parties[0].Players[0].(*DpsWarrior).GetWarrior(), sim.Encounter.TargetUnits[0]
}

type ccSimResult struct {
	dps     float64
	actions map[core.ActionID]*proto.TargetedActionMetrics
	auras   map[core.ActionID]*proto.AuraMetrics
}

func runCCSim(t *testing.T, req *proto.RaidSimRequest) ccSimResult {
	t.Helper()
	res := core.RunRaidSim(req)
	if res.ErrorResult != "" {
		t.Fatal(res.ErrorResult)
	}
	player := res.RaidMetrics.Parties[0].Players[0]
	out := ccSimResult{dps: player.Dps.Avg, actions: map[core.ActionID]*proto.TargetedActionMetrics{}, auras: map[core.ActionID]*proto.AuraMetrics{}}
	for _, a := range player.Actions {
		if len(a.Targets) > 0 {
			out.actions[core.ProtoToActionID(a.Id)] = a.Targets[0]
		}
	}
	for _, a := range player.Auras {
		out.auras[core.ProtoToActionID(a.Id)] = a
	}
	return out
}

// castBloodthirst casts Bloodthirst without cost or cooldown and reports its outcome.
func castBloodthirst(sim *core.Simulation, war *warrior.Warrior, target *core.Unit) (landed, crit bool) {
	m := &war.Bloodthirst.SpellMetrics[target.UnitIndex]
	hits, crits := m.Hits, m.Crits
	war.Bloodthirst.SkipCastAndApplyEffects(sim, target)
	return m.Hits+m.Crits > hits+crits, m.Crits > crits
}

func TestRecklessFury(t *testing.T) {
	sim, war, target := newFury(t, ring(warrior.VorraxsRecklessFuryItemID))
	if war.Talents.SuddenDeath != 0 || war.SuddenDeathAura == nil {
		t.Fatal("the ring must provide Sudden Death to a fury warrior without the talent")
	}

	// Sudden Death procs on 20% of landed melee hits.
	const casts = 4000
	procs, hits := 0, 0
	for i := 0; i < casts; i++ {
		war.SuddenDeathAura.Deactivate(sim)
		if landed, _ := castBloodthirst(sim, war, target); landed {
			hits++
			if war.SuddenDeathAura.IsActive() {
				procs++
			}
		}
	}
	if rate := float64(procs) / float64(hits); math.Abs(rate-0.2) > 0.025 {
		t.Errorf("Sudden Death on %.3f of landed hits, want 0.20", rate)
	}

	// Each Execute adds +5% damage and +5% crit to later Executes.
	damageBefore, critBefore := war.Execute.DamageMultiplierAdditive, war.Execute.BonusCritRating
	for i := 0; i < 3; i++ {
		war.OnCastComplete(sim, war.Execute)
	}
	if got := war.Execute.DamageMultiplierAdditive - damageBefore; math.Abs(got-0.15) > 1e-9 {
		t.Errorf("3 Undying Fury stacks add %.3f Execute damage, want 0.15", got)
	}
	if got := war.Execute.BonusCritRating - critBefore; math.Abs(got-15*core.CritRatingPerCritChance) > 1e-9 {
		t.Errorf("3 Undying Fury stacks add %.1f crit rating, want 15%%", got)
	}

	// In a full fight Execute is used outside the execute phase.
	res := runCCSim(t, furyRequest(ring(warrior.VorraxsRecklessFuryItemID), 20))
	plain := runCCSim(t, furyRequest(nil, 20))
	execute := core.ActionID{SpellID: 47471}
	if res.actions[execute].Casts < 3*plain.actions[execute].Casts {
		t.Errorf("%d Executes with the ring vs %d without: Sudden Death procs aren't used", res.actions[execute].Casts, plain.actions[execute].Casts)
	}
}

func TestSlayersEdge(t *testing.T) {
	sim, war, target := newFury(t, ring(warrior.RokthulsSlayersEdgeItemID))
	strike := war.GetSpell(core.ActionID{SpellID: 200083})
	slayer := war.GetAura("Slayer")

	const casts = 4000
	hits := 0
	btBase := war.Bloodthirst.DamageMultiplierAdditive
	for i := 0; i < casts; i++ {
		if landed, _ := castBloodthirst(sim, war, target); landed {
			hits++
		}
	}
	m := strike.SpellMetrics[target.UnitIndex]
	if rate := float64(m.Casts) / float64(hits); math.Abs(rate-slayersEdgeChance) > 0.02 {
		t.Errorf("Slayer's Strike on %.3f of landed hits, want %.2f", rate, slayersEdgeChance)
	}
	// Time never advances here, so every stack from a landed strike is still up.
	if stacks := slayer.GetStacks(); stacks != min(99, m.Hits+m.Crits) {
		t.Errorf("%d Slayer stacks for %d landed strikes", stacks, m.Hits+m.Crits)
	}
	if got := war.Bloodthirst.DamageMultiplierAdditive - btBase; math.Abs(got-0.03*float64(slayer.GetStacks())) > 1e-9 {
		t.Errorf("Bloodthirst bonus %.3f for %d Slayer stacks, want 3%% each", got, slayer.GetStacks())
	}

	// Over a fight, stacks expire 12 sec after being gained.
	res := runCCSim(t, furyRequest(ring(warrior.RokthulsSlayersEdgeItemID), 20))
	if a := res.auras[core.ActionID{SpellID: 200084}]; a == nil || a.UptimeSecondsAvg <= 0 || a.UptimeSecondsAvg > 0.99*300 {
		t.Errorf("Slayer uptime %v: stacks don't expire", a)
	}
}

const slayersEdgeChance = 0.15

func TestBerserkerOfGrizzlemaw(t *testing.T) {
	// 2pc: a Bloodthirst crit buffs the next Bloodthirst, which uses it up.
	sim, war, target := newFury(t, berserkerSet(2))
	buff := war.GetAura("Bloodbath")
	base := war.Bloodthirst.DamageMultiplierAdditive
	crits := 0
	for i := 0; i < 500; i++ {
		buffed := buff.IsActive()
		if buffed != (war.Bloodthirst.DamageMultiplierAdditive-base > 0.19) {
			t.Fatal("Bloodbath active without its +20% Bloodthirst damage (or the reverse)")
		}
		_, crit := castBloodthirst(sim, war, target)
		if crit {
			crits++
		}
		if buff.IsActive() != crit {
			t.Fatalf("after a Bloodthirst (crit=%v), Bloodbath active=%v", crit, buff.IsActive())
		}
	}
	if crits == 0 {
		t.Fatal("no Bloodthirst crits")
	}
	if war.GetSpell(core.ActionID{SpellID: 900593}) != nil {
		t.Error("Bloodbath bleed registered with only 2 pieces")
	}

	// 4pc: 28% chance to apply the bleed; Bloodthirst on a bleeding target extends it by 6 sec.
	sim, war, target = newFury(t, berserkerSet(4))
	bleed := war.GetSpell(core.ActionID{SpellID: 900593})
	dot := bleed.Dot(target)
	applied, tries := 0, 0
	for i := 0; i < 4000; i++ {
		dot.Cancel(sim)
		if landed, _ := castBloodthirst(sim, war, target); landed {
			tries++
			if dot.IsActive() {
				applied++
			}
		}
	}
	if rate := float64(applied) / float64(tries); math.Abs(rate-0.28) > 0.025 {
		t.Errorf("Bloodbath applied on %.3f of landed Bloodthirsts, want 0.28", rate)
	}
	for !dot.IsActive() {
		castBloodthirst(sim, war, target)
	}
	expires, ticks := dot.ExpiresAt(), dot.NumberOfTicks
	for landed := false; !landed; {
		landed, _ = castBloodthirst(sim, war, target)
	}
	if dot.ExpiresAt()-expires != 6e9 || dot.NumberOfTicks != ticks+3 {
		t.Errorf("extension: expires +%v with %d -> %d ticks, want +6s and 3 more ticks", dot.ExpiresAt()-expires, ticks, dot.NumberOfTicks)
	}
	if want := 0.25 * bleed.MeleeAttackPower() / 3; math.Abs(dot.SnapshotBaseDamage-want) > 1e-6 {
		t.Errorf("tick base damage %.1f, want %.1f (25%% AP over 3 ticks)", dot.SnapshotBaseDamage, want)
	}
}

// Logs the DPS impact of each custom item on the fury preset (go test -run CCDps -v).
func TestCCDpsImpactFury(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	base := runCCSim(t, furyRequest(nil, iterations)).dps
	for _, c := range []struct {
		name  string
		items map[proto.ItemSlot]int32
	}{
		{"Vorrax's Reckless Fury", ring(warrior.VorraxsRecklessFuryItemID)},
		{"Rok'thul's Slayer's Edge", ring(warrior.RokthulsSlayersEdgeItemID)},
		{"Berserker of Grizzlemaw 2pc", berserkerSet(2)},
		{"Berserker of Grizzlemaw 4pc", berserkerSet(4)},
	} {
		dps := runCCSim(t, furyRequest(c.items, iterations)).dps
		statsOnly := runCCSim(t, furyRequest(cc.StatOnlyCopies(c.items), iterations)).dps
		t.Logf("%-30s %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f)",
			c.name, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base)
	}
}
