package rogue

import (
	"math"
	"testing"
	"time"

	"github.com/wowsims/wotlk/sim/common/cc"
	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
)

// Tests for custom 3.3.5a server content in cc_items.go.

// The UI's Subtlety preset: Hemorrhage, Premeditation and Shadow Dance.
const danceSubTalents = "30532010114--5022012030321121350115031151"

var canopyPieces = []struct {
	slot proto.ItemSlot
	id   int32
}{{proto.ItemSlot_ItemSlotNeck, 901085}, {proto.ItemSlot_ItemSlotFinger2, 901086},
	{proto.ItemSlot_ItemSlotBack, 901087}, {proto.ItemSlot_ItemSlotMainHand, 901088}}

func canopySet(n int) map[proto.ItemSlot]int32 {
	m := map[proto.ItemSlot]int32{}
	for _, p := range canopyPieces[:n] {
		m[p.slot] = p.id
	}
	return m
}

func rogueRing(id int32) map[proto.ItemSlot]int32 {
	return map[proto.ItemSlot]int32{proto.ItemSlot_ItemSlotFinger1: id}
}

func subRequest(apl string, items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../ui/rogue/gear_sets", "p3_dancesub").GearSet
	for slot, id := range items {
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	return &proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceHuman,
			Class:         proto.Class_ClassRogue,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          PlayerOptionsSubtletyID,
			TalentsString: danceSubTalents,
			Glyphs:        SubtletyGlyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../ui/rogue/apls", apl).Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 11, IsTest: true},
	}
}

func newSub(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *Rogue, *core.Unit) {
	t.Helper()
	req := subRequest("subtlety", items, 1)
	req.Raid.Debuffs = &proto.Debuffs{}
	sim := core.NewSim(req)
	sim.Reset()
	return sim, sim.Raid.Parties[0].Players[0].(*Rogue), sim.Encounter.TargetUnits[0]
}

func setCP(sim *core.Simulation, rogue *Rogue, cp int32) {
	metrics := rogue.Eviscerate.ComboPointMetrics()
	if rogue.ComboPoints() > 0 {
		rogue.SpendComboPoints(sim, metrics)
	}
	rogue.AddComboPoints(sim, cp, metrics)
}

// castUntilLanded casts spell (skipping cost, cooldown and cast checks) until it lands and returns
// the damage it dealt. before runs ahead of every attempt.
func castUntilLanded(t *testing.T, sim *core.Simulation, spell *core.Spell, target *core.Unit, before func()) float64 {
	t.Helper()
	for i := 0; i < 100; i++ {
		before()
		m := &spell.SpellMetrics[target.UnitIndex]
		hits, damage := m.Hits+m.Crits, m.TotalDamage
		spell.SkipCastAndApplyEffects(sim, target)
		if m.Hits+m.Crits > hits {
			return m.TotalDamage - damage
		}
	}
	t.Fatalf("%v never landed", spell.ActionID)
	return 0
}

// afterPartialResist reports whether got is want minus a partial resist: Shadow damage against a
// level 83 boss loses 0-30% in 10% steps.
func afterPartialResist(got, want float64) bool {
	for _, resist := range []float64{0, 0.1, 0.2, 0.3} {
		if math.Abs(got-want*(1-resist)) < 1e-6 {
			return true
		}
	}
	return false
}

func TestEndlessDance(t *testing.T) {
	sim, rogue, target := newSub(t, rogueRing(EndlessDanceItemID))
	timer := rogue.ShadowDance.CD.Timer

	for cp := int32(1); cp <= 5; cp++ {
		readyAt := sim.CurrentTime + time.Minute
		castUntilLanded(t, sim, rogue.Eviscerate, target, func() {
			timer.Set(readyAt)
			setCP(sim, rogue, cp)
		})
		if got := readyAt - timer.ReadyAt(); got != time.Second*time.Duration(cp) {
			t.Errorf("%d CP Eviscerate took %v off Shadow Dance, want %ds", cp, got, cp)
		}
	}
	// Never past ready.
	timer.Set(sim.CurrentTime + time.Second)
	castUntilLanded(t, sim, rogue.Eviscerate, target, func() { setCP(sim, rogue, 5) })
	if timer.ReadyAt() != sim.CurrentTime {
		t.Errorf("Shadow Dance ready at %v, want now (%v)", timer.ReadyAt(), sim.CurrentTime)
	}

	rogue.SpendEnergy(sim, rogue.CurrentEnergy(), rogue.Eviscerate.Cost.(*core.EnergyCost).ResourceMetrics)
	rogue.OnCastComplete(sim, rogue.ShadowDance)
	if e := rogue.CurrentEnergy(); e != 40 {
		t.Errorf("Shadow Dance restored %.0f energy, want 40", e)
	}
}

func TestShadowsInvitation(t *testing.T) {
	sim, rogue, target := newSub(t, rogueRing(ShadowsInvitationItemID))
	invitation := rogue.cc.shadowsInvitation
	baseCost := rogue.Ambush.DefaultCast.Cost

	rogue.StealthAura.Deactivate(sim)
	if rogue.Ambush.ExtraCastCondition(sim, target) {
		t.Fatal("Ambush usable outside Stealth without Shadow's Invitation")
	}
	castUntilLanded(t, sim, rogue.Eviscerate, target, func() { setCP(sim, rogue, 5) })
	if !invitation.IsActive() || !rogue.Ambush.ExtraCastCondition(sim, target) {
		t.Fatal("Eviscerate must allow an Ambush outside Stealth")
	}
	if cost := rogue.Ambush.DefaultCast.Cost; cost != baseCost-20 {
		t.Errorf("Ambush costs %.0f under Shadow's Invitation, want %.0f", cost, baseCost-20)
	}
	rogue.OnCastComplete(sim, rogue.Ambush)
	if invitation.IsActive() || rogue.Ambush.DefaultCast.Cost != baseCost {
		t.Error("Ambush must consume Shadow's Invitation and restore its cost")
	}

	// Ambush adds 30% of its damage as Shadow damage.
	shadow := rogue.GetSpell(core.ActionID{SpellID: 200183})
	before := shadow.SpellMetrics[target.UnitIndex].TotalDamage
	ambush := castUntilLanded(t, sim, rogue.Ambush, target, func() {})
	if got := shadow.SpellMetrics[target.UnitIndex].TotalDamage - before; !afterPartialResist(got, 0.3*ambush) {
		t.Errorf("Shadow's Invitation dealt %.1f after a %.1f Ambush, want 30%%", got, ambush)
	}
}

func TestShadowOfTheCanopy(t *testing.T) {
	// 2pc: a landed Hemorrhage adds a Shadow strike with each weapon.
	sim, rogue, target := newSub(t, canopySet(2))
	mh, oh := rogue.GetSpell(core.ActionID{SpellID: 900512, Tag: 1}), rogue.GetSpell(core.ActionID{SpellID: 900512, Tag: 2})
	if mh == nil || oh == nil {
		t.Fatal("2pc: Shadow Strikes not registered")
	}
	for i := 0; i < 20; i++ {
		castUntilLanded(t, sim, rogue.Hemorrhage, target, func() {})
	}
	hemo := rogue.Hemorrhage.SpellMetrics[target.UnitIndex]
	if landed := hemo.Hits + hemo.Crits; mh.SpellMetrics[target.UnitIndex].Casts != landed || oh.SpellMetrics[target.UnitIndex].Casts != landed {
		t.Errorf("%d landed Hemorrhages but %d/%d Shadow Strikes", landed, mh.SpellMetrics[target.UnitIndex].Casts, oh.SpellMetrics[target.UnitIndex].Casts)
	}
	if rogue.GetSpell(core.ActionID{SpellID: 900514}) != nil {
		t.Error("the Shadow Dance strikes need the 4pc")
	}

	// 4pc: during Shadow Dance, abilities strike again for a share of their damage.
	sim, rogue, target = newSub(t, canopySet(4))
	echo := rogue.GetSpell(core.ActionID{SpellID: 900514})
	debuff := rogue.cc.shadowOfTheCanopyAuras.Get(target)

	rogue.ShadowDanceAura.Deactivate(sim)
	castUntilLanded(t, sim, rogue.Backstab, target, func() {})
	if echo.SpellMetrics[target.UnitIndex].TotalDamage != 0 {
		t.Error("abilities strike twice outside Shadow Dance")
	}
	rogue.ShadowDanceAura.Activate(sim)
	backstab := castUntilLanded(t, sim, rogue.Backstab, target, func() {})
	if got := echo.SpellMetrics[target.UnitIndex].TotalDamage; !afterPartialResist(got, dancingShadowStrikesDamage*backstab) {
		t.Errorf("second strike dealt %.1f after a %.1f Backstab", got, backstab)
	}

	// When Shadow Dance ends, the debuff increases the rogue's Shadow damage on the target.
	rogue.ShadowDanceAura.Deactivate(sim)
	if !debuff.IsActive() || debuff.Duration != shadowOfTheCanopyDuration {
		t.Fatal("Shadow Dance ending must apply Shadow of the Canopy")
	}
	rogue.ShadowDanceAura.Activate(sim)
	echoBefore := echo.SpellMetrics[target.UnitIndex].TotalDamage
	backstab = castUntilLanded(t, sim, rogue.Backstab, target, func() {})
	got := echo.SpellMetrics[target.UnitIndex].TotalDamage - echoBefore
	if want := dancingShadowStrikesDamage * backstab * (1 + shadowOfTheCanopyBonus); !afterPartialResist(got, want) {
		t.Errorf("second strike under the debuff dealt %.1f, want %.1f", got, want)
	}
}

// Logs the DPS impact of each custom item on the Subtlety presets (go test -run CCDps -v).
func TestCCDpsImpactSubtlety(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	for _, c := range []struct {
		name  string
		apl   string
		items map[proto.ItemSlot]int32
	}{
		{"Endless Dance", "subtlety", rogueRing(EndlessDanceItemID)},
		{"Shadow's Invitation", "subtlety", rogueRing(ShadowsInvitationItemID)},
		{"Shadow of the Canopy 2pc", "subtlety_hemo", canopySet(2)},
		{"Shadow of the Canopy 4pc", "subtlety_hemo", canopySet(4)},
	} {
		base := runSub(t, subRequest(c.apl, nil, iterations))
		dps := runSub(t, subRequest(c.apl, c.items, iterations))
		statsOnly := runSub(t, subRequest(c.apl, cc.StatOnlyCopies(c.items), iterations))
		t.Logf("%-30s %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f, %s)",
			c.name, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base, c.apl)
	}
}

func runSub(t *testing.T, req *proto.RaidSimRequest) float64 {
	t.Helper()
	res := core.RunRaidSim(req)
	if res.ErrorResult != "" {
		t.Fatal(res.ErrorResult)
	}
	return res.RaidMetrics.Parties[0].Players[0].Dps.Avg
}
