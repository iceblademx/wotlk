# wowsims-cc

Fork of wowsims/wotlk (Wrath Classic sim) targeting the 3.3.5a (12340) client plus private-server custom content. Remote `upstream` = github.com/wowsims/wotlk.

## Build (Windows, PowerShell)
- Always use `./build.ps1 <target>`; there is no make/bash. It puts `.tools/{go,protoc,git,gobin}` on PATH itself.
- For ad-hoc Go commands, prepend `C:\wowsims-cc\.tools\go\bin` (and `.tools\git\cmd` for git) to `$env:PATH`.
- `./build.ps1 serve` builds and runs `wowsimwotlk.exe` (UI embedded). `host`, `rundevserver` and `serve` take `-Port N` and `-Remote` (listen on all interfaces; localhost only by default).
- `./build.ps1 test` runs `go test --tags=with_db ./sim/...`. Spec tests compare against `*.results` files; after intended changes, run `./build.ps1 update-tests` and review the diff.
- `./build.ps1 items` regenerates `assets/database/db.{bin,json}`. With no custom content it is byte-identical to upstream.

## Custom content pipeline
- Raw inputs: `cc_data/` (git-ignored; layout in `cc_data/README.md`).
- `tools/cc/dbc`: WDBC reader and 3.3.5a layouts (Spell.dbc = 234 fields). `tools/cc/server`: item_template SQL/CSV, itemcache.wdb, spell_proc(_event), spell_bonus_data.
- `tools/cc/convert`: maps them to UIItem/UIGem/UIEnchant. Passive equip auras become plain stats.
- `tools/cc/extract` (`./build.ps1 cc`) writes `assets/db_inputs/cc/*` (committed) and `sim/common/cc/zz_generated.go`, then gen_db merges them in via `tools/database/custom_content.go`.
- Custom effects: hand-write generic ones in `sim/common/cc/*.go` (not `zz_generated.go`). Class-specific ones go in the class package as `cc_items.go` (e.g. `sim/druid/cc_items.go`), with tests in `<spec>/cc_items_test.go`. Keep hooks into upstream spell files to one line each (e.g. `druid.onFerociousBiteLanded(...)`).
- Server scripts are unavailable, but the server publishes their mechanics and live tuning at http://209.38.90.151:8087/sim/data.json (refreshed every 10 min; human view at `/sim/`). Treat it as the source of truth: name constants after its tuning keys, fill gaps from tooltip text plus helper-spell data, and write the assumptions in comments. Re-fetch before tuning passes, since the values change.
- `./build.ps1 tuning` fetches it, reports changes and stores a copy in `assets/db_inputs/cc/server_sim_data.json` (`-check`: report only, exit 1 on changes or errors; the report's last lines `Fingerprint:` and `Status: changed|unchanged` say which). The read-only `cc-server-check` skill wraps `-check` in a fixed-format verdict for other skills and `/loop` polling to call. Each class's `cc_tuning_test.go` (`TestServerTuning`) maps every server parameter of its implemented entries to a constant and fails on mismatches or unmapped new parameters, so new effects must add their mappings there. The `cc-tuning-sync` skill (`.claude/skills/`) runs the whole update after a tuning push.
- Done: Druid (Balance ring 900108 and set 9304, Feral Cat ring 900115 and set 9305, Bear set 9306), Rogue (all rings 900119, 900142-900147; sets 9320, 9321, 9322), Warlock (all rings 900106, 900125, 900127-900129; sets 9326, 9327, 9328), Fury (rings 900116, 900126, set 9330), Arms (ring 900116, set 9329), Hunter (rings 900107, 900133-900138; sets 9308, 9309, 9310). Restoration Druid set 9307 is skipped because the restoration sim has no heals. Hand-written code wins: the generator skips any item ID or set name that appears in sim code, and runtime helpers check `core.HasItemEffect` / `core.HasItemSet`.
- Item effects and set bonuses run before the class registers its spells. When an effect needs those spells, register it through `Env.RegisterPreFinalizeEffect`. Referencing the `ItemSet` var inside its own bonuses is an init cycle, so the 4pc callback sets a flag in the class's `cc ccItems` struct instead.
- Family flags are the source of truth for which class effects a custom spell gets. On the server, talents, glyphs, set bonuses, buffs, debuffs and class procs reach a spell only through its SpellFamilyFlags, and most custom spells have none. Before choosing flags, multipliers or crit multipliers for a helper spell, run `./build.ps1 inspect family <id>` and apply exactly the class modifiers it lists (usually none):
  - Don't use class opt-ins or helpers that fold in family-restricted talents unless listed: `SpellFlagHauntSE` (Haunt, Shadow Embrace), `warrior.critMultiplier` (Impale), `rogue.MeleeCritMultiplier(true)` (Lethality), `druid.BalanceCritMultiplier` (Vengeance), `hunter.critMultiplier(true, …)` (Mortal Shots, Marked for Death), `hunter.markedForDeathMultiplier`, `SpellCritMultiplier(1, Ruin)`, talent bonuses in `DamageMultiplier`. Use the class's `familyless`/default variants instead.
  - Opt out where class code applies a family-restricted talent to a whole school: `warlock.excludeFromFamilyTalents` (Death's Embrace), `warrior.familylessCritMultiplier`.
  - School-wide auras still apply by school whatever their mask: `MOD_DAMAGE_PERCENT_DONE` (Malediction, Murder, Hunger for Blood), damage-taken debuffs (Curse of the Elements), `MOD_CRIT_DAMAGE_BONUS` (Prey on the Weak, Predatory Instincts).
  - Physical periodic damage gets bleed modifiers (Mangle, Trauma) in the sim, so it must have the Bleed mechanic, and bleeds must be physical periodic.
  - `TestClassModifiers` (`sim/common/cc/ccfamily`, data in `assets/db_inputs/cc/sim_spells.json`) enforces the flag and bleed rules and needs a `Reviewed` entry for any custom spell that has class modifiers. After adding custom spell IDs to sim code, run `./build.ps1 cc` so they get family data, and equip the new item in the class's `TestClassModifiers`.
- If an effect needs rotation support that references its aura, add a separate preset APL (e.g. `affliction_withering_grasp`), because for everyone else an unknown aura or spell makes the core drop that condition (with a warning), so the action runs unconditionally.
- DPS impact: `go test --tags=with_db ./sim/<class>/... -run CCDps -v`. It compares each item against `cc.StatOnlyCopy` (same stats, no effect).
- To decode data while implementing, run `./build.ps1 inspect spell|item|set|enchant <id>` or `inspect search <text>`.
- Server: AzerothCore world DB. `./build.ps1 dump` exports tables using `.env` (read-only session; never commit `.env`). Custom items are IDs >= 900000 (`cc_data/cc.json`).
- `./build.ps1 audit [-fix]` checks the item effect values hardcoded in the sim against 3.3.5a Spell.dbc. Wrath Classic buffed Ulduar trinket procs, and the 3.3.5a values are restored. Re-run it after merging upstream.
- Stat mapping: hit, crit and haste ratings go to both melee and spell stats. AP goes to both AP and RAP. Expertise and defense aura points are converted to rating.

## Conventions
- Match upstream style (gofmt, tabs). Keep fork-specific changes small and isolated so upstream merges stay easy.
