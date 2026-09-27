// Package ccfamily checks the custom spells a unit registers against their 3.3.5a spell family data.
//
// On the server, class talents, glyphs, set bonuses, buffs, debuffs and procs reach a spell only through
// its family flags (and bleed modifiers through its Bleed mechanic). Most custom spells have no family
// flags, so none of them apply, whatever the sim's class code does for the class's own spells.
// `./build.ps1 cc` writes the data (assets/db_inputs/cc/sim_spells.json) for every custom spell ID in
// hand-written sim code; `./build.ps1 inspect family <id>` prints the same. Only tests import this
// package.
package ccfamily

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wowsims/wotlk/sim/core"
)

const DataPath = "assets/db_inputs/cc/sim_spells.json"

// Custom spell IDs on the server are 200000+; stock 3.3.5a spells stay below 100000.
const customSpellIDMin = 200000

type classModifier struct {
	SpellID uint32 `json:"spellId"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
}

type simSpell struct {
	ID             int32           `json:"id"`
	Name           string          `json:"name"`
	Bleed          bool            `json:"bleed"`
	ClassModifiers []classModifier `json:"classModifiers"`
}

// Reviewed maps a custom spell ID to how the sim handles the class modifiers its family flags let apply
// (see Check). Write down what each one does to the spell and where the sim applies it, or why it
// doesn't matter.
type Reviewed map[int32]string

func load(t *testing.T) map[int32]simSpell {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root (go.mod) not found")
		}
		dir = parent
	}
	data, err := os.ReadFile(filepath.Join(dir, DataPath))
	if err != nil {
		t.Fatalf("%v (run ./build.ps1 cc)", err)
	}
	var spells []simSpell
	if err := json.Unmarshal(data, &spells); err != nil {
		t.Fatalf("%s: %v", DataPath, err)
	}
	out := map[int32]simSpell{}
	for _, s := range spells {
		out[s.ID] = s
	}
	return out
}

func hasDot(spell *core.Spell, target *core.Unit) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return spell.AOEDot() != nil || spell.Dot(target) != nil
}

// Check verifies every custom spell registered by the player of each sim (together, the sims should
// equip all of a class's custom items):
//   - SpellFlagHauntSE (Haunt, Shadow Embrace) exactly when the family data says those auras apply.
//   - Physical periodic damage, which the sim amplifies with bleed debuffs (Mangle, Trauma), exactly
//     when the spell has the Bleed mechanic.
//   - Any other class modifier its family flags let apply has been reviewed: the sim can't tell which
//     talents its class code folds into a spell, so each such spell needs an entry in reviewed.
func Check(t *testing.T, reviewed Reviewed, sims ...*core.Simulation) {
	t.Helper()
	data := load(t)
	seen := map[int32]bool{}
	for _, sim := range sims {
		checkUnit(t, data, &sim.Raid.Parties[0].Players[0].GetCharacter().Unit, sim.Encounter.TargetUnits[0], reviewed, seen)
	}
	for id := range reviewed {
		if !seen[id] {
			t.Errorf("spell %d: listed in Reviewed but not registered", id)
		}
	}
}

func checkUnit(t *testing.T, data map[int32]simSpell, unit *core.Unit, target *core.Unit, reviewed Reviewed, seen map[int32]bool) {
	t.Helper()
	for _, spell := range unit.Spellbook {
		id := spell.ActionID.SpellID
		if id < customSpellIDMin || seen[id] {
			continue
		}
		seen[id] = true
		s, ok := data[id]
		if !ok {
			t.Errorf("spell %d (%s): no family data in %s; run ./build.ps1 cc", id, spell.ActionID, DataPath)
			continue
		}

		fromCaster := false
		var others []string
		for _, m := range s.ClassModifiers {
			if m.Kind == "damage taken from the caster" {
				fromCaster = true
				continue
			}
			others = append(others, m.Name)
		}

		haunt := spell.Flags.Matches(core.SpellFlagHauntSE)
		switch {
		case haunt && !fromCaster:
			t.Errorf("spell %d %s: SpellFlagHauntSE, but its family flags don't let Haunt or Shadow Embrace apply on the server", id, s.Name)
		case !haunt && fromCaster:
			t.Errorf("spell %d %s: its family flags let Haunt/Shadow Embrace (MOD_DAMAGE_FROM_CASTER) apply; set SpellFlagHauntSE", id, s.Name)
		}

		physicalPeriodic := spell.SpellSchool.Matches(core.SpellSchoolPhysical) && hasDot(spell, target)
		switch {
		case physicalPeriodic && !s.Bleed:
			t.Errorf("spell %d %s: physical periodic damage gets bleed modifiers in the sim, but it has no Bleed mechanic", id, s.Name)
		case s.Bleed && !physicalPeriodic:
			t.Errorf("spell %d %s: has the Bleed mechanic, so it should be physical periodic damage (bleed modifiers)", id, s.Name)
		}

		if len(others) > 0 && reviewed[id] == "" {
			t.Errorf("spell %d %s: its family flags let these class effects apply; model them and add a Reviewed entry: %s",
				id, s.Name, uniqueNames(others))
		}
		if len(others) == 0 && reviewed[id] != "" {
			t.Errorf("spell %d %s: listed in Reviewed, but no class modifier applies to it any more", id, s.Name)
		}
	}
}

func uniqueNames(names []string) string {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
