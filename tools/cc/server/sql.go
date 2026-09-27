package server

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Tables the SQL reader keeps, with their primary key column (normalized).
var sqlTables = map[string]string{
	"item_template":    "entry",
	"spell_proc":       "spellid",
	"spell_proc_event": "entry",
	"spell_bonus_data": "entry",
}

// Default TrinityCore/AzerothCore 3.3.5 item_template column order, used for
// `INSERT INTO item_template VALUES (...)` statements when the dump has no CREATE TABLE.
var defaultItemTemplateColumns = func() []string {
	cols := strings.Fields(`entry class subclass SoundOverrideSubclass name displayid Quality Flags FlagsExtra BuyCount
		BuyPrice SellPrice InventoryType AllowableClass AllowableRace ItemLevel RequiredLevel RequiredSkill RequiredSkillRank
		requiredspell requiredhonorrank RequiredCityRank RequiredReputationFaction RequiredReputationRank maxcount stackable
		ContainerSlots StatsCount`)
	for i := 1; i <= 10; i++ {
		cols = append(cols, fmt.Sprintf("stat_type%d", i), fmt.Sprintf("stat_value%d", i))
	}
	cols = append(cols, strings.Fields(`ScalingStatDistribution ScalingStatValue dmg_min1 dmg_max1 dmg_type1 dmg_min2 dmg_max2
		dmg_type2 armor holy_res fire_res nature_res frost_res shadow_res arcane_res delay ammo_type RangedModRange`)...)
	for i := 1; i <= 5; i++ {
		for _, c := range []string{"spellid_", "spelltrigger_", "spellcharges_", "spellppmRate_", "spellcooldown_", "spellcategory_", "spellcategorycooldown_"} {
			cols = append(cols, fmt.Sprintf("%s%d", c, i))
		}
	}
	cols = append(cols, strings.Fields(`bonding description PageText LanguageID PageMaterial startquest lockid Material sheath
		RandomProperty RandomSuffix block itemset MaxDurability area Map BagFamily TotemCategory socketColor_1 socketContent_1
		socketColor_2 socketContent_2 socketColor_3 socketContent_3 socketBonus GemProperties RequiredDisenchantSkill
		ArmorDamageModifier duration ItemLimitCategory HolidayId ScriptName DisenchantID FoodType minMoneyLoot maxMoneyLoot
		flagsCustom VerifiedBuild`)...)
	return cols
}()

// Data accumulates rows from any number of SQL/CSV/WDB sources; later sources override earlier ones.
type Data struct {
	Items      map[uint32]*ItemTemplate
	SpellProcs map[uint32]*SpellProc
	SpellBonus map[uint32]*SpellBonus
	// True when item data came from a complete item_template (SQL/CSV) rather than a partial client cache.
	CompleteItemList bool
	Warnings         []string

	rows    map[string]map[uint32]Row
	columns map[string][]string
}

func NewData() *Data {
	return &Data{
		Items:      map[uint32]*ItemTemplate{},
		SpellProcs: map[uint32]*SpellProc{},
		SpellBonus: map[uint32]*SpellBonus{},
		rows:       map[string]map[uint32]Row{},
		columns:    map[string][]string{"item_template": defaultItemTemplateColumns},
	}
}

func (d *Data) warn(format string, args ...any) {
	d.Warnings = append(d.Warnings, fmt.Sprintf(format, args...))
}

func (d *Data) putRow(table string, row Row) {
	key := row.u32(sqlTables[table])
	if key == 0 && table == "spell_proc" {
		key = row.u32("entry")
	}
	if key == 0 {
		return
	}
	if d.rows[table] == nil {
		d.rows[table] = map[uint32]Row{}
	}
	d.rows[table][key] = row
	if table == "item_template" {
		d.CompleteItemList = true
	}
}

// Finalize converts accumulated raw rows into typed records. Call after loading all sources.
func (d *Data) Finalize() {
	for id, row := range d.rows["item_template"] {
		d.Items[id] = row.ToItemTemplate()
	}
	for _, table := range []string{"spell_proc_event", "spell_proc"} { // spell_proc wins if both exist
		for id, row := range d.rows[table] {
			d.SpellProcs[id] = rowToSpellProc(table, row)
		}
	}
	for id, row := range d.rows["spell_bonus_data"] {
		d.SpellBonus[id] = rowToSpellBonus(row)
	}
}

func (d *Data) LoadSQLFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, stmt := range splitStatements(string(data)) {
		d.execStatement(stmt)
	}
	return nil
}

// splitStatements splits SQL text on ';' outside of quotes and comments.
func splitStatements(sql string) []string {
	var stmts []string
	start := 0
	var quote byte
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if quote != 0 {
			if c == '\\' && quote != '`' {
				i++
			} else if c == quote {
				if i+1 < len(sql) && sql[i+1] == quote { // '' escape
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		switch {
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-', c == '#':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = len(sql)
			} else {
				i += end + 3
			}
		case c == ';':
			stmts = append(stmts, sql[start:i])
			start = i + 1
		}
	}
	if strings.TrimSpace(sql[start:]) != "" {
		stmts = append(stmts, sql[start:])
	}
	return stmts
}

// lexer tokenizes one statement: identifiers/keywords, numbers, strings, punctuation.
type token struct {
	kind byte // 'i' ident/keyword, 'n' number, 's' string, 'p' punctuation, 'N' NULL
	text string
}

func tokenize(stmt string) []token {
	var toks []token
	for i := 0; i < len(stmt); {
		c := stmt[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '-' && i+1 < len(stmt) && stmt[i+1] == '-', c == '#':
			for i < len(stmt) && stmt[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(stmt) && stmt[i+1] == '*':
			end := strings.Index(stmt[i+2:], "*/")
			if end < 0 {
				i = len(stmt)
			} else {
				i += end + 4
			}
		case c == '\'' || c == '"':
			var sb strings.Builder
			i++
			for i < len(stmt) {
				ch := stmt[i]
				if ch == '\\' && i+1 < len(stmt) {
					switch stmt[i+1] {
					case 'n':
						sb.WriteByte('\n')
					case 'r':
						sb.WriteByte('\r')
					case 't':
						sb.WriteByte('\t')
					case '0':
						sb.WriteByte(0)
					default:
						sb.WriteByte(stmt[i+1])
					}
					i += 2
					continue
				}
				if ch == c {
					if i+1 < len(stmt) && stmt[i+1] == c {
						sb.WriteByte(c)
						i += 2
						continue
					}
					i++
					break
				}
				sb.WriteByte(ch)
				i++
			}
			toks = append(toks, token{'s', sb.String()})
		case c == '`':
			end := strings.IndexByte(stmt[i+1:], '`')
			if end < 0 {
				end = len(stmt) - i - 1
			}
			toks = append(toks, token{'i', stmt[i+1 : i+1+end]})
			i += end + 2
		case (c >= '0' && c <= '9') || c == '.' || ((c == '-' || c == '+') && i+1 < len(stmt) && (stmt[i+1] >= '0' && stmt[i+1] <= '9' || stmt[i+1] == '.') && lastIsOperator(toks)):
			j := i + 1
			for j < len(stmt) && (stmt[j] >= '0' && stmt[j] <= '9' || stmt[j] == '.' || stmt[j] == 'e' || stmt[j] == 'E' ||
				((stmt[j] == '-' || stmt[j] == '+') && (stmt[j-1] == 'e' || stmt[j-1] == 'E'))) {
				j++
			}
			toks = append(toks, token{'n', stmt[i:j]})
			i = j
		case isIdentChar(c):
			j := i
			for j < len(stmt) && isIdentChar(stmt[j]) {
				j++
			}
			word := stmt[i:j]
			if strings.EqualFold(word, "NULL") {
				toks = append(toks, token{'N', ""})
			} else {
				toks = append(toks, token{'i', word})
			}
			i = j
		default:
			toks = append(toks, token{'p', string(c)})
			i++
		}
	}
	return toks
}

func lastIsOperator(toks []token) bool {
	if len(toks) == 0 {
		return true
	}
	t := toks[len(toks)-1]
	return t.kind == 'p' && (t.text == "(" || t.text == "," || t.text == "=" || t.text == "<" || t.text == ">") ||
		t.kind == 'i' && (strings.EqualFold(t.text, "AND") || strings.EqualFold(t.text, "BETWEEN") || strings.EqualFold(t.text, "IN"))
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return token{}
}
func (p *parser) next() token { t := p.peek(); p.pos++; return t }
func (p *parser) done() bool  { return p.pos >= len(p.toks) }
func (p *parser) isKeyword(kw string) bool {
	t := p.peek()
	return t.kind == 'i' && strings.EqualFold(t.text, kw)
}
func (p *parser) acceptKeyword(kw string) bool {
	if p.isKeyword(kw) {
		p.pos++
		return true
	}
	return false
}
func (p *parser) acceptPunct(s string) bool {
	if t := p.peek(); t.kind == 'p' && t.text == s {
		p.pos++
		return true
	}
	return false
}

// tableName reads `db`.`table` or table, returning the lowercase table name.
func (p *parser) tableName() string {
	name := p.next().text
	if p.acceptPunct(".") {
		name = p.next().text
	}
	return strings.ToLower(name)
}

func (p *parser) value() (string, bool) {
	t := p.next()
	switch t.kind {
	case 'n', 's':
		return t.text, true
	case 'N':
		return "", true
	case 'i': // TRUE/FALSE or function calls are not supported
		if strings.EqualFold(t.text, "TRUE") {
			return "1", true
		} else if strings.EqualFold(t.text, "FALSE") {
			return "0", true
		}
	case 'p':
		if t.text == "-" { // "- 5"
			v := p.next()
			return "-" + v.text, v.kind == 'n'
		}
	}
	return t.text, false
}

func (d *Data) execStatement(stmt string) {
	trimmed := strings.TrimSpace(stmt)
	if trimmed == "" {
		return
	}
	// Fast reject: only tokenize statements that mention a table we care about.
	lower := strings.ToLower(trimmed[:min(len(trimmed), 256)])
	interesting := false
	for table := range sqlTables {
		if strings.Contains(lower, table) {
			interesting = true
			break
		}
	}
	if !interesting {
		return
	}

	p := &parser{toks: tokenize(trimmed)}
	switch {
	case p.acceptKeyword("CREATE"):
		p.acceptKeyword("TEMPORARY")
		if !p.acceptKeyword("TABLE") {
			return
		}
		if p.acceptKeyword("IF") {
			p.acceptKeyword("NOT")
			p.acceptKeyword("EXISTS")
		}
		table := p.tableName()
		if _, ok := sqlTables[table]; ok {
			d.parseCreateTable(table, p)
		}
	case p.isKeyword("INSERT") || p.isKeyword("REPLACE"):
		p.next()
		p.acceptKeyword("IGNORE")
		p.acceptKeyword("INTO")
		table := p.tableName()
		if _, ok := sqlTables[table]; ok {
			d.parseInsert(table, p)
		}
	case p.acceptKeyword("UPDATE"):
		table := p.tableName()
		if _, ok := sqlTables[table]; ok {
			d.parseUpdate(table, p, trimmed)
		}
	case p.acceptKeyword("DELETE"):
		p.acceptKeyword("FROM")
		table := p.tableName()
		if _, ok := sqlTables[table]; ok {
			keys, ok := d.parseWhere(table, p)
			if !ok {
				if p.done() { // DELETE FROM table; (full wipe)
					delete(d.rows, table)
				} else {
					d.warn("unsupported DELETE on %s: %.120s", table, trimmed)
				}
				return
			}
			for _, k := range keys {
				delete(d.rows[table], k)
			}
		}
	case p.acceptKeyword("TRUNCATE"):
		p.acceptKeyword("TABLE")
		delete(d.rows, p.tableName())
	}
}

func (d *Data) parseCreateTable(table string, p *parser) {
	if !p.acceptPunct("(") {
		return
	}
	var cols []string
	depth := 1
	expectColumn := true
	for !p.done() && depth > 0 {
		t := p.next()
		if t.kind == 'p' {
			switch t.text {
			case "(":
				depth++
			case ")":
				depth--
			case ",":
				if depth == 1 {
					expectColumn = true
				}
			}
			continue
		}
		if expectColumn && depth == 1 {
			expectColumn = false
			switch strings.ToUpper(t.text) {
			case "PRIMARY", "KEY", "UNIQUE", "INDEX", "CONSTRAINT", "FOREIGN", "FULLTEXT", "CHECK":
				continue
			}
			cols = append(cols, t.text)
		}
	}
	if len(cols) > 0 {
		d.columns[table] = cols
	}
}

func (d *Data) parseInsert(table string, p *parser) {
	cols := d.columns[table]
	if p.acceptPunct("(") {
		cols = nil
		for !p.done() && !p.acceptPunct(")") {
			t := p.next()
			if t.kind == 'i' {
				cols = append(cols, t.text)
			}
		}
	}
	if !p.acceptKeyword("VALUES") && !p.acceptKeyword("VALUE") {
		return
	}
	if len(cols) == 0 {
		d.warn("INSERT INTO %s without a column list and no CREATE TABLE seen; skipped", table)
		return
	}
	for !p.done() {
		if !p.acceptPunct("(") {
			break
		}
		var values []string
		ok := true
		for !p.done() {
			v, valid := p.value()
			ok = ok && valid
			values = append(values, v)
			if p.acceptPunct(",") {
				continue
			}
			if p.acceptPunct(")") {
				break
			}
		}
		if !ok {
			d.warn("%s: row with unsupported expression skipped", table)
		} else if len(values) != len(cols) {
			d.warn("%s: row has %d values but %d columns; skipped (key=%s)", table, len(values), len(cols), first(values))
		} else {
			d.putRow(table, NewRow(cols, values))
		}
		if !p.acceptPunct(",") {
			break
		}
	}
}

func (d *Data) parseUpdate(table string, p *parser, raw string) {
	if !p.acceptKeyword("SET") {
		return
	}
	var cols, vals []string
	for !p.done() && !p.isKeyword("WHERE") {
		col := p.next()
		if col.kind != 'i' || !p.acceptPunct("=") {
			d.warn("unsupported UPDATE on %s: %.160s", table, raw)
			return
		}
		v, ok := p.value()
		if !ok || !(p.isKeyword("WHERE") || p.acceptPunct(",") || p.done()) {
			d.warn("unsupported UPDATE (only literal values are supported) on %s: %.160s", table, raw)
			return
		}
		cols = append(cols, col.text)
		vals = append(vals, v)
	}
	keys, ok := d.parseWhere(table, p)
	if !ok {
		d.warn("unsupported UPDATE WHERE clause on %s: %.160s", table, raw)
		return
	}
	patch := NewRow(cols, vals)
	for _, k := range keys {
		row, exists := d.rows[table][k]
		if !exists {
			continue
		}
		for c, v := range patch {
			row[c] = v
		}
	}
}

// parseWhere supports `WHERE key = N`, `WHERE key IN (...)`, `WHERE key BETWEEN a AND b`,
// and `>=`/`<=`/`>`/`<` ranges combined with AND. Returns the matching existing keys.
func (d *Data) parseWhere(table string, p *parser) ([]uint32, bool) {
	if !p.acceptKeyword("WHERE") {
		return nil, false
	}
	keyCol := sqlTables[table]
	lo, hi := uint64(0), uint64(1<<32)
	var set map[uint32]bool
	for {
		col := p.next()
		if col.kind != 'i' || normalizeColumn(col.text) != keyCol && !(table == "item_template" && normalizeColumn(col.text) == "id") {
			return nil, false
		}
		switch {
		case p.acceptKeyword("IN"):
			if !p.acceptPunct("(") {
				return nil, false
			}
			set = map[uint32]bool{}
			for !p.done() && !p.acceptPunct(")") {
				v, ok := p.value()
				if !ok {
					return nil, false
				}
				n, _ := strconv.ParseUint(v, 10, 32)
				set[uint32(n)] = true
				p.acceptPunct(",")
			}
		case p.acceptKeyword("BETWEEN"):
			a, _ := p.value()
			p.acceptKeyword("AND")
			b, _ := p.value()
			na, _ := strconv.ParseUint(a, 10, 32)
			nb, _ := strconv.ParseUint(b, 10, 32)
			lo, hi = max(lo, na), min(hi, nb)
		default:
			op := p.next().text
			if (op == "<" || op == ">") && p.acceptPunct("=") {
				op += "="
			}
			v, ok := p.value()
			if !ok {
				return nil, false
			}
			n, _ := strconv.ParseUint(v, 10, 32)
			switch op {
			case "=":
				lo, hi = max(lo, n), min(hi, n)
			case ">=":
				lo = max(lo, n)
			case ">":
				lo = max(lo, n+1)
			case "<=":
				hi = min(hi, n)
			case "<":
				hi = min(hi, n-1)
			default:
				return nil, false
			}
		}
		if !p.acceptKeyword("AND") {
			break
		}
	}
	p.acceptPunct(";")
	if !p.done() {
		return nil, false
	}
	var keys []uint32
	for k := range d.rows[table] {
		if uint64(k) >= lo && uint64(k) <= hi && (set == nil || set[k]) {
			keys = append(keys, k)
		}
	}
	return keys, true
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
