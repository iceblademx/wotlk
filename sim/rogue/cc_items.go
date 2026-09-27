package rogue

// Custom content from the 3.3.5a server (see assets/db_inputs/cc/REPORT.md). The server implements
// these with C++ scripts we don't have, so behaviour follows the item/spell tooltips plus the spell
// data of their helper spells (inspect with ./build.ps1 inspect spell <id>).

import (
	"time"

	"github.com/wowsims/wotlk/sim/core"
)

const (
	// Endless Dance (ring, spell 200179): Your finishing moves reduce the remaining cooldown of Shadow
	// Dance by 1 sec per combo point spent. Activating Shadow Dance restores 40 Energy.
	EndlessDanceItemID = 900146
	// Shadow's Invitation (ring, spell 200181): Your Eviscerate allows your next Ambush to be used without
	// being in Stealth and reduces its Energy cost by 20. Your Ambush deals additional Shadow damage equal
	// to 30% of its damage dealt.
	ShadowsInvitationItemID = 900147
)

// State for custom items that upstream spell code checks.
type ccItems struct {
	endlessDance      bool
	shadowsInvitation *core.Aura // Ambush usable outside Stealth

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
}

func (rogue *Rogue) registerEndlessDance() {
	if rogue.ShadowDance == nil {
		return
	}
	rogue.cc.endlessDance = true

	// Spell 200180: energize 40.
	energyMetrics := rogue.NewEnergyMetrics(core.ActionID{SpellID: 200180})
	rogue.RegisterAura(core.Aura{
		Label:    "Endless Dance",
		ActionID: core.ActionID{SpellID: 200179},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == rogue.ShadowDance {
				rogue.AddEnergy(sim, 40, energyMetrics)
			}
		},
	})
}

// onFinisher runs after every finishing move spends its combo points.
func (rogue *Rogue) onFinisher(sim *core.Simulation, numPoints int32) {
	if rogue.cc.endlessDance {
		if timer := rogue.ShadowDance.CD.Timer; !timer.IsReady(sim) {
			timer.Set(max(sim.CurrentTime, timer.ReadyAt()-time.Second*time.Duration(numPoints)))
		}
	}
}

func (rogue *Rogue) registerShadowsInvitation() {
	const costReduction = 20

	// Spell 200182: next Ambush ignores the Stealth requirement and costs 20 less Energy; 15 sec, 1 charge.
	var reduced float64
	rogue.cc.shadowsInvitation = rogue.RegisterAura(core.Aura{
		Label:    "Shadow's Invitation",
		ActionID: core.ActionID{SpellID: 200182},
		Duration: time.Second * 15,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			reduced = min(costReduction, rogue.Ambush.DefaultCast.Cost)
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

	rogue.RegisterAura(core.Aura{
		Label:    "Shadow's Invitation Trigger",
		ActionID: core.ActionID{SpellID: 200181},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() {
				return
			}
			switch spell {
			case rogue.Eviscerate:
				rogue.cc.shadowsInvitation.Activate(sim)
			case rogue.Ambush:
				if result.Damage > 0 {
					damage := 0.3 * result.Damage * rogue.shadowStrikesDebuffMultiplier(result.Target)
					shadowDamage.CalcAndDealDamage(sim, result.Target, damage, shadowDamage.OutcomeAlwaysHit)
				}
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

// Shadow Strikes (spells 900512 and 900514) repeat the damage of the ability that triggered them, as
// Shadow damage. The server script computes it (spell_bonus_data is 0); the full amount was confirmed
// in game. It is a copy of the damage dealt, so it neither rolls its own crit nor is partially resisted.
const (
	// 4pc debuff (spell 900515, script tier_rog_sub_debuff): its aura tooltip reads 15%, the set bonus
	// 12 sec. The DBC effect itself says 50% for 18 sec, which looks left over from the spell it was
	// cloned from, so the tooltip values are used.
	shadowOfTheCanopyBonus    = 0.15
	shadowOfTheCanopyDuration = time.Second * 12
)

func (rogue *Rogue) newShadowStrike(actionID core.ActionID) *core.Spell {
	return rogue.RegisterSpell(core.SpellConfig{
		ActionID:    actionID,
		SpellSchool: core.SpellSchoolShadow,
		// Triggered strikes don't proc weapon effects, like other server-triggered spells.
		ProcMask: core.ProcMaskEmpty,
		Flags:    core.SpellFlagIgnoreModifiers | core.SpellFlagIgnoreResists | core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {},
	})
}

// dealShadowStrike deals a copy of an ability's damage as a Shadow Strike.
func (rogue *Rogue) dealShadowStrike(sim *core.Simulation, strike *core.Spell, target *core.Unit, damage float64) {
	strike.SpellMetrics[target.UnitIndex].Casts++
	strike.CalcAndDealDamage(sim, target, damage*rogue.shadowStrikesDebuffMultiplier(target), strike.OutcomeAlwaysHit)
}

func (rogue *Rogue) registerShadowStrikes() {
	if rogue.Hemorrhage == nil {
		return
	}

	// One strike per weapon, each for the full Hemorrhage damage.
	mhStrike := rogue.newShadowStrike(core.ActionID{SpellID: 900512, Tag: 1})
	var ohStrike *core.Spell
	if rogue.AutoAttacks.IsDualWielding {
		ohStrike = rogue.newShadowStrike(core.ActionID{SpellID: 900512, Tag: 2})
	}

	rogue.RegisterAura(core.Aura{
		Label:    "Shadow Strikes",
		ActionID: core.ActionID{SpellID: 900510},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell != rogue.Hemorrhage || !result.Landed() || result.Damage <= 0 {
				return
			}
			rogue.dealShadowStrike(sim, mhStrike, result.Target, result.Damage)
			if ohStrike != nil {
				rogue.dealShadowStrike(sim, ohStrike, result.Target, result.Damage)
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

	// Spell 900514: the second strike.
	echo := rogue.newShadowStrike(core.ActionID{SpellID: 900514})

	rogue.RegisterAura(core.Aura{
		Label:    "Shadow Strikes (Shadow Dance)",
		ActionID: core.ActionID{SpellID: 900511},
		Duration: core.NeverExpires,
		OnReset: func(aura *core.Aura, sim *core.Simulation) {
			aura.Activate(sim)
		},
		// "Abilities" are the rogue's builders and finishers that deal direct damage.
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !rogue.ShadowDanceAura.IsActive() || !result.Landed() || result.Damage <= 0 ||
				!spell.Flags.Matches(SpellFlagBuilder|SpellFlagFinisher) {
				return
			}
			rogue.dealShadowStrike(sim, echo, result.Target, result.Damage)
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
