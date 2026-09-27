package warrior

// Custom content from the 3.3.5a server (see assets/db_inputs/cc/REPORT.md). The server implements
// these with C++ scripts we don't have, so behaviour follows the item/spell tooltips plus the spell
// data of their helper spells (inspect with ./build.ps1 inspect spell <id>).

import (
	"time"

	"github.com/wowsims/wotlk/sim/core"
)

const (
	// Vorrax's Reckless Fury (ring, spell 200051): Execute grants a stack of Undying Fury, increasing the
	// damage of Execute by 5% and its critical strike chance by 5%. Stacks up to 99 times, lasting 60 sec.
	// Sudden Death has a 20% chance to activate.
	VorraxsRecklessFuryItemID = 900116
	// Rok'thul's Slayer's Edge (ring, spell 200082): Your attacks against your primary target have a chance
	// to overwhelm their defenses and trigger a Slayer's Strike, dealing Physical damage and granting you a
	// stack of Slayer, increasing your Bloodthirst damage for a short time. Multiple stacks may overlap.
	RokthulsSlayersEdgeItemID = 900126
)

// Tuning published by the server at http://209.38.90.151:8087/sim/data.json and checked against it by
// cc_tuning_test.go.
const (
	recklessFurySuddenDeathChance = 0.2
	undyingFuryPerStack           = 0.05 // Execute damage, and crit chance in percent points / 100
	undyingFuryMaxStacks          = 99
	undyingFuryDuration           = time.Second * 60
	slayersEdgeProcChance         = 0.15 // spell 200082 procChance, on melee auto attacks and specials (0x14)
	slayersStrikeAP               = 1.0
	slayerPerStack                = 0.03
	slayerMaxStacks               = 99
	slayerDuration                = time.Second * 12
	bloodthirstCritBuffBonus      = 0.2
	bloodthirstCritBuffDuration   = time.Second * 9
	bloodbathProcChance           = 0.28
	bloodbathAP                   = 0.25
	bloodbathTicks                = 3 // spell 900593: a tick every 2 sec for 6 sec
	fatalMarkResetChance          = 0.35
	fatalMarkCostReduction        = 0.33 // spell 900582: ADD_PCT_MODIFIER cost -33% on Mortal Strike
	fatalMarkCostBuffDuration     = time.Second * 15
	fatalMarkChance               = 1.0
	fatalMarkMaxStacks            = 5
	fatalMarkDuration             = time.Second * 30
	fatalMarkAP                   = 2.76 // per mark consumed
)

func init() {
	core.NewItemEffect(VorraxsRecklessFuryItemID, func(agent core.Agent) {
		warrior := agent.(WarriorAgent).GetWarrior()
		// Needs Execute and the Sudden Death talent aura, which are registered after item effects.
		warrior.Env.RegisterPreFinalizeEffect(warrior.registerRecklessFury)
	})
	core.NewItemEffect(RokthulsSlayersEdgeItemID, func(agent core.Agent) {
		warrior := agent.(WarriorAgent).GetWarrior()
		warrior.Env.RegisterPreFinalizeEffect(warrior.registerSlayersEdge)
	})
}

// familylessCritMultiplier is the crit multiplier of custom spells. They have no family flags, so the
// talents the server restricts by family don't reach them (./build.ps1 inspect family <id>): not
// Impale, which critMultiplier includes for the warrior's own abilities.
func (warrior *Warrior) familylessCritMultiplier() float64 {
	return warrior.MeleeCritMultiplier(primary(warrior, none), 0)
}

func (warrior *Warrior) registerRecklessFury() {
	// The server proc (script item_vorrax_reckless_fury_sudden_death) fires on every melee hit, auto
	// attacks and specials alike (proc flags 0x14). The server documents sd_chance as the combined chance
	// ("talent + ring top-up"), so the ring rolls only the top-up that brings the talent's independent
	// roll up to 20% per hit. Without the talent (fury) that is the full 20%.
	talentChance := []float64{0, 0.03, 0.06, 0.09}[warrior.Talents.SuddenDeath]
	topUpChance := 1 - (1-recklessFurySuddenDeathChance)/(1-talentChance)
	if warrior.SuddenDeathAura == nil {
		// Without the talent there is no minimum rage kept after Execute.
		warrior.SuddenDeathAura = warrior.RegisterAura(core.Aura{
			Label:    "Sudden Death Proc",
			ActionID: core.ActionID{SpellID: 29724},
			Duration: time.Second * 10,
			OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
				if result.Landed() && spell == warrior.Execute {
					aura.Deactivate(sim)
				}
			},
		})
	}

	// Spell 200052: +5% Execute damage (ADD_PCT_MODIFIER) and +5% Execute crit chance per stack.
	undyingFury := warrior.RegisterAura(core.Aura{
		Label:     "Undying Fury",
		ActionID:  core.ActionID{SpellID: 200052},
		Duration:  undyingFuryDuration,
		MaxStacks: undyingFuryMaxStacks,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			delta := float64(newStacks - oldStacks)
			warrior.Execute.DamageMultiplierAdditive += undyingFuryPerStack * delta
			warrior.Execute.BonusCritRating += 100 * undyingFuryPerStack * core.CritRatingPerCritChance * delta
		},
	})

	warrior.RegisterAura(core.Aura{
		Label:    "Vorrax's Reckless Fury",
		ActionID: core.ActionID{SpellID: 200051},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if result.Landed() && spell.ProcMask.Matches(core.ProcMaskMelee) && sim.RandomFloat("Vorrax's Reckless Fury") < topUpChance {
				warrior.SuddenDeathAura.Activate(sim)
			}
		},
		// After Execute's damage, so the stack an Execute grants doesn't boost that same Execute.
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == warrior.Execute {
				undyingFury.Activate(sim)
				undyingFury.AddStack(sim)
			}
		},
	})
}

func (warrior *Warrior) registerSlayersEdge() {
	// Spell 200084: +3% Bloodthirst damage per stack for 12 sec. Every stack has its own duration.
	slayer := warrior.RegisterAura(core.Aura{
		Label:     "Slayer",
		ActionID:  core.ActionID{SpellID: 200084},
		Duration:  core.NeverExpires,
		MaxStacks: slayerMaxStacks,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			if warrior.Bloodthirst != nil {
				warrior.Bloodthirst.DamageMultiplierAdditive += slayerPerStack * float64(newStacks-oldStacks)
			}
		},
	})
	addSlayerStack := func(sim *core.Simulation) {
		slayer.Activate(sim)
		slayer.AddStack(sim)
		core.StartDelayedAction(sim, core.DelayedActionOptions{
			DoAt: sim.CurrentTime + slayerDuration,
			OnAction: func(sim *core.Simulation) {
				if slayer.GetStacks() <= 1 {
					slayer.Deactivate(sim)
				} else {
					slayer.RemoveStack(sim)
				}
			},
		})
	}

	// Spell 200083: physical melee damage, 100% of attack power (spell_bonus_data: "retuned down from
	// 840.42% after live DPS testing"). Triggers the Slayer stack (200084) when it hits.
	slayersStrike := warrior.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200083},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   warrior.familylessCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			result := spell.CalcAndDealDamage(sim, target, slayersStrikeAP*spell.MeleeAttackPower(), spell.OutcomeMeleeSpecialHitAndCrit)
			if result.Landed() {
				addSlayerStack(sim)
			}
		},
	})

	warrior.RegisterAura(core.Aura{
		Label:    "Rok'thul's Slayer's Edge",
		ActionID: core.ActionID{SpellID: 200082},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if result.Landed() && spell.ProcMask.Matches(core.ProcMaskMelee) && result.Target == warrior.CurrentTarget &&
				sim.RandomFloat("Rok'thul's Slayer's Edge") < slayersEdgeProcChance {
				slayersStrike.Cast(sim, result.Target)
			}
		},
	})
}

// Berserker of Grizzlemaw (custom fury set 9330: neck, ring, cloak, weapon).
var ItemSetBerserkerOfGrizzlemaw = core.NewItemSet(core.ItemSet{
	Name: "Berserker of Grizzlemaw",
	Bonuses: map[int32]core.ApplyEffect{
		// Bloodthirst critical strikes increase the damage of your next Bloodthirst by 20% for 9 sec.
		2: func(agent core.Agent) {
			warrior := agent.(WarriorAgent).GetWarrior()
			warrior.Env.RegisterPreFinalizeEffect(warrior.registerBloodbathBuff)
		},
		// Bloodthirst has a 28% chance to apply Bloodbath to your target, causing them to suffer 25% of your
		// attack power as Bleed damage over 6 sec. Using Bloodthirst on a target affected by Bloodbath
		// extends the bleed by 6 sec.
		4: func(agent core.Agent) {
			warrior := agent.(WarriorAgent).GetWarrior()
			warrior.Env.RegisterPreFinalizeEffect(warrior.registerBloodbathBleed)
		},
	},
})

func (warrior *Warrior) registerBloodbathBuff() {
	if warrior.Bloodthirst == nil {
		return
	}

	// Spell 900592: +20% Bloodthirst damage (ADD_PCT_MODIFIER), 9 sec.
	buff := warrior.RegisterAura(core.Aura{
		Label:    "Bloodbath",
		ActionID: core.ActionID{SpellID: 900592},
		Duration: bloodthirstCritBuffDuration,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			warrior.Bloodthirst.DamageMultiplierAdditive += bloodthirstCritBuffBonus
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			warrior.Bloodthirst.DamageMultiplierAdditive -= bloodthirstCritBuffBonus
		},
	})

	warrior.RegisterAura(core.Aura{
		Label:    "Bloodbath 2pc Trigger",
		ActionID: core.ActionID{SpellID: 900590},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// Runs after the damage: the next Bloodthirst uses up the buff whatever its outcome, and a crit
		// grants a fresh one.
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell != warrior.Bloodthirst {
				return
			}
			buff.Deactivate(sim)
			if result.DidCrit() {
				buff.Activate(sim)
			}
		},
	})
}

func (warrior *Warrior) registerBloodbathBleed() {
	if warrior.Bloodthirst == nil {
		return
	}

	// Spell 900593: a Bleed (mechanic 15). Its damage comes from the server script, so it follows the set
	// bonus text: 25% of attack power over 6 sec, snapshotted when applied. Extensions add ticks of the
	// same size.
	bloodbath := warrior.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900593},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Bloodbath",
			},
			NumberOfTicks: bloodbathTicks,
			TickLength:    time.Second * 2,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				dot.SnapshotBaseDamage = bloodbathAP * dot.Spell.MeleeAttackPower() / bloodbathTicks
				dot.SnapshotAttackerMultiplier = dot.Spell.AttackerDamageMultiplier(dot.Spell.Unit.AttackTables[target.UnitIndex])
			},
			OnTick: func(sim *core.Simulation, target *core.Unit, dot *core.Dot) {
				dot.CalcAndDealPeriodicSnapshotDamage(sim, target, dot.OutcomeTick)
			},
		},

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			dot := spell.Dot(target)
			dot.NumberOfTicks = bloodbathTicks
			dot.Apply(sim)
		},
	})

	warrior.RegisterAura(core.Aura{
		Label:    "Bloodbath 4pc Trigger",
		ActionID: core.ActionID{SpellID: 900591},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell != warrior.Bloodthirst || !result.Landed() {
				return
			}
			if dot := bloodbath.Dot(result.Target); dot.IsActive() {
				dot.NumberOfTicks += bloodbathTicks
				dot.RecomputeAuraDuration()
				dot.UpdateExpires(dot.ExpiresAt() + bloodbathTicks*dot.TickLength)
			} else if sim.RandomFloat("Bloodbath") < bloodbathProcChance {
				bloodbath.Cast(sim, result.Target)
			}
		},
	})
}

// Reaver of the Tainted Grove (custom arms set 9329: neck, ring, cloak, weapon).
var ItemSetReaverOfTheTaintedGrove = core.NewItemSet(core.ItemSet{
	Name: "Reaver of the Tainted Grove",
	Bonuses: map[int32]core.ApplyEffect{
		// Overpower has a 35% chance to reset the cooldown of Mortal Strike and reduce the cost of your next
		// Mortal Strike by 33%.
		2: func(agent core.Agent) {
			warrior := agent.(WarriorAgent).GetWarrior()
			warrior.Env.RegisterPreFinalizeEffect(warrior.registerFatalMarkReset)
		},
		// Mortal Strike applies a Fatal Mark for 30 sec, stacking up to 5. Execute against a marked target
		// consumes all marks, dealing 276% of your attack power as physical damage per mark.
		4: func(agent core.Agent) {
			warrior := agent.(WarriorAgent).GetWarrior()
			warrior.Env.RegisterPreFinalizeEffect(warrior.registerFatalMarks)
		},
	},
})

func (warrior *Warrior) registerFatalMarkReset() {
	if warrior.MortalStrike == nil {
		return
	}

	// Spell 900582: the next Mortal Strike within 15 sec costs 33% less rage. Assumed used up by that
	// Mortal Strike's cast, whatever its outcome (a miss still refunds 80% of the reduced cost).
	costBuff := warrior.RegisterAura(core.Aura{
		Label:    "Fatal Mark (Mortal Strike cost)",
		ActionID: core.ActionID{SpellID: 900582},
		Duration: fatalMarkCostBuffDuration,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			warrior.MortalStrike.CostMultiplier -= fatalMarkCostReduction
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			warrior.MortalStrike.CostMultiplier += fatalMarkCostReduction
		},
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == warrior.MortalStrike {
				aura.Deactivate(sim)
			}
		},
	})

	warrior.RegisterAura(core.Aura{
		Label:    "Fatal Mark 2pc Trigger",
		ActionID: core.ActionID{SpellID: 900580},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// Assumed to need a landed Overpower (a server script on its hit).
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell == warrior.Overpower && result.Landed() && sim.RandomFloat("Fatal Mark Reset") < fatalMarkResetChance {
				warrior.MortalStrike.CD.Reset()
				costBuff.Activate(sim)
			}
		},
	})
}

func (warrior *Warrior) registerFatalMarks() {
	if warrior.MortalStrike == nil {
		return
	}

	// Spell 900583: the mark on the target, a DUMMY aura (30 sec, 5 stacks).
	marks := warrior.NewEnemyAuraArray(func(target *core.Unit) *core.Aura {
		return target.GetOrRegisterAura(core.Aura{
			Label:     "Fatal Mark-" + warrior.Label,
			ActionID:  core.ActionID{SpellID: 900583},
			Duration:  fatalMarkDuration,
			MaxStacks: fatalMarkMaxStacks,
		})
	})

	// Spell 900584: physical melee-class damage computed by the server script. It has
	// SPELL_ATTR3_IGNORE_HIT_RESULT (it can't miss, be dodged or parried) and can crit. No family flags, so
	// only school-wide modifiers (Two-Handed Weapon Specialization, armor, debuffs) apply.
	detonation := warrior.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900584},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   warrior.familylessCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			mark := marks.Get(target)
			baseDamage := fatalMarkAP * float64(mark.GetStacks()) * spell.MeleeAttackPower()
			mark.Deactivate(sim)
			spell.CalcAndDealDamage(sim, target, baseDamage, spell.OutcomeMeleeSpecialCritOnly)
		},
	})

	warrior.RegisterAura(core.Aura{
		Label:    "Fatal Mark 4pc Trigger",
		ActionID: core.ActionID{SpellID: 900581},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// Both assumed to need the ability to land.
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() {
				return
			}
			switch spell {
			case warrior.MortalStrike:
				if sim.RandomFloat("Fatal Mark") < fatalMarkChance {
					mark := marks.Get(result.Target)
					mark.Activate(sim)
					mark.AddStack(sim)
				}
			case warrior.Execute:
				if marks.Get(result.Target).IsActive() {
					detonation.Cast(sim, result.Target)
				}
			}
		},
	})
}
