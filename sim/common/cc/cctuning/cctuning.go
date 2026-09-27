// Package cctuning checks the tuning values hard-coded in the sim's custom content against the values
// the server publishes (http://209.38.90.151:8087/sim/data.json). `./build.ps1 tuning` stores a copy in
// assets/db_inputs/cc/server_sim_data.json; each class's cc_tuning_test.go maps the server's
// parameters to its constants. Only tests import this package.
package cctuning

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"
)

const SnapshotPath = "assets/db_inputs/cc/server_sim_data.json"

// Param maps a server tuning parameter to the sim's value.
type Param struct {
	Entry string  // tier set key (e.g. "rog_sub") or legendary item ID (e.g. "900146")
	Name  string  // the server's parameter name
	Const string  // the sim constant(s) holding it, for the failure message
	Sim   float64 // the sim's value, converted to the server's unit
}

// P builds a Param.
func P(entry, name, constant string, sim float64) Param {
	return Param{Entry: entry, Name: name, Const: constant, Sim: sim}
}

// Pct converts a fraction to the server's percent values.
func Pct(x float64) float64 { return 100 * x }

// Sec converts a duration to the server's second values.
func Sec(d time.Duration) float64 { return d.Seconds() }

// Ms converts a duration to the server's millisecond values.
func Ms(d time.Duration) float64 { return float64(d.Milliseconds()) }

type tuningValue struct {
	Value json.Number `json:"value"`
}

type snapshot struct {
	TierSets []struct {
		Key    string                 `json:"key"`
		Tuning map[string]tuningValue `json:"tuning"`
	} `json:"tier_sets"`
	Legendaries []struct {
		Entry  int                    `json:"entry"`
		Tuning map[string]tuningValue `json:"tuning"`
	} `json:"legendaries"`
}

// load reads the snapshot into entry -> parameter -> value.
func load(t *testing.T) map[string]map[string]float64 {
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
	data, err := os.ReadFile(filepath.Join(dir, SnapshotPath))
	if err != nil {
		t.Fatalf("%v (run ./build.ps1 tuning)", err)
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("%s: %v", SnapshotPath, err)
	}

	out := map[string]map[string]float64{}
	add := func(entry string, tuning map[string]tuningValue) {
		out[entry] = map[string]float64{}
		for name, v := range tuning {
			f, err := v.Value.Float64()
			if err != nil {
				t.Errorf("%s.%s: non-numeric server value %q", entry, name, v.Value)
				continue
			}
			out[entry][name] = f
		}
	}
	for _, s := range snap.TierSets {
		add(s.Key, s.Tuning)
	}
	for _, l := range snap.Legendaries {
		add(strconv.Itoa(l.Entry), l.Tuning)
	}
	return out
}

// Check fails for every param whose sim value differs from the server's, and for every server parameter
// of the given entries that is neither mapped by a param nor listed in notSimulated ("entry.param" ->
// reason). The second part catches parameters the server adds later.
func Check(t *testing.T, entries []string, params []Param, notSimulated map[string]string) {
	t.Helper()
	server := load(t)

	claimed := map[string]bool{}
	for _, entry := range entries {
		if _, ok := server[entry]; !ok {
			t.Errorf("%s: not published by the server any more", entry)
		}
		claimed[entry] = true
	}

	covered := map[string]bool{}
	for _, p := range params {
		id := p.Entry + "." + p.Name
		covered[id] = true
		if !claimed[p.Entry] {
			t.Errorf("%s: entry %s is not listed as implemented", id, p.Entry)
			continue
		}
		want, ok := server[p.Entry][p.Name]
		if !ok {
			t.Errorf("%s: not published by the server any more (sim: %s)", id, p.Const)
			continue
		}
		if math.Abs(p.Sim-want) > 1e-9*math.Max(1, math.Abs(want)) {
			t.Errorf("%s: server %s, sim %s -> update %s", id, format(want), format(p.Sim), p.Const)
		}
	}
	for id := range notSimulated {
		covered[id] = true
	}

	var missing []string
	for _, entry := range entries {
		for name := range server[entry] {
			if id := entry + "." + name; !covered[id] {
				missing = append(missing, id)
			}
		}
	}
	sort.Strings(missing)
	for _, id := range missing {
		t.Errorf("%s: new server parameter; map it to a sim constant or list it as not simulated", id)
	}
}

func format(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
