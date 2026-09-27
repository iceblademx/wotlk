package druid

// Custom content from the 3.3.5a server (see assets/db_inputs/cc/REPORT.md). The server implements
// these with C++ scripts we don't have, so behaviour follows the item/spell tooltips plus the spell
// data of their helper spells (inspect with ./build.ps1 inspect spell <id>).

import (
	"time"

	"github.com/wowsims/wotlk/sim/core"
)

// Morgrath's Ravaging Claw (ring, spell 200049): Ferocious Bite has a 20% chance per combo point spent
// to trigger Omen of Clarity. Shred or Mangle casts that consume Omen of Clarity deal 100% additional damage.
const MorgrathsRavagingClawItemID = 900115

func init() {
	core.NewItemEffect(MorgrathsRavagingClawItemID, func(agent core.Agent) {
		// Needs Clearcasting even without the Omen of Clarity talent: see applyOmenOfClarity.
		// Effects are in onFerociousBiteLanded and clearcastBuilderMultiplier.
		agent.(DruidAgent).GetDruid().hasRavagingClaw = true
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

const bloodseekerVinesProcChance = 0.10

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
			baseDamage := 0.30 * spell.MeleeAttackPower()
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
				dot.SnapshotBaseDamage = 0.35 * dot.Spell.MeleeAttackPower() / float64(dot.NumberOfTicks)
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

	tryProc := func(sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
		if result.Damage <= 0 || !(druid.Rake.IsEqual(spell) || druid.Rip.IsEqual(spell)) {
			return
		}
		if sim.RandomFloat("Bloodseeker Vines") < bloodseekerVinesProcChance {
			druid.BloodseekerVines.Cast(sim, result.Target)
		}
	}
	druid.RegisterAura(core.Aura{
		Label:    "Bloodseeker Vines Trigger",
		ActionID: core.ActionID{SpellID: 900340},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// Rake's initial hit.
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if result.Landed() {
				tryProc(sim, spell, result)
			}
		},
		// Rake and Rip ticks.
		OnPeriodicDamageDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			tryProc(sim, spell, result)
		},
	})
}

// onFerociousBiteLanded runs after a Ferocious Bite lands, before its combo points are spent.
func (druid *Druid) onFerociousBiteLanded(sim *core.Simulation, target *core.Unit, comboPoints int32) {
	if druid.hasRavagingClaw && druid.ClearcastingAura != nil &&
		sim.RandomFloat("Morgrath's Ravaging Claw") < 0.2*float64(comboPoints) {
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
		return 2
	}
	return 1
}
