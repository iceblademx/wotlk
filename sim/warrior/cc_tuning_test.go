package warrior

import (
	"testing"

	. "github.com/wowsims/wotlk/sim/common/cc/cctuning"
)

// Checks the constants in cc_items.go against the server's published tuning
// (assets/db_inputs/cc/server_sim_data.json, refreshed by ./build.ps1 tuning).
func TestServerTuning(t *testing.T) {
	Check(t, []string{"war_arms", "war_fury", "900116", "900126"}, []Param{
		P("war_arms", "reset_chance", "fatalMarkResetChance", Pct(fatalMarkResetChance)),
		P("war_arms", "ms_cost_red_pct", "fatalMarkCostReduction", Pct(fatalMarkCostReduction)),
		P("war_arms", "mark_chance", "fatalMarkChance", Pct(fatalMarkChance)),
		P("war_arms", "cap", "fatalMarkMaxStacks", fatalMarkMaxStacks),
		P("war_arms", "mark_dur", "fatalMarkDuration", Sec(fatalMarkDuration)),
		P("war_arms", "mark_ap_pct", "fatalMarkAP", Pct(fatalMarkAP)),

		P("war_fury", "bt_crit_pct", "bloodthirstCritBuffBonus", Pct(bloodthirstCritBuffBonus)),
		P("war_fury", "crit_buff_dur", "bloodthirstCritBuffDuration", Sec(bloodthirstCritBuffDuration)),
		P("war_fury", "bloodbath_chance", "bloodbathProcChance", Pct(bloodbathProcChance)),
		P("war_fury", "bloodbath_ap_pct", "bloodbathAP", Pct(bloodbathAP)),

		// Vorrax's Reckless Fury
		P("900116", "execute_pct_per_stack", "undyingFuryPerStack", Pct(undyingFuryPerStack)),
		P("900116", "max_stacks", "undyingFuryMaxStacks", undyingFuryMaxStacks),
		P("900116", "duration", "undyingFuryDuration", Sec(undyingFuryDuration)),
		P("900116", "sudden_death_chance", "recklessFurySuddenDeathChance", Pct(recklessFurySuddenDeathChance)),

		// Rok'thul's Slayer's Edge
		P("900126", "proc_chance", "slayersEdgeProcChance", Pct(slayersEdgeProcChance)),
		P("900126", "strike_ap_pct", "slayersStrikeAP", Pct(slayersStrikeAP)),
		P("900126", "bloodthirst_pct_per_stack", "slayerPerStack", Pct(slayerPerStack)),
		P("900126", "stack_duration", "slayerDuration", Sec(slayerDuration)),
	}, nil)
}
