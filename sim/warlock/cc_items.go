package warlock

// Custom content from the 3.3.5a server (see assets/db_inputs/cc/REPORT.md). The server implements
// these with C++ scripts we don't have, so behaviour follows the item/spell tooltips plus the spell
// data of their helper spells (inspect with ./build.ps1 inspect spell <id>).

import (
	"time"

	"github.com/wowsims/wotlk/sim/core"
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

// State for custom items.
type ccItems struct {
	soulAnathemaFourPiece bool
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
}

func (warlock *Warlock) registerWitheringGrasp() {
	// Spell 200016: -7500 ms Drain Soul duration (SPELLMOD_DURATION) and -50% tick period
	// (SPELLMOD_ACTIVATION_TIME), so still 5 ticks.
	for _, target := range warlock.Env.Encounter.TargetUnits {
		warlock.DrainSoul.Dot(target).TickLength /= 2
	}

	// Spell 200061: +50% Drain Soul damage per stack, 4 stacks, 18 sec.
	shadowedMark := warlock.RegisterAura(core.Aura{
		Label:     "Withering Grasp: Shadowed Mark",
		ActionID:  core.ActionID{SpellID: 200061},
		Duration:  time.Second * 18,
		MaxStacks: 4,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			warlock.DrainSoul.DamageMultiplierAdditive += 0.5 * float64(newStacks-oldStacks)
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

const reapedSoulStacks = 25

func (warlock *Warlock) registerReapedSoul() {
	// Spell 200078: Shadow periodic damage, 4 ticks 1 sec apart. spell_bonus_data gives 600% of spell
	// power per tick ("Reap Soul, 600% of Spell Power per tick"). Assumed not to crit, like other DoTs.
	reapSoul := warlock.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200078},
		SpellSchool: core.SpellSchoolShadow,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagHauntSE | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Reap Soul",
			},
			NumberOfTicks: 4,
			TickLength:    time.Second,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				dot.SnapshotBaseDamage = 6 * dot.Spell.SpellPower()
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
	soulAnathema := warlock.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900552},
		SpellSchool: core.SpellSchoolShadow,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagHauntSE | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: core.TernaryFloat64(hasFourPiece, 1.25, 1),
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Soul Anathema",
			},
			NumberOfTicks: 5,
			TickLength:    time.Second * 2,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				dot.SnapshotBaseDamage = 6.8 * dot.Spell.SpellPower() / float64(dot.NumberOfTicks)
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

	warlock.RegisterAura(core.Aura{
		Label:    "Soul Anathema Trigger",
		ActionID: core.ActionID{SpellID: 900550},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// Haunt's hit, after its travel time.
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell == warlock.Haunt && result.Landed() {
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
