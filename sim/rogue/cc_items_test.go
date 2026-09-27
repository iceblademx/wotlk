package rogue

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

// A rogue spec preset for the custom item tests.
type roguePreset struct {
	gear    string
	talents string
	glyphs  *proto.Glyphs
	spec    *proto.Player_Rogue
	apl     string // default rotation
}

var (
	assassinationPreset = roguePreset{"p4_assassination", AssassinationTalents, AssassinationGlyphs, PlayerOptionsAssassinationDI, "rupture_mutilate"}
	combatPreset        = roguePreset{"p4_combat", CombatTalents, CombatGlyphs, PlayerOptionsCombatDI, "combat"}
	subtletyPreset      = roguePreset{"p3_dancesub", danceSubTalents, SubtletyGlyphs, PlayerOptionsSubtletyID, "subtlety"}
)

func rogueRequest(preset roguePreset, apl string, items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	gear := core.GetGearSet("../../ui/rogue/gear_sets", preset.gear).GearSet
	for slot, id := range items {
		gear.Items[slot] = &proto.ItemSpec{Id: id}
	}
	// A deep copy: sims write their class buffs into the raid buffs, which are shared globals.
	return googleProto.Clone(&proto.RaidSimRequest{
		Raid: core.SinglePlayerRaidProto(&proto.Player{
			Race:          proto.Race_RaceHuman,
			Class:         proto.Class_ClassRogue,
			Equipment:     gear,
			Consumes:      FullConsumes,
			Spec:          preset.spec,
			TalentsString: preset.talents,
			Glyphs:        preset.glyphs,
			Buffs:         core.FullIndividualBuffs,
			Rotation:      core.GetAplRotation("../../ui/rogue/apls", apl).Rotation,
		}, core.FullPartyBuffs, core.FullRaidBuffs, core.FullDebuffs),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: iterations, RandomSeed: 11, IsTest: true},
	}).(*proto.RaidSimRequest)
}

func subRequest(apl string, items map[proto.ItemSlot]int32, iterations int32) *proto.RaidSimRequest {
	return rogueRequest(subtletyPreset, apl, items, iterations)
}

// newRogue builds a reset single-iteration sim without debuffs, for testing effects directly.
func newRogue(t *testing.T, preset roguePreset, items map[proto.ItemSlot]int32) (*core.Simulation, *Rogue, *core.Unit) {
	t.Helper()
	req := rogueRequest(preset, preset.apl, items, 1)
	req.Raid.Debuffs = &proto.Debuffs{}
	sim := core.NewSim(req)
	sim.Reset()
	return sim, sim.Raid.Parties[0].Players[0].(*Rogue), sim.Encounter.TargetUnits[0]
}

func newSub(t *testing.T, items map[proto.ItemSlot]int32) (*core.Simulation, *Rogue, *core.Unit) {
	t.Helper()
	return newRogue(t, subtletyPreset, items)
}

// setPieces equips the first n pieces of a custom set (neck, ring, cloak, weapon).
func setPieces(ids [4]int32, n int) map[proto.ItemSlot]int32 {
	slots := [4]proto.ItemSlot{proto.ItemSlot_ItemSlotNeck, proto.ItemSlot_ItemSlotFinger2, proto.ItemSlot_ItemSlotBack, proto.ItemSlot_ItemSlotMainHand}
	m := map[proto.ItemSlot]int32{}
	for i := 0; i < n; i++ {
		m[slots[i]] = ids[i]
	}
	return m
}

var (
	blightsapPieces = [4]int32{901077, 901078, 901079, 901080}
	cutthroatPieces = [4]int32{901081, 901082, 901083, 901084}
)

// strikeDamageOK reports whether damage is base after the attacker's damage multiplier, possibly a crit
// and possibly partially resisted (non-physical schools).
func strikeDamageOK(spell *core.Spell, target *core.Unit, base, damage float64) bool {
	want := base * spell.AttackerDamageMultiplier(spell.Unit.AttackTables[target.UnitIndex])
	return afterPartialResist(damage, want) || afterPartialResist(damage, want*spell.CritMultiplier)
}

// spellDamage returns the damage a spell dealt to target while f ran.
func spellDamage(spell *core.Spell, target *core.Unit, f func()) float64 {
	before := spell.SpellMetrics[target.UnitIndex].TotalDamage
	f()
	return spell.SpellMetrics[target.UnitIndex].TotalDamage - before
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
	// 2pc: a landed Hemorrhage adds a Shadow Strike with each weapon, each for 31.2% of attack power.
	sim, rogue, target := newSub(t, canopySet(2))
	mh, oh := rogue.GetSpell(core.ActionID{SpellID: 900512, Tag: 1}), rogue.GetSpell(core.ActionID{SpellID: 900512, Tag: 2})
	if mh == nil || oh == nil {
		t.Fatal("2pc: Shadow Strikes not registered")
	}
	for i := 0; i < 20; i++ {
		var mhDamage float64
		ohDamage := spellDamage(oh, target, func() {
			mhDamage = spellDamage(mh, target, func() { castUntilLanded(t, sim, rogue.Hemorrhage, target, func() {}) })
		})
		base := shadowStrikesHemorrhageAP * mh.MeleeAttackPower()
		if !strikeDamageOK(mh, target, base, mhDamage) || !strikeDamageOK(oh, target, base, ohDamage) {
			t.Fatalf("Shadow Strikes dealt %.1f/%.1f, want 31.2%% of %.0f attack power", mhDamage, ohDamage, mh.MeleeAttackPower())
		}
	}
	hemo := rogue.Hemorrhage.SpellMetrics[target.UnitIndex]
	if landed := hemo.Hits + hemo.Crits; mh.SpellMetrics[target.UnitIndex].Casts != landed || oh.SpellMetrics[target.UnitIndex].Casts != landed {
		t.Errorf("%d landed Hemorrhages but %d/%d Shadow Strikes", landed, mh.SpellMetrics[target.UnitIndex].Casts, oh.SpellMetrics[target.UnitIndex].Casts)
	}
	if rogue.GetSpell(core.ActionID{SpellID: 900514}) != nil {
		t.Error("the Shadow Dance strikes need the 4pc")
	}

	// 4pc: during Shadow Dance, abilities strike again for 100% of attack power.
	sim, rogue, target = newSub(t, canopySet(4))
	echo := rogue.GetSpell(core.ActionID{SpellID: 900514})
	debuff := rogue.cc.shadowOfTheCanopyAuras.Get(target)

	rogue.ShadowDanceAura.Deactivate(sim)
	debuff.Deactivate(sim)
	castUntilLanded(t, sim, rogue.Backstab, target, func() {})
	if echo.SpellMetrics[target.UnitIndex].Casts != 0 {
		t.Error("abilities strike twice outside Shadow Dance")
	}
	rogue.ShadowDanceAura.Activate(sim)
	got := spellDamage(echo, target, func() { castUntilLanded(t, sim, rogue.Backstab, target, func() {}) })
	if !strikeDamageOK(echo, target, echo.MeleeAttackPower(), got) {
		t.Errorf("second strike dealt %.1f, want 100%% of %.0f attack power", got, echo.MeleeAttackPower())
	}

	// When Shadow Dance ends, the debuff increases the rogue's Shadow damage on the target.
	rogue.ShadowDanceAura.Deactivate(sim)
	if !debuff.IsActive() || debuff.Duration != shadowOfTheCanopyDuration {
		t.Fatal("Shadow Dance ending must apply Shadow of the Canopy")
	}
	rogue.ShadowDanceAura.Activate(sim)
	got = spellDamage(echo, target, func() { castUntilLanded(t, sim, rogue.Backstab, target, func() {}) })
	if want := echo.MeleeAttackPower() * (1 + shadowOfTheCanopyBonus); !strikeDamageOK(echo, target, want, got) {
		t.Errorf("second strike under the debuff dealt %.1f, want %.1f before modifiers", got, want)
	}
}

func TestFateboundCoin(t *testing.T) {
	sim, rogue, target := newRogue(t, combatPreset, rogueRing(VorraxsFateboundCoinItemID))
	heads, tailsStreak := rogue.GetAura("Coin Flip: Heads"), rogue.GetAura("Coin Flip: Tails Streak")
	tails := rogue.GetSpell(core.ActionID{SpellID: 200059})
	baseRegen := rogue.EnergyTickMultiplier

	rogue.onFinisher(sim, rogue.Eviscerate, 3)
	if heads.IsActive() || tailsStreak.IsActive() {
		t.Fatal("a 3 combo point finisher flipped the coin")
	}
	sawHeadsStreak, sawTailsStreak := false, false
	for i := 0; i < 200; i++ {
		prevHeads, prevTails := heads.GetStacks(), tailsStreak.GetStacks()
		damage := spellDamage(tails, target, func() { rogue.onFinisher(sim, rogue.Eviscerate, 4) })
		if heads.IsActive() == tailsStreak.IsActive() {
			t.Fatalf("flip %d: Heads active %v, Tails streak active %v", i, heads.IsActive(), tailsStreak.IsActive())
		}
		if heads.IsActive() {
			if heads.GetStacks() != prevHeads+1 {
				t.Fatalf("Heads streak %d after %d", heads.GetStacks(), prevHeads)
			}
			sawHeadsStreak = sawHeadsStreak || heads.GetStacks() > 1
			want := baseRegen + fateboundHeadsBase + fateboundHeadsPerFlip*float64(heads.GetStacks())
			if math.Abs(rogue.EnergyTickMultiplier-want) > 1e-9 {
				t.Fatalf("energy regen x%.2f at %d Heads, want x%.2f", rogue.EnergyTickMultiplier, heads.GetStacks(), want)
			}
			continue
		}
		if tailsStreak.GetStacks() != prevTails+1 || math.Abs(rogue.EnergyTickMultiplier-baseRegen) > 1e-9 {
			t.Fatalf("Tails must end Heads and extend the streak (%d after %d)", tailsStreak.GetStacks(), prevTails)
		}
		sawTailsStreak = sawTailsStreak || tailsStreak.GetStacks() > 1
		base := fateboundTailsAP * tails.MeleeAttackPower() * (1 + fateboundTailsPerFlip*float64(tailsStreak.GetStacks()-1))
		if damage != 0 && !strikeDamageOK(tails, target, base, damage) {
			t.Fatalf("Tails %d of a streak dealt %.1f, want %.1f before modifiers", tailsStreak.GetStacks(), damage, base)
		}
	}
	if !sawHeadsStreak || !sawTailsStreak {
		t.Error("200 flips never produced both streaks")
	}
}

func TestRollTheBones(t *testing.T) {
	sim, rogue, target := newRogue(t, combatPreset, rogueRing(RollTheBonesItemID))
	aura := rogue.GetAura
	buffs := []string{"Broadside", "Buried Treasure", "Grand Melee", "Ruthless Precision", "Skull and Crossbones", "True Bearings"}

	// 5 combo points always grant a buff; other finishers never do.
	rogue.onFinisher(sim, rogue.Rupture, 5)
	for _, name := range buffs {
		if aura(name).IsActive() {
			t.Fatalf("Rupture granted %s", name)
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		rogue.onFinisher(sim, rogue.Eviscerate, 5)
		for _, name := range buffs {
			if aura(name).IsActive() {
				seen[name] = true
				aura(name).Deactivate(sim)
			}
		}
	}
	if len(seen) != len(buffs) {
		t.Errorf("200 rolls granted only %v", seen)
	}

	regen := rogue.EnergyTickMultiplier
	aura("Buried Treasure").Activate(sim)
	if math.Abs(rogue.EnergyTickMultiplier-regen-buriedTreasureRegen) > 1e-9 {
		t.Error("Buried Treasure: +25% energy regeneration")
	}
	speed := rogue.SwingSpeed()
	aura("Grand Melee").Activate(sim)
	if math.Abs(rogue.SwingSpeed()/speed-grandMeleeHaste) > 1e-9 {
		t.Error("Grand Melee: +25% attack speed")
	}
	crit := rogue.GetStat(stats.MeleeCrit)
	aura("Ruthless Precision").Activate(sim)
	if math.Abs(rogue.GetStat(stats.MeleeCrit)-crit-ruthlessPrecisionCrit*core.CritRatingPerCritChance) > 1e-6 {
		t.Error("Ruthless Precision: +15% crit")
	}

	aura("Broadside").Activate(sim)
	castUntilLanded(t, sim, rogue.SinisterStrike, target, func() { setCP(sim, rogue, 0) })
	if cp := rogue.ComboPoints(); cp < 2 {
		t.Errorf("Sinister Strike under Broadside awarded %d combo points, want 1 extra", cp)
	}

	aura("True Bearings").Activate(sim)
	timer := rogue.BladeFlurry.CD.Timer
	readyAt := sim.CurrentTime + time.Minute
	timer.Set(readyAt)
	rogue.onFinisher(sim, rogue.Rupture, 4)
	if got := readyAt - timer.ReadyAt(); got != 4*time.Second {
		t.Errorf("True Bearings took %v off Blade Flurry after 4 combo points, want 4s", got)
	}

	aura("Skull and Crossbones").Activate(sim)
	extra := rogue.GetSpell(core.ActionID{SpellID: 200171})
	const casts = 1000
	for i := 0; i < casts; i++ {
		castUntilLanded(t, sim, rogue.SinisterStrike, target, func() { aura("Skull and Crossbones").Activate(sim) })
	}
	if rate := float64(extra.SpellMetrics[target.UnitIndex].Casts) / casts; rate < 0.21 || rate > 0.29 {
		t.Errorf("Skull and Crossbones struck again after %.1f%% of Sinister Strikes, want 25%%", 100*rate)
	}
}

func TestBetweenTheEyes(t *testing.T) {
	sim, rogue, target := newRogue(t, combatPreset, rogueRing(BetweenTheEyesItemID))
	kidneyShot := rogue.GetSpell(core.ActionID{SpellID: 8643})
	damage := rogue.GetSpell(core.ActionID{SpellID: 200174})
	if kidneyShot == nil || damage == nil {
		t.Fatal("Kidney Shot must be available with the ring")
	}
	if kidneyShot.CD.Duration != 20*time.Second {
		t.Errorf("Kidney Shot cooldown %v, want 20s", kidneyShot.CD.Duration)
	}
	// The crit damage bonus is doubled: 3x instead of 2x (before meta gems and talents).
	normal := rogue.Character.MeleeCritMultiplier(rogue.preyOnTheWeakMultiplier(target), 0)
	if want := 1 + 2*(normal-1); math.Abs(damage.CritMultiplier-want) > 1e-9 {
		t.Errorf("crit multiplier %.3f, want %.3f", damage.CritMultiplier, want)
	}

	for cp := int32(1); cp <= 5; cp++ {
		var got float64
		for got == 0 {
			got = spellDamage(damage, target, func() {
				castUntilLanded(t, sim, kidneyShot, target, func() { setCP(sim, rogue, cp) })
			})
		}
		// Ruthlessness may award 1 back.
		if rogue.ComboPoints() > 1 {
			t.Fatal("Kidney Shot must spend its combo points")
		}
		// Physical: reduced by the boss's armor, the same way at every combo point count.
		armor := got / (betweenTheEyesAPPerPoint * float64(cp) * damage.MeleeAttackPower() * damage.AttackerDamageMultiplier(rogue.AttackTables[target.UnitIndex]))
		if !(armor > 0.5 && armor < 1) && !(armor > 0.5*damage.CritMultiplier && armor < damage.CritMultiplier) {
			t.Errorf("%d CP: %.1f damage, %.3f of 50%% attack power per combo point", cp, got, armor)
		}
	}

	res := core.RunRaidSim(rogueRequest(combatPreset, "combat_between_the_eyes", rogueRing(BetweenTheEyesItemID), 5))
	if res.ErrorResult != "" {
		t.Fatal(res.ErrorResult)
	}
	casts := int32(0)
	for _, a := range res.RaidMetrics.Parties[0].Players[0].Actions {
		// Split by combo points, so tagged.
		if core.ProtoToActionID(a.Id).SpellID == 8643 {
			for _, target := range a.Targets {
				casts += target.Casts
			}
		}
	}
	if casts == 0 {
		t.Error("the Between the Eyes rotation never cast Kidney Shot")
	}
}

func TestVenomdrinker(t *testing.T) {
	_, plain, _ := newRogue(t, assassinationPreset, nil)
	if plain.Shiv.CD.Timer != nil {
		t.Error("Shiv has a cooldown without Venomdrinker")
	}

	sim, rogue, _ := newRogue(t, assassinationPreset, rogueRing(VenomdrinkerItemID))
	if rogue.Shiv.DefaultCast.Cost != 0 || rogue.Shiv.CD.Duration != venomdrinkerShivCooldown {
		t.Errorf("Shiv costs %.0f energy with a %v cooldown, want free with 20s", rogue.Shiv.DefaultCast.Cost, rogue.Shiv.CD.Duration)
	}
	regen, nature := rogue.EnergyTickMultiplier, rogue.PseudoStats.SchoolDamageDealtMultiplier[stats.SchoolIndexNature]
	rogue.OnCastComplete(sim, rogue.Shiv)
	if math.Abs(rogue.EnergyTickMultiplier-regen-venomRushRegen) > 1e-9 ||
		math.Abs(rogue.PseudoStats.SchoolDamageDealtMultiplier[stats.SchoolIndexNature]/nature-venomRushNatureDamage) > 1e-9 {
		t.Error("Venom Rush: +50% energy regeneration and +20% Nature damage")
	}
}

func TestSanguineHunger(t *testing.T) {
	sim, rogue, target := newRogue(t, assassinationPreset, rogueRing(SanguineHungerItemID))
	castUntilLanded(t, sim, rogue.Rupture, target, func() { setCP(sim, rogue, 5) })
	rupture := rogue.Rupture.Dot(target)

	rogue.SpendEnergy(sim, rogue.CurrentEnergy(), rogue.Eviscerate.Cost.(*core.EnergyCost).ResourceMetrics)
	rupture.TickOnce(sim)
	if e := rogue.CurrentEnergy(); e != sanguineHungerEnergy {
		t.Errorf("a Rupture tick restored %.0f energy, want %d", e, sanguineHungerEnergy)
	}

	// Envenom extends the bleed by 1 sec per combo point, but never past its full duration.
	rupture.UpdateExpires(sim.CurrentTime + rupture.Duration - 6*time.Second)
	envenom := func(cp int32) time.Duration {
		before := rupture.ExpiresAt()
		castUntilLanded(t, sim, rogue.Envenom, target, func() { setCP(sim, rogue, cp) })
		return rupture.ExpiresAt() - before
	}
	if got := envenom(4); got != 4*time.Second {
		t.Errorf("4 combo point Envenom extended Rupture by %v, want 4s", got)
	}
	if got := envenom(5); got != 2*time.Second {
		t.Errorf("Envenom extended Rupture by %v, want 2s (up to its full duration)", got)
	}
}

func TestBlightsapPoisonersGarb(t *testing.T) {
	sim, rogue, target := newRogue(t, assassinationPreset, setPieces(blightsapPieces, 4))
	mark := rogue.GetSpell(core.ActionID{SpellID: 900493})
	marks := target.GetAura("Deathstalker's Mark-" + rogue.Label)

	castUntilLanded(t, sim, rogue.Garrote, target, func() {})
	if !marks.IsActive() || marks.GetStacks() != deathstalkersMarkStacks {
		t.Fatalf("Garrote must apply 3 marks (%d)", marks.GetStacks())
	}
	castUntilLanded(t, sim, rogue.Envenom, target, func() { setCP(sim, rogue, 3) })
	if marks.GetStacks() != 3 || mark.SpellMetrics[target.UnitIndex].Casts != 0 {
		t.Error("a 3 combo point Envenom consumed a mark")
	}

	vanish := rogue.Vanish.CD.Timer
	readyAt := sim.CurrentTime + 3*time.Minute
	vanish.Set(readyAt)
	got := spellDamage(mark, target, func() {
		castUntilLanded(t, sim, rogue.Envenom, target, func() { setCP(sim, rogue, 4) })
	})
	if marks.GetStacks() != 2 || !strikeDamageOK(mark, target, deathstalkersMarkAP*mark.MeleeAttackPower(), got) {
		t.Errorf("4 combo point Envenom: %d marks left, %.1f damage", marks.GetStacks(), got)
	}
	if cdr := readyAt - vanish.ReadyAt(); cdr != deathstalkersMarkVanishCDR {
		t.Errorf("4pc: consuming a mark took %v off Vanish, want 30s", cdr)
	}

	res := core.RunRaidSim(rogueRequest(assassinationPreset, "mutilate_deathstalker", setPieces(blightsapPieces, 4), 5))
	if res.ErrorResult != "" {
		t.Fatal(res.ErrorResult)
	}
	for _, a := range res.RaidMetrics.Parties[0].Players[0].Actions {
		if core.ProtoToActionID(a.Id) == (core.ActionID{SpellID: 900493}) && a.Targets[0].Damage > 0 {
			return
		}
	}
	t.Error("the Deathstalker's Mark rotation never consumed a mark")
}

func TestBurrowCutthroatsKit(t *testing.T) {
	sim, rogue, target := newRogue(t, combatPreset, setPieces(cutthroatPieces, 4))
	form := rogue.GetAura("Flawless Form")
	flurry := rogue.GetSpell(core.ActionID{SpellID: 900503})
	additive := rogue.Eviscerate.DamageMultiplierAdditive

	castUntilLanded(t, sim, rogue.Rupture, target, func() { setCP(sim, rogue, 5) })
	for i := 0; i < 7; i++ {
		rogue.Rupture.Dot(target).TickOnce(sim)
	}
	if form.GetStacks() != flawlessFormMaxStacks {
		t.Fatalf("%d Flawless Form stacks after 7 Rupture ticks, want 5", form.GetStacks())
	}
	if got := rogue.Eviscerate.DamageMultiplierAdditive - additive; math.Abs(got-5*flawlessFormPerStack) > 1e-9 {
		t.Errorf("Eviscerate +%.2f damage at 5 stacks, want +0.20", got)
	}
	castUntilLanded(t, sim, rogue.Eviscerate, target, func() { setCP(sim, rogue, 5) })
	if form.IsActive() || math.Abs(rogue.Eviscerate.DamageMultiplierAdditive-additive) > 1e-9 {
		t.Error("Eviscerate must consume Flawless Form")
	}
	if flurry.SpellMetrics[target.UnitIndex].Casts != 1 {
		t.Errorf("4pc: %d flurries after consuming Flawless Form, want 1", flurry.SpellMetrics[target.UnitIndex].Casts)
	}
	castUntilLanded(t, sim, rogue.Eviscerate, target, func() { setCP(sim, rogue, 5) })
	if flurry.SpellMetrics[target.UnitIndex].Casts != 1 {
		t.Error("4pc: Eviscerate without Flawless Form swung a flurry")
	}
}

type ccDpsCase struct {
	name    string
	apl     string
	items   map[proto.ItemSlot]int32
	baseAPL string // rotation for the comparisons, if not apl (e.g. without the item's ability)
}

// logCCDpsImpact logs each case's DPS against stat-only copies of its items and against the preset gear.
func logCCDpsImpact(t *testing.T, preset roguePreset, cases []ccDpsCase) {
	if !testing.Verbose() {
		t.Skip("only logs numbers; run with -v")
	}
	const iterations = 500
	for _, c := range cases {
		baseAPL := c.apl
		if c.baseAPL != "" {
			baseAPL = c.baseAPL
		}
		base := runSub(t, rogueRequest(preset, baseAPL, nil, iterations))
		dps := runSub(t, rogueRequest(preset, c.apl, c.items, iterations))
		statsOnly := runSub(t, rogueRequest(preset, baseAPL, cc.StatOnlyCopies(c.items), iterations))
		t.Logf("%-32s %6.0f DPS: effect %+5.1f%% (vs %.0f same stats), %+5.1f%% vs preset gear (%.0f, %s)",
			c.name, dps, 100*(dps/statsOnly-1), statsOnly, 100*(dps/base-1), base, c.apl)
	}
}

// Logs the DPS impact of each custom item (go test -run CCDps -v).
func TestCCDpsImpactSubtlety(t *testing.T) {
	logCCDpsImpact(t, subtletyPreset, []ccDpsCase{
		{"Endless Dance", "subtlety", rogueRing(EndlessDanceItemID), "subtlety"},
		{"Shadow's Invitation", "subtlety", rogueRing(ShadowsInvitationItemID), "subtlety"},
		{"Vorrax's Fatebound Coin", "subtlety", rogueRing(VorraxsFateboundCoinItemID), "subtlety"},
		{"Shadow of the Canopy 2pc", "subtlety_hemo", canopySet(2), "subtlety_hemo"},
		{"Shadow of the Canopy 4pc", "subtlety_hemo", canopySet(4), "subtlety_hemo"},
	})
}

func TestCCDpsImpactAssassination(t *testing.T) {
	logCCDpsImpact(t, assassinationPreset, []ccDpsCase{
		{"Vorrax's Fatebound Coin", "rupture_mutilate", rogueRing(VorraxsFateboundCoinItemID), "rupture_mutilate"},
		{"Venomdrinker", "mutilate_venomdrinker", rogueRing(VenomdrinkerItemID), "rupture_mutilate"},
		{"Sanguine Hunger", "rupture_mutilate", rogueRing(SanguineHungerItemID), "rupture_mutilate"},
		{"Blightsap Poisoner's Garb 2pc", "mutilate_deathstalker", setPieces(blightsapPieces, 2), "rupture_mutilate"},
		{"Blightsap Poisoner's Garb 4pc", "mutilate_deathstalker", setPieces(blightsapPieces, 4), "rupture_mutilate"},
	})
}

func TestCCDpsImpactCombat(t *testing.T) {
	logCCDpsImpact(t, combatPreset, []ccDpsCase{
		{"Vorrax's Fatebound Coin", "combat", rogueRing(VorraxsFateboundCoinItemID), "combat"},
		{"Roll the Bones", "combat_eviscerate", rogueRing(RollTheBonesItemID), "combat"},
		{"Between the Eyes", "combat_between_the_eyes", rogueRing(BetweenTheEyesItemID), "combat"},
		{"Burrow Cutthroat's Kit 2pc", "combat_eviscerate", setPieces(cutthroatPieces, 2), "combat"},
		{"Burrow Cutthroat's Kit 4pc", "combat_eviscerate", setPieces(cutthroatPieces, 4), "combat"},
		{"Combat (Eviscerate) rotation", "combat_eviscerate", nil, "combat"},
	})
}

func runSub(t *testing.T, req *proto.RaidSimRequest) float64 {
	t.Helper()
	res := core.RunRaidSim(req)
	if res.ErrorResult != "" {
		t.Fatal(res.ErrorResult)
	}
	return res.RaidMetrics.Parties[0].Players[0].Dps.Avg
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
	sim := func(preset roguePreset, items map[proto.ItemSlot]int32) *core.Simulation {
		s, _, _ := newRogue(t, preset, items)
		return s
	}
	ccfamily.Check(t, ccfamily.Reviewed{
		200174: "Between the Eyes damage: 200173 is the ring's own crit damage bonus (betweenTheEyesCritMultiplier); " +
			"the rogue T9 2pc (67209, proc 67210) fires on periodic damage only and this is a direct hit",
	},
		sim(subtletyPreset, mergeItems(rogueRing(EndlessDanceItemID), canopySet(4))),
		sim(subtletyPreset, rogueRing(ShadowsInvitationItemID)),
		sim(assassinationPreset, mergeItems(rogueRing(VenomdrinkerItemID), setPieces(blightsapPieces, 4))),
		sim(assassinationPreset, rogueRing(SanguineHungerItemID)),
		sim(assassinationPreset, rogueRing(VorraxsFateboundCoinItemID)),
		sim(combatPreset, mergeItems(rogueRing(RollTheBonesItemID), setPieces(cutthroatPieces, 4))),
		sim(combatPreset, rogueRing(BetweenTheEyesItemID)),
	)
}
