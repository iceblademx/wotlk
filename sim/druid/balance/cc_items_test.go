package balance

import (
	"math"
	"testing"
	"time"

	"github.com/wowsims/wotlk/sim/common/cc"
	"github.com/wowsims/wotlk/sim/common/cc/ccfamily"
	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/druid"
	googleProto "google.golang.org/protobuf/proto"
)

// Tests for custom 3.3.5a server content in sim/druid/cc_items.go.

var canopyDreamerPieces = [4]int32{901013, 901014, 901015, 901016}

// canopySet equips the first n pieces (neck, ring, cloak, staff).
func canopySet(n int) map[proto.ItemSlot]int32 {
	slots := [4]proto.ItemSlot{proto.ItemSlot_ItemSlotNeck, proto.ItemSlot_ItemSlotFinger2, proto.ItemSlot_ItemSlotBack, proto.ItemSlot_ItemSlotMainHand}
	m := map[proto.ItemSlot]int32{}
	for i := 0; i < n; i++ {
		m[slots[i]] = canopyDreamerPieces[i]
	}
	return m
}

var fallingStar = map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: druid.SkarethsFallingStarItemID}

func balanceRequest(items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../../ui/balance_druid/gear_sets", "p4_horde").GearSet
	for slot, id := range items {
		if slot == proto.ItemSlot_ItemSlotMainHand {
			gear.Items[proto.ItemSlot_ItemSlotOffHand] = &proto.ItemSpec{}
		}
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	// A deep copy: sims write their class buffs into the raid buffs, which are shared globals.
	return googleProto.Clone(&proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceTauren,
			Class:         proto.Class_ClassDruid,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          PlayerOptionsAdaptive,
			TalentsString: StandardTalents,
			Glyphs:        StandardGlyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../../ui/balance_druid/apls", "basic_p3").Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 3, IsTest: true},
	}).(*proto.RaidSimRequest)
}

func newBalance(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *BalanceDruid, *core.Unit) {
	t.Helper()
	req := balanceRequest(items, 1)
	req.Raid.Debuffs = &proto.Debuffs{}
	sim := core.NewSim(req)
	sim.Reset()
	return sim, sim.Raid.Parties[0].Players[0].(*BalanceDruid), sim.Encounter.TargetUnits[0]
}

type simMetrics struct {
	dps    float64
	casts  map[int32]int32
	landed map[int32]int32
}

func runBalance(t *testing.T, req *proto.RaidSimRequest) simMetrics {
	t.Helper()
	res := core.RunRaidSim(req)
	if res.ErrorResult != "" {
		t.Fatal(res.ErrorResult)
	}
	player := res.RaidMetrics.Parties[0].Players[0]
	out := simMetrics{dps: player.Dps.Avg, casts: map[int32]int32{}, landed: map[int32]int32{}}
	for _, a := range player.Actions {
		for _, target := range a.Targets {
			out.casts[a.Id.GetSpellId()] += target.Casts
			out.landed[a.Id.GetSpellId()] += target.Hits + target.Crits
		}
	}
	return out
}

func landedCount(spell *core.Spell, target *core.Unit) int32 {
	m := spell.SpellMetrics[target.UnitIndex]
	return m.Hits + m.Crits
}

func TestFallingStar(t *testing.T) {
	sim, moonkin, target := newBalance(t, fallingStar)
	fury := moonkin.GetSpell(core.ActionID{SpellID: 200056})
	if fury == nil {
		t.Fatal("Fury of the Goddess not registered")
	}
	moonkin.OnCastComplete(sim, moonkin.Starfall.Spell)
	dot := fury.Dot(target)
	if !dot.IsActive() || moonkin.ChanneledDot != dot || dot.Duration != 3*time.Second {
		t.Fatalf("Starfall must start a 3 sec channel (active %v, %v)", dot.IsActive(), dot.Duration)
	}

	// Tick 1 is Moonfire, the other 11 are Starfire or Wrath at random.
	dot.TickCount = 1
	dot.OnTick(sim, target, dot)
	if !moonkin.Moonfire.Dot(target).IsActive() {
		t.Error("the first tick must cast Moonfire")
	}
	// Wrath deals its damage after its travel time, which doesn't pass here, so count Starfires.
	starfires := 0
	for i := 0; i < 1100; i++ {
		dot.TickCount = 2 + int32(i%11)
		sf := moonkin.Starfire.SpellMetrics[target.UnitIndex]
		before := sf.Hits + sf.Crits + sf.Misses
		dot.OnTick(sim, target, dot)
		if sf = moonkin.Starfire.SpellMetrics[target.UnitIndex]; sf.Hits+sf.Crits+sf.Misses > before {
			starfires++
		}
	}
	if rate := float64(starfires) / 1100; rate < 0.45 || rate > 0.55 {
		t.Errorf("Starfire on %.1f%% of the volley, want 50%%", 100*rate)
	}

	// In a fight, every Starfall is followed by the channel.
	m := runBalance(t, balanceRequest(fallingStar, 20))
	if m.casts[53201] == 0 || m.casts[200056] != m.casts[53201] {
		t.Errorf("%d channels for %d Starfalls", m.casts[200056], m.casts[53201])
	}
}

func TestCanopyDreamersRegalia(t *testing.T) {
	_, two, _ := newBalance(t, canopySet(2))
	_, four, _ := newBalance(t, canopySet(4))
	if got := four.Starfire.DamageMultiplier / two.Starfire.DamageMultiplier; math.Abs(got-1.04) > 1e-9 {
		t.Errorf("4pc: Starfire damage x%.3f, want x1.04", got)
	}
	if got := four.Wrath.DamageMultiplier / two.Wrath.DamageMultiplier; math.Abs(got-1.04) > 1e-9 {
		t.Errorf("4pc: Wrath damage x%.3f, want x1.04", got)
	}
	if two.GetAura("Power of Elune") != nil {
		t.Error("Power of Elune registered with 2 pieces")
	}

	// 2pc: 15% of landed Starfires and Wraths fire a Dream Burst.
	m := runBalance(t, balanceRequest(canopySet(2), 50))
	triggers := float64(m.landed[48465] + m.landed[48461])
	if rate := float64(m.casts[900332]) / triggers; rate < 0.13 || rate > 0.17 {
		t.Errorf("2pc: Dream Burst on %.1f%% of Starfire/Wrath hits, want 15%%", 100*rate)
	}
	// 4pc: Power of Elune makes every one fire while it lasts.
	m4 := runBalance(t, balanceRequest(canopySet(4), 50))
	triggers4 := float64(m4.landed[48465] + m4.landed[48461])
	if rate := float64(m4.casts[900332]) / triggers4; rate <= 0.17 {
		t.Errorf("4pc: Dream Burst on %.1f%% of Starfire/Wrath hits, want more than 15%%", 100*rate)
	}
}

// Logs the DPS impact of each custom item (go test -run CCDps -v).
func TestCCDpsImpactBalance(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	base := runBalance(t, balanceRequest(nil, iterations)).dps
	for _, c := range []struct {
		name  string
		items map[proto.ItemSlot]int32
	}{
		{"Skareth's Falling Star", fallingStar},
		{"Canopy Dreamer's Regalia 2pc", canopySet(2)},
		{"Canopy Dreamer's Regalia 4pc", canopySet(4)},
	} {
		dps := runBalance(t, balanceRequest(c.items, iterations)).dps
		statsOnly := runBalance(t, balanceRequest(cc.StatOnlyCopies(c.items), iterations)).dps
		t.Logf("%-30s %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f)",
			c.name, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base)
	}
}

// Every custom spell gets only the class modifiers its 3.3.5a family flags allow.
func TestClassModifiers(t *testing.T) {
	items := canopySet(4)
	for slot, id := range fallingStar {
		items[slot] = id
	}
	sim, _, _ := newBalance(t, items)
	ccfamily.Check(t, ccfamily.Reviewed{
		200056: "Fury of the Goddess is a dummy channel with Tranquility's family flag: the modifiers that reach it " +
			"(cooldown, threat, pushback, mana cost, healing) don't change the spells it casts, which use their own",
	}, sim)
}
