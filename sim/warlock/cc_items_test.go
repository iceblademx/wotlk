package warlock

import (
	"math"
	"testing"
	"time"

	"github.com/wowsims/wotlk/sim/common/cc"
	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
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

func afflictionRequest(apl string, items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../ui/warlock/gear_sets", "p4_affliction").GearSet
	for slot, id := range items {
		if slot == proto.ItemSlot_ItemSlotMainHand {
			gear.Items[proto.ItemSlot_ItemSlotOffHand] = &proto.ItemSpec{}
		}
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	return &proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceOrc,
			Class:         proto.Class_ClassWarlock,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          DefaultAfflictionWarlock,
			TalentsString: AfflictionTalents,
			Glyphs:        AfflictionGlyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../ui/warlock/apls", apl).Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 7, IsTest: true},
	}
}

func newAffliction(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *Warlock, *core.Unit) {
	t.Helper()
	req := afflictionRequest("affliction", items, 1)
	req.Raid.Debuffs = &proto.Debuffs{}
	sim := core.NewSim(req)
	sim.Reset()
	return sim, sim.Raid.Parties[0].Players[0].(*Warlock), sim.Encounter.TargetUnits[0]
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

// Logs the DPS impact of each custom item on the affliction preset (go test -run CCDps -v).
func TestCCDpsImpactAffliction(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	base := runCCSim(t, afflictionRequest("affliction", nil, iterations)).dps
	for _, c := range []struct {
		name  string
		apl   string
		items map[proto.ItemSlot]int32
	}{
		{"Vaelith's Withering Grasp", "affliction_withering_grasp", map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: VaelithsWitheringGraspItemID}},
		{"Vaelith's Reaped Soul", "affliction", map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: VaelithsReapedSoulItemID}},
		{"Rot of the Fevered Dream 2pc", "affliction", rotSet(2)},
		{"Rot of the Fevered Dream 4pc", "affliction", rotSet(4)},
	} {
		dps := runCCSim(t, afflictionRequest(c.apl, c.items, iterations)).dps
		// The same items without their effects: what the effect alone is worth.
		statsOnly := runCCSim(t, afflictionRequest("affliction", cc.StatOnlyCopies(c.items), iterations)).dps
		t.Logf("%-30s %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f)",
			c.name, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base)
	}
}
