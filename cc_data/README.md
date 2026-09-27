# cc_data: raw 3.3.5a inputs

This folder holds the raw client and server files for custom content. Everything here except this README is git-ignored. Only the reviewable output of the import under `assets/db_inputs/cc/` is committed.

```
cc_data/
  cc.json            optional settings (see below)
  dbc/               DBFilesClient\*.dbc from your client patch (patch-*.MPQ)
  dbc_baseline/      optional: the same DBCs from an unmodified 3.3.5a client, to find tuned spells
  server/            item data plus optional proc/coefficient tables
```

## What goes where

**`dbc/`** needs 3.3.5a (build 12340) DBCs, extracted from the MPQs (e.g. with Ladik's MPQ Editor). Take them from the *highest* patch that contains each file, since custom patches override `enUS\patch-enUS-*.MPQ`.

| File | Used for |
|---|---|
| `Spell.dbc` | equip, use and proc effects; set bonuses; enchants; changed class spells |
| `SpellDuration.dbc`, `SpellIcon.dbc` | buff durations, spell icons |
| `ItemSet.dbc` | set names and bonus spells |
| `SpellItemEnchantment.dbc` | gem stats, socket bonuses, enchants |
| `GemProperties.dbc` | gem colours |
| `ItemDisplayInfo.dbc` | item icons |
| `Item.dbc` | optional |

**`server/`** is needed because the 3.3.5a client DBCs do **not** contain item stats. Provide one or more of these:

- `*.sql`: `item_template` dumps or patches. INSERT, REPLACE, simple UPDATE and DELETE statements are understood. Files load in name order, so `01_world.sql` then `02_custom_items.sql` applies patches correctly.
- `*.csv` / `*.tsv`: an export with a header row. The file name must contain the table name, e.g. `item_template.csv`.
- `itemcache.wdb`: the client cache, `Cache\WDB\enUS\itemcache.wdb`. It only contains items your client has seen and has no proc PPM, so use it only when you can't get server data.
- Optional, but improves generated procs: `spell_proc` (TrinityCore) or `spell_proc_event` (AzerothCore), for internal cooldowns and hit masks, plus `spell_bonus_data` for spell coefficients.

### Exporting from the server database

With read access to the world database, put the credentials in `.env` at the repo root (git-ignored):

```
SQL_HOSTNAME=...
SQL_PORT=3306
SQL_USERNAME=...
SQL_PASSWORD=...
# SQL_DATABASE=world   (optional; auto-detected as the database containing item_template)
```

`./build.ps1 dump` opens a read-only session and exports `item_template`, `spell_proc`, `spell_proc_event`, `spell_bonus_data`, `spell_dbc`, `spell_enchant_proc_data`, `spell_cooldown_overrides` and `item_set_names` to `server/*.csv`. `spell_dbc` (server-side spells) is merged over `Spell.dbc`, and `spell_cooldown_overrides` is applied on top.

## cc.json

```jsonc
{
  "phase": 1,                     // UI phase assigned to imported custom items
  "customItemIdMin": 0,           // IDs >= this are always custom (0 = decide by stock data)
  "includeItems": [],             // force-import these IDs as custom
  "excludeItems": [],             // never import these IDs
  "applyStockItemChanges": true,  // use server stats for stock items that differ from wowhead
  "restrictToServerItems": true,  // hide stock items missing from item_template (Classic-only items)
  "extraSpells": []               // extra spell IDs to decode into spells.json
}
```

## Running

```powershell
./build.ps1 cc                       # import, regenerate the item DB, write reports
./build.ps1 inspect item 123456      # decode an item and its spells
./build.ps1 inspect spell 71519
./build.ps1 inspect set 883
./build.ps1 inspect search "gladiator"
./build.ps1 test
```

Then read `assets/db_inputs/cc/REPORT.md`, which lists effects that still need hand-written Go code, and `STOCK_CHANGES.md`.
