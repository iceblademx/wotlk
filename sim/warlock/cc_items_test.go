package warlock

import (
	"math"
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

const (
	rotNeck   = 901101
	rotRing   = 901102
	rotCloak  = 901103
	rotWeapon = 901104
)

var rotPieces = []struct {
	slot proto.ItemSlot
	id   int32
}{{proto.ItemSlot_ItemSlotNeck, rotNeck}, {proto.ItemSlot_ItemSlotFinger2, rotRing},
	{proto.ItemSlot_ItemSlotBack, rotCloak}, {proto.ItemSlot_ItemSlotMainHand, rotWeapon}}

func rotSet(n int) map[proto.ItemSlot]int32 {
	m := map[proto.ItemSlot]int32{}
	for _, p := range rotPieces[:n] {
		m[p.slot] = p.id
	}
	return m
}

// A warlock spec preset for the custom item tests.
type warlockPreset struct {
	gear    string
	talents string
	glyphs  *proto.Glyphs
	spec    *proto.Player_Warlock
}

var (
	afflictionPreset  = warlockPreset{"p4_affliction", AfflictionTalents, AfflictionGlyphs, DefaultAfflictionWarlock}
	demonologyPreset  = warlockPreset{"p4_demo", DemonologyTalents, DemonologyGlyphs, DefaultDemonologyWarlock}
	destructionPreset = warlockPreset{"p4_destro", DestructionTalents, DestructionGlyphs, DefaultDestroWarlock}
)

func warlockRequest(preset warlockPreset, apl string, items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../ui/warlock/gear_sets", preset.gear).GearSet
	for slot, id := range items {
		if slot == proto.ItemSlot_ItemSlotMainHand {
			gear.Items[proto.ItemSlot_ItemSlotOffHand] = &proto.ItemSpec{}
		}
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	// A deep copy: sims write their class buffs into the raid buffs, which are shared globals.
	return googleProto.Clone(&proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceOrc,
			Class:         proto.Class_ClassWarlock,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          preset.spec,
			TalentsString: preset.talents,
			Glyphs:        preset.glyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../ui/warlock/apls", apl).Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 7, IsTest: true},
	}).(*proto.RaidSimRequest)
}

func afflictionRequest(apl string, items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	return warlockRequest(afflictionPreset, apl, items, iterations)
}

// newWarlock builds a reset single-iteration sim without debuffs, for testing effects directly.
func newWarlock(t *testing.T, preset warlockPreset, apl string, items map[proto.ItemSlot]int32) (*core.Simulation, *Warlock, *core.Unit) {
	t.Helper()
	req := warlockRequest(preset, apl, items, 1)
	req.Raid.Debuffs = &proto.Debuffs{}
	sim := core.NewSim(req)
	sim.Reset()
	return sim, sim.Raid.Parties[0].Players[0].(*Warlock), sim.Encounter.TargetUnits[0]
}

func newAffliction(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *Warlock, *core.Unit) {
	t.Helper()
	return newWarlock(t, afflictionPreset, "affliction", items)
}

type ccSimResult struct {
	dps     float64
	actions map[core.ActionID]*proto.TargetedActionMetrics
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
	for _, a := range player.Actions {
		id := core.ProtoToActionID(a.Id)
		for _, target := range a.Targets {
			out.casts[id] += target.Casts
		}
		if len(a.Targets) > 0 {
			out.actions[id] = a.Targets[0]
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

func TestWitheringGrasp(t *testing.T) {
	ring := map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: VaelithsWitheringGraspItemID}
	sim, lock, target := newAffliction(t, ring)

	dot := lock.DrainSoul.Dot(target)
	if dot.TickLength != 1500*time.Millisecond || dot.NumberOfTicks != 5 {
		t.Fatalf("Drain Soul: %d ticks of %v, want 5 ticks of 1.5s", dot.NumberOfTicks, dot.TickLength)
	}

	snapshot := func() float64 {
		dot.Apply(sim)
		defer dot.Cancel(sim)
		return dot.SnapshotBaseDamage * dot.SnapshotAttackerMultiplier
	}
	plain := snapshot()
	// The stacks add to Drain Soul's other additive damage bonuses (Shadow Mastery, spellstone).
	additive := lock.DrainSoul.DamageMultiplierAdditive
	for i := 0; i < 5; i++ {
		lock.OnCastComplete(sim, lock.ShadowBolt)
	}
	if got, want := snapshot()/plain, (additive+2)/additive; math.Abs(got-want) > 1e-9 {
		t.Errorf("Drain Soul after 5 Shadow Bolts: %.4fx damage, want %.4fx (4 stacks of +50%%)", got, want)
	}
	lock.OnCastComplete(sim, lock.DrainSoul)
	if got := snapshot() / plain; math.Abs(got-1) > 1e-9 {
		t.Errorf("Drain Soul must consume the stacks: %.4fx damage after consumption", got)
	}

	// Without the ring Drain Soul is unchanged.
	_, plainLock, plainTarget := newAffliction(t, nil)
	if tl := plainLock.DrainSoul.Dot(plainTarget).TickLength; tl != 3*time.Second {
		t.Errorf("Drain Soul tick length %v without the ring", tl)
	}
}

func TestReapedSoul(t *testing.T) {
	ring := map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: VaelithsReapedSoulItemID}
	const iterations = 50
	res := runCCSim(t, afflictionRequest("affliction", ring, iterations))

	ticks := 0
	for _, id := range []int32{47813, 47864, 47843} { // Corruption, Curse of Agony, Unstable Affliction
		ticks += int(landed(res.actions[core.ActionID{SpellID: id}]))
	}
	reaps := int(res.casts[core.ActionID{SpellID: 200078}])
	// Every 25 damaging ticks reap once; each iteration can end with up to 24 unused stacks.
	if max := ticks / reapedSoulStacks; reaps > max || reaps < max-iterations {
		t.Errorf("%d Reap Soul casts for %d DoT ticks, want %d-%d", reaps, ticks, max-iterations, max)
	}
	if reap := res.actions[core.ActionID{SpellID: 200078}]; reap == nil || reap.Damage <= 0 {
		t.Error("Reap Soul dealt no damage")
	}
}

func TestRotOfTheFeveredDream(t *testing.T) {
	anathema := core.ActionID{SpellID: 900552}

	// Reapplying carries the remaining damage over: two back-to-back casts double every tick.
	sim, lock, target := newAffliction(t, rotSet(2))
	spell := lock.GetSpell(anathema)
	if spell == nil {
		t.Fatal("2pc: Soul Anathema not registered")
	}
	spell.SkipCastAndApplyEffects(sim, target)
	single := spell.Dot(target).SnapshotBaseDamage
	spell.SkipCastAndApplyEffects(sim, target)
	if got := spell.Dot(target).SnapshotBaseDamage / single; math.Abs(got-2) > 1e-9 {
		t.Errorf("reapplied Soul Anathema ticks for %.4fx, want 2x", got)
	}
	if single*5 != 6.8*spell.SpellPower() {
		t.Errorf("Soul Anathema deals %.1f over 5 ticks, want 680%% of %.0f spell power", single*5, spell.SpellPower())
	}

	_, lock4, _ := newAffliction(t, rotSet(4))
	if m := lock4.GetSpell(anathema).DamageMultiplier; m != 1.25 {
		t.Errorf("4pc damage multiplier %.2f, want 1.25", m)
	}
	if _, lock1, _ := newAffliction(t, rotSet(1)); lock1.GetSpell(anathema) != nil {
		t.Error("Soul Anathema registered with 1 set piece")
	}

	two := runCCSim(t, afflictionRequest("affliction", rotSet(2), 50))
	four := runCCSim(t, afflictionRequest("affliction", rotSet(4), 50))
	hauntHits := landed(two.actions[core.ActionID{SpellID: 59164}])
	if two.casts[anathema] != hauntHits {
		t.Errorf("2pc: %d Soul Anathema applications for %d Haunt hits", two.casts[anathema], hauntHits)
	}
	// 4pc: every Nightfall Shadow Bolt applies it too.
	if extra := four.casts[anathema] - landed(four.actions[core.ActionID{SpellID: 59164}]); extra <= 0 {
		t.Errorf("4pc: no Soul Anathema from Nightfall (%d extra applications)", extra)
	}
}

var (
	facelessPactPieces = [4]int32{901105, 901106, 901107, 901108}
	worldTreePieces    = [4]int32{901109, 901110, 901111, 901112}
)

// setPieces equips the first n pieces of a custom set (neck, ring, cloak, weapon).
func setPieces(ids [4]int32, n int) map[proto.ItemSlot]int32 {
	slots := [4]proto.ItemSlot{proto.ItemSlot_ItemSlotNeck, proto.ItemSlot_ItemSlotFinger2, proto.ItemSlot_ItemSlotBack, proto.ItemSlot_ItemSlotMainHand}
	m := map[proto.ItemSlot]int32{}
	for i := 0; i < n; i++ {
		m[slots[i]] = ids[i]
	}
	return m
}

func warlockRing(id int32) map[proto.ItemSlot]int32 {
	return map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: id}
}

func TestAbyssalCalling(t *testing.T) {
	sim, lock, _ := newWarlock(t, demonologyPreset, "demo", warlockRing(VaelithsAbyssalCallingItemID))
	if cd := lock.DemonicEmpowerment.CD.Duration; cd != abyssalCallingDECooldown {
		t.Errorf("Demonic Empowerment cooldown %v with the ring, want %v", cd, abyssalCallingDECooldown)
	}
	sp := lock.GetStat(stats.SpellPower)
	lock.OnCastComplete(sim, lock.DemonicEmpowerment)
	if got, want := lock.GetStat(stats.SpellPower), sp*(1+abyssalCallingAuraSP); math.Abs(got-want) > 1e-6 {
		t.Errorf("spell power %.1f after Demonic Empowerment, want %.1f (+14%%)", got, want)
	}

	const iterations = 20
	res := runCCSim(t, warlockRequest(demonologyPreset, "demo", warlockRing(VaelithsAbyssalCallingItemID), iterations))
	empowerments := res.casts[core.ActionID{SpellID: 47193}]
	strikes := res.casts[core.ActionID{SpellID: 200086}]
	// 8 strikes per Pit Lord; the last one of each iteration may be cut short.
	if empowerments == 0 || strikes > 8*empowerments || strikes < 8*(empowerments-iterations) {
		t.Errorf("%d Abyssal Strikes for %d Demonic Empowerments, want 8 each", strikes, empowerments)
	}
}

func TestMalediction(t *testing.T) {
	sim, lock, target := newWarlock(t, destructionPreset, "destro", warlockRing(MorgrathsMaledictionItemID))
	dot := lock.GetSpell(core.ActionID{SpellID: 200089}).Dot(target)

	for i := 0; i < 100 && !dot.IsActive(); i++ {
		lock.Immolate.SkipCastAndApplyEffects(sim, target)
	}
	if !dot.IsActive() || dot.GetStacks() != 1 {
		t.Fatalf("Immolate must apply Malediction at 1 stack (active %v, %d stacks)", dot.IsActive(), dot.GetStacks())
	}
	if got, want := dot.SnapshotBaseDamage*maledictionTicks, maledictionTotalSP*dot.Spell.SpellPower(); math.Abs(got-want) > 1e-6 {
		t.Errorf("Malediction deals %.1f over 8 ticks, want 300%% of spell power (%.1f)", got, want)
	}
	// Chaos Bolt adds a stack until the collapse starts, then only refreshes. The collapse consumes
	// stacks over time, which doesn't pass here.
	snapshot := dot.SnapshotBaseDamage
	for i := 0; i < 30; i++ {
		lock.ChaosBolt.SkipCastAndApplyEffects(sim, target)
	}
	if stacks := dot.GetStacks(); stacks < 2 || stacks > 31 {
		t.Errorf("%d Malediction stacks after 30 Chaos Bolts", stacks)
	}
	if dot.SnapshotBaseDamage != snapshot {
		t.Error("Chaos Bolt must keep the Immolate snapshot")
	}

	res := runCCSim(t, warlockRequest(destructionPreset, "destro", warlockRing(MorgrathsMaledictionItemID), 20))
	for _, id := range []int32{200089, 200091} {
		if m := res.actions[core.ActionID{SpellID: id}]; m == nil || m.Damage <= 0 {
			t.Errorf("spell %d dealt no damage", id)
		}
	}
}

func TestChaosflame(t *testing.T) {
	sim, lock, _ := newWarlock(t, destructionPreset, "destro", warlockRing(VorraxsChaosflameItemID))
	additive, crit := lock.ChaosBolt.DamageMultiplierAdditive, lock.ChaosBolt.BonusCritRating
	for i := 0; i < 7; i++ {
		lock.OnCastComplete(sim, lock.Incinerate)
	}
	if got := lock.ChaosBolt.DamageMultiplierAdditive - additive; math.Abs(got-2.5) > 1e-9 {
		t.Errorf("Chaos Bolt +%.2f damage after 7 Incinerates, want +2.5 (5 stacks of 50%%)", got)
	}
	if got := lock.ChaosBolt.BonusCritRating - crit; math.Abs(got-100*core.CritRatingPerCritChance) > 1e-6 {
		t.Errorf("Chaos Bolt +%.0f crit rating under Chaosflame, want a guaranteed crit", got)
	}
	lock.OnCastComplete(sim, lock.ChaosBolt)
	if math.Abs(lock.ChaosBolt.DamageMultiplierAdditive-additive) > 1e-9 || math.Abs(lock.ChaosBolt.BonusCritRating-crit) > 1e-6 {
		t.Error("Chaos Bolt must consume Chaosflame")
	}
}

func TestBindingsOfTheFacelessPact(t *testing.T) {
	sim, lock, _ := newWarlock(t, demonologyPreset, "demo", setPieces(facelessPactPieces, 2))
	imps := lock.GetAura("Wild Imps")
	mc := lock.MoltenCoreAura
	mc.Activate(sim)
	mc.SetStacks(sim, 3)
	mc.Deactivate(sim)
	if imps.IsActive() {
		t.Error("Molten Core expiring summoned Wild Imps")
	}
	mc.Activate(sim)
	mc.SetStacks(sim, 3)
	lock.OnCastComplete(sim, lock.Incinerate)
	if !imps.IsActive() {
		t.Error("consuming a Molten Core charge must summon Wild Imps")
	}

	fireball, empowered, splash := core.ActionID{SpellID: 900562}, core.ActionID{SpellID: 900564}, core.ActionID{SpellID: 900563}
	two := runCCSim(t, warlockRequest(demonologyPreset, "demo", setPieces(facelessPactPieces, 2), 20))
	if two.casts[fireball] == 0 || two.casts[empowered] != 0 {
		t.Errorf("2pc: %d Fel Fireballs and %d empowered ones", two.casts[fireball], two.casts[empowered])
	}
	// 4pc: every third imp is empowered, so it casts half as many bolts as the other two.
	four := runCCSim(t, warlockRequest(demonologyPreset, "demo", setPieces(facelessPactPieces, 4), 20))
	if ratio := float64(four.casts[fireball]) / float64(four.casts[empowered]); ratio < 1.8 || ratio > 2.2 {
		t.Errorf("4pc: %d Fel Fireballs for %d empowered ones, want 2:1", four.casts[fireball], four.casts[empowered])
	}
	if four.casts[splash] != four.casts[empowered] {
		t.Errorf("4pc: %d splashes for %d empowered Fel Fireballs", four.casts[splash], four.casts[empowered])
	}
}

func TestImmolationOfTheWorldTree(t *testing.T) {
	sim, lock, _ := newWarlock(t, destructionPreset, "destro", setPieces(worldTreePieces, 2))
	if lock.cc.chaosBoltBonusCoeff != crashingChaosChaosBoltSP {
		t.Errorf("2pc: Chaos Bolt bonus coefficient %.2f", lock.cc.chaosBoltBonusCoeff)
	}
	castTime := lock.Incinerate.DefaultCast.CastTime
	lock.OnCastComplete(sim, lock.ChaosBolt)
	if lock.Incinerate.DefaultCast.CastTime != 0 {
		t.Error("2pc: Chaos Bolt must make the next Incinerate instant")
	}
	lock.OnCastComplete(sim, lock.Incinerate)
	if lock.Incinerate.DefaultCast.CastTime != castTime {
		t.Error("2pc: the instant Incinerate must be consumed")
	}
	if lock.GetAura("Crashing Chaos") != nil {
		t.Error("Crashing Chaos registered with 2 pieces")
	}

	sim, lock, _ = newWarlock(t, destructionPreset, "destro", setPieces(worldTreePieces, 4))
	crashing := lock.GetAura("Crashing Chaos")
	additive := lock.ChaosBolt.DamageMultiplierAdditive
	const casts = 2000
	procs := 0
	for i := 0; i < casts; i++ {
		crashing.Deactivate(sim)
		lock.OnCastComplete(sim, lock.ChaosBolt)
		if crashing.IsActive() {
			procs++
			if got := lock.ChaosBolt.DamageMultiplierAdditive - additive; math.Abs(got-crashingChaosBonus) > 1e-9 {
				t.Fatalf("Crashing Chaos adds %.2f to Chaos Bolt, want %.2f", got, crashingChaosBonus)
			}
		}
	}
	if rate := float64(procs) / casts; rate < 0.19 || rate > 0.25 {
		t.Errorf("Infernal on %.1f%% of Chaos Bolts, want 22%%", 100*rate)
	}
}

type ccDpsCase struct {
	name  string
	apl   string
	items map[proto.ItemSlot]int32
}

// logCCDpsImpact logs each case's DPS against stat-only copies of its items and against the preset gear.
func logCCDpsImpact(t *testing.T, preset warlockPreset, baseAPL string, cases []ccDpsCase) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	base := runCCSim(t, warlockRequest(preset, baseAPL, nil, iterations)).dps
	for _, c := range cases {
		dps := runCCSim(t, warlockRequest(preset, c.apl, c.items, iterations)).dps
		// The same items without their effects: what the effect alone is worth.
		statsOnly := runCCSim(t, warlockRequest(preset, baseAPL, cc.StatOnlyCopies(c.items), iterations)).dps
		t.Logf("%-34s %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f)",
			c.name, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base)
	}
}

// Logs the DPS impact of each custom item (go test -run CCDps -v).
func TestCCDpsImpactAffliction(t *testing.T) {
	logCCDpsImpact(t, afflictionPreset, "affliction", []ccDpsCase{
		{"Vaelith's Withering Grasp", "affliction_withering_grasp", warlockRing(VaelithsWitheringGraspItemID)},
		{"Vaelith's Reaped Soul", "affliction", warlockRing(VaelithsReapedSoulItemID)},
		{"Rot of the Fevered Dream 2pc", "affliction", rotSet(2)},
		{"Rot of the Fevered Dream 4pc", "affliction", rotSet(4)},
	})
}

func TestCCDpsImpactDemonology(t *testing.T) {
	logCCDpsImpact(t, demonologyPreset, "demo", []ccDpsCase{
		{"Vaelith's Abyssal Calling", "demo", warlockRing(VaelithsAbyssalCallingItemID)},
		{"Bindings of the Faceless Pact 2pc", "demo", setPieces(facelessPactPieces, 2)},
		{"Bindings of the Faceless Pact 4pc", "demo", setPieces(facelessPactPieces, 4)},
	})
}

func TestCCDpsImpactDestruction(t *testing.T) {
	logCCDpsImpact(t, destructionPreset, "destro", []ccDpsCase{
		{"Morgrath's Malediction", "destro", warlockRing(MorgrathsMaledictionItemID)},
		{"Vorrax's Chaosflame", "destro", warlockRing(VorraxsChaosflameItemID)},
		{"Immolation of the World Tree 2pc", "destro", setPieces(worldTreePieces, 2)},
		{"Immolation of the World Tree 4pc", "destro", setPieces(worldTreePieces, 4)},
	})
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

// Every custom spell gets only the class modifiers its 3.3.5a family flags allow.
func TestClassModifiers(t *testing.T) {
	sim := func(preset warlockPreset, apl string, items map[proto.ItemSlot]int32) *core.Simulation {
		s, _, _ := newWarlock(t, preset, apl, items)
		return s
	}
	ccfamily.Check(t, nil,
		sim(afflictionPreset, "affliction", mergeItems(warlockRing(VaelithsWitheringGraspItemID), rotSet(4))),
		sim(afflictionPreset, "affliction", warlockRing(VaelithsReapedSoulItemID)),
		sim(demonologyPreset, "demo", mergeItems(warlockRing(VaelithsAbyssalCallingItemID), setPieces(facelessPactPieces, 4))),
		sim(destructionPreset, "destro", mergeItems(warlockRing(MorgrathsMaledictionItemID), setPieces(worldTreePieces, 4))),
		sim(destructionPreset, "destro", warlockRing(VorraxsChaosflameItemID)),
	)
}
