// Command audit compares item effect values hardcoded in the sim (upstream values, from Wrath
// Classic, which e.g. raised Ulduar item levels and trinket procs) against the 3.3.5a spell data in
// cc_data/, and optionally rewrites mismatched numbers in place.
//
//	go run ./tools/cc/audit          # report only -> assets/db_inputs/cc/EFFECT_AUDIT.md
//	go run ./tools/cc/audit -fix     # also rewrite unambiguous mismatches in sim/**/*.go
//
// Audited definitions:
//   - composite literals with ID + Bonus (ProcStatBonusEffect, StackingStatBonusEffect, StackingStatBonusCD, ...)
//   - core.NewSimpleStat*Effect(itemID, stats.Stats{...}, duration, cooldown) calls
//   - composite literals with ID + MinDmg + MaxDmg (ProcDamageEffect, CapacitorDamageEffect)
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wowsims/wotlk/sim/core/stats"
	"github.com/wowsims/wotlk/tools/cc"
	"github.com/wowsims/wotlk/tools/cc/convert"
	"github.com/wowsims/wotlk/tools/cc/dbc"
	"github.com/wowsims/wotlk/tools/cc/server"
)

var inDir = flag.String("in", "cc_data", "Directory with raw 3.3.5a client/server files")
var simDir = flag.String("sim", "sim", "Sim source directory")
var reportPath = flag.String("report", "assets/db_inputs/cc/EFFECT_AUDIT.md", "Report output")
var fix = flag.Bool("fix", false, "Rewrite unambiguous mismatches in the source")

type edit struct {
	start, end int
	text       string
}

type finding struct {
	file   string
	line   int
	itemID int32
	name   string
	status string // ok, fixed/fixable, review, skipped
	detail string
}

type auditor struct {
	src      *convert.Source
	fset     *token.FileSet
	edits    map[string][]edit
	findings []finding
	files    map[string][]byte
}

func main() {
	flag.Parse()
	in, err := cc.Load(*inDir)
	if err != nil {
		log.Fatal(err)
	}
	a := &auditor{src: in.Source, fset: token.NewFileSet(), edits: map[string][]edit{}, files: map[string][]byte{}}

	filepath.WalkDir(*simDir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") ||
			strings.HasSuffix(p, ".pb.go") || strings.Contains(filepath.ToSlash(p), "sim/common/cc/") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(a.fset, p, data, 0)
		if err != nil {
			return err
		}
		a.files[p] = data
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				a.checkCompositeLit(p, x)
			case *ast.CallExpr:
				a.checkSimpleStatCall(p, x)
			}
			return true
		})
		return nil
	})

	if *fix {
		for path, edits := range a.edits {
			data := a.files[path]
			sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
			for _, e := range edits {
				data = append(data[:e.start:e.start], append([]byte(e.text), data[e.end:]...)...)
			}
			if formatted, err := format.Source(data); err == nil {
				data = formatted
			}
			if err := os.WriteFile(path, data, 0666); err != nil {
				log.Fatal(err)
			}
		}
	}
	a.writeReport()
}

// ---------------------------------------------------------------------------
// Source extraction helpers
// ---------------------------------------------------------------------------

func fieldsOf(lit *ast.CompositeLit) map[string]ast.Expr {
	m := map[string]ast.Expr{}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				m[id.Name] = kv.Value
			}
		}
	}
	return m
}

func intLit(e ast.Expr) (int64, bool) {
	neg := false
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		neg, e = true, u.X
	}
	bl, ok := e.(*ast.BasicLit)
	if !ok || (bl.Kind != token.INT && bl.Kind != token.FLOAT) {
		return 0, false
	}
	f, err := strconv.ParseFloat(bl.Value, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		f = -f
	}
	return int64(f), true
}

func floatLit(e ast.Expr) (float64, *ast.BasicLit, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || (bl.Kind != token.INT && bl.Kind != token.FLOAT) {
		return 0, nil, false
	}
	f, err := strconv.ParseFloat(bl.Value, 64)
	return f, bl, err == nil
}

// statsLit parses stats.Stats{stats.X: 123, ...}. ok=false if any value isn't a numeric literal.
type statEntry struct {
	stat stats.Stat
	val  float64
	lit  *ast.BasicLit
}

var statByName = func() map[string]stats.Stat {
	m := map[string]stats.Stat{}
	for i := 0; i < int(stats.Len); i++ {
		m[stats.Stat(i).StatName()] = stats.Stat(i)
	}
	return m
}()

func statsLit(e ast.Expr) ([]statEntry, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	if sel, ok := lit.Type.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Stats" {
		return nil, false
	}
	var out []statEntry
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return nil, false
		}
		key, ok := kv.Key.(*ast.SelectorExpr)
		if !ok {
			return nil, false
		}
		st, ok := statByName[key.Sel.Name]
		if !ok {
			return nil, false
		}
		v, bl, ok := floatLit(kv.Value)
		if !ok {
			return nil, false
		}
		out = append(out, statEntry{stat: st, val: v, lit: bl})
	}
	return out, true
}

// durationMs evaluates `time.Second * 10`, `10 * time.Second`, `time.Millisecond * 1500`, `time.Second`.
func durationMs(e ast.Expr) (int64, bool) {
	unit := func(x ast.Expr) (int64, bool) {
		sel, ok := x.(*ast.SelectorExpr)
		if !ok {
			return 0, false
		}
		switch sel.Sel.Name {
		case "Second":
			return 1000, true
		case "Millisecond":
			return 1, true
		case "Minute":
			return 60000, true
		}
		return 0, false
	}
	if u, ok := unit(e); ok {
		return u, true
	}
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.MUL {
		if u, ok := unit(b.X); ok {
			if n, ok := intLit(b.Y); ok {
				return u * n, true
			}
		}
		if u, ok := unit(b.Y); ok {
			if n, ok := intLit(b.X); ok {
				return u * n, true
			}
		}
	}
	return 0, false
}

func stringLit(e ast.Expr) string {
	if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
		s, _ := strconv.Unquote(bl.Value)
		return s
	}
	return ""
}

// ---------------------------------------------------------------------------
// Expected values from 3.3.5a data
// ---------------------------------------------------------------------------

// buffSpells returns the stat buffs an item grants: use spells, equip-proc triggered spells and
// chance-on-hit spells.
func (a *auditor) buffSpells(it *server.ItemTemplate, use bool) []*dbc.Spell {
	var out []*dbc.Spell
	for _, s := range it.Spells {
		sp := a.src.Spells.Spells[s.SpellID]
		if sp == nil {
			continue
		}
		switch {
		case use && (s.Trigger == server.SpellTriggerUse || s.Trigger == server.SpellTriggerUseNoDelay):
			out = append(out, sp)
		case !use && s.Trigger == server.SpellTriggerChanceHit:
			out = append(out, sp)
		case !use && s.Trigger == server.SpellTriggerEquip:
			for _, e := range sp.Effects {
				if (e.Aura == dbc.AuraProcTriggerSpell || e.Aura == 231) && e.TriggerSpell != 0 {
					if t := a.src.Spells.Spells[e.TriggerSpell]; t != nil {
						out = append(out, t)
					}
				}
			}
		}
	}
	return out
}

// damageRange finds the SCHOOL_DAMAGE effect reachable from the item's spells (depth-limited).
func (a *auditor) damageRange(it *server.ItemTemplate) (min, max int32, spellID uint32, ok bool) {
	seen := map[uint32]bool{}
	var walk func(id uint32, depth int) bool
	walk = func(id uint32, depth int) bool {
		sp := a.src.Spells.Spells[id]
		if sp == nil || depth > 3 || seen[id] {
			return false
		}
		seen[id] = true
		for _, e := range sp.Effects {
			if e.Effect == 2 { // SCHOOL_DAMAGE
				min, max, spellID = e.Min(), e.Max(), id
				return true
			}
		}
		for _, e := range sp.Effects {
			if e.TriggerSpell != 0 && walk(e.TriggerSpell, depth+1) {
				return true
			}
		}
		return false
	}
	for _, s := range it.Spells {
		if walk(s.SpellID, 0) {
			return min, max, spellID, true
		}
	}
	return 0, 0, 0, false
}

func vector(entries []statEntry) stats.Stats {
	var s stats.Stats
	for _, e := range entries {
		s[e.stat] += e.val
	}
	return s
}

func sameStatSet(a, b stats.Stats) bool {
	for i := range a {
		if (a[i] != 0) != (b[i] != 0) {
			return false
		}
	}
	return true
}

func statsEqual(a, b stats.Stats) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > 0.5 {
			return false
		}
	}
	return true
}

func statDiff(a, b stats.Stats) string {
	var parts []string
	for i := range a {
		if math.Abs(a[i]-b[i]) > 0.5 {
			parts = append(parts, fmt.Sprintf("%s %g -> %g", stats.Stat(i).StatName(), a[i], b[i]))
		}
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// Checks
// ---------------------------------------------------------------------------

func (a *auditor) add(path string, pos token.Pos, id int32, name, status, detail string) {
	a.findings = append(a.findings, finding{file: filepath.ToSlash(path), line: a.fset.Position(pos).Line, itemID: id, name: name, status: status, detail: detail})
}

func (a *auditor) checkCompositeLit(path string, lit *ast.CompositeLit) {
	f := fieldsOf(lit)
	idExpr, hasID := f["ID"]
	if !hasID {
		return
	}
	_, hasBonus := f["Bonus"]
	_, hasMin := f["MinDmg"]
	if !hasBonus && !hasMin {
		return
	}
	name := stringLit(f["Name"])
	id64, ok := intLit(idExpr)
	if !ok {
		a.add(path, lit.Pos(), 0, name, "skipped", "item ID is not a literal (heroic/normal helper); check by hand")
		return
	}
	id := int32(id64)
	it := a.src.Server.Items[uint32(id)]
	if it == nil {
		a.add(path, lit.Pos(), id, name, "skipped", "item not in server item_template")
		return
	}
	if name == "" {
		name = it.Name
	}

	if hasMin {
		a.checkDamage(path, lit, f, id, name, it)
		return
	}

	entries, ok := statsLit(f["Bonus"])
	if !ok {
		a.add(path, lit.Pos(), id, name, "review", "Bonus is not a plain stats literal")
		return
	}
	// A stacking CD (e.g. use: gain X per stack) is a use effect; everything else is a proc.
	use := strings.Contains(fmt.Sprint(lit.Type), "CD")
	var candidates []*dbc.Spell
	if aura, ok := intLit(f["AuraID"]); ok && aura != 0 {
		if sp := a.src.Spells.Spells[uint32(aura)]; sp != nil {
			candidates = append(candidates, sp)
		}
	}
	candidates = append(candidates, a.buffSpells(it, use)...)
	var durMs int64 = -1
	if d, ok := durationMs(f["Duration"]); ok {
		durMs = d
	}
	a.compareStats(path, lit.Pos(), id, name, entries, candidates, durMs)
}

func (a *auditor) checkSimpleStatCall(path string, call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(sel.Sel.Name, "NewSimpleStat") || len(call.Args) < 3 {
		return
	}
	id64, ok := intLit(call.Args[0])
	if !ok {
		return
	}
	id := int32(id64)
	it := a.src.Server.Items[uint32(id)]
	if it == nil {
		a.add(path, call.Pos(), id, "", "skipped", "item not in server item_template")
		return
	}
	entries, ok := statsLit(call.Args[1])
	if !ok {
		a.add(path, call.Pos(), id, it.Name, "review", "bonus is not a plain stats literal")
		return
	}
	var durMs int64 = -1
	if d, ok := durationMs(call.Args[2]); ok {
		durMs = d
	}
	a.compareStats(path, call.Pos(), id, it.Name, entries, a.buffSpells(it, true), durMs)
}

func (a *auditor) compareStats(path string, pos token.Pos, id int32, name string, entries []statEntry, candidates []*dbc.Spell, durMs int64) {
	have := vector(entries)
	var best *dbc.Spell
	var bestStats stats.Stats
	for _, sp := range candidates {
		s, ok, _ := convert.SpellStats(sp)
		if !ok || s == (stats.Stats{}) {
			continue
		}
		if statsEqual(have, s) {
			best, bestStats = sp, s
			break
		}
		if best == nil && sameStatSet(have, s) {
			best, bestStats = sp, s
		}
	}
	if best == nil {
		a.add(path, pos, id, name, "review", "no 3.3.5a stat buff with the same stats found for this item")
		return
	}
	durNote := ""
	if durMs >= 0 && best.DurationMs > 0 && int64(best.DurationMs) != durMs {
		durNote = fmt.Sprintf("; duration %gs in code vs %gs in spell %d", float64(durMs)/1000, float64(best.DurationMs)/1000, best.ID)
	}
	if statsEqual(have, bestStats) {
		status := "ok"
		if durNote != "" {
			status = "review"
		}
		a.add(path, pos, id, name, status, fmt.Sprintf("matches spell %d%s", best.ID, durNote))
		return
	}
	// Same stats, different numbers: rewrite each literal.
	for _, e := range entries {
		want := bestStats[e.stat]
		if math.Abs(want-e.val) > 0.5 {
			a.edits[path] = append(a.edits[path], edit{
				start: a.fset.Position(e.lit.Pos()).Offset, end: a.fset.Position(e.lit.End()).Offset,
				text: strconv.FormatFloat(want, 'f', -1, 64)})
		}
	}
	status := "fixable"
	if *fix {
		status = "fixed"
	}
	a.add(path, pos, id, name, status, fmt.Sprintf("%s (spell %d)%s", statDiff(have, bestStats), best.ID, durNote))
}

func (a *auditor) checkDamage(path string, lit *ast.CompositeLit, f map[string]ast.Expr, id int32, name string, it *server.ItemTemplate) {
	minV, minLit, ok1 := floatLit(f["MinDmg"])
	maxV, maxLit, ok2 := floatLit(f["MaxDmg"])
	if !ok1 || !ok2 {
		a.add(path, lit.Pos(), id, name, "review", "MinDmg/MaxDmg are not literals")
		return
	}
	lo, hi, spellID, ok := a.damageRange(it)
	if !ok {
		a.add(path, lit.Pos(), id, name, "review", "no SCHOOL_DAMAGE effect found in the item's 3.3.5a spells")
		return
	}
	if minV == float64(lo) && maxV == float64(hi) {
		a.add(path, lit.Pos(), id, name, "ok", fmt.Sprintf("matches spell %d", spellID))
		return
	}
	a.edits[path] = append(a.edits[path],
		edit{a.fset.Position(minLit.Pos()).Offset, a.fset.Position(minLit.End()).Offset, strconv.Itoa(int(lo))},
		edit{a.fset.Position(maxLit.Pos()).Offset, a.fset.Position(maxLit.End()).Offset, strconv.Itoa(int(hi))})
	status := "fixable"
	if *fix {
		status = "fixed"
	}
	a.add(path, lit.Pos(), id, name, status, fmt.Sprintf("damage %g-%g -> %d-%d (spell %d)", minV, maxV, lo, hi, spellID))
}

func (a *auditor) writeReport() {
	sort.Slice(a.findings, func(i, j int) bool {
		if a.findings[i].status != a.findings[j].status {
			return a.findings[i].status < a.findings[j].status
		}
		if a.findings[i].file != a.findings[j].file {
			return a.findings[i].file < a.findings[j].file
		}
		return a.findings[i].line < a.findings[j].line
	})
	counts := map[string]int{}
	var b strings.Builder
	b.WriteString("# Stock item effect audit (sim code vs 3.3.5a spell data)\n\n")
	b.WriteString("Generated by `go run ./tools/cc/audit`. `fixed`/`fixable` entries had Wrath Classic values that differ from the 3.3.5a spell data; `review` entries need a manual look.\n\n")
	b.WriteString("| Status | Item | Where | Detail |\n|---|---|---|---|\n")
	for _, f := range a.findings {
		counts[f.status]++
		fmt.Fprintf(&b, "| %s | %d %s | %s:%d | %s |\n", f.status, f.itemID, f.name, f.file, f.line, f.detail)
	}
	summary := make([]string, 0, len(counts))
	for k, v := range counts {
		summary = append(summary, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(summary)
	if err := os.WriteFile(*reportPath, []byte(b.String()), 0666); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("audit: %s -> %s\n", strings.Join(summary, " "), *reportPath)
}
