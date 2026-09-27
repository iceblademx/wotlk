package dps

import (
	"math"
	"testing"
	"time"

	"github.com/wowsims/wotlk/sim/common/cc"
	"github.com/wowsims/wotlk/sim/common/cc/ccfamily"
	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/warrior"
	googleProto "google.golang.org/protobuf/proto"
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

var reaverPieces = []struct {
	slot proto.ItemSlot
	id   int32
}{{proto.ItemSlot_ItemSlotNeck, 901113}, {proto.ItemSlot_ItemSlotFinger2, 901114},
	{proto.ItemSlot_ItemSlotBack, 901115}, {proto.ItemSlot_ItemSlotMainHand, 901116}}

func reaverSet(n int) map[proto.ItemSlot]int32 {
	m := map[proto.ItemSlot]int32{}
	for _, p := range reaverPieces[:n] {
		m[p.slot] = p.id
	}
	return m
}

func furyRequest(items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	return specRequest("p4_fury_horde", PlayerOptionsFury, FuryTalents, FuryGlyphs, "fury", items, iterations)
}

func armsRequest(items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	return armsAplRequest("arms", items, iterations)
}

func armsAplRequest(apl string, items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	return specRequest("p4_arms_horde", PlayerOptionsArms, ArmsTalents, ArmsGlyphs, apl, items, iterations)
}

func specRequest(gearSet string, spec *proto.Player_Warrior, talents string, glyphs *proto.Glyphs, apl string,
	items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../../ui/warrior/gear_sets", gearSet).GearSet
	for slot, id := range items {
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	// A deep copy: sims write their class buffs into the raid buffs, which are shared globals.
	return googleProto.Clone(&proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceOrc,
			Class:         proto.Class_ClassWarrior,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          spec,
			TalentsString: talents,
			Glyphs:        glyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../../ui/warrior/apls", apl).Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 3, IsTest: true},
	}).(*proto.RaidSimRequest)
}

func newFury(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *warrior.Warrior, *core.Unit) {
	t.Helper()
	return newSim(t, furyRequest(items, 1))
}

func newArms(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *warrior.Warrior, *core.Unit) {
	t.Helper()
	return newSim(t, armsRequest(items, 1))
}

func newSim(t *testing.T, req *proto.RaidSimRequest) (*core.Simulation, *warrior.Warrior, *core.Unit) {
	t.Helper()
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

func TestRecklessFuryArms(t *testing.T) {
	// With the Sudden Death talent (3/3, 9%) the ring tops the chance up to 20% combined per hit.
	sim, war, target := newArms(t, ring(warrior.VorraxsRecklessFuryItemID))
	if war.Talents.SuddenDeath != 3 {
		t.Fatalf("arms preset has Sudden Death %d/3", war.Talents.SuddenDeath)
	}
	procs, hits := 0, 0
	for i := 0; i < 6000; i++ {
		// The preset wears Ymirjar 4pc (T10), which turns some talent procs into its own aura.
		war.SuddenDeathAura.Deactivate(sim)
		war.Ymirjar4pcProcAura.Deactivate(sim)
		if landed := castMortalStrike(sim, war, target); landed {
			hits++
			if war.IsSuddenDeathActive() {
				procs++
			}
		}
	}
	if rate := float64(procs) / float64(hits); math.Abs(rate-0.2) > 0.02 {
		t.Errorf("Sudden Death on %.3f of landed hits, want 0.20 combined", rate)
	}
}

// castMortalStrike casts Mortal Strike without cost or cooldown and reports whether it landed.
func castMortalStrike(sim *core.Simulation, war *warrior.Warrior, target *core.Unit) bool {
	m := &war.MortalStrike.SpellMetrics[target.UnitIndex]
	landed := m.Hits + m.Crits
	war.MortalStrike.SkipCastAndApplyEffects(sim, target)
	return m.Hits+m.Crits > landed
}

func TestReaverOfTheTaintedGrove(t *testing.T) {
	// 2pc: a landed Overpower resets Mortal Strike 35% of the time and makes the next one cost 33% less.
	sim, war, target := newArms(t, reaverSet(2))
	costBuff := war.GetAura("Fatal Mark (Mortal Strike cost)")
	resets, landed := 0, 0
	for i := 0; i < 4000; i++ {
		war.MortalStrike.CD.Set(sim.CurrentTime + time.Second*5)
		costBuff.Deactivate(sim)
		m := &war.Overpower.SpellMetrics[target.UnitIndex]
		before := m.Hits + m.Crits
		war.Overpower.SkipCastAndApplyEffects(sim, target)
		if m.Hits+m.Crits == before {
			continue
		}
		landed++
		if war.MortalStrike.IsReady(sim) != costBuff.IsActive() {
			t.Fatal("Mortal Strike reset without the cost buff (or the reverse)")
		}
		if costBuff.IsActive() {
			resets++
		}
	}
	if rate := float64(resets) / float64(landed); math.Abs(rate-0.35) > 0.025 {
		t.Errorf("Mortal Strike reset on %.3f of landed Overpowers, want 0.35", rate)
	}
	costBuff.Activate(sim)
	if cost := war.MortalStrike.DefaultCast.Cost * war.MortalStrike.CostMultiplier; math.Abs(cost-20.1) > 1e-9 {
		t.Errorf("Mortal Strike costs %.2f with the buff, want 20.1", cost)
	}
	war.OnCastComplete(sim, war.MortalStrike)
	if costBuff.IsActive() || war.MortalStrike.CostMultiplier != 1 {
		t.Error("the cost buff isn't used up by Mortal Strike")
	}
	if war.GetSpell(core.ActionID{SpellID: 900584}) != nil {
		t.Error("Fatal Mark detonation registered with only 2 pieces")
	}

	// 4pc: each landed Mortal Strike adds a mark (up to 5); a landed Execute detonates them all for
	// 276% of attack power each.
	sim, war, target = newArms(t, reaverSet(4))
	mark := target.GetAura("Fatal Mark-" + war.Label)
	detonation := war.GetSpell(core.ActionID{SpellID: 900584})
	for mark.GetStacks() < 5 {
		castMortalStrike(sim, war, target)
	}
	castMortalStrike(sim, war, target)
	if mark.GetStacks() != 5 {
		t.Fatalf("%d marks, want the cap of 5", mark.GetStacks())
	}
	m := &detonation.SpellMetrics[target.UnitIndex]
	for m.Casts == 0 {
		war.Execute.SkipCastAndApplyEffects(sim, target)
	}
	if mark.IsActive() {
		t.Error("marks remain after the detonation")
	}
	if m.Hits+m.Crits != 1 {
		t.Error("the detonation didn't land: it can't miss")
	}
	// Remove crits, armor and multipliers to compare against the raw coefficient.
	unmitigated := 5 * 2.76 * detonation.MeleeAttackPower()
	if m.Crits == 0 && (m.TotalDamage < 0.5*unmitigated || m.TotalDamage > 1.2*unmitigated) {
		t.Errorf("detonation dealt %.0f for 5 marks, want about %.0f before armor", m.TotalDamage, unmitigated)
	}
	war.Execute.SkipCastAndApplyEffects(sim, target)
	if m.Casts != 1 {
		t.Error("Execute on an unmarked target detonated")
	}
}

func TestFatalMarkRotation(t *testing.T) {
	// arms_fatal_mark holds Sudden Death Executes until the target has 5 marks, so each detonation
	// consumes more marks than with the stock rotation.
	detonation := core.ActionID{SpellID: 900584}
	avg := func(apl string) float64 {
		a := runCCSim(t, armsAplRequest(apl, reaverSet(4), 20)).actions[detonation]
		if a == nil || a.Casts == 0 {
			t.Fatalf("%s: no Fatal Mark detonations", apl)
		}
		return a.Damage / float64(a.Casts)
	}
	if held, stock := avg("arms_fatal_mark"), avg("arms"); held < 1.3*stock {
		t.Errorf("average detonation %.0f with arms_fatal_mark vs %.0f with arms: marks aren't held", held, stock)
	}
}

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

// Logs the DPS impact of each custom item on the arms preset (go test -run CCDps -v).
func TestCCDpsImpactArms(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	base := runCCSim(t, armsRequest(nil, iterations)).dps
	for _, c := range []struct {
		name  string
		apl   string
		items map[proto.ItemSlot]int32
	}{
		{"Vorrax's Reckless Fury", "arms", ring(warrior.VorraxsRecklessFuryItemID)},
		{"Reaver of the Tainted Grove 2pc", "arms", reaverSet(2)},
		{"Reaver of the Tainted Grove 4pc", "arms", reaverSet(4)},
		{"Reaver 4pc (arms_fatal_mark)", "arms_fatal_mark", reaverSet(4)},
	} {
		// The stat-only copy has no marks, so it always uses the stock rotation.
		dps := runCCSim(t, armsAplRequest(c.apl, c.items, iterations)).dps
		statsOnly := runCCSim(t, armsRequest(cc.StatOnlyCopies(c.items), iterations)).dps
		t.Logf("%-32s %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f)",
			c.name, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base)
	}
}

// Every custom spell gets only the class modifiers its 3.3.5a family flags allow.
func TestClassModifiers(t *testing.T) {
	sim := func(items map[proto.ItemSlot]int32) *core.Simulation {
		s, _, _ := newFury(t, items)
		return s
	}
	withSet := berserkerSet(4)
	withSet[proto.ItemSlot_ItemSlotFinger1] = warrior.VorraxsRecklessFuryItemID
	arms, _, _ := newArms(t, reaverSet(4))
	ccfamily.Check(t, nil, sim(withSet), sim(ring(warrior.RokthulsSlayersEdgeItemID)), arms)
}
