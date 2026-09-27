package hunter

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/wowsims/wotlk/sim/common/cc"
	"github.com/wowsims/wotlk/sim/common/cc/ccfamily"
	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/core/stats"
	googleProto "google.golang.org/protobuf/proto"
)

// Tests for custom 3.3.5a server content in cc_items.go.

// A hunter spec preset for the custom item tests.
type hunterPreset struct {
	gear    string
	talents string
	glyphs  *proto.Glyphs
}

var (
	bmPreset = hunterPreset{"p4_sv", BMTalents, BMGlyphs} // as TestBM, which uses the SV gear
	mmPreset = hunterPreset{"p4_mm", MMTalents, MMGlyphs}
	svPreset = hunterPreset{"p4_sv", SVTalents, SVGlyphs}
)

var (
	grizzlemawPieces = [4]int32{901029, 901030, 901031, 901032}
	wardenPieces     = [4]int32{901033, 901034, 901035, 901036}
	nightmarePieces  = [4]int32{901037, 901038, 901039, 901040}
)

// setPieces equips the first n pieces of a custom set (neck, ring, cloak, ranged weapon).
func setPieces(ids [4]int32, n int) map[proto.ItemSlot]int32 {
	slots := [4]proto.ItemSlot{proto.ItemSlot_ItemSlotNeck, proto.ItemSlot_ItemSlotFinger2, proto.ItemSlot_ItemSlotBack, proto.ItemSlot_ItemSlotRanged}
	m := map[proto.ItemSlot]int32{}
	for i := 0; i < n; i++ {
		m[slots[i]] = ids[i]
	}
	return m
}

func hunterRing(id int32) map[proto.ItemSlot]int32 {
	return map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: id}
}

func mergeItems(sets ...map[proto.ItemSlot]int32) map[proto.ItemSlot]int32 {
	out := map[proto.ItemSlot]int32{}
	for _, s := range sets {
		for slot, id := range s {
			out[slot] = id
		}
	}
	return out
}

func hunterRequest(preset hunterPreset, apl string, items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../ui/hunter/gear_sets", preset.gear).GearSet
	for slot, id := range items {
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	// A deep copy: sims write their class buffs into the raid buffs, which are shared globals.
	return googleProto.Clone(&proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceOrc,
			Class:         proto.Class_ClassHunter,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          PlayerOptionsBasic,
			TalentsString: preset.talents,
			Glyphs:        preset.glyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../ui/hunter/apls", apl).Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 7, IsTest: true},
	}).(*proto.RaidSimRequest)
}

// newHunter builds a reset single-iteration sim, for testing effects directly.
func newHunter(t *testing.T, preset hunterPreset, apl string, items map[proto.ItemSlot]int32) (*core.Simulation, *Hunter, *core.Unit) {
	t.Helper()
	sim := core.NewSim(hunterRequest(preset, apl, items, 1))
	sim.Reset()
	return sim, sim.Raid.Parties[0].Players[0].(*Hunter), sim.Encounter.TargetUnits[0]
}

type ccSimResult struct {
	dps     float64
	actions map[core.ActionID]*proto.TargetedActionMetrics // the hunter's and its pet's, first target
	casts   map[core.ActionID]int32
}

func runCCSim(t *testing.T, req *proto.RaidSimRequest) ccSimResult {
	t.Helper()
	res := core.RunRaidSim(req)
	if res.ErrorResult != "" {
		t.Fatal(res.ErrorResult)
	}
	player := res.RaidMetrics.Parties[0].Players[0]
	out := ccSimResult{dps: player.Dps.Avg, actions: map[core.ActionID]*proto.TargetedActionMetrics{}, casts: map[core.ActionID]int32{}}
	for _, unit := range append([]*proto.UnitMetrics{player}, player.Pets...) {
		for _, a := range unit.Actions {
			id := core.ProtoToActionID(a.Id)
			for _, target := range a.Targets {
				out.casts[id] += target.Casts
			}
			if len(a.Targets) > 0 {
				out.actions[id] = a.Targets[0]
			}
		}
	}
	return out
}

func landed(m *proto.TargetedActionMetrics) int32 {
	if m == nil {
		return 0
	}
	return m.Hits + m.Crits
}

// attempts counts a spell's hit rolls: landed or not.
func attempts(m *proto.TargetedActionMetrics) int32 {
	if m == nil {
		return 0
	}
	return m.Hits + m.Crits + m.Misses + m.Dodges + m.Parries + m.Blocks + m.Glances
}

func dealtDamage(t *testing.T, res ccSimResult, ids ...int32) {
	t.Helper()
	for _, id := range ids {
		if m := res.actions[core.ActionID{SpellID: id}]; m == nil || m.Damage <= 0 {
			t.Errorf("spell %d dealt no damage", id)
		}
	}
}

func hitOn(target *core.Unit) *core.SpellResult {
	return &core.SpellResult{Target: target, Outcome: core.OutcomeHit}
}

func TestKillingEye(t *testing.T) {
	sim, hunter, target := newHunter(t, mmPreset, "mm", hunterRing(SkarethsKillingEyeItemID))
	misdirection := hunter.GetSpell(core.ActionID{SpellID: 34477})
	if misdirection == nil || !misdirection.Flags.Matches(core.SpellFlagMCD) {
		t.Fatal("Misdirection must be a major cooldown with the ring")
	}
	if hunter.KillShot.ExtraCastCondition(sim, target) {
		t.Fatal("Kill Shot usable above 20% health without the buff")
	}
	additive := hunter.KillShot.DamageMultiplierAdditive
	misdirection.SkipCastAndApplyEffects(sim, target)
	if !hunter.KillShot.ExtraCastCondition(sim, target) {
		t.Error("Misdirection must make Kill Shot usable above 20% health")
	}
	if got := hunter.KillShot.DamageMultiplierAdditive - additive; math.Abs(got-killingEyeBonus) > 1e-9 {
		t.Errorf("Kill Shot +%.2f damage after Misdirection, want +%.2f", got, killingEyeBonus)
	}
	hunter.OnCastComplete(sim, hunter.KillShot)
	if hunter.KillShot.ExtraCastCondition(sim, target) || hunter.KillShot.DamageMultiplierAdditive != additive {
		t.Error("Kill Shot must consume the buff")
	}

	if _, plain, _ := newHunter(t, mmPreset, "mm", nil); plain.GetSpell(core.ActionID{SpellID: 34477}) != nil {
		t.Error("Misdirection registered without the ring")
	}

	res := runCCSim(t, hunterRequest(mmPreset, "mm", hunterRing(SkarethsKillingEyeItemID), 20))
	misdirections, killShots := res.casts[core.ActionID{SpellID: 34477}], res.casts[core.ActionID{SpellID: 61006}]
	// Every Misdirection is followed by a Kill Shot, and Kill Shot is also used in execute.
	if misdirections < 20*8 || killShots < misdirections {
		t.Errorf("%d Misdirections and %d Kill Shots in 20 fights of 300 sec", misdirections, killShots)
	}
}

func TestWildHunt(t *testing.T) {
	ring := hunterRing(VorraxsWildHuntItemID)
	_, hunter, _ := newHunter(t, bmPreset, "bm", ring)
	_, plain, _ := newHunter(t, bmPreset, "bm", cc.StatOnlyCopies(ring))
	owner := hunter.GetStats()
	if owner != plain.GetStats() {
		t.Fatal("the stat-only copy changes the hunter's stats")
	}
	critRating := owner[stats.MeleeCrit] - owner[stats.Agility]*core.CritPerAgiMaxLevel[proto.Class_ClassHunter]*core.CritRatingPerCritChance
	diff := hunter.pet.GetStats().Subtract(plain.pet.GetStats())
	for _, c := range []struct {
		stat stats.Stat
		want float64
	}{
		{stats.ArmorPenetration, owner[stats.ArmorPenetration]},
		{stats.MeleeCrit, 0.3 * critRating},
		{stats.SpellCrit, 0.3 * critRating},
		{stats.MeleeHaste, 0.3 * owner[stats.MeleeHaste]},
	} {
		if math.Abs(diff[c.stat]-c.want) > 1e-6 {
			t.Errorf("pet %s +%.2f from the ring, want +%.2f", c.stat.StatName(), diff[c.stat], c.want)
		}
	}
	if owner[stats.MeleeHaste] == 0 || critRating <= 0 {
		t.Fatalf("test gear has no haste or crit rating (%.0f, %.0f)", owner[stats.MeleeHaste], critRating)
	}

	// Only with the Beast Mastery talent.
	_, mm, _ := newHunter(t, mmPreset, "mm", ring)
	_, mmPlain, _ := newHunter(t, mmPreset, "mm", cc.StatOnlyCopies(ring))
	if mm.pet.GetStats() != mmPlain.pet.GetStats() {
		t.Error("the pet inherits stats without the Beast Mastery talent")
	}

	const iterations = 20
	res := runCCSim(t, hunterRequest(bmPreset, "bm", ring, iterations))
	calls, cleaves := res.casts[core.ActionID{SpellID: 53434}], res.casts[core.ActionID{SpellID: 200145}]
	perCall := int32(wildHuntBeasts * (wildHuntStampedeTime / wildHuntCleaveInterval))
	if calls == 0 || cleaves > perCall*calls || cleaves < perCall*(calls-iterations) {
		t.Errorf("%d Spirit Cleaves for %d Calls of the Wild, want %d each", cleaves, calls, perCall)
	}
	dealtDamage(t, res, 200145)
}

func TestPackLeader(t *testing.T) {
	sim, hunter, target := newHunter(t, bmPreset, "bm", hunterRing(PackLeadersInsigniaItemID))
	additive := hunter.SteadyShot.DamageMultiplierAdditive
	for i, label := range []string{"Pack Leader: Wyvern", "Pack Leader: Bear", "Pack Leader: Boar"} {
		hunter.KillCommand.SkipCastAndApplyEffects(sim, target)
		if !hunter.GetAura(label).IsActive() {
			t.Errorf("Kill Command #%d must summon %s", i+1, label)
		}
	}
	if got := hunter.SteadyShot.DamageMultiplierAdditive - additive; math.Abs(got-packLeaderSteadyShot) > 1e-9 {
		t.Errorf("Steady Shot +%.2f damage with the Wyvern, want +%.2f", got, packLeaderSteadyShot)
	}
	if !hunter.pet.GetAura("Might of the Elder").IsActive() {
		t.Error("the Bear must grant the pet Might of the Elder")
	}
	gouge := target.GetAura("Gouge-" + strconv.Itoa(int(hunter.Index)))
	for i := 0; i < 7; i++ {
		hunter.OnSpellHitDealt(sim, hunter.ArcaneShot, hitOn(target))
	}
	if gouge.GetStacks() != packLeaderGougeStacks {
		t.Errorf("%d Gouge stacks after 7 Arcane Shots with the Boar, want %d", gouge.GetStacks(), packLeaderGougeStacks)
	}

	res := runCCSim(t, hunterRequest(bmPreset, "bm_pack_leader", hunterRing(PackLeadersInsigniaItemID), 20))
	dealtDamage(t, res, 200148, 200150, 200151, 200153)
}

func TestDeadeye(t *testing.T) {
	sim, hunter, target := newHunter(t, mmPreset, "mm", hunterRing(DeadeyesOathItemID))
	if cd := hunter.AimedShot.CD.Duration; cd != 15*time.Second {
		t.Fatalf("Aimed Shot cooldown %v with the ring, want 15s", cd)
	}
	hunter.AimedShot.CD.Use(sim)
	readyAt := hunter.AimedShot.CD.ReadyAt()
	hunter.OnSpellHitDealt(sim, hunter.AutoAttacks.RangedAuto(), hitOn(target))
	hunter.OnSpellHitDealt(sim, hunter.AimedShot, hitOn(target))
	if hunter.AimedShot.CD.ReadyAt() != readyAt {
		t.Error("only other ranged abilities may shorten Aimed Shot's cooldown")
	}
	hunter.OnSpellHitDealt(sim, hunter.SteadyShot, hitOn(target))
	hunter.OnSpellHitDealt(sim, hunter.SerpentSting, hitOn(target))
	if got := readyAt - hunter.AimedShot.CD.ReadyAt(); got != 2*deadeyeCDPerShot {
		t.Errorf("Steady Shot and Serpent Sting took %v off Aimed Shot's cooldown, want %v", got, 2*deadeyeCDPerShot)
	}

	withRing := runCCSim(t, hunterRequest(mmPreset, "mm", hunterRing(DeadeyesOathItemID), 20))
	statsOnly := runCCSim(t, hunterRequest(mmPreset, "mm", cc.StatOnlyCopies(hunterRing(DeadeyesOathItemID)), 20))
	perHit := func(res ccSimResult) float64 {
		m := res.actions[core.ActionID{SpellID: 49050}]
		if landed(m) == 0 {
			t.Fatal("no Aimed Shot landed")
		}
		return m.Damage / float64(landed(m))
	}
	// 650% of ~5000 ranged attack power on top of ~5000 damage.
	if ratio := perHit(withRing) / perHit(statsOnly); ratio < 4 {
		t.Errorf("Aimed Shot deals %.1fx damage per hit with the ring", ratio)
	}
}

func TestWitheringArrow(t *testing.T) {
	sim, hunter, target := newHunter(t, mmPreset, "mm", hunterRing(LoopOfTheBansheeItemID))
	arrow := hunter.GetSpell(core.ActionID{SpellID: 200157})
	burst := hunter.GetSpell(core.ActionID{SpellID: 200158})
	eruption := hunter.GetSpell(core.ActionID{SpellID: 200159})

	hunter.OnSpellHitDealt(sim, hunter.SilencingShot, hitOn(target))
	dot := arrow.Dot(target)
	if !dot.IsActive() {
		t.Fatal("Silencing Shot must apply Withering Arrow")
	}
	if want := bansheeTickAP * hunter.scriptAP(); math.Abs(dot.SnapshotBaseDamage-want) > 1e-6 {
		t.Errorf("Withering Arrow ticks for %.1f base, want %.1f (12%% of attack power)", dot.SnapshotBaseDamage, want)
	}
	if got := bansheeRamp(8); math.Abs(got-2.05) > 1e-9 {
		t.Errorf("the 8th tick deals %.2fx the first, want 2.05x", got)
	}

	hunter.OnSpellHitDealt(sim, hunter.ChimeraShot, hitOn(target))
	if dot.IsActive() {
		t.Error("Chimera Shot must consume Withering Arrow")
	}
	for _, spell := range []*core.Spell{burst, eruption} {
		if m := spell.SpellMetrics[target.UnitIndex]; m.Hits+m.Crits+m.Misses != 1 {
			t.Errorf("spell %s: %d results after one Chimera Shot, want 1", spell.ActionID, m.Hits+m.Crits+m.Misses)
		}
	}
	// A consumed arrow does nothing more.
	hunter.OnSpellHitDealt(sim, hunter.ChimeraShot, hitOn(target))
	if m := burst.SpellMetrics[target.UnitIndex]; m.Hits+m.Crits+m.Misses != 1 {
		t.Error("Chimera Shot without an arrow dealt burst damage")
	}

	res := runCCSim(t, hunterRequest(mmPreset, "mm", hunterRing(LoopOfTheBansheeItemID), 20))
	dealtDamage(t, res, 200157, 200158, 200159)
}

func TestWildfireBomb(t *testing.T) {
	_, hunter, _ := newHunter(t, svPreset, "sv", hunterRing(BombardiersPinItemID))
	bomb := hunter.GetSpell(core.ActionID{SpellID: 200161})
	if bomb == nil || !bomb.Flags.Matches(core.SpellFlagMCD) || bomb.DefaultCast.GCD != 0 || bomb.CD.Duration != wildfireBombCooldown {
		t.Fatal("Wildfire Bomb must be an off-GCD major cooldown with an 18 sec cooldown")
	}

	const iterations = 20
	withRing := runCCSim(t, hunterRequest(svPreset, "sv", hunterRing(BombardiersPinItemID), iterations))
	statsOnly := runCCSim(t, hunterRequest(svPreset, "sv", cc.StatOnlyCopies(hunterRing(BombardiersPinItemID)), iterations))
	if bombs := withRing.casts[core.ActionID{SpellID: 200161}]; bombs < iterations*8 {
		t.Errorf("%d Wildfire Bombs in %d fights of 300 sec", bombs, iterations)
	}
	dealtDamage(t, withRing, 200161)
	explosiveShots := func(res ccSimResult) int32 {
		return res.casts[core.ActionID{SpellID: 60053}] + res.casts[core.ActionID{SpellID: 60052}]
	}
	if explosiveShots(withRing) <= explosiveShots(statsOnly) {
		t.Errorf("Lock and Load from the bomb: %d Explosive Shots, %d without it", explosiveShots(withRing), explosiveShots(statsOnly))
	}
}

func TestDarkMinion(t *testing.T) {
	const iterations = 20
	// The default rotations trap weave, and Explosive Trap shares Black Arrow's cooldown.
	res := runCCSim(t, hunterRequest(svPreset, "sv_dark_deeds", hunterRing(SignetOfDarkDeedsItemID), iterations))
	arrows := landed(res.actions[core.ActionID{SpellID: 63672}])
	if arrows < iterations*10 {
		t.Fatalf("%d Black Arrows in %d fights of 300 sec", arrows, iterations)
	}
	echoes := attempts(res.actions[core.ActionID{SpellID: 200163}])
	// One echo per Black Arrow; the last minion of a fight may still be up when it ends.
	if echoes > arrows || echoes < arrows-iterations {
		t.Errorf("%d Dark Echoes for %d Black Arrows", echoes, arrows)
	}
	// 90% of about 10 sec of the hunter's damage.
	echo := res.actions[core.ActionID{SpellID: 200163}]
	if perEcho := echo.Damage / float64(landed(echo)); perEcho < 3*res.dps || perEcho > 12*res.dps {
		t.Errorf("Dark Echo hits for %.0f, the hunter does %.0f DPS", perEcho, res.dps)
	}
}

func TestGrizzlemawTrackersGarb(t *testing.T) {
	sim, hunter, target := newHunter(t, bmPreset, "bm", setPieces(grizzlemawPieces, 4))
	barbedShot := hunter.GetSpell(core.ActionID{SpellID: 900372})
	stomp := hunter.pet.GetSpell(core.ActionID{SpellID: 900373})
	callOfTheWild := hunter.GetSpell(core.ActionID{SpellID: 53434})
	bestialWrath := hunter.GetSpell(core.ActionID{SpellID: 19574})
	cooldowns := []struct {
		spell *core.Spell
		cdr   time.Duration
	}{{callOfTheWild, stompCotWRapidFire}, {hunter.RapidFire, stompCotWRapidFire}, {bestialWrath, stompBWKillCommand}, {hunter.KillCommand, stompBWKillCommand}}
	readyAt := make([]time.Duration, len(cooldowns))
	for i, c := range cooldowns {
		c.spell.CD.Use(sim)
		readyAt[i] = c.spell.CD.ReadyAt()
	}

	const shots = 100
	for i := 0; i < shots; i++ {
		hunter.OnSpellHitDealt(sim, hunter.SteadyShot, hitOn(target))
	}
	procs := barbedShot.SpellMetrics[target.UnitIndex].Casts
	if rate := float64(procs) / shots; rate < 0.12 || rate > 0.32 {
		t.Errorf("Barbed Shot on %.0f%% of shots, want 22%%", 100*rate)
	}
	if stomps := stomp.SpellMetrics[target.UnitIndex].Casts; stomps != procs {
		t.Errorf("%d Stomps for %d Barbed Shots", stomps, procs)
	}
	for i, c := range cooldowns {
		if got, want := readyAt[i]-c.spell.CD.ReadyAt(), time.Duration(procs)*c.cdr; got != want {
			t.Errorf("4pc: %s cooldown %v shorter after %d Stomps, want %v", c.spell.ActionID, got, procs, want)
		}
	}

	// 2pc only: no cooldown reduction.
	sim, hunter, target = newHunter(t, bmPreset, "bm", setPieces(grizzlemawPieces, 2))
	hunter.RapidFire.CD.Use(sim)
	rapidFire := hunter.RapidFire.CD.ReadyAt()
	for i := 0; i < shots; i++ {
		hunter.OnSpellHitDealt(sim, hunter.AutoAttacks.RangedAuto(), hitOn(target))
	}
	if hunter.RapidFire.CD.ReadyAt() != rapidFire {
		t.Error("2pc: Stomp shortened Rapid Fire")
	}
	if hunter.GetSpell(core.ActionID{SpellID: 900372}).SpellMetrics[target.UnitIndex].Casts == 0 {
		t.Error("2pc: Auto Shot never applied Barbed Shot")
	}

	res := runCCSim(t, hunterRequest(bmPreset, "bm", setPieces(grizzlemawPieces, 4), 20))
	dealtDamage(t, res, 900372, 900373)
}

func TestWardenOfTheSilentCanopy(t *testing.T) {
	sim, hunter, target := newHunter(t, mmPreset, "mm", setPieces(wardenPieces, 4))
	additive := hunter.SteadyShot.DamageMultiplierAdditive
	for i := 0; i < 7; i++ {
		hunter.OnCastComplete(sim, hunter.SteadyShot)
	}
	if got := hunter.SteadyShot.DamageMultiplierAdditive - additive; math.Abs(got-0.1) > 1e-9 {
		t.Errorf("Steady Shot +%.2f damage after 7 Steady Shots, want +0.10 (5 stacks of 2%%)", got)
	}

	quarryShot := hunter.GetSpell(core.ActionID{SpellID: 900383})
	arcaneAdditive, arcaneCost := hunter.ArcaneShot.DamageMultiplierAdditive, hunter.ArcaneShot.CostMultiplier
	hunter.OnSpellHitDealt(sim, hunter.AimedShot, hitOn(target))
	if hunter.SteadyShot.DamageMultiplierAdditive != additive {
		t.Error("4pc: Aimed Shot must consume Marked Quarry")
	}
	if m := quarryShot.SpellMetrics[target.UnitIndex]; m.Hits+m.Crits != 1 {
		t.Error("4pc: consuming Marked Quarry must deal damage")
	}
	if got := hunter.ArcaneShot.DamageMultiplierAdditive - arcaneAdditive; math.Abs(got-markedQuarryArcaneBonus) > 1e-9 || hunter.ArcaneShot.CostMultiplier > 0 {
		t.Errorf("4pc: next Arcane Shot +%.2f damage at %.2fx cost, want +0.50 and free", got, hunter.ArcaneShot.CostMultiplier)
	}
	hunter.OnCastComplete(sim, hunter.ArcaneShot)
	if hunter.ArcaneShot.DamageMultiplierAdditive != arcaneAdditive || hunter.ArcaneShot.CostMultiplier != arcaneCost {
		t.Error("4pc: Arcane Shot must consume the buff")
	}

	// 2pc only: Aimed Shot doesn't consume the stacks.
	sim, hunter, target = newHunter(t, mmPreset, "mm", setPieces(wardenPieces, 2))
	hunter.OnCastComplete(sim, hunter.SteadyShot)
	hunter.OnSpellHitDealt(sim, hunter.AimedShot, hitOn(target))
	if hunter.GetAura("Marked Quarry").GetStacks() != 1 || hunter.GetSpell(core.ActionID{SpellID: 900383}) != nil {
		t.Error("2pc: Aimed Shot consumed Marked Quarry")
	}
}

func TestNightmareStalkersTrappings(t *testing.T) {
	sim, hunter, target := newHunter(t, svPreset, "sv", setPieces(nightmarePieces, 4))
	mark := target.GetAura("Sentinel's Mark-" + strconv.Itoa(int(hunter.Index)))
	missile := hunter.GetSpell(core.ActionID{SpellID: 900394})
	lockAndLoad := hunter.LockAndLoadAura
	es := hunter.ExplosiveShotR4
	multiplier := es.DamageMultiplier

	// Explosive Shot consuming Lock and Load marks the target for the next one.
	const tries = 400
	marked := 0
	for i := 0; i < tries; i++ {
		mark.Deactivate(sim)
		lockAndLoad.Activate(sim)
		lockAndLoad.SetStacks(sim, 2)
		hunter.OnSpellHitDealt(sim, es, hitOn(target))
		hunter.OnCastComplete(sim, es)
		if mark.IsActive() {
			marked++
		}
	}
	if rate := float64(marked) / tries; rate < 0.38 || rate > 0.52 {
		t.Errorf("Sentinel's Mark on %.0f%% of consumed Lock and Loads, want 45%%", 100*rate)
	}
	missiles := missile.SpellMetrics[target.UnitIndex].Casts

	// Without Lock and Load, Explosive Shot only consumes the mark.
	lockAndLoad.Deactivate(sim)
	mark.Activate(sim)
	if got := es.DamageMultiplier / multiplier; math.Abs(got-1.4) > 1e-9 {
		t.Errorf("Explosive Shot deals %.2fx damage against the mark, want 1.4x", got)
	}
	hunter.OnSpellHitDealt(sim, es, hitOn(target))
	hunter.OnCastComplete(sim, es)
	if mark.IsActive() || es.DamageMultiplier != multiplier {
		t.Error("Explosive Shot must consume Sentinel's Mark")
	}
	if got := missile.SpellMetrics[target.UnitIndex].Casts - missiles; got != lunarMissiles {
		t.Errorf("4pc: %d Lunar Missiles for a consumed mark, want %d", got, lunarMissiles)
	}

	if _, hunter2, _ := newHunter(t, svPreset, "sv", setPieces(nightmarePieces, 2)); hunter2.GetSpell(core.ActionID{SpellID: 900394}) != nil {
		t.Error("Lunar Missile registered with 2 pieces")
	}

	res := runCCSim(t, hunterRequest(svPreset, "sv", setPieces(nightmarePieces, 4), 20))
	dealtDamage(t, res, 900394)
}

type ccDpsCase struct {
	name  string
	apl   string
	items map[proto.ItemSlot]int32
}

// logCCDpsImpact logs each case's DPS against stat-only copies of its items and against the preset gear.
func logCCDpsImpact(t *testing.T, preset hunterPreset, baseAPL string, cases []ccDpsCase) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	base := runCCSim(t, hunterRequest(preset, baseAPL, nil, iterations)).dps
	for _, c := range cases {
		dps := runCCSim(t, hunterRequest(preset, c.apl, c.items, iterations)).dps
		// The same items without their effects: what the effect alone is worth.
		statsOnly := runCCSim(t, hunterRequest(preset, baseAPL, cc.StatOnlyCopies(c.items), iterations)).dps
		t.Logf("%-40s %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f)",
			c.name, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base)
	}
}

// Logs the DPS impact of each custom item (go test -run CCDps -v).
func TestCCDpsImpactBeastMastery(t *testing.T) {
	logCCDpsImpact(t, bmPreset, "bm", []ccDpsCase{
		{"Vorrax's Wild Hunt", "bm", hunterRing(VorraxsWildHuntItemID)},
		{"Pack Leader's Insignia", "bm", hunterRing(PackLeadersInsigniaItemID)},
		{"Pack Leader's Insignia (APL)", "bm_pack_leader", hunterRing(PackLeadersInsigniaItemID)},
		{"Grizzlemaw Tracker's Garb 2pc", "bm", setPieces(grizzlemawPieces, 2)},
		{"Grizzlemaw Tracker's Garb 4pc", "bm", setPieces(grizzlemawPieces, 4)},
	})
}

func TestCCDpsImpactMarksmanship(t *testing.T) {
	logCCDpsImpact(t, mmPreset, "mm", []ccDpsCase{
		{"Skareth's Killing Eye", "mm", hunterRing(SkarethsKillingEyeItemID)},
		{"Deadeye's Oath", "mm", hunterRing(DeadeyesOathItemID)},
		{"Deadeye's Oath (APL)", "mm_deadeye", hunterRing(DeadeyesOathItemID)},
		{"Loop of the Banshee", "mm", hunterRing(LoopOfTheBansheeItemID)},
		{"Loop of the Banshee (APL)", "mm_banshee", hunterRing(LoopOfTheBansheeItemID)},
		{"Warden of the Silent Canopy 2pc", "mm", setPieces(wardenPieces, 2)},
		{"Warden of the Silent Canopy 4pc", "mm", setPieces(wardenPieces, 4)},
	})
}

func TestCCDpsImpactSurvival(t *testing.T) {
	logCCDpsImpact(t, svPreset, "sv", []ccDpsCase{
		{"Bombardier's Pin", "sv", hunterRing(BombardiersPinItemID)},
		{"Signet of Dark Deeds", "sv", hunterRing(SignetOfDarkDeedsItemID)},
		{"Signet of Dark Deeds (APL)", "sv_dark_deeds", hunterRing(SignetOfDarkDeedsItemID)},
		{"Nightmare Stalker's Trappings 2pc", "sv", setPieces(nightmarePieces, 2)},
		{"Nightmare Stalker's Trappings 4pc", "sv", setPieces(nightmarePieces, 4)},
	})
}

// Every custom spell gets only the class modifiers its 3.3.5a family flags allow.
func TestClassModifiers(t *testing.T) {
	sim := func(preset hunterPreset, apl string, items map[proto.ItemSlot]int32) *core.Simulation {
		s, _, _ := newHunter(t, preset, apl, items)
		return s
	}
	ccfamily.Check(t, nil,
		sim(bmPreset, "bm", mergeItems(hunterRing(VorraxsWildHuntItemID), setPieces(grizzlemawPieces, 4))),
		sim(bmPreset, "bm", hunterRing(PackLeadersInsigniaItemID)),
		sim(mmPreset, "mm", mergeItems(hunterRing(SkarethsKillingEyeItemID), setPieces(wardenPieces, 4))),
		sim(mmPreset, "mm", mergeItems(hunterRing(DeadeyesOathItemID), map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger2: LoopOfTheBansheeItemID})),
		sim(svPreset, "sv", mergeItems(hunterRing(BombardiersPinItemID), setPieces(nightmarePieces, 4))),
		sim(svPreset, "sv", hunterRing(SignetOfDarkDeedsItemID)),
	)
}
