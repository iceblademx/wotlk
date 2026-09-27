package hunter

import (
	"testing"

	. "github.com/wowsims/wotlk/sim/common/cc/cctuning"
)

// Checks the constants in cc_items.go against the server's published tuning
// (assets/db_inputs/cc/server_sim_data.json, refreshed by ./build.ps1 tuning).
func TestServerTuning(t *testing.T) {
	Check(t, []string{"hun_bm", "hun_mm", "hun_surv", "900107", "900133", "900134", "900135", "900136", "900137", "900138"}, []Param{
		P("hun_bm", "proc_chance", "barbedShotChance", Pct(barbedShotChance)),
		P("hun_bm", "bleed_ap_pct", "barbedShotAP", Pct(barbedShotAP)),
		P("hun_bm", "stomp_ap_pct", "stompAP", Pct(stompAP)),
		P("hun_bm", "cdr_cotw_rf", "stompCotWRapidFire", Sec(stompCotWRapidFire)),
		P("hun_bm", "cdr_bw_kc", "stompBWKillCommand", Sec(stompBWKillCommand)),

		P("hun_mm", "ss_pct_per_stack", "markedQuarryPerStack", Pct(markedQuarryPerStack)),
		P("hun_mm", "cap", "markedQuarryMaxStacks", markedQuarryMaxStacks),
		P("hun_mm", "quarry_dur", "markedQuarryDuration", Sec(markedQuarryDuration)),
		P("hun_mm", "aimed_ap_pct_per_stack", "markedQuarryAimedAP", Pct(markedQuarryAimedAP)),
		P("hun_mm", "arcane_bonus_pct", "markedQuarryArcaneBonus", Pct(markedQuarryArcaneBonus)),
		P("hun_mm", "arcane_buff_dur", "markedQuarryArcaneWindow", Sec(markedQuarryArcaneWindow)),

		P("hun_surv", "proc_chance", "sentinelsMarkChance", Pct(sentinelsMarkChance)),
		P("hun_surv", "mark_bonus_pct", "sentinelsMarkBonus", Pct(sentinelsMarkBonus)),
		P("hun_surv", "missiles", "lunarMissiles", lunarMissiles),
		P("hun_surv", "missile_ap_pct", "lunarMissileAP", Pct(lunarMissileAP)),

		// Skareth's Killing Eye
		P("900107", "kill_shot_bonus_pct", "killingEyeBonus", Pct(killingEyeBonus)),
		P("900107", "duration", "killingEyeDuration", Sec(killingEyeDuration)),

		// Vorrax's Wild Hunt
		P("900133", "pet_arp_share_pct", "wildHuntArPShare", Pct(wildHuntArPShare)),
		P("900133", "pet_crit_share_pct", "wildHuntCritShare", Pct(wildHuntCritShare)),
		P("900133", "pet_haste_share_pct", "wildHuntHasteShare", Pct(wildHuntHasteShare)),
		P("900133", "stampede_beasts", "wildHuntBeasts", wildHuntBeasts),
		P("900133", "stampede_duration", "wildHuntStampedeTime", Sec(wildHuntStampedeTime)),
		P("900133", "cleave_interval", "wildHuntCleaveInterval", Sec(wildHuntCleaveInterval)),
		P("900133", "cleave_ap_pct", "wildHuntCleaveAP", Pct(wildHuntCleaveAP)),

		// Pack Leader's Insignia
		P("900134", "beast_duration", "packLeaderBeastTime", Sec(packLeaderBeastTime)),
		P("900134", "steady_shot_pct", "packLeaderSteadyShot", Pct(packLeaderSteadyShot)),
		P("900134", "poison_bolt_ap_pct", "packLeaderPoisonBoltAP", Pct(packLeaderPoisonBoltAP)),
		P("900134", "poison_bolt_interval", "packLeaderPoisonBoltCD", Sec(packLeaderPoisonBoltCD)),
		P("900134", "elder_pet_ap_pct", "packLeaderElderAP", Pct(packLeaderElderAP)),
		P("900134", "heavy_cleave_ap_pct", "packLeaderHeavyCleaveAP", Pct(packLeaderHeavyCleaveAP)),
		P("900134", "heavy_cleave_interval", "packLeaderHeavyCleaveCD", Sec(packLeaderHeavyCleaveCD)),
		P("900134", "gouge_tick_ap_pct", "packLeaderGougeTickAP", Pct(packLeaderGougeTickAP)),
		P("900134", "gouge_max_stacks", "packLeaderGougeStacks", packLeaderGougeStacks),
		P("900134", "gouge_duration", "packLeaderGougeDuration", Sec(packLeaderGougeDuration)),

		// Deadeye's Oath
		P("900135", "ap_pct", "deadeyeAP", Pct(deadeyeAP)),
		P("900135", "cd_increase", "deadeyeCDIncrease", Ms(deadeyeCDIncrease)),
		P("900135", "cd_reduction", "deadeyeCDPerShot", Ms(deadeyeCDPerShot)),

		// Loop of the Banshee
		P("900136", "base_tick_ap_pct", "bansheeTickAP", Pct(bansheeTickAP)),
		P("900136", "ramp_per_tick", "bansheeRampPerTick", Pct(bansheeRampPerTick)),
		P("900136", "ticks", "bansheeTicks", bansheeTicks),
		P("900136", "eruption_share", "bansheeEruptionShare", Pct(bansheeEruptionShare)),

		// Bombardier's Pin
		P("900137", "bomb_ap_pct", "wildfireBombAP", Pct(wildfireBombAP)),

		// Signet of Dark Deeds
		P("900138", "duration", "darkMinionDuration", Sec(darkMinionDuration)),
		P("900138", "echo_pct", "darkMinionEcho", Pct(darkMinionEcho)),
	}, map[string]string{
		"900136.eruption_radius": "the sim has no positions; the eruption hits every target",
	})
}
