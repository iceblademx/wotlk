package rogue

// Custom content from the 3.3.5a server (see assets/db_inputs/cc/REPORT.md). The server implements
// these with C++ scripts we don't have. The mechanics and tuning below follow what the server
// publishes at http://209.38.90.151:8087/sim/data.json (documented from its scripts), plus the spell
// data of the helper spells (inspect with ./build.ps1 inspect spell <id>).

import (
	"time"

	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/stats"
)

const (
	// Endless Dance (ring, spell 200179): Your finishing moves reduce the remaining cooldown of Shadow
	// Dance by 1 sec per combo point spent. Activating Shadow Dance restores 40 Energy.
	EndlessDanceItemID = 900146
	// Shadow's Invitation (ring, spell 200181): Your Eviscerate allows your next Ambush to be used without
	// being in Stealth and reduces its Energy cost by 20. Your Ambush deals additional Shadow damage equal
	// to 30% of its damage dealt.
	ShadowsInvitationItemID = 900147
	// Vorrax's Fatebound Coin (ring, spell 200057): Each finishing move that consumes 4 or more combo
	// points flips a Fatebound Coin. Heads increases your Energy regeneration by 30% for 15 sec or until
	// you flip Tails, gaining an additional 10% for each consecutive Heads flip. Tails deals Arcane damage
	// to your target, dealing additional damage for each consecutive Tails flip.
	VorraxsFateboundCoinItemID = 900119
	// Roll the Bones (ring, spell 200165): Your Eviscerate has a 20% chance per combo point spent to grant
	// you one of six combat enhancements for 15 sec. Different enhancements may overlap; receiving an
	// enhancement you already have refreshes its duration.
	RollTheBonesItemID = 900142
	// Between the Eyes (ring, spell 200173): Your Kidney Shot also deals Physical damage based on your
	// attack power and the combo points spent, retaining its stun and cooldown. This damage strikes for
	// three times normal damage on a critical strike.
	BetweenTheEyesItemID = 900143
	// Venomdrinker (ring, spell 200175): Your Shiv costs no Energy and grants Venom Rush for 8 sec,
	// increasing your Energy regeneration by 50% and your poison damage by 20%. Shiv now has a 20-sec
	// cooldown.
	VenomdrinkerItemID = 900144
	// Sanguine Hunger (ring, spell 200177): Your Rupture and Garrote restore 8 Energy each time they deal
	// periodic damage. Your Envenom extends your active bleeds on its target by 1 sec per combo point
	// spent, up to their original maximum durations.
	SanguineHungerItemID = 900145
)

// Tuning published by the server (data.json), checked against it by cc_tuning_test.go.
const (
	endlessDanceCDRPerPoint      = time.Second
	endlessDanceEnergy           = 40
	shadowsInvitationCostCut     = 20
	shadowsInvitationEcho        = 0.3
	fateboundCoinMinPoints       = 4
	fateboundHeadsBase           = 0.20 // + fateboundHeadsPerFlip for every consecutive Heads, so 30% at first
	fateboundHeadsPerFlip        = 0.10
	fateboundHeadsDuration       = time.Second * 15
	fateboundTailsAP             = 1.68
	fateboundTailsPerFlip        = 0.10 // per consecutive Tails after the first
	rollTheBonesChancePerPoint   = 0.20
	rollTheBonesDuration         = time.Second * 15
	buriedTreasureRegen          = 0.25
	grandMeleeHaste              = 1.25
	ruthlessPrecisionCrit        = 15
	skullAndCrossbonesChance     = 0.25
	trueBearingsPerPoint         = time.Second
	betweenTheEyesAPPerPoint     = 0.5
	betweenTheEyesCritMultiplier = 3.0
	venomdrinkerShivCooldown     = time.Second * 20
	venomRushDuration            = time.Second * 8
	venomRushRegen               = 0.5
	venomRushNatureDamage        = 1.2
	sanguineHungerEnergy         = 8
	sanguineHungerPerPoint       = time.Second
	deathstalkersMarkStacks      = 3
	deathstalkersMarkDuration    = time.Second * 30
	deathstalkersMarkMinPoints   = 4
	deathstalkersMarkAP          = 1.56
	deathstalkersMarkVanishCDR   = time.Second * 30
	flawlessFormPerStack         = 0.04
	flawlessFormMaxStacks        = 5
	flawlessFormDuration         = time.Second * 12
	flawlessFlurryPrimaryAP      = 0.50
	flawlessFlurrySecondaryAP    = 0.30
	flawlessFlurryBladeFlurryMod = 2.0 // +100% to non-primary targets during Blade Flurry
	shadowStrikesHemorrhageAP    = 0.312
	shadowStrikesDanceAP         = 1.0
	shadowOfTheCanopyBonus       = 0.15
	shadowOfTheCanopyDuration    = time.Second * 12
)

type finisherEffect func(sim *core.Simulation, spell *core.Spell, numPoints int32)

// State for custom items that upstream spell code checks.
type ccItems struct {
	shadowsInvitation *core.Aura // Ambush usable outside Stealth
	venomdrinker      bool       // Shiv costs no energy and has a cooldown

	finisherEffects []finisherEffect
	// Combo points spent by the last finisher. Finishers spend them before dealing damage, so
	// OnSpellHitDealt handlers read them here.
	lastFinisherPoints int32

	deathstalkersMarkFourPiece bool
	flawlessFormFourPiece      bool

	shadowOfTheCanopyAuras core.AuraArray
}

func init() {
	core.NewItemEffect(EndlessDanceItemID, func(agent core.Agent) {
		rogue := agent.(RogueAgent).GetRogue()
		// Needs Shadow Dance, which is registered after item effects.
		rogue.Env.RegisterPreFinalizeEffect(rogue.registerEndlessDance)
	})
	core.NewItemEffect(ShadowsInvitationItemID, func(agent core.Agent) {
		rogue := agent.(RogueAgent).GetRogue()
		rogue.Env.RegisterPreFinalizeEffect(rogue.registerShadowsInvitation)
	})
	core.NewItemEffect(VorraxsFateboundCoinItemID, func(agent core.Agent) {
		rogue := agent.(RogueAgent).GetRogue()
		rogue.Env.RegisterPreFinalizeEffect(rogue.registerFateboundCoin)
	})
	core.NewItemEffect(RollTheBonesItemID, func(agent core.Agent) {
		rogue := agent.(RogueAgent).GetRogue()
		rogue.Env.RegisterPreFinalizeEffect(rogue.registerRollTheBones)
	})
	core.NewItemEffect(BetweenTheEyesItemID, func(agent core.Agent) {
		rogue := agent.(RogueAgent).GetRogue()
		rogue.Env.RegisterPreFinalizeEffect(rogue.registerBetweenTheEyes)
	})
	core.NewItemEffect(VenomdrinkerItemID, func(agent core.Agent) {
		rogue := agent.(RogueAgent).GetRogue()
		// Read by registerShivSpell for the cooldown.
		rogue.cc.venomdrinker = true
		rogue.Env.RegisterPreFinalizeEffect(rogue.registerVenomdrinker)
	})
	core.NewItemEffect(SanguineHungerItemID, func(agent core.Agent) {
		rogue := agent.(RogueAgent).GetRogue()
		rogue.Env.RegisterPreFinalizeEffect(rogue.registerSanguineHunger)
	})
}

// onFinisher runs after every landed finishing move has spent its combo points.
func (rogue *Rogue) onFinisher(sim *core.Simulation, spell *core.Spell, numPoints int32) {
	rogue.cc.lastFinisherPoints = numPoints
	for _, effect := range rogue.cc.finisherEffects {
		effect(sim, spell, numPoints)
	}
}

// reduceCooldown takes d off a spell's remaining cooldown, if the spell exists.
func reduceCooldown(sim *core.Simulation, spell *core.Spell, d time.Duration) {
	if spell == nil || spell.CD.Timer == nil || spell.CD.IsReady(sim) {
		return
	}
	spell.CD.Set(max(sim.CurrentTime, spell.CD.ReadyAt()-d))
}

// alwaysActive registers a hidden aura that is active the whole fight, for item triggers.
func (rogue *Rogue) alwaysActive(aura core.Aura) *core.Aura {
	aura.Duration = core.NeverExpires
	aura.OnReset = func(aura *core.Aura, sim *core.Simulation) {
		aura.Activate(sim)
	}
	return rogue.RegisterAura(aura)
}

func (rogue *Rogue) registerEndlessDance() {
	if rogue.ShadowDance == nil {
		return
	}

	rogue.cc.finisherEffects = append(rogue.cc.finisherEffects, func(sim *core.Simulation, _ *core.Spell, numPoints int32) {
		reduceCooldown(sim, rogue.ShadowDance, endlessDanceCDRPerPoint*time.Duration(numPoints))
	})

	// Spell 200180: energize 40.
	energyMetrics := rogue.NewEnergyMetrics(core.ActionID{SpellID: 200180})
	rogue.alwaysActive(core.Aura{
		Label:    "Endless Dance",
		ActionID: core.ActionID{SpellID: 200179},
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == rogue.ShadowDance {
				rogue.AddEnergy(sim, endlessDanceEnergy, energyMetrics)
			}
		},
	})
}

func (rogue *Rogue) registerShadowsInvitation() {
	// Spell 200182: next Ambush ignores the Stealth requirement and costs 20 less Energy; 15 sec, 1 charge.
	var reduced float64
	rogue.cc.shadowsInvitation = rogue.RegisterAura(core.Aura{
		Label:    "Shadow's Invitation",
		ActionID: core.ActionID{SpellID: 200182},
		Duration: time.Second * 15,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			reduced = min(shadowsInvitationCostCut, rogue.Ambush.DefaultCast.Cost)
			rogue.Ambush.DefaultCast.Cost -= reduced
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			rogue.Ambush.DefaultCast.Cost += reduced
		},
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == rogue.Ambush {
				aura.Deactivate(sim)
			}
		},
	})

	// Spell 200183: Shadow damage, a flat 30% of the Ambush damage (the server passes it as base points).
	shadowDamage := rogue.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200183},
		SpellSchool: core.SpellSchoolShadow,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagIgnoreModifiers | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {},
	})

	rogue.alwaysActive(core.Aura{
		Label:    "Shadow's Invitation Trigger",
		ActionID: core.ActionID{SpellID: 200181},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() {
				return
			}
			switch spell {
			case rogue.Eviscerate:
				rogue.cc.shadowsInvitation.Activate(sim)
			case rogue.Ambush:
				if result.Damage > 0 {
					damage := shadowsInvitationEcho * result.Damage * rogue.shadowStrikesDebuffMultiplier(result.Target)
					shadowDamage.CalcAndDealDamage(sim, result.Target, damage, shadowDamage.OutcomeAlwaysHit)
				}
			}
		},
	})
}

func (rogue *Rogue) registerFateboundCoin() {
	// Spell 200059: Arcane, magic damage class.
	tails := rogue.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200059},
		SpellSchool: core.SpellSchoolArcane,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   rogue.DefaultSpellCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {},
	})

	// Spell 200060: counts consecutive Tails flips until the next Heads.
	tailsStreak := rogue.RegisterAura(core.Aura{
		Label:     "Coin Flip: Tails Streak",
		ActionID:  core.ActionID{SpellID: 200060},
		Duration:  core.NeverExpires,
		MaxStacks: 255,
	})

	// Spell 200058: energy regeneration, 20% + 10% per consecutive Heads flip. Its stacks count the
	// flips; the streak ends when it expires or on Tails.
	var regen float64
	heads := rogue.RegisterAura(core.Aura{
		Label:     "Coin Flip: Heads",
		ActionID:  core.ActionID{SpellID: 200058},
		Duration:  fateboundHeadsDuration,
		MaxStacks: 255,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			rogue.ApplyEnergyTickMultiplier(-regen)
			regen = 0
			if newStacks > 0 {
				regen = fateboundHeadsBase + fateboundHeadsPerFlip*float64(newStacks)
			}
			rogue.ApplyEnergyTickMultiplier(regen)
		},
	})

	rogue.cc.finisherEffects = append(rogue.cc.finisherEffects, func(sim *core.Simulation, _ *core.Spell, numPoints int32) {
		if numPoints < fateboundCoinMinPoints {
			return
		}
		if sim.RandomFloat("Fatebound Coin") < 0.5 {
			tailsStreak.Deactivate(sim)
			heads.Activate(sim)
			heads.AddStack(sim)
			return
		}
		heads.Deactivate(sim)
		tailsStreak.Activate(sim)
		tailsStreak.AddStack(sim)
		target := rogue.CurrentTarget
		damage := fateboundTailsAP * tails.MeleeAttackPower() * (1 + fateboundTailsPerFlip*float64(tailsStreak.GetStacks()-1))
		tails.CalcAndDealDamage(sim, target, damage, tails.OutcomeMagicHitAndCrit)
	})
}

func (rogue *Rogue) registerRollTheBones() {
	newBuff := func(spellID int32, label string, onGain, onExpire core.OnGain) *core.Aura {
		return rogue.RegisterAura(core.Aura{
			Label:    label,
			ActionID: core.ActionID{SpellID: spellID},
			Duration: rollTheBonesDuration,
			OnGain:   onGain,
			OnExpire: core.OnExpire(onExpire),
		})
	}
	noop := func(*core.Aura, *core.Simulation) {}

	// Spell 200166: combo point builders award 1 extra combo point.
	broadside := newBuff(200166, "Broadside", noop, noop)
	// Spell 200167: +25% energy regeneration.
	buriedTreasure := newBuff(200167, "Buried Treasure",
		func(_ *core.Aura, _ *core.Simulation) { rogue.ApplyEnergyTickMultiplier(buriedTreasureRegen) },
		func(_ *core.Aura, _ *core.Simulation) { rogue.ApplyEnergyTickMultiplier(-buriedTreasureRegen) })
	// Spell 200168: +25% attack speed.
	grandMelee := newBuff(200168, "Grand Melee",
		func(_ *core.Aura, sim *core.Simulation) { rogue.MultiplyAttackSpeed(sim, grandMeleeHaste) },
		func(_ *core.Aura, sim *core.Simulation) { rogue.MultiplyAttackSpeed(sim, 1/grandMeleeHaste) })
	// Spell 200169: +15% melee critical strike chance.
	ruthlessPrecision := newBuff(200169, "Ruthless Precision",
		func(_ *core.Aura, sim *core.Simulation) {
			rogue.AddStatDynamic(sim, stats.MeleeCrit, ruthlessPrecisionCrit*core.CritRatingPerCritChance)
		},
		func(_ *core.Aura, sim *core.Simulation) {
			rogue.AddStatDynamic(sim, stats.MeleeCrit, -ruthlessPrecisionCrit*core.CritRatingPerCritChance)
		})
	// Spell 200170: Sinister Strike (spell_proc family mask 0x2) has a 25% chance to strike again.
	skullAndCrossbones := newBuff(200170, "Skull and Crossbones", noop, noop)
	// Spell 200172: finishers reduce Adrenaline Rush, Blade Flurry, Killing Spree, Sprint and Evasion
	// cooldowns by 1 sec per combo point (Sprint and Evasion aren't simulated).
	trueBearings := newBuff(200172, "True Bearings", noop, noop)
	buffs := []*core.Aura{broadside, buriedTreasure, grandMelee, ruthlessPrecision, skullAndCrossbones, trueBearings}

	// Spell 200171: the extra strike, normalized main hand weapon damage + 180 (Sinister Strike's bonus).
	extraStrike := rogue.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200171},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskMeleeMHSpecial,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagIncludeTargetBonusDamage | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   rogue.MeleeCritMultiplier(false),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			baseDamage := 180 +
				spell.Unit.MHNormalizedWeaponDamage(sim, spell.MeleeAttackPower()) +
				spell.BonusWeaponDamage()
			spell.CalcAndDealDamage(sim, target, baseDamage, spell.OutcomeMeleeWeaponSpecialHitAndCrit)
		},
	})

	adrenalineRush := rogue.GetSpell(core.ActionID{SpellID: 13750})
	killingSpree := rogue.GetSpell(core.ActionID{SpellID: 51690})
	broadsideMetrics := rogue.NewComboPointMetrics(core.ActionID{SpellID: 200166})

	rogue.cc.finisherEffects = append(rogue.cc.finisherEffects, func(sim *core.Simulation, spell *core.Spell, numPoints int32) {
		if trueBearings.IsActive() {
			cdr := trueBearingsPerPoint * time.Duration(numPoints)
			reduceCooldown(sim, adrenalineRush, cdr)
			reduceCooldown(sim, rogue.BladeFlurry, cdr)
			reduceCooldown(sim, killingSpree, cdr)
		}
		if spell == rogue.Eviscerate && sim.RandomFloat("Roll the Bones") < rollTheBonesChancePerPoint*float64(numPoints) {
			// Rolling one that is already up refreshes it.
			buffs[int(sim.RandomFloat("Roll the Bones buff")*float64(len(buffs)))].Activate(sim)
		}
	})

	rogue.alwaysActive(core.Aura{
		Label:    "Roll the Bones",
		ActionID: core.ActionID{SpellID: 200165},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() {
				return
			}
			// Mutilate's two hits award its combo points once.
			if broadside.IsActive() && spell.Flags.Matches(SpellFlagBuilder) && spell != rogue.MutilateOH {
				rogue.AddComboPoints(sim, 1, broadsideMetrics)
			}
			if spell == rogue.SinisterStrike && skullAndCrossbones.IsActive() && sim.Proc(skullAndCrossbonesChance, "Skull and Crossbones") {
				extraStrike.Cast(sim, result.Target)
			}
		},
	})
}

func (rogue *Rogue) registerBetweenTheEyes() {
	// Spell 200174: Physical, 50% of attack power per combo point. The ring's SPELLMOD_CRIT_DAMAGE_BONUS
	// (+100%) doubles the crit bonus: 3x damage on a crit. With a secondary modifier s the multiplier is
	// 1 + (2-1)*(1+s), so s = multiplier - 2.
	damage := rogue.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200174},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   rogue.Character.MeleeCritMultiplier(rogue.preyOnTheWeakMultiplier(rogue.CurrentTarget), betweenTheEyesCritMultiplier-2),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {},
	})

	// Kidney Shot (rank 2) isn't in the upstream sim, whose targets are bosses immune to stuns. The server
	// adds the damage to the ability, so it is simulated as landing whenever Kidney Shot hits.
	rogue.RegisterSpell(core.SpellConfig{
		ActionID:     core.ActionID{SpellID: 8643},
		SpellSchool:  core.SpellSchoolPhysical,
		ProcMask:     core.ProcMaskMeleeMHSpecial,
		Flags:        core.SpellFlagMeleeMetrics | rogue.finisherFlags() | core.SpellFlagAPL,
		MetricSplits: 6,

		EnergyCost: core.EnergyCostOptions{
			Cost:          rogue.costModifier(25),
			Refund:        0.4 * float64(rogue.Talents.QuickRecovery),
			RefundMetrics: rogue.QuickRecoveryMetrics,
		},
		Cast: core.CastConfig{
			DefaultCast: core.Cast{
				GCD: time.Second,
			},
			IgnoreHaste: true,
			CD: core.Cooldown{
				Timer:    rogue.NewTimer(),
				Duration: time.Second * 20,
			},
			ModifyCast: func(sim *core.Simulation, spell *core.Spell, cast *core.Cast) {
				spell.SetMetricsSplit(spell.Unit.ComboPoints())
			},
		},
		ExtraCastCondition: func(sim *core.Simulation, target *core.Unit) bool {
			return rogue.ComboPoints() > 0
		},

		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			rogue.BreakStealth(sim)
			comboPoints := rogue.ComboPoints()
			result := spell.CalcOutcome(sim, target, spell.OutcomeMeleeSpecialHit)
			if result.Landed() {
				rogue.ApplyFinisher(sim, spell)
				baseDamage := betweenTheEyesAPPerPoint * float64(comboPoints) * damage.MeleeAttackPower()
				damage.CalcAndDealDamage(sim, target, baseDamage, damage.OutcomeMeleeSpecialCritOnly)
			} else {
				spell.IssueRefund(sim)
			}
			spell.DealOutcome(sim, result)
		},
	})
}

// venomdrinkerCooldown gives Shiv its 20 sec cooldown when Venomdrinker is equipped.
func (rogue *Rogue) venomdrinkerCooldown() core.Cooldown {
	if !rogue.cc.venomdrinker {
		return core.Cooldown{}
	}
	return core.Cooldown{Timer: rogue.NewTimer(), Duration: venomdrinkerShivCooldown}
}

func (rogue *Rogue) registerVenomdrinker() {
	// Spell 200175: SPELLMOD_COST -100% on Shiv.
	rogue.Shiv.DefaultCast.Cost = 0

	// Spell 200176: +50% energy regeneration and +20% Nature damage done (poisons, and also Envenom and
	// Deathstalker's Mark, which are Nature too).
	venomRush := rogue.RegisterAura(core.Aura{
		Label:    "Venom Rush",
		ActionID: core.ActionID{SpellID: 200176},
		Duration: venomRushDuration,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			rogue.ApplyEnergyTickMultiplier(venomRushRegen)
			rogue.PseudoStats.SchoolDamageDealtMultiplier[stats.SchoolIndexNature] *= venomRushNatureDamage
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			rogue.ApplyEnergyTickMultiplier(-venomRushRegen)
			rogue.PseudoStats.SchoolDamageDealtMultiplier[stats.SchoolIndexNature] /= venomRushNatureDamage
		},
	})

	rogue.alwaysActive(core.Aura{
		Label:    "Venomdrinker",
		ActionID: core.ActionID{SpellID: 200175},
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == rogue.Shiv {
				venomRush.Activate(sim)
			}
		},
	})
}

func (rogue *Rogue) registerSanguineHunger() {
	// Spell 200178: energize 8 per Rupture or Garrote tick.
	energyMetrics := rogue.NewEnergyMetrics(core.ActionID{SpellID: 200178})

	// Extends a bleed's remaining time, never past its full duration. Like the server's SetDuration, the
	// ticks keep their period, so an extension only adds a tick once it reaches the next tick time.
	extend := func(sim *core.Simulation, dot *core.Dot, by time.Duration) {
		if dot.IsActive() {
			dot.UpdateExpires(min(dot.ExpiresAt()+by, sim.CurrentTime+dot.Duration))
		}
	}

	rogue.alwaysActive(core.Aura{
		Label:    "Sanguine Hunger",
		ActionID: core.ActionID{SpellID: 200177},
		OnPeriodicDamageDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell == rogue.Rupture || spell == rogue.Garrote {
				rogue.AddEnergy(sim, sanguineHungerEnergy, energyMetrics)
			}
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell != rogue.Envenom || !result.Landed() {
				return
			}
			by := sanguineHungerPerPoint * time.Duration(rogue.cc.lastFinisherPoints)
			extend(sim, rogue.Rupture.Dot(result.Target), by)
			extend(sim, rogue.Garrote.Dot(result.Target), by)
		},
	})
}

// Blightsap Poisoner's Garb (custom assassination set 9320: neck, ring, cloak, weapon).
var ItemSetBlightsapPoisonersGarb = core.NewItemSet(core.ItemSet{
	Name: "Blightsap Poisoner's Garb",
	Bonuses: map[int32]core.ApplyEffect{
		// Garrote applies 3 stacks of Deathstalker's Mark to your target for 30 sec. When you spend 4 or
		// more combo points on Envenom against a marked target, you consume a stack, dealing 156% of your
		// attack power as Poison damage.
		2: func(agent core.Agent) {
			rogue := agent.(RogueAgent).GetRogue()
			rogue.Env.RegisterPreFinalizeEffect(rogue.registerDeathstalkersMark)
		},
		// Each time you consume a stack of Deathstalker's Mark, reduce the cooldown of Vanish by 30 sec.
		// Handled in registerDeathstalkersMark.
		4: func(agent core.Agent) {
			agent.(RogueAgent).GetRogue().cc.deathstalkersMarkFourPiece = true
		},
	},
})

func (rogue *Rogue) registerDeathstalkersMark() {
	// Runs before finalization, after every set bonus has been applied.
	hasFourPiece := rogue.cc.deathstalkersMarkFourPiece

	// Spell 900493: Nature ("Poison") damage, melee damage class.
	mark := rogue.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900493},
		SpellSchool: core.SpellSchoolNature,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   rogue.MeleeCritMultiplier(false),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.CalcAndDealDamage(sim, target, deathstalkersMarkAP*spell.MeleeAttackPower(), spell.OutcomeMeleeSpecialCritOnly)
		},
	})

	// Spell 900492: 3 stacks on the target for 30 sec.
	marks := rogue.NewEnemyAuraArray(func(target *core.Unit) *core.Aura {
		return target.GetOrRegisterAura(core.Aura{
			Label:     "Deathstalker's Mark-" + rogue.Label,
			ActionID:  core.ActionID{SpellID: 900492},
			Duration:  deathstalkersMarkDuration,
			MaxStacks: deathstalkersMarkStacks,
		})
	})

	rogue.alwaysActive(core.Aura{
		Label:    "Deathstalker's Mark Trigger",
		ActionID: core.ActionID{SpellID: 900490},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() {
				return
			}
			switch spell {
			case rogue.Garrote:
				m := marks.Get(result.Target)
				m.Activate(sim)
				m.SetStacks(sim, deathstalkersMarkStacks)
			case rogue.Envenom:
				m := marks.Get(result.Target)
				if !m.IsActive() || rogue.cc.lastFinisherPoints < deathstalkersMarkMinPoints {
					return
				}
				m.RemoveStack(sim)
				mark.Cast(sim, result.Target)
				if hasFourPiece {
					reduceCooldown(sim, rogue.Vanish, deathstalkersMarkVanishCDR)
				}
			}
		},
	})
}

// Burrow Cutthroat's Kit (custom combat set 9321: neck, ring, cloak, weapon).
var ItemSetBurrowCutthroatsKit = core.NewItemSet(core.ItemSet{
	Name: "Burrow Cutthroat's Kit",
	Bonuses: map[int32]core.ApplyEffect{
		// Rupture periodic damage increases the damage of your next Eviscerate by 4% for 12 sec, stacking
		// up to 5 times.
		2: func(agent core.Agent) {
			rogue := agent.(RogueAgent).GetRogue()
			rogue.Env.RegisterPreFinalizeEffect(rogue.registerFlawlessForm)
		},
		// Flawless Form makes your next Eviscerate swing a flurry of attacks, dealing 50% of your attack
		// power as physical damage to the target and 30% to all other nearby enemies. While Blade Flurry is
		// active, damage to non-primary targets is increased by 100%. Handled in registerFlawlessForm.
		4: func(agent core.Agent) {
			agent.(RogueAgent).GetRogue().cc.flawlessFormFourPiece = true
		},
	},
})

func (rogue *Rogue) registerFlawlessForm() {
	hasFourPiece := rogue.cc.flawlessFormFourPiece

	// Spell 900502: +4% Eviscerate damage per stack (SPELLMOD_DAMAGE), consumed by the next Eviscerate.
	flawlessForm := rogue.RegisterAura(core.Aura{
		Label:     "Flawless Form",
		ActionID:  core.ActionID{SpellID: 900502},
		Duration:  flawlessFormDuration,
		MaxStacks: flawlessFormMaxStacks,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			rogue.Eviscerate.DamageMultiplierAdditive += flawlessFormPerStack * float64(newStacks-oldStacks)
		},
	})

	// Spells 900503 (primary target) and 900504 (other enemies within 8 yards): the 4pc flurry.
	var flurry *core.Spell
	if hasFourPiece {
		newFlurrySpell := func(spellID int32, applyEffects core.ApplySpellResults) *core.Spell {
			return rogue.RegisterSpell(core.SpellConfig{
				ActionID:    core.ActionID{SpellID: spellID},
				SpellSchool: core.SpellSchoolPhysical,
				ProcMask:    core.ProcMaskEmpty,
				Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

				DamageMultiplier: 1,
				CritMultiplier:   rogue.MeleeCritMultiplier(false),
				ThreatMultiplier: 1,

				ApplyEffects: applyEffects,
			})
		}
		secondary := newFlurrySpell(900504, func(sim *core.Simulation, primary *core.Unit, spell *core.Spell) {
			ap := flawlessFlurrySecondaryAP
			if rogue.BladeFlurryAura.IsActive() {
				ap *= flawlessFlurryBladeFlurryMod
			}
			for _, other := range sim.Encounter.TargetUnits {
				if other != primary {
					spell.CalcAndDealDamage(sim, other, ap*spell.MeleeAttackPower(), spell.OutcomeMeleeSpecialHitAndCrit)
				}
			}
		})
		flurry = newFlurrySpell(900503, func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.CalcAndDealDamage(sim, target, flawlessFlurryPrimaryAP*spell.MeleeAttackPower(), spell.OutcomeMeleeSpecialHitAndCrit)
			secondary.Cast(sim, target)
		})
	}

	rogue.alwaysActive(core.Aura{
		Label:    "Flawless Form Trigger",
		ActionID: core.ActionID{SpellID: 900500},
		OnPeriodicDamageDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell == rogue.Rupture {
				flawlessForm.Activate(sim)
				flawlessForm.AddStack(sim)
			}
		},
		// After Eviscerate has dealt its damage with the stacks.
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell != rogue.Eviscerate || !flawlessForm.IsActive() {
				return
			}
			flawlessForm.Deactivate(sim)
			if flurry != nil && result.Landed() {
				flurry.Cast(sim, result.Target)
			}
		},
	})
}

// Shadow of the Canopy (custom subtlety set 9322: neck, ring, cloak, weapon).
var ItemSetShadowOfTheCanopy = core.NewItemSet(core.ItemSet{
	Name: "Shadow of the Canopy",
	Bonuses: map[int32]core.ApplyEffect{
		// Hemorrhage deals a secondary strike with both weapons, each dealing Shadow damage.
		2: func(agent core.Agent) {
			rogue := agent.(RogueAgent).GetRogue()
			rogue.Env.RegisterPreFinalizeEffect(rogue.registerShadowStrikes)
		},
		// While in Shadow Dance, your abilities strike a second time, dealing Shadow damage. When Shadow
		// Dance ends, all nearby enemies take increased Shadow damage from you for 12 sec.
		4: func(agent core.Agent) {
			rogue := agent.(RogueAgent).GetRogue()
			rogue.Env.RegisterPreFinalizeEffect(rogue.registerDancingShadowStrikes)
		},
	},
})

// Shadow Strikes (spells 900512 and 900514) are melee-class Shadow spells whose damage the server script
// computes from attack power: 31.2% per Hemorrhage strike, 100% for the Shadow Dance strike. The 4pc
// debuff (spell 900515) is 15% for 12 sec; the DBC effect's 50% for 18 sec is stale.
func (rogue *Rogue) newShadowStrike(actionID core.ActionID, apCoeff float64) *core.Spell {
	return rogue.RegisterSpell(core.SpellConfig{
		ActionID:    actionID,
		SpellSchool: core.SpellSchoolShadow,
		// Triggered strikes don't proc weapon effects, like other server-triggered spells.
		ProcMask: core.ProcMaskEmpty,
		Flags:    core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   rogue.MeleeCritMultiplier(false),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			baseDamage := apCoeff * spell.MeleeAttackPower() * rogue.shadowStrikesDebuffMultiplier(target)
			spell.CalcAndDealDamage(sim, target, baseDamage, spell.OutcomeMeleeSpecialCritOnly)
		},
	})
}

func (rogue *Rogue) registerShadowStrikes() {
	if rogue.Hemorrhage == nil {
		return
	}

	// One strike per weapon.
	mhStrike := rogue.newShadowStrike(core.ActionID{SpellID: 900512, Tag: 1}, shadowStrikesHemorrhageAP)
	var ohStrike *core.Spell
	if rogue.AutoAttacks.IsDualWielding {
		ohStrike = rogue.newShadowStrike(core.ActionID{SpellID: 900512, Tag: 2}, shadowStrikesHemorrhageAP)
	}

	rogue.alwaysActive(core.Aura{
		Label:    "Shadow Strikes",
		ActionID: core.ActionID{SpellID: 900510},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell != rogue.Hemorrhage || !result.Landed() {
				return
			}
			mhStrike.Cast(sim, result.Target)
			if ohStrike != nil {
				ohStrike.Cast(sim, result.Target)
			}
		},
	})
}

func (rogue *Rogue) registerDancingShadowStrikes() {
	if rogue.ShadowDanceAura == nil {
		return
	}

	rogue.cc.shadowOfTheCanopyAuras = rogue.NewEnemyAuraArray(func(target *core.Unit) *core.Aura {
		return target.GetOrRegisterAura(core.Aura{
			Label:    "Shadow of the Canopy-" + rogue.Label,
			ActionID: core.ActionID{SpellID: 900515},
			Duration: shadowOfTheCanopyDuration,
		})
	})
	rogue.ShadowDanceAura.ApplyOnExpire(func(aura *core.Aura, sim *core.Simulation) {
		for _, target := range sim.Encounter.TargetUnits {
			rogue.cc.shadowOfTheCanopyAuras.Get(target).Activate(sim)
		}
	})

	echo := rogue.newShadowStrike(core.ActionID{SpellID: 900514}, shadowStrikesDanceAP)

	rogue.alwaysActive(core.Aura{
		Label:    "Shadow Strikes (Shadow Dance)",
		ActionID: core.ActionID{SpellID: 900511},
		// "Abilities" are the rogue's builders and finishers that deal direct damage.
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !rogue.ShadowDanceAura.IsActive() || !result.Landed() || result.Damage <= 0 ||
				!spell.Flags.Matches(SpellFlagBuilder|SpellFlagFinisher) {
				return
			}
			echo.Cast(sim, result.Target)
		},
	})
}

// shadowStrikesDebuffMultiplier applies the Shadow of the Canopy 4pc debuff. The rogue's only Shadow
// damage comes from the custom items in this file, so they apply it themselves.
func (rogue *Rogue) shadowStrikesDebuffMultiplier(target *core.Unit) float64 {
	if rogue.cc.shadowOfTheCanopyAuras != nil && rogue.cc.shadowOfTheCanopyAuras.Get(target).IsActive() {
		return 1 + shadowOfTheCanopyBonus
	}
	return 1
}
