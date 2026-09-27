# wowsims-cc

Fork of wowsims/wotlk (Wrath Classic sim) targeting the 3.3.5a (12340) client plus private-server custom content. Remote `upstream` = github.com/wowsims/wotlk.

## Build (Windows, PowerShell)
- Always use `./build.ps1 <target>`; there is no make/bash. It puts `.tools/{go,protoc,git,gobin}` on PATH itself.
- For ad-hoc Go commands, prepend `C:\wowsims-cc\.tools\go\bin` (and `.tools\git\cmd` for git) to `$env:PATH`.
- `./build.ps1 test` runs `go test --tags=with_db ./sim/...`. Spec tests compare against `*.results` files; after intended changes, run `./build.ps1 update-tests` and review the diff.
- `./build.ps1 items` regenerates `assets/database/db.{bin,json}`. With no custom content it is byte-identical to upstream.

## Custom content pipeline
- Raw inputs: `cc_data/` (git-ignored; layout in `cc_data/README.md`).
- `tools/cc/dbc`: WDBC reader and 3.3.5a layouts (Spell.dbc = 234 fields). `tools/cc/server`: item_template SQL/CSV, itemcache.wdb, spell_proc(_event), spell_bonus_data.
- `tools/cc/convert`: maps them to UIItem/UIGem/UIEnchant. Passive equip auras become plain stats.
- `tools/cc/extract` (`./build.ps1 cc`) writes `assets/db_inputs/cc/*` (committed) and `sim/common/cc/zz_generated.go`, then gen_db merges them in via `tools/database/custom_content.go`.
- Custom effects: hand-write generic ones in `sim/common/cc/*.go` (not `zz_generated.go`). Class-specific ones go in the class package as `cc_items.go` (e.g. `sim/druid/cc_items.go`), with tests in `<spec>/cc_items_test.go`. Keep hooks into upstream spell files to one line each (e.g. `druid.onFerociousBiteLanded(...)`).
- Server scripts are unavailable, so implement from tooltip text plus helper-spell data, and write the assumptions in comments.
- Done: Feral Cat (Morgrath's Ravaging Claw 900115, Prowler of the Fevered Canopy set 9305), Affliction (rings 900106, 900125, set 9326), Subtlety (rings 900146, 900147, set 9322), Fury (rings 900116, 900126, set 9330). Hand-written code wins: the generator skips any item ID or set name that appears in sim code, and runtime helpers check `core.HasItemEffect` / `core.HasItemSet`.
- Item effects and set bonuses run before the class registers its spells. When an effect needs those spells, register it through `Env.RegisterPreFinalizeEffect`. Referencing the `ItemSet` var inside its own bonuses is an init cycle, so the 4pc callback sets a flag in the class's `cc ccItems` struct instead.
- If an effect needs rotation support that references its aura, add a separate preset APL (e.g. `affliction_withering_grasp`), because an unknown aura drops the action with a warning for everyone else.
- DPS impact: `go test --tags=with_db ./sim/<class>/... -run CCDps -v`. It compares each item against `cc.StatOnlyCopy` (same stats, no effect).
- To decode data while implementing, run `./build.ps1 inspect spell|item|set|enchant <id>` or `inspect search <text>`.
- Server: AzerothCore world DB. `./build.ps1 dump` exports tables using `.env` (read-only session; never commit `.env`). Custom items are IDs >= 900000 (`cc_data/cc.json`).
- `./build.ps1 audit [-fix]` checks the item effect values hardcoded in the sim against 3.3.5a Spell.dbc. Wrath Classic buffed Ulduar trinket procs, and the 3.3.5a values are restored. Re-run it after merging upstream.
- Stat mapping: hit, crit and haste ratings go to both melee and spell stats. AP goes to both AP and RAP. Expertise and defense aura points are converted to rating.

## Conventions
- Match upstream style (gofmt, tabs). Keep fork-specific changes small and isolated so upstream merges stay easy.
