package hunter

// Custom content from the 3.3.5a server (see assets/db_inputs/cc/REPORT.md). The server implements
// these with C++ scripts we don't have, so behaviour follows the mechanics and tuning the server
// publishes (http://209.38.90.151:8087/sim/data.json), the item/spell tooltips and the spell data of
// their helper spells (inspect with ./build.ps1 inspect spell <id>).
//
// Conventions for the helper spells:
//   - None of them has family flags that select a hunter talent or glyph (./build.ps1 inspect family),
//     so they get no Mortal Shots, Marked for Death, TNT, Barrage and so on: school-wide modifiers only,
//     and hunter.critMultiplier(false, false, false) for ranged-class crits.
//   - Outcomes follow the spell's damage class: magic-class spells roll spell hit and crit (x1.5),
//     ranged-class spells ranged hit and crit (x2).
//   - Damage the server scripts compute from "attack power" uses the hunter's ranged attack power
//     (hunter tooltips call it attack power), without Hunter's Mark: see scriptAP.
//   - Spells cast by script-summoned beasts (Stampede, Pack Leader, Dark Minion) don't get the
//     hunter's damage modifiers; like the hunter's pet, they use the hunter's hit and crit.

import (
	"strconv"
	"time"

	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/stats"
)

const (
	// Skareth's Killing Eye (ring, spell 200019): Casting Misdirection allows your next Kill Shot to be
	// cast regardless of target health, dealing 300% additional damage.
	SkarethsKillingEyeItemID = 900107
	// Vorrax's Wild Hunt (ring, spell 200143): While you have the Beast Mastery talent, your pet
	// inherits 100% of your armor penetration and 30% of your critical strike and haste ratings. Call of
	// the Wild also unleashes a stampede of spirit beasts upon your target for 20 sec.
	VorraxsWildHuntItemID = 900133
	// Pack Leader's Insignia (ring, spell 200146): Kill Command calls upon a legendary beast to aid your
	// hunt. The Wyvern, Bear and Boar answer in succession, each granting their blessing for 30 sec.
	PackLeadersInsigniaItemID = 900134
	// Deadeye's Oath (ring, spell 200154): Your Aimed Shot deals additional Physical damage equal to
	// 650% of your ranged attack power, but its base cooldown is increased to 15 sec. Casting another
	// Shot reduces the cooldown by 2 sec.
	DeadeyesOathItemID = 900135
	// Loop of the Banshee (ring, spell 200156): Your Silencing Shot fires a Withering Arrow at the
	// target, dealing Shadow damage over 8 sec. Each periodic tick strengthens the arrow's eruption. Your
	// Chimera Shot consumes the arrow, immediately dealing its remaining periodic damage to the target
	// and unleashing a Shadow eruption that strikes all enemies within 8 yards.
	LoopOfTheBansheeItemID = 900136
	// Bombardier's Pin (ring, spell 200160): Grants the Wildfire Bomb ability.
	BombardiersPinItemID = 900137
	// Signet of Dark Deeds (ring, spell 200162): Your Black Arrow summons a Dark Minion for 10 sec. When
	// it fades, it strikes your target for 90% of all damage you dealt while it was active, as Shadow
	// damage.
	SignetOfDarkDeedsItemID = 900138
)

// Tuning published by the server and checked against it by cc_tuning_test.go. Values marked live come
// from crimson_tier_config.
const (
	killingEyeBonus    = 3.0 // SPELLMOD_DAMAGE, additive with Kill Shot's other percent modifiers
	killingEyeDuration = time.Second * 10

	wildHuntArPShare        = 1.0
	wildHuntCritShare       = 0.3
	wildHuntHasteShare      = 0.3
	wildHuntBeasts          = 4
	wildHuntStampedeTime    = time.Second * 20
	wildHuntCleaveInterval  = time.Second * 2
	wildHuntCleaveAP        = 0.10
	packLeaderBeastTime     = time.Second * 30
	packLeaderSteadyShot    = 0.51 // Skyhunter's Cry: the spell data applies 51% (designed as 50)
	packLeaderPoisonBoltAP  = 0.15
	packLeaderPoisonBoltCD  = time.Millisecond * 2500
	packLeaderElderAP       = 0.60
	packLeaderHeavyCleaveAP = 0.35
	packLeaderHeavyCleaveCD = time.Second * 6
	packLeaderGougeTickAP   = 0.08 // per stack
	packLeaderGougeStacks   = 5
	packLeaderGougeDuration = time.Second * 15
	packLeaderGougeTick     = time.Second * 2 // spell 200153's period

	deadeyeAP         = 6.5             // live: leg_deadeye ap_pct
	deadeyeCDIncrease = time.Second * 5 // live: leg_deadeye cd_increase_ms
	deadeyeCDPerShot  = time.Second * 2 // live: leg_deadeye cd_reduction_ms

	bansheeTickAP        = 0.12
	bansheeRampPerTick   = 0.15
	bansheeTicks         = 8
	bansheeTickLength    = time.Second
	bansheeEruptionShare = 0.5

	wildfireBombAP       = 0.81 // spell 200161 effect base points + 1
	wildfireBombCooldown = time.Second * 18

	darkMinionDuration = time.Second * 10
	darkMinionEcho     = 0.9

	barbedShotChance   = 0.22
	barbedShotAP       = 0.35 // over the whole bleed
	barbedShotTicks    = 3    // spell 900372: 6 sec, every 2 sec
	barbedShotTick     = time.Second * 2
	stompAP            = 0.30
	stompCotWRapidFire = time.Second * 2
	stompBWKillCommand = time.Second

	markedQuarryPerStack     = 0.02
	markedQuarryMaxStacks    = 5
	markedQuarryDuration     = time.Second * 30
	markedQuarryAimedAP      = 0.20 // per stack consumed
	markedQuarryArcaneBonus  = 0.5
	markedQuarryArcaneWindow = time.Second * 30

	sentinelsMarkChance = 0.45
	sentinelsMarkBonus  = 0.40
	lunarMissiles       = 4
	lunarMissileAP      = 0.30
)

// State for custom items.
type ccItems struct {
	// Vorrax's Wild Hunt pet stat sharing, with the Beast Mastery talent.
	wildHunt bool
	// Deadeye's Oath: added to Aimed Shot's base damage, as a fraction of scriptAP.
	aimedShotBonusAP float64

	barbedShotFourPiece    bool
	markedQuarryFourPiece  bool
	sentinelsMarkFourPiece bool
}

func init() {
	core.NewItemEffect(SkarethsKillingEyeItemID, func(agent core.Agent) {
		hunter := agent.(HunterAgent).GetHunter()
		// Needs Kill Shot, which is registered after item effects.
		hunter.Env.RegisterPreFinalizeEffect(hunter.registerKillingEye)
	})
	core.NewItemEffect(VorraxsWildHuntItemID, func(agent core.Agent) {
		hunter := agent.(HunterAgent).GetHunter()
		hunter.cc.wildHunt = hunter.Talents.BeastMastery
		hunter.Env.RegisterPreFinalizeEffect(hunter.registerStampede)
	})
	core.NewItemEffect(PackLeadersInsigniaItemID, func(agent core.Agent) {
		hunter := agent.(HunterAgent).GetHunter()
		hunter.Env.RegisterPreFinalizeEffect(hunter.registerPackLeader)
	})
	core.NewItemEffect(DeadeyesOathItemID, func(agent core.Agent) {
		hunter := agent.(HunterAgent).GetHunter()
		hunter.cc.aimedShotBonusAP = deadeyeAP
		hunter.Env.RegisterPreFinalizeEffect(hunter.registerDeadeye)
	})
	core.NewItemEffect(LoopOfTheBansheeItemID, func(agent core.Agent) {
		hunter := agent.(HunterAgent).GetHunter()
		hunter.Env.RegisterPreFinalizeEffect(hunter.registerWitheringArrow)
	})
	core.NewItemEffect(BombardiersPinItemID, func(agent core.Agent) {
		hunter := agent.(HunterAgent).GetHunter()
		hunter.Env.RegisterPreFinalizeEffect(hunter.registerWildfireBomb)
	})
	core.NewItemEffect(SignetOfDarkDeedsItemID, func(agent core.Agent) {
		hunter := agent.(HunterAgent).GetHunter()
		hunter.Env.RegisterPreFinalizeEffect(hunter.registerDarkMinion)
	})
}

// scriptAP is the attack power the server scripts compute custom damage from: the hunter's ranged
// attack power. Hunter's Mark only adds to the hunter's own weapon attacks against the marked target, so
// it isn't included (as for Black Arrow).
func (hunter *Hunter) scriptAP() float64 {
	return hunter.GetStat(stats.RangedAttackPower) + hunter.PseudoStats.MobTypeAttackPower
}

// alwaysActive registers a hidden aura that is active the whole fight, for item triggers.
func (hunter *Hunter) alwaysActive(aura core.Aura) *core.Aura {
	aura.Duration = core.NeverExpires
	aura.OnReset = func(aura *core.Aura, sim *core.Simulation) {
		aura.Activate(sim)
	}
	return hunter.RegisterAura(aura)
}

// reduceCooldown takes d off a spell's remaining cooldown, if the spell exists.
func reduceCooldown(sim *core.Simulation, spell *core.Spell, d time.Duration) {
	if spell == nil || spell.CD.Timer == nil || spell.CD.IsReady(sim) {
		return
	}
	spell.CD.Set(max(sim.CurrentTime, spell.CD.ReadyAt()-d))
}

// isRangedAbility reports whether a spell is a ranged-class ability the hunter casts (proc flag 0x100).
// Server-triggered spells (Wild Quiver, Chimera Shot's sting) don't trigger procs, and Volley is
// magic-class; Serpent Sting is ranged-class although the sim gives it no proc mask.
func (hunter *Hunter) isRangedAbility(spell *core.Spell) bool {
	if spell == hunter.SerpentSting {
		return true
	}
	return spell.ProcMask.Matches(core.ProcMaskRangedSpecial) && spell.Flags.Matches(core.SpellFlagAPL) && spell != hunter.Volley
}

// registerMinionSpell registers a magic-class spell cast by a script-summoned beast. The server passes
// the damage as base points; the beast casts it, so the hunter's damage-done modifiers don't apply.
func (hunter *Hunter) registerMinionSpell(actionID core.ActionID, school core.SpellSchool, applyEffects core.ApplySpellResults) *core.Spell {
	return hunter.RegisterSpell(core.SpellConfig{
		ActionID:    actionID,
		SpellSchool: school,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagIgnoreAttackerModifiers | core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   hunter.DefaultSpellCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: applyEffects,
	})
}

// summonBeast starts a beast casting spell at the hunter's target every interval, and returns the
// action so the caller can cancel it when the beast leaves.
func (hunter *Hunter) summonBeast(sim *core.Simulation, spell *core.Spell, interval, duration time.Duration, casts int) *core.PendingAction {
	return core.StartPeriodicAction(sim, core.PeriodicActionOptions{
		Period:   interval,
		NumTicks: int(duration / interval),
		OnAction: func(sim *core.Simulation) {
			for i := 0; i < casts; i++ {
				spell.Cast(sim, hunter.CurrentTarget)
			}
		},
	})
}

// addCCPetStats adds what the pet inherits from custom items to its regular stat inheritance. It must
// stay linear: it also converts the owner's stat changes.
func (hunter *Hunter) addCCPetStats(ownerStats stats.Stats, inherited stats.Stats) stats.Stats {
	if !hunter.cc.wildHunt {
		return inherited
	}
	// Spell 200144 (Bonded Instincts) converts the shares to percentages, which is the same as sharing
	// the ratings. The sim folds agility's crit into the crit stat; that part isn't rating, so it's taken
	// out. Crit from talents and buffs is part of the same stat and can't be separated.
	critRating := ownerStats[stats.MeleeCrit] - ownerStats[stats.Agility]*core.CritPerAgiMaxLevel[hunter.Class]*core.CritRatingPerCritChance
	inherited[stats.ArmorPenetration] += wildHuntArPShare * ownerStats[stats.ArmorPenetration]
	inherited[stats.MeleeCrit] += wildHuntCritShare * critRating
	inherited[stats.SpellCrit] += wildHuntCritShare * critRating
	inherited[stats.MeleeHaste] += wildHuntHasteShare * ownerStats[stats.MeleeHaste]
	return inherited
}

func (hunter *Hunter) registerKillingEye() {
	// Spell 200020: Kill Shot ignores the target's health (ABILITY_IGNORE_AURASTATE) and deals +300%
	// damage (SPELLMOD_DAMAGE, bp 299), one charge, 10 sec.
	killingEye := hunter.RegisterAura(core.Aura{
		Label:    "Skareth's Killing Eye",
		ActionID: core.ActionID{SpellID: 200020},
		Duration: killingEyeDuration,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			hunter.KillShot.DamageMultiplierAdditive += killingEyeBonus
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			hunter.KillShot.DamageMultiplierAdditive -= killingEyeBonus
		},
		// OnCastComplete runs after Kill Shot has dealt its damage.
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == hunter.KillShot {
				aura.Deactivate(sim)
			}
		},
	})
	inExecute := hunter.KillShot.ExtraCastCondition
	hunter.KillShot.ExtraCastCondition = func(sim *core.Simulation, target *core.Unit) bool {
		return killingEye.IsActive() || inExecute(sim, target)
	}

	// Misdirection (spell 34477: 9% of base mana, 30 sec cooldown, on the GCD). The sim doesn't model
	// threat, so it only exists for the ring. As a cooldown it is cast when Kill Shot will be ready by the
	// end of its GCD.
	misdirection := hunter.RegisterSpell(core.SpellConfig{
		ActionID: core.ActionID{SpellID: 34477},

		ManaCost: core.ManaCostOptions{
			BaseCost: 0.09,
		},
		Cast: core.CastConfig{
			DefaultCast: core.Cast{
				GCD: core.GCDDefault,
			},
			IgnoreHaste: true,
			CD: core.Cooldown{
				Timer:    hunter.NewTimer(),
				Duration: time.Second * 30,
			},
		},

		ApplyEffects: func(sim *core.Simulation, _ *core.Unit, _ *core.Spell) {
			killingEye.Activate(sim)
		},
	})
	hunter.AddMajorCooldown(core.MajorCooldown{
		Spell: misdirection,
		Type:  core.CooldownTypeDPS,
		ShouldActivate: func(sim *core.Simulation, character *core.Character) bool {
			return hunter.KillShot.CD.TimeToReady(sim) <= core.GCDDefault
		},
	})
}

func (hunter *Hunter) registerStampede() {
	callOfTheWild := hunter.GetSpell(core.ActionID{SpellID: 53434})
	if callOfTheWild == nil {
		return
	}

	// Spell 200145: magic-class Physical damage to every enemy within 12 yards of the target.
	spiritCleave := hunter.registerMinionSpell(core.ActionID{SpellID: 200145}, core.SpellSchoolPhysical,
		func(sim *core.Simulation, _ *core.Unit, spell *core.Spell) {
			damage := wildHuntCleaveAP * hunter.scriptAP()
			for _, aoeTarget := range sim.Encounter.TargetUnits {
				spell.CalcAndDealDamage(sim, aoeTarget, damage, spell.OutcomeMagicHitAndCrit)
			}
		})

	var beasts *core.PendingAction
	stampede := hunter.RegisterAura(core.Aura{
		Label:    "Stampede",
		ActionID: core.ActionID{SpellID: 200143},
		Duration: wildHuntStampedeTime,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			beasts = hunter.summonBeast(sim, spiritCleave, wildHuntCleaveInterval, wildHuntStampedeTime, wildHuntBeasts)
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			beasts.Cancel(sim)
		},
	})

	hunter.alwaysActive(core.Aura{
		Label: "Vorrax's Wild Hunt",
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == callOfTheWild {
				stampede.Deactivate(sim)
				stampede.Activate(sim)
			}
		},
	})
}

func (hunter *Hunter) registerPackLeader() {
	if hunter.KillCommand == nil {
		return
	}
	pet := hunter.pet

	// Wyvern: Skyhunter's Cry (spell 200147, Steady Shot +51%) and Poison Bolt (spell 200148, magic-class
	// Nature).
	poisonBolt := hunter.registerMinionSpell(core.ActionID{SpellID: 200148}, core.SpellSchoolNature,
		func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.CalcAndDealDamage(sim, target, packLeaderPoisonBoltAP*hunter.scriptAP(), spell.OutcomeMagicHitAndCrit)
		})
	var wyvernAttacks *core.PendingAction
	wyvern := hunter.RegisterAura(core.Aura{
		Label:    "Pack Leader: Wyvern",
		ActionID: core.ActionID{SpellID: 200147},
		Duration: packLeaderBeastTime,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			hunter.SteadyShot.DamageMultiplierAdditive += packLeaderSteadyShot
			wyvernAttacks = hunter.summonBeast(sim, poisonBolt, packLeaderPoisonBoltCD, packLeaderBeastTime, 1)
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			hunter.SteadyShot.DamageMultiplierAdditive -= packLeaderSteadyShot
			wyvernAttacks.Cancel(sim)
		},
	})

	// Bear: Might of the Elder (spell 200149 on the pet; its extra hit, spell 200150, is magic-class
	// Physical cast by the pet, so the pet's modifiers apply) and Heavy Cleave (spell 200151, magic-class
	// Physical to every enemy within 12 yards).
	elder := pet.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200150},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		CritMultiplier:   pet.DefaultSpellCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.CalcAndDealDamage(sim, target, packLeaderElderAP*hunter.scriptAP(), spell.OutcomeMagicHitAndCrit)
		},
	})
	mightOfTheElder := pet.RegisterAura(core.Aura{
		Label:    "Might of the Elder",
		ActionID: core.ActionID{SpellID: 200149},
		Duration: packLeaderBeastTime,
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			// Bite, Claw or Smack.
			if spell == pet.focusDump && result.Landed() {
				elder.Cast(sim, result.Target)
			}
		},
	})
	heavyCleave := hunter.registerMinionSpell(core.ActionID{SpellID: 200151}, core.SpellSchoolPhysical,
		func(sim *core.Simulation, _ *core.Unit, spell *core.Spell) {
			damage := packLeaderHeavyCleaveAP * hunter.scriptAP()
			for _, aoeTarget := range sim.Encounter.TargetUnits {
				spell.CalcAndDealDamage(sim, aoeTarget, damage, spell.OutcomeMagicHitAndCrit)
			}
		})
	var bearAttacks *core.PendingAction
	bear := hunter.RegisterAura(core.Aura{
		Label:    "Pack Leader: Bear",
		ActionID: core.ActionID{SpellID: 200151},
		Duration: packLeaderBeastTime,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			if pet.IsEnabled() {
				mightOfTheElder.Activate(sim)
			}
			bearAttacks = hunter.summonBeast(sim, heavyCleave, packLeaderHeavyCleaveCD, packLeaderBeastTime, 1)
		},
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			bearAttacks.Cancel(sim)
		},
	})

	// Boar: Relentless Pursuit (spell 200152): Arcane Shot makes the Boar Gouge the target. Gouge (spell
	// 200153) is periodic Physical damage without the Bleed mechanic, so armor applies and bleed
	// modifiers don't; the sim only exempts periodic Physical damage from armor as a bleed, so its ticks
	// are dealt as direct damage that can't miss or crit.
	gouge := hunter.registerMinionSpell(core.ActionID{SpellID: 200153}, core.SpellSchoolPhysical, nil)
	gouges := hunter.NewEnemyAuraArray(func(target *core.Unit) *core.Aura {
		var ticks *core.PendingAction
		return target.RegisterAura(core.Aura{
			Label:     "Gouge-" + strconv.Itoa(int(hunter.Index)),
			ActionID:  core.ActionID{SpellID: 200153},
			Duration:  packLeaderGougeDuration,
			MaxStacks: packLeaderGougeStacks,
			OnGain: func(aura *core.Aura, sim *core.Simulation) {
				ticks = core.StartPeriodicAction(sim, core.PeriodicActionOptions{
					Period: packLeaderGougeTick,
					OnAction: func(sim *core.Simulation) {
						damage := packLeaderGougeTickAP * hunter.scriptAP() * float64(aura.GetStacks())
						gouge.CalcAndDealDamage(sim, target, damage, gouge.OutcomeAlwaysHit)
					},
				})
			},
			OnExpire: func(aura *core.Aura, sim *core.Simulation) {
				ticks.Cancel(sim)
			},
		})
	})
	boar := hunter.RegisterAura(core.Aura{
		Label:    "Pack Leader: Boar",
		ActionID: core.ActionID{SpellID: 200152},
		Duration: packLeaderBeastTime,
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell == hunter.ArcaneShot && result.Landed() {
				gouge := gouges.Get(result.Target)
				gouge.Activate(sim)
				gouge.AddStack(sim)
			}
		},
	})

	// Kill Command summons them in turn. Each stays 30 sec, alongside the others.
	beasts := []*core.Aura{wyvern, bear, boar}
	next := 0
	hunter.RegisterResetEffect(func(sim *core.Simulation) {
		next = 0
	})
	killCommand := hunter.KillCommand.ApplyEffects
	hunter.KillCommand.ApplyEffects = func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
		killCommand(sim, target, spell)
		beast := beasts[next]
		next = (next + 1) % len(beasts)
		beast.Deactivate(sim)
		beast.Activate(sim)
	}
}

func (hunter *Hunter) registerDeadeye() {
	if hunter.AimedShot == nil {
		return
	}
	// The extra damage is added to Aimed Shot's base damage (aimed_shot.go, cc.aimedShotBonusAP). The
	// server adds 5 sec to the cooldown on each Aimed Shot hit; the sim adds it to every cast.
	hunter.AimedShot.CD.Duration += deadeyeCDIncrease

	// Proc flag 0x100: every other ranged ability that lands. Aimed Shot shares its cooldown timer with
	// Multi-Shot in the sim, so Multi-Shot's cooldown shortens too.
	hunter.alwaysActive(core.Aura{
		Label:    "Deadeye's Oath",
		ActionID: core.ActionID{SpellID: 200154},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if result.Landed() && spell != hunter.AimedShot && hunter.isRangedAbility(spell) {
				reduceCooldown(sim, hunter.AimedShot, deadeyeCDPerShot)
			}
		},
	})
}

// Withering Arrow tick n (1-based) deals bansheeTickAP * (1 + bansheeRampPerTick * (n-1)) of scriptAP.
func bansheeRamp(tick int32) float64 {
	return 1 + bansheeRampPerTick*float64(tick-1)
}

func (hunter *Hunter) registerWitheringArrow() {
	if hunter.SilencingShot == nil || hunter.ChimeraShot == nil {
		return
	}

	// Spell 200157: Shadow periodic damage, 8 ticks 1 sec apart, snapshotted when applied.
	witheringArrow := hunter.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 200157},
		SpellSchool: core.SpellSchoolShadow,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Withering Arrow",
			},
			NumberOfTicks: bansheeTicks,
			TickLength:    bansheeTickLength,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				dot.SnapshotBaseDamage = bansheeTickAP * hunter.scriptAP()
				dot.SnapshotAttackerMultiplier = dot.Spell.AttackerDamageMultiplier(dot.Spell.Unit.AttackTables[target.UnitIndex])
			},
			OnTick: func(sim *core.Simulation, target *core.Unit, dot *core.Dot) {
				base := dot.SnapshotBaseDamage
				dot.SnapshotBaseDamage = base * bansheeRamp(dot.TickCount)
				dot.CalcAndDealPeriodicSnapshotDamage(sim, target, dot.OutcomeTick)
				dot.SnapshotBaseDamage = base
			},
		},

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			spell.Dot(target).Apply(sim)
		},
	})

	// Spells 200158 (the remaining ticks at once) and 200159 (the eruption, every enemy within 8 yards):
	// magic-class Shadow damage from the hunter, with the unmodified tick amounts as base points.
	newBurst := func(spellID int32) *core.Spell {
		return hunter.RegisterSpell(core.SpellConfig{
			ActionID:    core.ActionID{SpellID: spellID},
			SpellSchool: core.SpellSchoolShadow,
			ProcMask:    core.ProcMaskEmpty,
			Flags:       core.SpellFlagNoOnCastComplete,

			DamageMultiplier: 1,
			CritMultiplier:   hunter.DefaultSpellCritMultiplier(),
			ThreatMultiplier: 1,
		})
	}
	burst := newBurst(200158)
	eruption := newBurst(200159)

	hunter.alwaysActive(core.Aura{
		Label:    "Loop of the Banshee",
		ActionID: core.ActionID{SpellID: 200156},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() {
				return
			}
			switch spell {
			case hunter.SilencingShot:
				witheringArrow.Cast(sim, result.Target)
			case hunter.ChimeraShot:
				dot := witheringArrow.Dot(result.Target)
				if !dot.IsActive() {
					return
				}
				remaining := 0.0
				for tick := dot.TickCount + 1; tick <= dot.NumberOfTicks; tick++ {
					remaining += dot.SnapshotBaseDamage * bansheeRamp(tick)
				}
				dot.Deactivate(sim)
				burst.CalcAndDealDamage(sim, result.Target, remaining, burst.OutcomeMagicHitAndCrit)
				for _, aoeTarget := range sim.Encounter.TargetUnits {
					eruption.CalcAndDealDamage(sim, aoeTarget, bansheeEruptionShare*remaining, eruption.OutcomeMagicHitAndCrit)
				}
			}
		},
	})
}

func (hunter *Hunter) registerWildfireBomb() {
	// Spell 200161: ranged-class Fire damage, 18 sec cooldown, no GCD, no cost, a 40 yd/s missile. The
	// explosion grants the Lock and Load buff directly, without the talent's proc cooldown; without the
	// talent the sim has no Lock and Load to grant.
	bomb := hunter.RegisterSpell(core.SpellConfig{
		ActionID:     core.ActionID{SpellID: 200161},
		SpellSchool:  core.SpellSchoolFire,
		ProcMask:     core.ProcMaskRangedSpecial,
		Flags:        core.SpellFlagMeleeMetrics,
		MissileSpeed: 40,

		Cast: core.CastConfig{
			CD: core.Cooldown{
				Timer:    hunter.NewTimer(),
				Duration: wildfireBombCooldown,
			},
		},

		DamageMultiplier: 1,
		CritMultiplier:   hunter.critMultiplier(false, false, false),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			result := spell.CalcDamage(sim, target, wildfireBombAP*hunter.scriptAP(), spell.OutcomeRangedHitAndCrit)
			spell.WaitTravelTime(sim, func(sim *core.Simulation) {
				spell.DealDamage(sim, result)
				if hunter.LockAndLoadAura != nil {
					hunter.LockAndLoadAura.Activate(sim)
					hunter.LockAndLoadAura.SetStacks(sim, 2)
				}
			})
		},
	})

	// Used on cooldown, but not while Lock and Load charges would be overwritten.
	hunter.AddMajorCooldown(core.MajorCooldown{
		Spell: bomb,
		Type:  core.CooldownTypeDPS,
		ShouldActivate: func(sim *core.Simulation, character *core.Character) bool {
			return hunter.LockAndLoadAura == nil || !hunter.LockAndLoadAura.IsActive()
		},
	})
}

func (hunter *Hunter) registerDarkMinion() {
	if hunter.BlackArrow == nil {
		return
	}

	// Spell 200163: magic-class Shadow damage from the minion.
	darkEcho := hunter.registerMinionSpell(core.ActionID{SpellID: 200163}, core.SpellSchoolShadow, nil)

	var dealt float64
	var fadesAt time.Duration
	addDamage := func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
		if spell != darkEcho {
			dealt += result.Damage
		}
	}
	darkMinion := hunter.RegisterAura(core.Aura{
		Label:    "Dark Minion",
		ActionID: core.ActionID{SpellID: 200162},
		Duration: darkMinionDuration,
		OnGain: func(aura *core.Aura, sim *core.Simulation) {
			dealt = 0
			fadesAt = sim.CurrentTime + aura.Duration
		},
		OnSpellHitDealt:       addDamage,
		OnPeriodicDamageDealt: addDamage,
		OnExpire: func(aura *core.Aura, sim *core.Simulation) {
			// Not when the fight ends first.
			if sim.CurrentTime >= fadesAt && dealt > 0 {
				darkEcho.CalcAndDealDamage(sim, hunter.CurrentTarget, darkMinionEcho*dealt, darkEcho.OutcomeMagicHitAndCrit)
			}
		},
	})

	hunter.alwaysActive(core.Aura{
		Label: "Signet of Dark Deeds",
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if spell == hunter.BlackArrow && result.Landed() {
				darkMinion.Activate(sim)
			}
		},
	})
}

// Grizzlemaw Tracker's Garb (custom beast mastery set 9308: neck, ring, cloak, ranged weapon).
var ItemSetGrizzlemawTrackersGarb = core.NewItemSet(core.ItemSet{
	Name: "Grizzlemaw Tracker's Garb",
	Bonuses: map[int32]core.ApplyEffect{
		// Your shots have a 22% chance to apply Barbed Shot to the target, causing them to bleed for 35%
		// of your attack power over 6 sec. Each time Barbed Shot is applied, your pet stomps the ground,
		// dealing 30% of your attack power as physical damage to all nearby enemies.
		2: func(agent core.Agent) {
			hunter := agent.(HunterAgent).GetHunter()
			hunter.Env.RegisterPreFinalizeEffect(hunter.registerBarbedShot)
		},
		// Each time your pet Stomps, reduce the cooldown of Call of the Wild and Rapid Fire by 2 sec, and
		// Bestial Wrath and Kill Command by 1 sec. Handled in registerBarbedShot.
		4: func(agent core.Agent) {
			agent.(HunterAgent).GetHunter().cc.barbedShotFourPiece = true
		},
	},
})

func (hunter *Hunter) registerBarbedShot() {
	// Runs before finalization, after every set bonus has been applied.
	hasFourPiece := hunter.cc.barbedShotFourPiece

	// Spell 900372: a bleed (Physical periodic damage, Bleed mechanic) from the hunter, snapshotted
	// when applied; a new application replaces the old one.
	barbedShot := hunter.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 900372},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskEmpty,
		Flags:       core.SpellFlagNoOnCastComplete,

		DamageMultiplier: 1,
		ThreatMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label: "Barbed Shot",
			},
			NumberOfTicks: barbedShotTicks,
			TickLength:    barbedShotTick,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				dot.SnapshotBaseDamage = barbedShotAP * hunter.scriptAP() / barbedShotTicks
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

	// Spell 900373: melee-class Physical damage from the pet to every enemy near it, so the pet's
	// modifiers, hit and crit apply.
	pet := hunter.pet
	var stomp *core.Spell
	if pet != nil {
		stomp = pet.RegisterSpell(core.SpellConfig{
			ActionID:    core.ActionID{SpellID: 900373},
			SpellSchool: core.SpellSchoolPhysical,
			ProcMask:    core.ProcMaskEmpty,
			Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

			DamageMultiplier: 1,
			CritMultiplier:   pet.DefaultMeleeCritMultiplier(),
			ThreatMultiplier: 1,

			ApplyEffects: func(sim *core.Simulation, _ *core.Unit, spell *core.Spell) {
				damage := stompAP * hunter.scriptAP()
				for _, aoeTarget := range sim.Encounter.TargetUnits {
					spell.CalcAndDealDamage(sim, aoeTarget, damage, spell.OutcomeMeleeSpecialHitAndCrit)
				}
			},
		})
	}
	callOfTheWild := hunter.GetSpell(core.ActionID{SpellID: 53434})
	bestialWrath := hunter.GetSpell(core.ActionID{SpellID: 19574})

	// Proc flags 0x40 and 0x100: Auto Shot and the hunter's ranged abilities, when they land.
	hunter.alwaysActive(core.Aura{
		Label:    "Grizzlemaw Tracker's Garb",
		ActionID: core.ActionID{SpellID: 900370},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !result.Landed() || !(spell == hunter.AutoAttacks.RangedAuto() || hunter.isRangedAbility(spell)) {
				return
			}
			if !sim.Proc(barbedShotChance, "Barbed Shot") {
				return
			}
			barbedShot.Cast(sim, result.Target)
			if stomp == nil || !pet.IsEnabled() {
				return
			}
			stomp.Cast(sim, result.Target)
			if hasFourPiece {
				reduceCooldown(sim, callOfTheWild, stompCotWRapidFire)
				reduceCooldown(sim, hunter.RapidFire, stompCotWRapidFire)
				reduceCooldown(sim, bestialWrath, stompBWKillCommand)
				reduceCooldown(sim, hunter.KillCommand, stompBWKillCommand)
				hunter.UpdateMajorCooldowns()
			}
		},
	})
}

// Warden of the Silent Canopy (custom marksmanship set 9309: neck, ring, cloak, ranged weapon).
var ItemSetWardenOfTheSilentCanopy = core.NewItemSet(core.ItemSet{
	Name: "Warden of the Silent Canopy",
	Bonuses: map[int32]core.ApplyEffect{
		// Steady Shot grants Marked Quarry, increasing the damage of Steady Shot by 2% per stack, stacking
		// up to 5 times. Lasts 30 sec.
		2: func(agent core.Agent) {
			hunter := agent.(HunterAgent).GetHunter()
			hunter.Env.RegisterPreFinalizeEffect(hunter.registerMarkedQuarry)
		},
		// Aimed Shot consumes all stacks of Marked Quarry, dealing 20% of your attack power as additional
		// damage per stack, and causes your next Arcane Shot to deal 50% more damage and cost no mana.
		// Handled in registerMarkedQuarry.
		4: func(agent core.Agent) {
			agent.(HunterAgent).GetHunter().cc.markedQuarryFourPiece = true
		},
	},
})

func (hunter *Hunter) registerMarkedQuarry() {
	// Runs before finalization, after every set bonus has been applied.
	hasFourPiece := hunter.cc.markedQuarryFourPiece && hunter.AimedShot != nil

	// Spell 900382: +2% Steady Shot damage per stack (SPELLMOD_DAMAGE, bp 1), 5 stacks, 30 sec.
	markedQuarry := hunter.RegisterAura(core.Aura{
		Label:     "Marked Quarry",
		ActionID:  core.ActionID{SpellID: 900382},
		Duration:  markedQuarryDuration,
		MaxStacks: markedQuarryMaxStacks,
		OnStacksChange: func(aura *core.Aura, sim *core.Simulation, oldStacks int32, newStacks int32) {
			hunter.SteadyShot.DamageMultiplierAdditive += markedQuarryPerStack * float64(newStacks-oldStacks)
		},
	})

	// Spell 900383: ranged-class Physical damage from the hunter. Spell 900384: the next Arcane Shot
	// deals +50% damage (SPELLMOD_DAMAGE, bp 49) and costs no mana (SPELLMOD_COST, bp -101), 30 sec.
	var quarryShot *core.Spell
	var freeArcaneShot *core.Aura
	if hasFourPiece {
		quarryShot = hunter.RegisterSpell(core.SpellConfig{
			ActionID:    core.ActionID{SpellID: 900383},
			SpellSchool: core.SpellSchoolPhysical,
			ProcMask:    core.ProcMaskEmpty,
			Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagNoOnCastComplete,

			DamageMultiplier: 1,
			CritMultiplier:   hunter.critMultiplier(false, false, false),
			ThreatMultiplier: 1,
		})
		freeArcaneShot = hunter.RegisterAura(core.Aura{
			Label:    "Marked Quarry (Arcane Shot)",
			ActionID: core.ActionID{SpellID: 900384},
			Duration: markedQuarryArcaneWindow,
			OnGain: func(aura *core.Aura, sim *core.Simulation) {
				hunter.ArcaneShot.DamageMultiplierAdditive += markedQuarryArcaneBonus
				hunter.ArcaneShot.CostMultiplier -= 1
			},
			OnExpire: func(aura *core.Aura, sim *core.Simulation) {
				hunter.ArcaneShot.DamageMultiplierAdditive -= markedQuarryArcaneBonus
				hunter.ArcaneShot.CostMultiplier += 1
			},
			OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
				if spell == hunter.ArcaneShot {
					aura.Deactivate(sim)
				}
			},
		})
	}

	hunter.alwaysActive(core.Aura{
		Label:    "Warden of the Silent Canopy",
		ActionID: core.ActionID{SpellID: 900380},
		// OnCastComplete runs after Steady Shot has dealt its damage.
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if spell == hunter.SteadyShot {
				markedQuarry.Activate(sim)
				markedQuarry.AddStack(sim)
			}
		},
		OnSpellHitDealt: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
			if !hasFourPiece || spell != hunter.AimedShot || !result.Landed() {
				return
			}
			if stacks := markedQuarry.GetStacks(); stacks > 0 {
				markedQuarry.Deactivate(sim)
				damage := markedQuarryAimedAP * float64(stacks) * hunter.scriptAP()
				quarryShot.CalcAndDealDamage(sim, result.Target, damage, quarryShot.OutcomeRangedCritOnly)
			}
			freeArcaneShot.Activate(sim)
		},
	})
}

// Nightmare Stalker's Trappings (custom survival set 9310: neck, ring, cloak, ranged weapon).
var ItemSetNightmareStalkersTrappings = core.NewItemSet(core.ItemSet{
	Name: "Nightmare Stalker's Trappings",
	Bonuses: map[int32]core.ApplyEffect{
		// Consuming Lock and Load with Explosive Shot has a 45% chance to summon a Sentinel Owl, applying
		// Sentinel's Mark to your target. Your next Explosive Shot deals 40% increased damage to the
		// marked target.
		2: func(agent core.Agent) {
			hunter := agent.(HunterAgent).GetHunter()
			hunter.Env.RegisterPreFinalizeEffect(hunter.registerSentinelsMark)
		},
		// When Sentinel's Mark is consumed, it summons a barrage of 4 lunar missiles, each dealing 30% of
		// your attack power as Arcane damage to enemies within 10 yards. Handled in registerSentinelsMark.
		4: func(agent core.Agent) {
			agent.(HunterAgent).GetHunter().cc.sentinelsMarkFourPiece = true
		},
	},
})

func (hunter *Hunter) isExplosiveShot(spell *core.Spell) bool {
	return spell != nil && (spell == hunter.ExplosiveShotR4 || spell == hunter.ExplosiveShotR3)
}

func (hunter *Hunter) registerSentinelsMark() {
	if hunter.LockAndLoadAura == nil || hunter.ExplosiveShotR4 == nil {
		return
	}

	// Spell 900394: ranged-class Arcane damage from the hunter to every enemy within 10 yards.
	var lunarMissile *core.Spell
	if hunter.cc.sentinelsMarkFourPiece {
		lunarMissile = hunter.RegisterSpell(core.SpellConfig{
			ActionID:    core.ActionID{SpellID: 900394},
			SpellSchool: core.SpellSchoolArcane,
			ProcMask:    core.ProcMaskEmpty,
			Flags:       core.SpellFlagNoOnCastComplete,

			DamageMultiplier: 1,
			CritMultiplier:   hunter.critMultiplier(false, false, false),
			ThreatMultiplier: 1,

			ApplyEffects: func(sim *core.Simulation, _ *core.Unit, spell *core.Spell) {
				damage := lunarMissileAP * hunter.scriptAP()
				for _, aoeTarget := range sim.Encounter.TargetUnits {
					spell.CalcAndDealDamage(sim, aoeTarget, damage, spell.OutcomeRangedHitAndCrit)
				}
			},
		})
	}

	// Spell 900392: the mark, until an Explosive Shot consumes it. The script scales that Explosive
	// Shot's damage, so the bonus multiplies (it doesn't add to TNT). The sim gives it to Explosive Shot
	// on any target while the mark is up.
	marks := hunter.NewEnemyAuraArray(func(target *core.Unit) *core.Aura {
		return target.RegisterAura(core.Aura{
			Label:    "Sentinel's Mark-" + strconv.Itoa(int(hunter.Index)),
			ActionID: core.ActionID{SpellID: 900392},
			Duration: core.NeverExpires,
			OnGain: func(aura *core.Aura, sim *core.Simulation) {
				hunter.ExplosiveShotR4.DamageMultiplier *= 1 + sentinelsMarkBonus
				hunter.ExplosiveShotR3.DamageMultiplier *= 1 + sentinelsMarkBonus
			},
			OnExpire: func(aura *core.Aura, sim *core.Simulation) {
				hunter.ExplosiveShotR4.DamageMultiplier /= 1 + sentinelsMarkBonus
				hunter.ExplosiveShotR3.DamageMultiplier /= 1 + sentinelsMarkBonus
			},
		})
	})

	// Explosive Shot removes a Lock and Load charge from the Lock and Load aura's own hit callback.
	consumedLockAndLoad := false
	lockAndLoadOnHit := hunter.LockAndLoadAura.OnSpellHitDealt
	hunter.LockAndLoadAura.OnSpellHitDealt = func(aura *core.Aura, sim *core.Simulation, spell *core.Spell, result *core.SpellResult) {
		stacks := aura.GetStacks()
		lockAndLoadOnHit(aura, sim, spell, result)
		if hunter.isExplosiveShot(spell) && aura.GetStacks() < stacks {
			consumedLockAndLoad = true
		}
	}

	hunter.RegisterResetEffect(func(sim *core.Simulation) {
		consumedLockAndLoad = false
	})
	hunter.alwaysActive(core.Aura{
		Label:    "Nightmare Stalker's Trappings",
		ActionID: core.ActionID{SpellID: 900390},
		// OnCastComplete runs after Explosive Shot has snapshotted its damage: the mark applied by this
		// Explosive Shot is for the next one.
		OnCastComplete: func(aura *core.Aura, sim *core.Simulation, spell *core.Spell) {
			if !hunter.isExplosiveShot(spell) {
				return
			}
			target := hunter.CurrentTarget
			mark := marks.Get(target)
			if mark.IsActive() {
				mark.Deactivate(sim)
				if lunarMissile != nil {
					for i := 0; i < lunarMissiles; i++ {
						lunarMissile.Cast(sim, target)
					}
				}
			}
			if consumedLockAndLoad {
				consumedLockAndLoad = false
				if sim.Proc(sentinelsMarkChance, "Sentinel's Mark") {
					mark.Activate(sim)
				}
			}
		},
	})
}
