package druid

import (
	"testing"

	. "github.com/wowsims/wotlk/sim/common/cc/cctuning"
)

// Checks the constants in cc_items.go against the server's published tuning
// (assets/db_inputs/cc/server_sim_data.json, refreshed by ./build.ps1 tuning).
func TestServerTuning(t *testing.T) {
	Check(t, []string{"dru_balance", "dru_cat", "dru_bear", "900108", "900115"}, []Param{
		P("dru_balance", "burst_chance", "dreamBurstChance", Pct(dreamBurstChance)),
		P("dru_balance", "burst_sp_pct", "dreamBurstSP", Pct(dreamBurstSP)),
		P("dru_balance", "elune_chance", "powerOfEluneChance", Pct(powerOfEluneChance)),
		P("dru_balance", "elune_dur", "powerOfEluneDuration", Sec(powerOfEluneDuration)),
		P("dru_balance", "elune_icd", "powerOfEluneICD", Sec(powerOfEluneICD)),
		P("dru_balance", "flat_pct", "canopyDreamerDamageBonus", Pct(canopyDreamerDamageBonus-1)),

		P("dru_cat", "proc_chance", "bloodseekerVinesProcChance", Pct(bloodseekerVinesProcChance)),
		P("dru_cat", "icd", "bloodseekerVinesICD", Sec(bloodseekerVinesICD)),
		P("dru_cat", "vine_ap_pct", "bloodseekerVinesAP", Pct(bloodseekerVinesAP)),
		P("dru_cat", "explode_ap_pct", "bloodseekerThornsAP", Pct(bloodseekerThornsAP)),

		P("dru_bear", "emp_chance", "empoweredMaulChance", Pct(empoweredMaulChance)),
		P("dru_bear", "maul_primary_ap_pct", "empoweredMaulPrimaryAP", Pct(empoweredMaulPrimaryAP)),
		P("dru_bear", "maul_cleave_ap_pct", "empoweredMaulCleaveAP", Pct(empoweredMaulCleaveAP)),
		P("dru_bear", "ravage_chance", "ravageChance", Pct(ravageChance)),
		P("dru_bear", "ravage_mult_pct", "ravageMultiplier", Pct(ravageMultiplier)),

		// Skareth's Falling Star
		P("900108", "channel", "furyOfTheGoddessCasts * furyOfTheGoddessTick", furyOfTheGoddessCasts*Sec(furyOfTheGoddessTick)),
		P("900108", "casts", "furyOfTheGoddessCasts", furyOfTheGoddessCasts),
		P("900108", "starfire_chance", "furyOfTheGoddessStarfire", Pct(furyOfTheGoddessStarfire)),

		// Morgrath's Ravaging Claw
		P("900115", "chance_per_cp", "ravagingClawChancePerPoint", Pct(ravagingClawChancePerPoint)),
		P("900115", "shred_mangle_bonus_pct", "ravagingClawBuilderBonus", Pct(ravagingClawBuilderBonus)),
	}, nil)
}
