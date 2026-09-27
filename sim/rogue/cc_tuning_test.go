package rogue

import (
	"testing"

	. "github.com/wowsims/wotlk/sim/common/cc/cctuning"
)

// Checks the constants in cc_items.go against the server's published tuning
// (assets/db_inputs/cc/server_sim_data.json, refreshed by ./build.ps1 tuning).
func TestServerTuning(t *testing.T) {
	Check(t, []string{"rog_assa", "rog_combat", "rog_sub", "900119", "900142", "900143", "900144", "900145", "900146", "900147"}, []Param{
		P("rog_assa", "stacks", "deathstalkersMarkStacks", deathstalkersMarkStacks),
		P("rog_assa", "mark_dur", "deathstalkersMarkDuration", Sec(deathstalkersMarkDuration)),
		P("rog_assa", "min_cp", "deathstalkersMarkMinPoints", deathstalkersMarkMinPoints),
		P("rog_assa", "mark_ap_pct", "deathstalkersMarkAP", Pct(deathstalkersMarkAP)),
		P("rog_assa", "vanish_cdr", "deathstalkersMarkVanishCDR", Sec(deathstalkersMarkVanishCDR)),

		P("rog_combat", "evis_pct_per_stack", "flawlessFormPerStack", Pct(flawlessFormPerStack)),
		P("rog_combat", "cap", "flawlessFormMaxStacks", flawlessFormMaxStacks),
		P("rog_combat", "dur", "flawlessFormDuration", Sec(flawlessFormDuration)),
		P("rog_combat", "flurry_primary_ap_pct", "flawlessFlurryPrimaryAP", Pct(flawlessFlurryPrimaryAP)),
		P("rog_combat", "flurry_secondary_ap_pct", "flawlessFlurrySecondaryAP", Pct(flawlessFlurrySecondaryAP)),
		P("rog_combat", "bf_bonus_pct", "flawlessFlurryBladeFlurryMod", Pct(flawlessFlurryBladeFlurryMod-1)),

		P("rog_sub", "hemo_ap_pct", "shadowStrikesHemorrhageAP", Pct(shadowStrikesHemorrhageAP)),
		P("rog_sub", "dance_ap_pct", "shadowStrikesDanceAP", Pct(shadowStrikesDanceAP)),
		P("rog_sub", "debuff_pct", "shadowOfTheCanopyBonus", Pct(shadowOfTheCanopyBonus)),
		P("rog_sub", "debuff_dur", "shadowOfTheCanopyDuration", Sec(shadowOfTheCanopyDuration)),

		// Vorrax's Fatebound Coin
		P("900119", "min_cp", "fateboundCoinMinPoints", fateboundCoinMinPoints),
		P("900119", "heads_base_pct", "fateboundHeadsBase", Pct(fateboundHeadsBase)),
		P("900119", "heads_per_stack_pct", "fateboundHeadsPerFlip", Pct(fateboundHeadsPerFlip)),
		P("900119", "heads_duration", "fateboundHeadsDuration", Sec(fateboundHeadsDuration)),
		P("900119", "tails_ap_pct", "fateboundTailsAP", Pct(fateboundTailsAP)),
		P("900119", "tails_per_streak_pct", "fateboundTailsPerFlip", Pct(fateboundTailsPerFlip)),

		// Roll the Bones
		P("900142", "chance_per_cp", "rollTheBonesChancePerPoint", Pct(rollTheBonesChancePerPoint)),
		P("900142", "duration", "rollTheBonesDuration", Sec(rollTheBonesDuration)),
		P("900142", "buried_treasure_regen_pct", "buriedTreasureRegen", Pct(buriedTreasureRegen)),
		P("900142", "grand_melee_haste_pct", "grandMeleeHaste", Pct(grandMeleeHaste-1)),
		P("900142", "ruthless_precision_crit_pct", "ruthlessPrecisionCrit", ruthlessPrecisionCrit),
		P("900142", "skull_extra_attack_chance", "skullAndCrossbonesChance", Pct(skullAndCrossbonesChance)),
		P("900142", "true_bearings_cdr_per_cp", "trueBearingsPerPoint", Sec(trueBearingsPerPoint)),

		// Between the Eyes
		P("900143", "ap_pct_per_cp", "betweenTheEyesAPPerPoint", Pct(betweenTheEyesAPPerPoint)),
		P("900143", "crit_multiplier", "betweenTheEyesCritMultiplier", betweenTheEyesCritMultiplier),

		// Venomdrinker
		P("900144", "shiv_cooldown", "venomdrinkerShivCooldown", Sec(venomdrinkerShivCooldown)),
		P("900144", "duration", "venomRushDuration", Sec(venomRushDuration)),
		P("900144", "regen_pct", "venomRushRegen", Pct(venomRushRegen)),
		P("900144", "poison_pct", "venomRushNatureDamage", Pct(venomRushNatureDamage-1)),

		// Sanguine Hunger
		P("900145", "energy_per_tick", "sanguineHungerEnergy", sanguineHungerEnergy),
		P("900145", "extension_per_cp", "sanguineHungerPerPoint", Sec(sanguineHungerPerPoint)),

		// Endless Dance
		P("900146", "cdr_per_cp", "endlessDanceCDRPerPoint", Sec(endlessDanceCDRPerPoint)),
		P("900146", "energy", "endlessDanceEnergy", endlessDanceEnergy),

		// Shadow's Invitation
		P("900147", "ambush_cost_reduction", "shadowsInvitationCostCut", shadowsInvitationCostCut),
		P("900147", "shadow_echo_pct", "shadowsInvitationEcho", Pct(shadowsInvitationEcho)),
	}, nil)
}
