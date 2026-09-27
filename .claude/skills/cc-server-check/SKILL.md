---
name: cc-server-check
description: Read-only check of whether the live Crimson Chains server has published new custom-content data (a new server build or tuning push) that the sim doesn't reflect yet. Fetches sim/data.json, compares it with the sim's stored copy and imported items, and returns a short fixed-format verdict (changed / unchanged / unreachable, fingerprint, affected entries). Changes no files. Built to be called at the start of other skills (cc-tuning-sync, release or build workflows) and from /loop for polling. Use when asked whether the server changed, whether the sim is up to date with the server, or when invoked as /cc-server-check.
---

# Check the server for new content

The server republishes http://209.38.90.151:8087/sim/data.json after every build and tuning change
(and refreshes it every 10 minutes). It has no build number, so a new build shows up as changed
content. The repo's copy, `assets/db_inputs/cc/server_sim_data.json`, is what the sim was last synced
against.

This skill only reports. It never edits, stores, commits or runs the sync; the caller (or the user)
decides what to do with the verdict.

## Arguments

Optional: the fingerprint from a previous check (`/cc-server-check <fingerprint>`, or passed by the
calling skill). With it, the verdict also says whether this is a *new* server build since that check or
the same unsynced one.

## Steps

1. From PowerShell in the repo root, run `./build.ps1 tuning -check`. It needs network access and takes
   a few seconds (longer the first time Go compiles the tool). The exit code is 1 both for "changed"
   and for errors, so don't rely on it.
2. Read the last lines of the output:
   - `Fingerprint: <hex>`: identifies the fetched content (ignoring the refresh timestamp).
   - `Status: changed` or `Status: unchanged`.
   - Neither line: the check failed (server unreachable, bad JSON, Go error). The verdict is
     `unreachable`; include the error line.
3. If changed, list the report's sections (`## ...`) and the entries in them. Entries marked
   `[simulated]` are implemented in the sim and need syncing; the rest are informational. The section
   "Imported item data differs from the server" means item stats need a DB dump and re-import.

## Output

Return exactly this block (one line per field, omit none), then at most two sentences of detail:

```
Server check: changed | unchanged | unreachable
Fingerprint: <hex, or "none">
New since last check: yes | no | unknown (no previous fingerprint given)
Simulated entries affected: <comma-separated keys and names, or "none">
Other changes: <section titles for non-simulated or informational changes, or "none">
Next step: /cc-tuning-sync | none | retry later
```

- `Next step` is `/cc-tuning-sync` whenever the status is `changed` (that skill also handles item stat
  re-imports and new entries), `none` when unchanged, `retry later` when unreachable.
- `New since last check` is `no` when the fingerprint equals the one passed in, even if the status is
  `changed`: that means the server hasn't moved since the last check and the sim is still unsynced.

## Using it from another skill

Put a step like this at the start of the other skill:

> Run the `cc-server-check` skill (pass the last fingerprint if you have one). If it says `unchanged`,
> continue / stop as appropriate. If `changed`, run `cc-tuning-sync` before continuing. If
> `unreachable`, tell the user and continue with the stored copy.

## Polling for new server builds

`/loop 10m /cc-server-check` checks at the feed's refresh rate. On each tick pass the previous tick's
fingerprint, and only notify the user when `New since last check` is `yes` (or on the first `changed`),
so an unsynced change isn't re-announced every ten minutes.
