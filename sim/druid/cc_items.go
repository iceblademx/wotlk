package druid

// Custom content from the 3.3.5a server (see assets/db_inputs/cc/REPORT.md). The server implements
// these with C++ scripts we don't have. The mechanics and tuning below follow what the server
// publishes at http://209.38.90.151:8087/sim/data.json (documented from its scripts), plus the spell
// data of the helper spells (inspect with ./build.ps1 inspect spell <id>).
//
// Not simulated: Sapwarden's Vestments (restoration set 9307), since the restoration sim has no heals.

import (
	"time"

	"github.com/wowsims/wotlk/sim/core"
)

const (
	// Morgrath's Ravaging Claw (ring, spell 200049): Ferocious Bite has a 20% chance per combo point
	// spent to trigger Omen of Clarity. Shred or Mangle casts that consume Omen of Clarity deal 100%
	// additional damage.
	MorgrathsRavagingClawItemID = 900115
	// Skareth's Falling Star (ring, spell 200021): Casting Starfall causes you to channel Fury of the
	// Goddess for 3 sec, unleashing a rapid volley of spells at your current target.
	SkarethsFallingStarItemID = 900108
)

// Tuning published by the server (data.json), checked against it by cc_tuning_test.go.
const (
	ravagingClawChancePerPoint = 0.2
	ravagingClawBuilderBonus   = 1.0 // +100% Shred/Mangle damage when consuming Clearcasting
	bloodseekerVinesAP         = 0.35
	bloodseekerThornsAP        = 0.30
	furyOfTheGoddessCasts      = 12 // 1 Moonfire, then Starfire or Wrath
	furyOfTheGoddessTick       = time.Millisecond * 250
	furyOfTheGoddessStarfire   = 0.5
	dreamBurstChance           = 0.15
	dreamBurstSP               = 1.80
	powerOfEluneChance         = 0.15
	powerOfEluneDuration       = time.Second * 12
	powerOfEluneICD            = time.Second * 45
	canopyDreamerDamageBonus   = 1.04
	bloodseekerVinesProcChance = 0.10
	bloodseekerVinesICD        = time.Second * 6 // per target
	empoweredMaulChance        = 0.20
	empoweredMaulPrimaryAP     = 0.50
	empoweredMaulCleaveAP      = 0.25
	ravageChance               = 0.20
	ravageMultiplier           = 2.5
	nextMaulBuffDuration       = time.Second * 30 // Empowered Maul and Ravage
)

// State for custom items.
type ccItems struct {
	canopyDreamerFourPiece bool
	ursocsClawFourPiece    bool
}

func init() {
	core.NewItemEffect(MorgrathsRavagingClawItemID, func(agent core.Agent) {
		// Needs Clearcasting even without the Omen of Clarity talent: see applyOmenOfClarity.
		// Effects are in onFerociousBiteLanded and clearcastBuilderMultiplier.
		agent.(DruidAgent).GetDruid().hasRavagingClaw = true
	})
	core.NewItemEffect(SkarethsFallingStarItemID, func(agent core.Agent) {
		druid := agent.(DruidAgent).GetDruid()
		// Needs Starfall, Moonfire, Starfire and Wrath, which are registered after item effects.
		druid.Env.RegisterPreFinalizeEffect(druid.registerFallingStar)
	})
}

func (druid *Druid) registerFallingStar() {
	if druid.Starfall == nil {
		return
	}

	// Spell 200056: a 3 sec channel (PERIODIC_DUMMY every 250 ms) that casts max-rank Moonfire, then
	// Starfire or Wrath at random, at the target, with their normal damage.
	fury := druid.RegisterSpell(Humanoid|Moonkin, core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200056},
		SpellSchool: core.SpellSchoolNature,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagChanneled | core.SpellFlagNoOnCastComplete,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Fury of the Goddess",
			},
			NumberOfTicks: furyOfTheGoddessCasts,
			TickLength:    furyOfTheGoddessTick,
			OnTick: func(sim *core.Simulation, target *core.Unit, dot *core.Dot) {
				switch {
				case dot.TickCount == 1:
					druid.Moonfire.SkipCastAndApplyEffects(sim, target)
				case sim.RandomFloat("Fury of the Goddess") < furyOfTheGoddessStarfire:
					druid.Starfire.SkipCastAndApplyEffects(sim, target)
				default:
					druid.Wrath.SkipCastAndApplyEffects(sim, target)
				}
			},
		},

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.Dot(target).Apply(sim)
		},
	})

	druid.RegisterAura(core.Aura{
		Label:    "Skareth's Falling Star",
		ActionID: core.ActionID{SpellID: 200021},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// The channel occupies the druid like any other: the rotation waits for it to finish.
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if druid.Starfall.IsEqual(spell) {
				fury.Cast(sim, druid.CurrentTarget)
			}
		},
	})
}

// Canopy Dreamer's Regalia (custom balance set 9304: neck, ring, cloak, weapon).
var ItemSetCanopyDreamersRegalia = core.NewItemSet(core.ItemSet{
	Name: "Canopy Dreamer's Regalia",
	Bonuses: map[int32]core.ApplyEffect{
		// Starfire and Wrath have a 15% chance to fire a Dream Burst at your target, dealing Nature damage.
		2: func(agent core.Agent) {
			druid := agent.(DruidAgent).GetDruid()
			druid.Env.RegisterPreFinalizeEffect(druid.registerDreamBurst)
		},
		// Starfire and Wrath deal 4% increased damage, and have a 15% chance to grant Power of Elune for 12
		// sec, guaranteeing that Starfire and Wrath fire a Dream Burst. Handled in registerDreamBurst.
		4: func(agent core.Agent) {
			agent.(DruidAgent).GetDruid().cc.canopyDreamerFourPiece = true
		},
	},
})

func (druid *Druid) registerDreamBurst() {
	if druid.Starfire == nil || druid.Wrath == nil {
		return
	}
	hasFourPiece := druid.cc.canopyDreamerFourPiece

	// Spell 900332: Nature, 180% of spell power (the script's value; spell_bonus_data is 0).
	dreamBurst := druid.RegisterSpell(Any, core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900332},
		SpellSchool: core.SpellSchoolNature,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   druid.DefaultSpellCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.CalcAndDealDamage(sim, target, dreamBurstSP*spell.SpellPower(), spell.OutcomeMagicHitAndCrit)
		},
	})

	// Spell 900334: 12 sec, 45 sec internal cooldown. The set spell's DBC modifier (8%) predates the
	// live 4%.
	var powerOfElune *core.Aura
	if hasFourPiece {
		druid.Starfire.DamageMultiplier *= canopyDreamerDamageBonus
		druid.Wrath.DamageMultiplier *= canopyDreamerDamageBonus
		powerOfElune = druid.RegisterAura(core.Aura{
			Label:    "Power of Elune",
			ActionID: core.ActionID{SpellID: 900334},
			Duration: powerOfEluneDuration,
			Icd: &core.Cooldown{
				Timer:    druid.NewTimer(),
				Duration: powerOfEluneICD,
			},
		})
	}

	druid.RegisterAura(core.Aura{
		Label:    "Canopy Dreamer's Regalia",
		ActionID: core.ActionID{SpellID: 900330},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() || !(druid.Starfire.IsEqual(spell) || druid.Wrath.IsEqual(spell)) {
				return
			}
			if powerOfElune.IsActive() || sim.Proc(dreamBurstChance, "Dream Burst") {
				dreamBurst.Cast(sim, result.Target)
			}
			if powerOfElune != nil && powerOfElune.Icd.IsReady(sim) && sim.Proc(powerOfEluneChance, "Power of Elune") {
				powerOfElune.Icd.Use(sim)
				powerOfElune.Activate(sim)
			}
		},
	})
}

// Ursoc's Tainted Claw (custom bear set 9306: neck, ring, cloak, weapon).
var ItemSetUrsocsTaintedClaw = core.NewItemSet(core.ItemSet{
	Name: "Ursoc's Tainted Claw",
	Bonuses: map[int32]core.ApplyEffect{
		// Your auto-attacks have a 20% chance to empower your next Maul, dealing 50% of your attack power as
		// physical damage to your target and 25% to all other enemies in front of you.
		2: func(agent core.Agent) {
			druid := agent.(DruidAgent).GetDruid()
			druid.Env.RegisterPreFinalizeEffect(druid.registerEmpoweredMaul)
		},
		// During Berserk, Mangle, Lacerate and Swipe have a 20% chance to make your next Maul become Ravage,
		// dealing 250% of an empowered Maul. Handled in registerEmpoweredMaul.
		4: func(agent core.Agent) {
			agent.(DruidAgent).GetDruid().cc.ursocsClawFourPiece = true
		},
	},
})

func (druid *Druid) registerEmpoweredMaul() {
	if druid.Maul == nil {
		return
	}
	hasFourPiece := druid.cc.ursocsClawFourPiece

	newStrike := func(spellID int32) *DruidSpell {
		return druid.RegisterSpell(Bear, core.SpellConfig{
			ActionID:    core.ActionID{SpellID: spellID},
			SpellSchool: core.SpellSchoolPhysical,
			ProcMask:    core.ProcMaskEmpty,
			Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

			DamageMultiplier: 1,
			CritMultiplier:   druid.MeleeCritMultiplier(Bear),
			ThreatMultiplier: 1,
		})
	}
	// Spells 900353 (the target) and 900354 (other enemies in front).
	primary, cleave := newStrike(900353), newStrike(900354)

	// Spell 900352: the next Maul. Its DBC also makes that Maul free (SPELLMOD_COST -100%).
	var maulCost float64
	empowered := druid.RegisterAura(core.Aura{
		Label:    "Empowered Maul",
		ActionID: core.ActionID{SpellID: 900352},
		Duration: nextMaulBuffDuration,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			maulCost = druid.Maul.DefaultCast.Cost
			druid.Maul.DefaultCast.Cost = 0
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			druid.Maul.DefaultCast.Cost = maulCost
		},
	})
	// Spell 900355: the next Maul becomes Ravage.
	var ravage *core.Aura
	if hasFourPiece {
		ravage = druid.RegisterAura(core.Aura{
			Label:    "Ravage",
			ActionID: core.ActionID{SpellID: 900355},
			Duration: nextMaulBuffDuration,
		})
	}

	druid.RegisterAura(core.Aura{
		Label:    "Ursoc's Tainted Claw",
		ActionID: core.ActionID{SpellID: 900350},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			switch {
			case druid.Maul.IsEqual(spell):
				// A Maul uses Ravage if both are up; the empowerment then waits for the next one.
				multiplier := 0.0
				if ravage.IsActive() {
					multiplier = ravageMultiplier
					ravage.Deactivate(sim)
				} else if empowered.IsActive() {
					multiplier = 1
					empowered.Deactivate(sim)
				}
				if multiplier == 0 || !result.Landed() {
					return
				}
				ap := primary.MeleeAttackPower()
				primary.CalcAndDealDamage(sim, result.Target, multiplier*empoweredMaulPrimaryAP*ap, primary.OutcomeMeleeSpecialCritOnly)
				for _, other := range sim.Encounter.TargetUnits {
					if other != result.Target {
						cleave.CalcAndDealDamage(sim, other, multiplier*empoweredMaulCleaveAP*ap, cleave.OutcomeMeleeSpecialHitAndCrit)
					}
				}
			case spell.ProcMask.Matches(core.ProcMaskMeleeWhiteHit):
				if result.Landed() && sim.Proc(empoweredMaulChance, "Empowered Maul") {
					empowered.Activate(sim)
				}
			case ravage != nil && druid.BerserkAura.IsActive() &&
				(druid.MangleBear.IsEqual(spell) || druid.Lacerate.IsEqual(spell) || druid.SwipeBear.IsEqual(spell)):
				if result.Landed() && sim.Proc(ravageChance, "Ravage") {
					ravage.Activate(sim)
				}
			}
		},
	})
}

// Prowler of the Fevered Canopy (custom feral set 9305: neck, ring, cloak, weapon).
var ItemSetProwlerOfTheFeveredCanopy = core.NewItemSet(core.ItemSet{
	Name: "Prowler of the Fevered Canopy",
	Bonuses: map[int32]core.ApplyEffect{
		// Rip and Rake damage has a 10% chance to cause Bloodseeker Vines to grow on the victim,
		// dealing 35% of your attack power as Bleed damage over 6 sec.
		2: func(agent core.Agent) {
			agent.(DruidAgent).GetDruid().registerBloodseekerVines()
		},
		// When Bloodseeker Vines expire, or you use Ferocious Bite on their target, they explode in
		// thorns, dealing 30% of your attack power as physical damage to the victim and nearby enemies.
		4: func(agent core.Agent) {
			agent.(DruidAgent).GetDruid().registerBloodseekerThorns()
		},
	},
})

func (druid *Druid) registerBloodseekerThorns() {
	// Spell 900343: physical, melee damage class, hits the target and enemies within 8 yards.
	druid.BloodseekerThorns = druid.RegisterSpell(Any, core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900343},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   druid.MeleeCritMultiplier(Cat),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, _ *core.Unit, spell *core.Spell) {
			baseDamage := bloodseekerThornsAP * spell.MeleeAttackPower()
			for _, aoeTarget := range sim.Encounter.TargetUnits {
				spell.CalcAndDealDamage(sim, aoeTarget, baseDamage, spell.OutcomeMeleeSpecialHitAndCrit)
			}
		},
	})
}

func (druid *Druid) registerBloodseekerVines() {
	// Spell 900342: physical periodic damage (a bleed), 3 ticks over 6 sec, cannot crit.
	druid.BloodseekerVines = druid.RegisterSpell(Any, core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900342},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Bloodseeker Vines",
			},
			NumberOfTicks: 3,
			TickLength:    time.Second * 2,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				dot.SnapshotBaseDamage = bloodseekerVinesAP * dot.Spell.MeleeAttackPower() / float64(dot.NumberOfTicks)
				dot.SnapshotAttackerMultiplier = dot.Spell.AttackerDamageMultiplier(dot.Spell.Unit.AttackTables[target.UnitIndex])
			},
			OnTick: func(sim *core.Simulation, target *core.Unit, dot *core.Dot) {
				dot.CalcAndDealPeriodicSnapshotDamage(sim, target, dot.OutcomeTick)
				// Natural expiry explodes the vines (4pc). Refreshing restarts the ticks, and Ferocious Bite
				// consumes the vines before this point, so neither causes a second explosion.
				if druid.BloodseekerThorns != nil && dot.TickCount >= dot.NumberOfTicks {
					druid.BloodseekerThorns.Cast(sim, target)
				}
			},
		},

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.SpellMetrics[target.UnitIndex].Hits++
			spell.Dot(target).Apply(sim)
		},
	})

	// Each target has its own 6 sec internal cooldown.
	icds := make([]core.Cooldown, len(druid.Env.AllUnits))
	for _, target := range druid.Env.Encounter.TargetUnits {
		icds[target.UnitIndex] = core.Cooldown{Timer: druid.NewTimer(), Duration: bloodseekerVinesICD}
	}
	druid.RegisterAura(core.Aura{
		Label:    "Bloodseeker Vines Trigger",
		ActionID: core.ActionID{SpellID: 900340},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// Rake and Rip ticks (not Rake's initial hit).
		OnPeriodicDamageDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if result.Damage <= 0 || !(druid.Rake.IsEqual(spell) || druid.Rip.IsEqual(spell)) {
				return
			}
			icd := &icds[result.Target.UnitIndex]
			if !icd.IsReady(sim) || sim.RandomFloat("Bloodseeker Vines") >= bloodseekerVinesProcChance {
				return
			}
			icd.Use(sim)
			druid.BloodseekerVines.Cast(sim, result.Target)
		},
	})
}

// onFerociousBiteLanded runs after a Ferocious Bite lands, before its combo points are spent.
func (druid *Druid) onFerociousBiteLanded(sim *core.Simulation, target *core.Unit, comboPoints int32) {
	if druid.hasRavagingClaw && druid.ClearcastingAura != nil &&
		sim.RandomFloat("Morgrath's Ravaging Claw") < ravagingClawChancePerPoint*float64(comboPoints) {
		if druid.ProcOoc != nil {
			druid.ProcOoc(sim)
		} else {
			druid.ClearcastingAura.Activate(sim)
		}
	}

	if druid.BloodseekerThorns != nil {
		if vines := druid.BloodseekerVines.Dot(target); vines.IsActive() {
			druid.BloodseekerThorns.Cast(sim, target)
			vines.Deactivate(sim)
		}
	}
}

// clearcastBuilderMultiplier is the damage multiplier for a Shred or Mangle cast.
func (druid *Druid) clearcastBuilderMultiplier() float64 {
	// Clearcasting is consumed in OnCastComplete, after the spell's effects, so it is still active here.
	if druid.hasRavagingClaw && druid.ClearcastingAura != nil && druid.ClearcastingAura.IsActive() {
		return 1 + ravagingClawBuilderBonus
	}
	return 1
}
