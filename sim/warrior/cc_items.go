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

func (warrior *Warrior) registerRecklessFury() {
	// The server proc (script item_vorrax_reckless_fury_sudden_death) fires on every melee hit, auto
	// attacks and specials alike (proc flags 0x14). Assumed to activate Sudden Death on 20% of them
	// whether or not the warrior has the talent; the talent's own procs still happen on top.
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
		Duration:  time.Second * 60,
		MaxStacks: 99,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			delta := float64(newStacks - oldStacks)
			warrior.Execute.DamageMultiplierAdditive += 0.05 * delta
			warrior.Execute.BonusCritRating += 5 * core.CritRatingPerCritChance * delta
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
			if result.Landed() && spell.ProcMask.Matches(core.ProcMaskMelee) && sim.RandomFloat("Vorrax's Reckless Fury") < 0.2 {
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

const (
	slayersEdgeProcChance = 0.15 // spell 200082 procChance, on melee auto attacks and specials (0x14)
	slayerDuration        = time.Second * 12
)

func (warrior *Warrior) registerSlayersEdge() {
	// Spell 200084: +3% Bloodthirst damage per stack for 12 sec. Every stack has its own duration.
	slayer := warrior.RegisterAura(core.Aura{
		Label:     "Slayer",
		ActionID:  core.ActionID{SpellID: 200084},
		Duration:  core.NeverExpires,
		MaxStacks: 99,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			if warrior.Bloodthirst != nil {
				warrior.Bloodthirst.DamageMultiplierAdditive += 0.03 * float64(newStacks-oldStacks)
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
		CritMultiplier:   warrior.critMultiplier(none),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			result := spell.CalcAndDealDamage(sim, target, spell.MeleeAttackPower(), spell.OutcomeMeleeSpecialHitAndCrit)
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
		Duration: time.Second * 9,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			warrior.Bloodthirst.DamageMultiplierAdditive += 0.2
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			warrior.Bloodthirst.DamageMultiplierAdditive -= 0.2
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

const (
	bloodbathProcChance = 0.28
	bloodbathTicks      = 3 // spell 900593: a tick every 2 sec for 6 sec
)

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
				dot.SnapshotBaseDamage = 0.25 * dot.Spell.MeleeAttackPower() / bloodbathTicks
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
