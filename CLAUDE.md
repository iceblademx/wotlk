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
- Custom effects: hand-write them in `sim/common/cc/*.go` (not `zz_generated.go`). Hand-written code wins: the generator skips any item ID or set name that appears in sim code, and runtime helpers check `core.HasItemEffect` / `core.HasItemSet`.
- To decode data while implementing, run `./build.ps1 inspect spell|item|set|enchant <id>` or `inspect search <text>`.
- Server: AzerothCore world DB. `./build.ps1 dump` exports tables using `.env` (read-only session; never commit `.env`). Custom items are IDs >= 900000 (`cc_data/cc.json`).
- `./build.ps1 audit [-fix]` checks the item effect values hardcoded in the sim against 3.3.5a Spell.dbc. Wrath Classic buffed Ulduar trinket procs, and the 3.3.5a values are restored. Re-run it after merging upstream.
- Stat mapping: hit, crit and haste ratings go to both melee and spell stats. AP goes to both AP and RAP. Expertise and defense aura points are converted to rating.

## Conventions
- Match upstream style (gofmt, tabs). Keep fork-specific changes small and isolated so upstream merges stay easy.
