---
name: cc-tuning-sync
description: Sync the sim with the live Crimson Chains server after tuning is pushed. Fetches the server's published custom-content data (sim/data.json), reports what changed, updates the hard-coded constants and mechanics of simulated sets/rings, re-imports item stats if needed, runs the tests and reports the DPS impact. Use when the user says tuning was pushed/changed on the live server, asks to sync or check server tuning, or invokes /cc-tuning-sync.
---

# Sync server tuning into the sim

The server publishes every custom tier set and legendary ring (stats, tooltips, script mechanics, live
tuning values) at http://209.38.90.151:8087/sim/data.json. The repo keeps a copy in
`assets/db_inputs/cc/server_sim_data.json`. Each class's `sim/<class>/cc_tuning_test.go`
(`TestServerTuning`) checks the constants in `cc_items.go` against that copy.

Environment (Windows, see CLAUDE.md): use `./build.ps1 <target>` from PowerShell. For raw Go commands,
first put `C:\wowsims-cc\.tools\go\bin` on `$env:PATH`. Node is available for scripting; Python is not.

## 1. Fetch and read the report

First run the `cc-server-check` skill. If it reports `unchanged`, stop and tell the user nothing
changed; if `unreachable`, stop and report the error.

Otherwise run `./build.ps1 tuning`. It fetches the live data, prints a markdown report and stores the
new copy.

Otherwise the report has some of these sections. Entries marked `[simulated]` are implemented in the
sim; the rest are only informational.

- **Tuning values changed**: numbers to update (step 3).
- **Tuning parameters added or removed**: new or dropped knobs (step 4).
- **Text changed**: tooltips, set bonus text or documented mechanics (step 4).
- **New entries / Removed entries**: sets or rings added or dropped (step 5).
- **Item stats changed on the server / Imported item data differs from the server**: re-import (step 6).

## 2. Baseline DPS (before changing code)

For each class with a changed `[simulated]` entry, record the current impact so you can compare later:
`go test --tags=with_db ./sim/<class>/... -run CCDps -v` (classes: `druid`, `rogue`, `warlock`,
`warrior`). Keep the `DPS:` lines. This takes a minute or two per class.

## 3. Update changed values

Run `go test --tags=with_db ./sim/... -run TestServerTuning -count=1`. Every mismatch prints like:

    rog_sub.hemo_ap_pct: server 35, sim 31.2 -> update shadowStrikesHemorrhageAP

Update that constant in the class's `cc_items.go`, converting to the sim's unit the way the
`cc_tuning_test.go` mapping does: `Pct(x)` means the constant is a fraction (35 -> 0.35), `Sec(d)` a
duration (`time.Second * n`), `Ms(d)` a duration from milliseconds, and bare values are used as is
(stacks, combo points, energy). Constants built from several parts are named in the message (for
example `maledictionTicks * maledictionTickLength`): change the part that the server's note implies.
Keep existing comments accurate (e.g. `// live: leg_abyss blast_sp_pct`). Re-run until it passes.

## 4. New parameters and text changes

A new parameter or changed text on a `[simulated]` entry can mean the mechanic itself changed. Read the
server's mechanics text and tuning notes for that entry in `server_sim_data.json`, compare with the
code in `cc_items.go`, and:

- If behaviour changed, implement it (follow CLAUDE.md: hand-written effects, one-line hooks into
  upstream files, assumptions in comments) and extend that item's tests in the class's
  `cc_items_test.go`. For every helper spell you add or change, run `./build.ps1 inspect family <id>`
  and give it exactly the class modifiers listed there (CLAUDE.md, "Family flags are the source of
  truth"); run `./build.ps1 cc` after adding spell IDs so `TestClassModifiers` has their data.
- Map every new parameter in `cc_tuning_test.go` to a named constant, or, if the sim genuinely can't
  model it, add it to the `notSimulated` map with the reason.
- If a parameter was removed, drop its mapping (and the code path if it no longer applies).
- If the text change is only wording, nothing to do; say so in the summary.

When the right behaviour is ambiguous, implement the most literal reading of the server's text, note
the assumption in a comment, and list it for the user.

## 5. New or removed entries

Do not implement new sets or rings unasked: list them (class, spec, name, what they do) as candidates.
For a removed `[simulated]` entry, ask the user before deleting its code; `TestServerTuning` fails
until the entry is removed from the `cc_tuning_test.go` list.

## 6. Item stats and set text

If the report says the imported item data differs, run `./build.ps1 dump` (read-only export using the
credentials in `.env`; never commit `.env`), then `./build.ps1 cc`, and review
`git diff assets/db_inputs/cc`. If `.env` is missing or the dump fails, don't work around it: tell the
user the stats are out of date and which items differ. Re-run `./build.ps1 tuning -check` afterwards;
the "Imported item data differs" section should be gone.

## 7. Test and review

1. `./build.ps1 test`. Spec suites (`TestAffliction`, `TestSubtlety`, ...) failing on DPS values for
   the changed items and sets are the expected result of the new tuning. Any other failure, including
   an effect test in `cc_items_test.go`, is a bug to fix (or a test to update when it hard-codes the
   old value).
2. `./build.ps1 update-tests`, then check `git diff -U0 -- '*.results'`: only entries for the changed
   items and sets (and the spec suites that equip them) should move. Investigate anything else.
3. Re-run `./build.ps1 test` until it is green.
4. Re-run the CCDps logs from step 2 and compare.

## 8. Report to the user

Summarize, in this order:

- What changed on the server (entry, parameter, old -> new), and which constants/code you changed.
- DPS impact per affected item: before -> after (effect % vs same stats).
- Mechanic changes you implemented and any assumptions made.
- Anything not done: new entries (candidates), removed entries awaiting a decision, stats that need a
  dump you couldn't run.

Do not commit unless the user asked for it. If they did, one commit with a message like
`Server tuning <date>: <short list of changes>`, ending with the attribution line from the system
prompt, and include `assets/db_inputs/cc/server_sim_data.json` so the stored copy matches the code.
