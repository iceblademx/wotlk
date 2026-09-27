package warlock

// Custom content from the 3.3.5a server (see assets/db_inputs/cc/REPORT.md). The server implements
// these with C++ scripts we don't have, so behaviour follows the item/spell tooltips plus the spell
// data of their helper spells (inspect with ./build.ps1 inspect spell <id>).

import (
	"math"
	"time"

	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/stats"
)

const (
	// Vaelith's Withering Grasp (ring, spell 200016): Shadow Bolt increases the damage of your Drain Soul
	// by 50%, stacking up to 4 times, consumed by your next Drain Soul cast. Drain Soul channels twice as
	// fast: half the duration, same number of ticks.
	VaelithsWitheringGraspItemID = 900106
	// Vaelith's Reaped Soul (ring, spell 200076): Corruption, Curse of Agony, and Unstable Affliction grant
	// a stack of Reaped Soul each time they deal damage. At 25 stacks, consume all stacks to Reap your
	// primary target's soul, dealing Shadow damage every 1 sec for 4 sec.
	VaelithsReapedSoulItemID = 900125
)

const (
	// Vaelith's Abyssal Calling (ring, spell 200085): Casting Demonic Empowerment calls forth a Pit Lord
	// from the Twisting Nether that attacks your target with Fire damage, and increases the spell power
	// of allies within 30 yards for 20 sec.
	VaelithsAbyssalCallingItemID = 900127
	// Morgrath's Malediction (ring, spell 200088): Casting Immolate also applies Malediction to your
	// target, dealing Fire damage over 24 sec. If the target is afflicted by Malediction, casting Chaos
	// Bolt and Shadowburn increases its stack count by 1. Each time Malediction gains a stack it has a
	// chance to collapse, consuming a stack every 1 sec to deal Shadowflame damage until 1 stack remains.
	MorgrathsMaledictionItemID = 900128
	// Vorrax's Chaosflame (ring, spell 200092): Casting Incinerate grants a stack of Chaosflame,
	// increasing the damage of your next Chaos Bolt by 50% and guaranteeing it will Critically Strike,
	// stacking up to 5 times. Casting Chaos Bolt consumes all stacks of Chaosflame.
	VorraxsChaosflameItemID = 900129
)

// Tuning published by the server at http://209.38.90.151:8087/sim/data.json and checked against it by
// cc_tuning_test.go. The mechanics there are documented from the server scripts; values marked live
// come from crimson_tier_config.
const (
	witheringGraspPerStack      = 0.5
	witheringGraspMaxStacks     = 4
	witheringGraspDuration      = time.Second * 18
	witheringGraspChannelSpeed  = 2  // half the duration, same number of ticks
	reapedSoulStacks            = 25 // live: leg_reaped stacks
	reapSoulTickSP              = 6.0
	reapSoulTicks               = 4
	reapSoulTickLength          = time.Second
	soulAnathemaChance          = 1.0 // per Haunt hit
	soulAnathemaSP              = 6.8
	soulAnathemaFourPieceBonus  = 0.25
	abyssalCallingDuration      = time.Second * 20
	abyssalCallingStrikeEvery   = time.Millisecond * 2500
	abyssalCallingStrikeSP      = 1.512            // live: leg_abyss blast_sp_pct
	abyssalCallingAuraSP        = 0.14             // of the warlock's bonus spell power, snapshot at cast
	abyssalCallingDECooldown    = time.Second * 60 // live: leg_abyss de_cooldown_ms
	maledictionTotalSP          = 3.0
	maledictionTicks            = 8
	maledictionTickLength       = time.Second * 3
	maledictionIgniteInterval   = time.Second
	maledictionIgniteChance     = 0.25
	maledictionIgniteSP         = 0.6
	chaosflameBonusPerStack     = 0.5
	chaosflameMaxStacks         = 5
	chaosflameDuration          = time.Second * 20
	wildImpsPerSummon           = 3
	wildImpDuration             = time.Second * 12
	felFireballCastTime         = time.Second * 2
	felFireballSP               = 0.20
	empoweredImpBonus           = 1.0
	empoweredImpSplashSP        = 0.125
	crashingChaosChaosBoltSP    = 0.40
	crashingChaosIncinerateTime = time.Second * 12
	crashingChaosInfernalChance = 0.22
	crashingChaosInfernalTime   = time.Second * 8
	crashingChaosBonus          = 0.35
)

// State for custom items.
type ccItems struct {
	soulAnathemaFourPiece bool

	wildImpsFourPiece      bool
	crashingChaosFourPiece bool
	// Added to Chaos Bolt's spell power coefficient (Immolation of the World Tree 2pc).
	chaosBoltBonusCoeff float64
}

func init() {
	core.NewItemEffect(VaelithsWitheringGraspItemID, func(agent core.Agent) {
		warlock := agent.(WarlockAgent).GetWarlock()
		// Needs Shadow Bolt and Drain Soul, which are registered after item effects.
		warlock.Env.RegisterPreFinalizeEffect(warlock.registerWitheringGrasp)
	})
	core.NewItemEffect(VaelithsReapedSoulItemID, func(agent core.Agent) {
		warlock := agent.(WarlockAgent).GetWarlock()
		warlock.Env.RegisterPreFinalizeEffect(warlock.registerReapedSoul)
	})
	core.NewItemEffect(VaelithsAbyssalCallingItemID, func(agent core.Agent) {
		warlock := agent.(WarlockAgent).GetWarlock()
		warlock.Env.RegisterPreFinalizeEffect(warlock.registerAbyssalCalling)
	})
	core.NewItemEffect(MorgrathsMaledictionItemID, func(agent core.Agent) {
		warlock := agent.(WarlockAgent).GetWarlock()
		warlock.Env.RegisterPreFinalizeEffect(warlock.registerMalediction)
	})
	core.NewItemEffect(VorraxsChaosflameItemID, func(agent core.Agent) {
		warlock := agent.(WarlockAgent).GetWarlock()
		warlock.Env.RegisterPreFinalizeEffect(warlock.registerChaosflame)
	})
}

// excludeFromFamilyTalents undoes the class modifiers the sim applies to all of the warlock's damage
// of a school but the server restricts by spell family. Custom spells have no family flags, so on the
// server they never get them (./build.ps1 inspect family <id> lists what can apply to a spell):
//   - Death's Embrace (OVERRIDE_CLASS_SCRIPTS on masked Shadow spells), which the sim applies to all
//     Shadow damage below 35% health.
//
// Haunt and Shadow Embrace are opt-in (SpellFlagHauntSE), so custom spells simply don't set the flag.
func (warlock *Warlock) excludeFromFamilyTalents(spells ...*core.Spell) {
	if warlock.Talents.DeathsEmbrace == 0 {
		return
	}
	deathsEmbrace := 1.0 + 0.04*float64(warlock.Talents.DeathsEmbrace) // as applyDeathsEmbrace
	warlock.RegisterResetEffect(func(sim *core.Simulation) {
		sim.RegisterExecutePhaseCallback(func(sim *core.Simulation, isExecute int32) {
			if isExecute != 35 {
				return
			}
			for _, spell := range spells {
				if spell.SpellSchool.Matches(core.SpellSchoolShadow) {
					spell.DamageMultiplier /= deathsEmbrace
				}
			}
		})
	})
}

// registerMinionSpell registers the spell of a script-summoned minion (Pit Lord, Wild Imp). The server
// passes the damage as a percent of the warlock's spell power; the minion casts it, so the warlock's
// damage-done modifiers don't apply. Hit and crit use the warlock's, like its other pets.
func (warlock *Warlock) registerMinionSpell(actionID core.ActionID, school core.SpellSchool, applyEffects core.ApplySpellResults) *core.Spell {
	return warlock.RegisterSpell(core.SpellConfig{
		ActionID:    actionID,
		SpellSchool: school,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagIgnoreAttackerModifiers | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   warlock.DefaultSpellCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: applyEffects,
	})
}

func (warlock *Warlock) registerAbyssalCalling() {
	if warlock.DemonicEmpowerment == nil {
		return
	}
	// The server gives Demonic Empowerment a fixed cooldown while the ring is worn (Nemesis included).
	warlock.DemonicEmpowerment.CD.Duration = abyssalCallingDECooldown

	// Spell 200086: Fire. spell_bonus_data says 168%, the live config 151.2%.
	strike := warlock.registerMinionSpell(core.ActionID{SpellID: 200086}, core.SpellSchoolFire,
		func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.CalcAndDealDamage(sim, target, abyssalCallingStrikeSP*spell.SpellPower(), spell.OutcomeMagicHitAndCrit)
		})

	// Spell 200087: spell power for the warlock and allies in range; only the warlock is simulated.
	var bonus float64
	empowered := warlock.RegisterAura(core.Aura{
		Label:    "Empowered by the Abyss",
		ActionID: core.ActionID{SpellID: 200087},
		Duration: abyssalCallingDuration,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			bonus = abyssalCallingAuraSP * warlock.GetStat(stats.SpellPower)
			warlock.AddStatDynamic(sim, stats.SpellPower, bonus)
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			warlock.AddStatDynamic(sim, stats.SpellPower, -bonus)
		},
	})

	// Shows the Pit Lord on the timeline; its strikes are a periodic action of their own.
	pitLordAura := warlock.RegisterAura(core.Aura{
		Label:    "Pit Lord",
		ActionID: core.ActionID{SpellID: 200085},
		Duration: abyssalCallingDuration,
	})

	var pitLord *core.PendingAction
	warlock.RegisterAura(core.Aura{
		Label:    "Vaelith's Abyssal Calling",
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			pitLord = nil
			aura.Activate(sim)
		},
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell != warlock.DemonicEmpowerment {
				return
			}
			// A new Pit Lord and a new spell power snapshot replace the old ones.
			if pitLord != nil {
				pitLord.Cancel(sim)
			}
			pitLord = core.StartPeriodicAction(sim, core.PeriodicActionOptions{
				Period:   abyssalCallingStrikeEvery,
				NumTicks: int(abyssalCallingDuration / abyssalCallingStrikeEvery),
				OnAction: func(sim *core.Simulation) {
					strike.Cast(sim, warlock.CurrentTarget)
				},
			})
			pitLordAura.Activate(sim)
			empowered.Deactivate(sim)
			empowered.Activate(sim)
		},
	})
}

func (warlock *Warlock) registerMalediction() {
	// Spell 200091: the collapse, a flat 60% of spell power per stack consumed. Shadow per its DBC entry.
	ignite := warlock.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200091},
		SpellSchool: core.SpellSchoolShadow,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   warlock.DefaultSpellCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.CalcAndDealDamage(sim, target, maledictionIgniteSP*spell.SpellPower(), spell.OutcomeMagicHitAndCrit)
		},
	})
	warlock.excludeFromFamilyTalents(ignite)

	igniting := make([]*core.PendingAction, len(warlock.Env.AllUnits))
	stopIgniting := func(sim *core.Simulation, target *core.Unit) {
		if pa := igniting[target.UnitIndex]; pa != nil {
			pa.Cancel(sim)
			igniting[target.UnitIndex] = nil
		}
	}

	// Spell 200089: Fire periodic damage every 3 sec for 24 sec; the stack count only drives the collapse.
	malediction := warlock.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200089},
		SpellSchool: core.SpellSchoolFire,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label:     "Malediction",
				MaxStacks: math.MaxInt32,
				OnExpire: func(aura *core.Aura, sim *core.Simulation) {
					stopIgniting(sim, aura.Unit)
				},
			},
			NumberOfTicks: maledictionTicks,
			TickLength:    maledictionTickLength,

			// Snapshotted at the Immolate; Chaos Bolt and Shadowburn refreshes keep it.
			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, isRollover bool) {
				if isRollover {
					return
				}
				dot.SnapshotBaseDamage = maledictionTotalSP * dot.Spell.SpellPower() / maledictionTicks
				dot.SnapshotAttackerMultiplier = dot.Spell.AttackerDamageMultiplier(dot.Spell.Unit.AttackTables[target.UnitIndex])
			},
			OnTick: func(sim *core.Simulation, target *core.Unit, dot *core.Dot) {
				dot.CalcAndDealPeriodicSnapshotDamage(sim, target, dot.OutcomeTick)
			},
		},

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.SpellMetrics[target.UnitIndex].Hits++
			dot := spell.Dot(target)
			dot.Apply(sim)
			dot.SetStacks(sim, 1)
		},
	})

	// Igniting: every 1 sec, consume a stack for a Shadowflame hit until 1 stack remains.
	startIgniting := func(sim *core.Simulation, target *core.Unit) {
		dot := malediction.Dot(target)
		igniting[target.UnitIndex] = core.StartPeriodicAction(sim, core.PeriodicActionOptions{
			Period: maledictionIgniteInterval,
			OnAction: func(sim *core.Simulation) {
				dot.RemoveStack(sim)
				ignite.Cast(sim, target)
				if dot.GetStacks() <= 1 {
					stopIgniting(sim, target)
				}
			},
		})
	}

	warlock.RegisterAura(core.Aura{
		Label:    "Morgrath's Malediction",
		ActionID: core.ActionID{SpellID: 200088},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() {
				return
			}
			switch {
			case spell == warlock.Immolate:
				// Immolate (re)starts it at 1 stack with a new snapshot.
				malediction.Cast(sim, result.Target)
			case spell == warlock.ChaosBolt || spell == warlock.Shadowburn:
				dot := malediction.Dot(result.Target)
				if !dot.IsActive() {
					return
				}
				dot.ApplyOrReset(sim)
				// While igniting, Chaos Bolt and Shadowburn only refresh it.
				if igniting[result.Target.UnitIndex] == nil {
					dot.AddStack(sim)
					if sim.Proc(maledictionIgniteChance, "Malediction: Ignite") {
						startIgniting(sim, result.Target)
					}
				}
			}
		},
	})
}

func (warlock *Warlock) registerChaosflame() {
	if warlock.ChaosBolt == nil {
		return
	}

	// Spell 200093: +50% Chaos Bolt damage per stack (SPELLMOD_DAMAGE) and +100% crit chance.
	guaranteedCrit := 100 * core.CritRatingPerCritChance
	chaosflame := warlock.RegisterAura(core.Aura{
		Label:     "Chaosflame",
		ActionID:  core.ActionID{SpellID: 200093},
		Duration:  chaosflameDuration,
		MaxStacks: chaosflameMaxStacks,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			warlock.ChaosBolt.BonusCritRating += guaranteedCrit
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			warlock.ChaosBolt.BonusCritRating -= guaranteedCrit
		},
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			warlock.ChaosBolt.DamageMultiplierAdditive += chaosflameBonusPerStack * float64(newStacks-oldStacks)
		},
	})

	warlock.RegisterAura(core.Aura{
		Label:    "Vorrax's Chaosflame",
		ActionID: core.ActionID{SpellID: 200092},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// OnCastComplete runs after Chaos Bolt has dealt its damage with the stacks.
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			switch spell {
			case warlock.Incinerate:
				chaosflame.Activate(sim)
				chaosflame.AddStack(sim)
			case warlock.ChaosBolt:
				chaosflame.Deactivate(sim)
			}
		},
	})
}

// Bindings of the Faceless Pact (custom demonology set 9327: neck, ring, cloak, weapon).
var ItemSetBindingsOfTheFacelessPact = core.NewItemSet(core.ItemSet{
	Name: "Bindings of the Faceless Pact",
	Bonuses: map[int32]core.ApplyEffect{
		// Consuming a Molten Core charge summons 3 Wild Imps for 12 sec, each casting Fel Fireball at
		// your primary target for 20% of your spell power as Fire damage.
		2: func(agent core.Agent) {
			warlock := agent.(WarlockAgent).GetWarlock()
			warlock.Env.RegisterPreFinalizeEffect(warlock.registerWildImps)
		},
		// For each 3 Wild Imps summoned, 1 becomes Empowered and casts Empowered Fel Fireball instead,
		// dealing 100% increased damage and also dealing 12.5% of your spell power as Fire damage to all
		// enemies within 10 yards of the impact. Handled in registerWildImps.
		4: func(agent core.Agent) {
			agent.(WarlockAgent).GetWarlock().cc.wildImpsFourPiece = true
		},
	},
})

func (warlock *Warlock) registerWildImps() {
	if warlock.MoltenCoreAura == nil {
		return
	}
	hasFourPiece := warlock.cc.wildImpsFourPiece

	// Spell 900562.
	felFireball := warlock.registerMinionSpell(core.ActionID{SpellID: 900562}, core.SpellSchoolFire,
		func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.CalcAndDealDamage(sim, target, felFireballSP*spell.SpellPower(), spell.OutcomeMagicHitAndCrit)
		})
	// Spells 900564 (the empowered bolt) and 900563 (its splash, on everything within 10 yards).
	var empoweredFireball, empoweredSplash *core.Spell
	if hasFourPiece {
		empoweredSplash = warlock.registerMinionSpell(core.ActionID{SpellID: 900563}, core.SpellSchoolFire,
			func(sim *core.Simulation, _ *core.Unit, spell *core.Spell) {
				for _, aoeTarget := range sim.Encounter.TargetUnits {
					spell.CalcAndDealDamage(sim, aoeTarget, empoweredImpSplashSP*spell.SpellPower(), spell.OutcomeMagicHitAndCrit)
				}
			})
		empoweredFireball = warlock.registerMinionSpell(core.ActionID{SpellID: 900564}, core.SpellSchoolFire,
			func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
				damage := (1 + empoweredImpBonus) * felFireballSP * spell.SpellPower()
				spell.CalcAndDealDamage(sim, target, damage, spell.OutcomeMagicHitAndCrit)
				empoweredSplash.Cast(sim, target)
			})
	}

	impsSummoned := 0
	summonImp := func(sim *core.Simulation) {
		impsSummoned++
		bolt := felFireball
		if hasFourPiece && impsSummoned%3 == 0 {
			bolt = empoweredFireball
		}
		// Each imp chain-casts its 2 sec (unhasted) Fel Fireball at the primary target until it expires.
		core.StartPeriodicAction(sim, core.PeriodicActionOptions{
			Period:   felFireballCastTime,
			NumTicks: int(wildImpDuration / felFireballCastTime),
			OnAction: func(sim *core.Simulation) {
				bolt.Cast(sim, warlock.CurrentTarget)
			},
		})
	}

	// Shows the imps on the timeline.
	wildImps := warlock.RegisterAura(core.Aura{
		Label:    "Wild Imps",
		ActionID: core.ActionID{SpellID: 900560},
		Duration: wildImpDuration,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			impsSummoned = 0
		},
	})

	// A charge is consumed when Incinerate or Soul Fire removes a stack while the aura is up; expiry
	// clears the stacks after deactivating it.
	onStacksChange := warlock.MoltenCoreAura.OnStacksChange
	warlock.MoltenCoreAura.OnStacksChange = func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
		if onStacksChange != nil {
			onStacksChange(aura, sim, oldStacks, newStacks)
		}
		if !aura.IsActive() || newStacks != oldStacks-1 {
			return
		}
		for i := 0; i < wildImpsPerSummon; i++ {
			summonImp(sim)
		}
		wildImps.Activate(sim)
	}
}

// Immolation of the World Tree (custom destruction set 9328: neck, ring, cloak, weapon).
var ItemSetImmolationOfTheWorldTree = core.NewItemSet(core.ItemSet{
	Name: "Immolation of the World Tree",
	Bonuses: map[int32]core.ApplyEffect{
		// Chaos Bolt deals an additional 40% of your spell power as damage, and makes your next
		// Incinerate within 12 sec an instant cast.
		2: func(agent core.Agent) {
			warlock := agent.(WarlockAgent).GetWarlock()
			warlock.cc.chaosBoltBonusCoeff = crashingChaosChaosBoltSP
			warlock.Env.RegisterPreFinalizeEffect(warlock.registerCrashingChaos)
		},
		// Chaos Bolt has a 22% chance to summon an Infernal at your target for 8 sec, granting Crashing
		// Chaos while it is active. Crashing Chaos increases Chaos Bolt and Incinerate damage by 35%.
		// Handled in registerCrashingChaos.
		4: func(agent core.Agent) {
			agent.(WarlockAgent).GetWarlock().cc.crashingChaosFourPiece = true
		},
	},
})

func (warlock *Warlock) registerCrashingChaos() {
	if warlock.ChaosBolt == nil {
		return
	}

	// Spell 900572: -100% Incinerate cast time for 12 sec, consumed by the next Incinerate.
	var castTime time.Duration
	instantIncinerate := warlock.RegisterAura(core.Aura{
		Label:    "Crashing Chaos (Incinerate)",
		ActionID: core.ActionID{SpellID: 900572},
		Duration: crashingChaosIncinerateTime,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			castTime = warlock.Incinerate.DefaultCast.CastTime
			warlock.Incinerate.DefaultCast.CastTime = 0
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			warlock.Incinerate.DefaultCast.CastTime = castTime
		},
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == warlock.Incinerate {
				aura.Deactivate(sim)
			}
		},
	})

	// Spell 900573: +35% Chaos Bolt and Incinerate damage while the Infernal is up. The Infernal's own
	// attacks aren't published and aren't simulated.
	var crashingChaos *core.Aura
	if warlock.cc.crashingChaosFourPiece {
		crashingChaos = warlock.RegisterAura(core.Aura{
			Label:    "Crashing Chaos",
			ActionID: core.ActionID{SpellID: 900573},
			Duration: crashingChaosInfernalTime,
			OnGain: func(aura *core.Aura, sim *core.Simulation) {
				warlock.ChaosBolt.DamageMultiplierAdditive += crashingChaosBonus
				warlock.Incinerate.DamageMultiplierAdditive += crashingChaosBonus
			},
			OnExpire: func(aura *core.Aura, sim *core.Simulation) {
				warlock.ChaosBolt.DamageMultiplierAdditive -= crashingChaosBonus
				warlock.Incinerate.DamageMultiplierAdditive -= crashingChaosBonus
			},
		})
	}

	warlock.RegisterAura(core.Aura{
		Label:    "Immolation of the World Tree",
		ActionID: core.ActionID{SpellID: 900570},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell != warlock.ChaosBolt {
				return
			}
			instantIncinerate.Activate(sim)
			if crashingChaos != nil && sim.Proc(crashingChaosInfernalChance, "Crashing Chaos") {
				crashingChaos.Activate(sim)
			}
		},
	})
}

func (warlock *Warlock) registerWitheringGrasp() {
	// Spell 200016: -7500 ms Drain Soul duration (SPELLMOD_DURATION) and -50% tick period
	// (SPELLMOD_ACTIVATION_TIME), so still 5 ticks.
	for _, target := range warlock.Env.Encounter.TargetUnits {
		warlock.DrainSoul.Dot(target).TickLength /= witheringGraspChannelSpeed
	}

	// Spell 200061: +50% Drain Soul damage per stack, 4 stacks, 18 sec.
	shadowedMark := warlock.RegisterAura(core.Aura{
		Label:     "Withering Grasp: Shadowed Mark",
		ActionID:  core.ActionID{SpellID: 200061},
		Duration:  witheringGraspDuration,
		MaxStacks: witheringGraspMaxStacks,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			warlock.DrainSoul.DamageMultiplierAdditive += witheringGraspPerStack * float64(newStacks-oldStacks)
		},
	})

	warlock.RegisterAura(core.Aura{
		Label:    "Vaelith's Withering Grasp",
		ActionID: core.ActionID{SpellID: 200016},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// OnCastComplete runs after the spell's effects: Drain Soul has already snapshotted the bonus
		// for its whole channel when the stacks are consumed.
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			switch spell {
			case warlock.ShadowBolt:
				shadowedMark.Activate(sim)
				shadowedMark.AddStack(sim)
			case warlock.DrainSoul:
				shadowedMark.Deactivate(sim)
			}
		},
	})
}

func (warlock *Warlock) registerReapedSoul() {
	// Spell 200078: Shadow periodic damage, 4 ticks 1 sec apart. spell_bonus_data gives 600% of spell
	// power per tick ("Reap Soul, 600% of Spell Power per tick"). Assumed not to crit, like other DoTs.
	// No family flags, so Haunt and Shadow Embrace don't apply (see excludeFromFamilyTalents).
	reapSoul := warlock.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200078},
		SpellSchool: core.SpellSchoolShadow,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Reap Soul",
			},
			NumberOfTicks: reapSoulTicks,
			TickLength:    reapSoulTickLength,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				dot.SnapshotBaseDamage = reapSoulTickSP * dot.Spell.SpellPower()
				dot.SnapshotAttackerMultiplier = dot.Spell.AttackerDamageMultiplier(dot.Spell.Unit.AttackTables[target.UnitIndex])
			},
			OnTick: func(sim *core.Simulation, target *core.Unit, dot *core.Dot) {
				dot.CalcAndDealPeriodicSnapshotDamage(sim, target, dot.OutcomeTick)
			},
		},

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.Dot(target).Apply(sim)
		},
	})
	warlock.excludeFromFamilyTalents(reapSoul)

	// Spell 200077: the stack counter.
	reapedSoul := warlock.RegisterAura(core.Aura{
		Label:     "Reaped Soul",
		ActionID:  core.ActionID{SpellID: 200077},
		Duration:  core.NeverExpires,
		MaxStacks: reapedSoulStacks,
	})

	warlock.RegisterAura(core.Aura{
		Label:    "Vaelith's Reaped Soul",
		ActionID: core.ActionID{SpellID: 200076},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnPeriodicDamageDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if result.Damage <= 0 || !(spell == warlock.Corruption || spell == warlock.CurseOfAgony || spell == warlock.UnstableAffliction) {
				return
			}
			reapedSoul.Activate(sim)
			reapedSoul.AddStack(sim)
			if reapedSoul.GetStacks() >= reapedSoulStacks {
				reapedSoul.Deactivate(sim)
				reapSoul.Cast(sim, warlock.CurrentTarget)
			}
		},
	})
}

// Rot of the Fevered Dream (custom affliction set 9326: neck, ring, cloak, weapon).
var ItemSetRotOfTheFeveredDream = core.NewItemSet(core.ItemSet{
	Name: "Rot of the Fevered Dream",
	Bonuses: map[int32]core.ApplyEffect{
		// Haunt unleashes a demonic entity unto the soul of your target, dealing 680% of your spell power
		// as Shadow damage over 10 sec. If reapplied, any remaining damage is added to the new Soul Anathema.
		2: func(agent core.Agent) {
			warlock := agent.(WarlockAgent).GetWarlock()
			warlock.Env.RegisterPreFinalizeEffect(warlock.registerSoulAnathema)
		},
		// Soul Anathema damage increased by 25%. Consuming Nightfall is guaranteed to apply Soul Anathema.
		// Both parts are handled in registerSoulAnathema.
		4: func(agent core.Agent) {
			agent.(WarlockAgent).GetWarlock().cc.soulAnathemaFourPiece = true
		},
	},
})

func (warlock *Warlock) registerSoulAnathema() {
	// Runs before finalization, after every set bonus has been applied.
	hasFourPiece := warlock.cc.soulAnathemaFourPiece

	// Spell 900552: Shadow periodic damage every 2 sec for 10 sec. The damage is computed by the server
	// script (spell_bonus_data is 0), so it follows the set bonus text: 680% of spell power in total.
	// No family flags, so Haunt and Shadow Embrace don't apply (see excludeFromFamilyTalents).
	soulAnathema := warlock.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900552},
		SpellSchool: core.SpellSchoolShadow,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1 + core.TernaryFloat64(hasFourPiece, soulAnathemaFourPieceBonus, 0),
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Soul Anathema",
			},
			NumberOfTicks: 5,
			TickLength:    time.Second * 2,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				dot.SnapshotBaseDamage = soulAnathemaSP * dot.Spell.SpellPower() / float64(dot.NumberOfTicks)
				dot.SnapshotAttackerMultiplier = dot.Spell.AttackerDamageMultiplier(dot.Spell.Unit.AttackTables[target.UnitIndex])
			},
			OnTick: func(sim *core.Simulation, target *core.Unit, dot *core.Dot) {
				dot.CalcAndDealPeriodicSnapshotDamage(sim, target, dot.OutcomeTick)
			},
		},

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			dot := spell.Dot(target)
			// Damage left from the previous application carries over, spread over the new ticks.
			carriedOver := 0.0
			if dot.IsActive() {
				carriedOver = dot.SnapshotBaseDamage * float64(dot.MaxTicksRemaining())
			}
			dot.Apply(sim)
			dot.SnapshotBaseDamage += carriedOver / float64(dot.NumberOfTicks)
		},
	})
	warlock.excludeFromFamilyTalents(soulAnathema)

	warlock.RegisterAura(core.Aura{
		Label:    "Soul Anathema Trigger",
		ActionID: core.ActionID{SpellID: 900550},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// Haunt's hit, after its travel time.
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell == warlock.Haunt && result.Landed() && sim.Proc(soulAnathemaChance, "Soul Anathema") {
				soulAnathema.Cast(sim, result.Target)
			}
		},
		// 4pc: an instant Shadow Bolt is one that consumed Nightfall (Backlash is out of reach for affliction).
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if hasFourPiece && warlock.NightfallProcAura != nil && spell == warlock.ShadowBolt && spell.CurCast.CastTime == 0 {
				soulAnathema.Cast(sim, warlock.CurrentTarget)
			}
		},
	})
}
