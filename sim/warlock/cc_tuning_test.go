package warlock

import (
	"testing"

	. "github.com/wowsims/wotlk/sim/common/cc/cctuning"
)

// Checks the constants in cc_items.go against the server's published tuning
// (assets/db_inputs/cc/server_sim_data.json, refreshed by ./build.ps1 tuning).
func TestServerTuning(t *testing.T) {
	Check(t, []string{"wl_affl", "wl_demo", "wl_destro", "900106", "900125", "900127", "900128", "900129"}, []Param{
		P("wl_affl", "proc_chance", "soulAnathemaChance", Pct(soulAnathemaChance)),
		P("wl_affl", "anathema_sp_pct", "soulAnathemaSP", Pct(soulAnathemaSP)),
		P("wl_affl", "bonus_pct", "soulAnathemaFourPieceBonus", Pct(soulAnathemaFourPieceBonus)),

		P("wl_demo", "fireball_sp_pct", "felFireballSP", Pct(felFireballSP)),
		P("wl_demo", "cast_ms", "felFireballCastTime", Ms(felFireballCastTime)),
		P("wl_demo", "imp_dur", "wildImpDuration", Sec(wildImpDuration)),
		P("wl_demo", "empowered_pct", "empoweredImpBonus", Pct(empoweredImpBonus)),
		P("wl_demo", "splash_sp_pct", "empoweredImpSplashSP", Pct(empoweredImpSplashSP)),

		P("wl_destro", "cb_sp_pct", "crashingChaosChaosBoltSP", Pct(crashingChaosChaosBoltSP)),
		P("wl_destro", "incin_window", "crashingChaosIncinerateTime", Sec(crashingChaosIncinerateTime)),
		P("wl_destro", "infernal_chance", "crashingChaosInfernalChance", Pct(crashingChaosInfernalChance)),
		P("wl_destro", "infernal_dur", "crashingChaosInfernalTime", Sec(crashingChaosInfernalTime)),
		P("wl_destro", "cc_pct", "crashingChaosBonus", Pct(crashingChaosBonus)),

		// Vaelith's Withering Grasp
		P("900106", "drain_soul_pct_per_stack", "witheringGraspPerStack", Pct(witheringGraspPerStack)),
		P("900106", "max_stacks", "witheringGraspMaxStacks", witheringGraspMaxStacks),
		P("900106", "mark_duration", "witheringGraspDuration", Sec(witheringGraspDuration)),
		P("900106", "channel_time_pct", "witheringGraspChannelSpeed", -Pct(1-1.0/witheringGraspChannelSpeed)),

		// Vaelith's Reaped Soul
		P("900125", "stacks", "reapedSoulStacks", reapedSoulStacks),
		P("900125", "reap_tick_sp_pct", "reapSoulTickSP", Pct(reapSoulTickSP)),
		P("900125", "reap_duration", "reapSoulTicks * reapSoulTickLength", reapSoulTicks*Sec(reapSoulTickLength)),
		P("900125", "reap_tick", "reapSoulTickLength", Sec(reapSoulTickLength)),

		// Vaelith's Abyssal Calling
		P("900127", "pit_lord_duration", "abyssalCallingDuration", Sec(abyssalCallingDuration)),
		P("900127", "blast_interval", "abyssalCallingStrikeEvery", Sec(abyssalCallingStrikeEvery)),
		P("900127", "blast_sp_pct", "abyssalCallingStrikeSP", Pct(abyssalCallingStrikeSP)),
		P("900127", "aura_sp_pct", "abyssalCallingAuraSP", Pct(abyssalCallingAuraSP)),
		P("900127", "aura_duration", "abyssalCallingDuration", Sec(abyssalCallingDuration)),
		P("900127", "de_cooldown", "abyssalCallingDECooldown", Ms(abyssalCallingDECooldown)),

		// Morgrath's Malediction
		P("900128", "dot_total_sp_pct", "maledictionTotalSP", Pct(maledictionTotalSP)),
		P("900128", "dot_ticks", "maledictionTicks", maledictionTicks),
		P("900128", "dot_duration", "maledictionTicks * maledictionTickLength", maledictionTicks*Sec(maledictionTickLength)),
		P("900128", "ignite_chance", "maledictionIgniteChance", Pct(maledictionIgniteChance)),
		P("900128", "ignite_sp_pct", "maledictionIgniteSP", Pct(maledictionIgniteSP)),
		P("900128", "ignite_interval", "maledictionIgniteInterval", Sec(maledictionIgniteInterval)),

		// Vorrax's Chaosflame
		P("900129", "chaos_bolt_pct_per_stack", "chaosflameBonusPerStack", Pct(chaosflameBonusPerStack)),
		P("900129", "max_stacks", "chaosflameMaxStacks", chaosflameMaxStacks),
		P("900129", "duration", "chaosflameDuration", Sec(chaosflameDuration)),
	}, map[string]string{
		"900127.aura_radius": "only the warlock is simulated, so the aura reaches no one else",
	})
}
