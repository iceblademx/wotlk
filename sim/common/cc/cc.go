// Package cc holds effects for custom 3.3.5a content (items, gems, enchants, set bonuses) imported
// from client/server data by tools/cc.
//
// Layout:
//   - zz_generated.go is written by `./build.ps1 cc` and must not be edited by hand. It registers
//     effects the importer can express from data alone (stat-only set bonuses, on-use stat items,
//     stat procs).
//   - Every other file is hand-written. Hand-written registrations always win: files initialize in
//     name order, so they run before zz_generated.go, and the generated code skips any item or set
//     that is already registered.
package cc

import (
	"time"

	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/stats"
)

// 3.3.5a proc flags (Spell.dbc procFlags / spell_proc.ProcFlags).
const (
	procFlagDoneMeleeAutoAttack        = 0x00000004
	procFlagTakenMeleeAutoAttack       = 0x00000008
	procFlagDoneSpellMeleeDmgClass     = 0x00000010
	procFlagTakenSpellMeleeDmgClass    = 0x00000020
	procFlagDoneRangedAutoAttack       = 0x00000040
	procFlagTakenRangedAutoAttack      = 0x00000080
	procFlagDoneSpellRangedDmgClass    = 0x00000100
	procFlagTakenSpellRangedDmgClass   = 0x00000200
	procFlagDoneSpellNoneDmgClassPos   = 0x00000400
	procFlagDoneSpellNoneDmgClassNeg   = 0x00001000
	procFlagDoneSpellMagicDmgClassPos  = 0x00004000
	procFlagDoneSpellMagicDmgClassNeg  = 0x00010000
	procFlagTakenSpellMagicDmgClassNeg = 0x00020000
	procFlagDonePeriodic               = 0x00040000
	procFlagTakenPeriodic              = 0x00080000
	procFlagTakenDamage                = 0x00100000

	// procEx / HitMask
	procExCriticalHit = 0x2
)

// ProcStat describes "chance on <event> to gain <stats> for <duration>" effects.
type ProcStat struct {
	ItemID      int32
	Name        string
	AuraSpellID int32
	Bonus       stats.Stats
	Duration    time.Duration
	MaxStacks   int32 // >1 for stacking procs; Bonus is per stack

	ProcFlags  uint32
	HitMask    uint32 // procEx; 0 = any landed hit
	ProcChance float64
	PPM        float64
	ICD        time.Duration
}

// ProcTriggers splits 3.3.5a proc flags into the sim's callback/proc-mask model.
// A single flag set can map to several triggers (e.g. melee and spell).
func ProcTriggers(flags uint32) []core.ProcTrigger {
	var out []core.ProcTrigger
	add := func(cb core.AuraCallback, mask core.ProcMask, harmful bool) {
		out = append(out, core.ProcTrigger{Callback: cb, ProcMask: mask, Harmful: harmful})
	}

	dealtMask := core.ProcMaskUnknown
	if flags&procFlagDoneMeleeAutoAttack != 0 {
		dealtMask |= core.ProcMaskMeleeWhiteHit
	}
	if flags&procFlagDoneSpellMeleeDmgClass != 0 {
		dealtMask |= core.ProcMaskMeleeSpecial
	}
	if flags&procFlagDoneRangedAutoAttack != 0 {
		dealtMask |= core.ProcMaskRangedAuto
	}
	if flags&procFlagDoneSpellRangedDmgClass != 0 {
		dealtMask |= core.ProcMaskRangedSpecial
	}
	if flags&(procFlagDoneSpellMagicDmgClassNeg|procFlagDoneSpellNoneDmgClassNeg) != 0 {
		dealtMask |= core.ProcMaskSpellDamage
	}
	if dealtMask != core.ProcMaskUnknown {
		add(core.CallbackOnSpellHitDealt, dealtMask, true)
	}
	if flags&procFlagDonePeriodic != 0 {
		add(core.CallbackOnPeriodicDamageDealt, core.ProcMaskUnknown, true)
	}
	if flags&(procFlagDoneSpellMagicDmgClassPos|procFlagDoneSpellNoneDmgClassPos) != 0 {
		add(core.CallbackOnHealDealt|core.CallbackOnPeriodicHealDealt, core.ProcMaskUnknown, false)
	}

	takenMask := core.ProcMaskUnknown
	if flags&(procFlagTakenMeleeAutoAttack|procFlagTakenSpellMeleeDmgClass) != 0 {
		takenMask |= core.ProcMaskMelee
	}
	if flags&(procFlagTakenRangedAutoAttack|procFlagTakenSpellRangedDmgClass) != 0 {
		takenMask |= core.ProcMaskRanged
	}
	if flags&(procFlagTakenSpellMagicDmgClassNeg|procFlagTakenPeriodic) != 0 {
		takenMask |= core.ProcMaskSpellDamage
	}
	if flags&procFlagTakenDamage != 0 {
		takenMask = core.ProcMaskDirect
	}
	if takenMask != core.ProcMaskUnknown {
		add(core.CallbackOnSpellHitTaken, takenMask, true)
	}
	return out
}

// RegisterProcStat registers a stat proc item effect unless the item already has one.
func RegisterProcStat(cfg ProcStat) {
	if core.HasItemEffect(cfg.ItemID) {
		return
	}
	triggers := ProcTriggers(cfg.ProcFlags)
	if len(triggers) == 0 {
		return
	}
	outcome := core.OutcomeLanded
	if cfg.HitMask&procExCriticalHit != 0 && cfg.HitMask&^procExCriticalHit == 0 {
		outcome = core.OutcomeCrit
	}

	core.NewItemEffect(cfg.ItemID, func(agent core.Agent) {
		character := agent.GetCharacter()
		actionID := core.ActionID{SpellID: cfg.AuraSpellID}
		if actionID.IsEmptyAction() {
			actionID = core.ActionID{ItemID: cfg.ItemID}
		}

		var activate func(sim *core.Simulation)
		var procAura *core.Aura
		if cfg.MaxStacks > 1 {
			procAura = character.GetOrRegisterAura(core.Aura{
				Label:     cfg.Name + " Proc",
				ActionID:  actionID,
				Duration:  cfg.Duration,
				MaxStacks: cfg.MaxStacks,
				OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
					character.AddStatsDynamic(sim, cfg.Bonus.Multiply(float64(newStacks-oldStacks)))
				},
			})
			activate = func(sim *core.Simulation) {
				procAura.Activate(sim)
				procAura.AddStack(sim)
			}
		} else {
			procAura = character.NewTemporaryStatsAura(cfg.Name+" Proc", actionID, cfg.Bonus, cfg.Duration)
			activate = procAura.Activate
		}

		var icd core.Cooldown
		if cfg.ICD > 0 {
			icd = core.Cooldown{Timer: character.NewTimer(), Duration: cfg.ICD}
			procAura.Icd = &icd
		}
		for _, trigger := range triggers {
			trigger.ActionID = core.ActionID{ItemID: cfg.ItemID}
			trigger.Name = cfg.Name
			trigger.Outcome = outcome
			trigger.ProcChance = cfg.ProcChance
			trigger.PPM = cfg.PPM
			trigger.Handler = func(sim *core.Simulation, _ *core.Spell, _ *core.SpellResult) {
				if icd.Timer != nil {
					if !icd.IsReady(sim) {
						return
					}
					icd.Use(sim)
				}
				activate(sim)
			}
			core.MakeProcTriggerAura(&character.Unit, trigger)
		}
	})
}

// RegisterUseStat registers an on-use stat item unless the item already has an effect.
func RegisterUseStat(itemID int32, bonus stats.Stats, duration, cooldown time.Duration, trinket, defensive bool) {
	if core.HasItemEffect(itemID) {
		return
	}
	switch {
	case trinket && defensive:
		core.NewSimpleStatDefensiveTrinketEffect(itemID, bonus, duration, cooldown)
	case trinket:
		core.NewSimpleStatOffensiveTrinketEffect(itemID, bonus, duration, cooldown)
	default:
		core.NewSimpleStatItemEffect(itemID, bonus, duration, cooldown)
	}
}

// RegisterStatSet registers a set whose bonuses are all flat stats, unless a hand-written
// implementation of the set already exists.
func RegisterStatSet(name string, bonuses map[int32]stats.Stats) {
	if core.HasItemSet(name) {
		return
	}
	effects := make(map[int32]core.ApplyEffect, len(bonuses))
	for pieces, bonus := range bonuses {
		bonus := bonus
		effects[pieces] = func(agent core.Agent) {
			agent.GetCharacter().AddStats(bonus)
		}
	}
	core.NewItemSet(core.ItemSet{Name: name, Bonuses: effects})
}
