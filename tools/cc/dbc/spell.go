package dbc

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Spell.dbc 3.3.5a (build 12340): 234 fields.
const spellFieldCount = 234

// Field offsets into Spell.dbc (matches TrinityCore 3.3.5 SpellEntry).
const (
	spId                         = 0
	spCategory                   = 1
	spDispel                     = 2
	spMechanic                   = 3
	spAttributes                 = 4 // 4..11: Attributes, AttributesEx..AttributesEx7
	spStances                    = 12
	spTargets                    = 16
	spCasterAuraSpell            = 24
	spTargetAuraSpell            = 25
	spCastingTimeIndex           = 28
	spRecoveryTime               = 29
	spCategoryRecoveryTime       = 30
	spProcFlags                  = 34
	spProcChance                 = 35
	spProcCharges                = 36
	spMaxLevel                   = 37
	spBaseLevel                  = 38
	spSpellLevel                 = 39
	spDurationIndex              = 40
	spPowerType                  = 41
	spManaCost                   = 42
	spRangeIndex                 = 46
	spSpeed                      = 47
	spStackAmount                = 49
	spEquippedItemClass          = 68
	spEquippedItemSubClassMask   = 69
	spEquippedItemInvTypeMask    = 70
	spEffect                     = 71
	spEffectDieSides             = 74
	spEffectRealPointsPerLevel   = 77
	spEffectBasePoints           = 80
	spEffectMechanic             = 83
	spEffectImplicitTargetA      = 86
	spEffectImplicitTargetB      = 89
	spEffectRadiusIndex          = 92
	spEffectApplyAuraName        = 95
	spEffectAmplitude            = 98
	spEffectValueMultiplier      = 101
	spEffectChainTarget          = 104
	spEffectItemType             = 107
	spEffectMiscValue            = 110
	spEffectMiscValueB           = 113
	spEffectTriggerSpell         = 116
	spEffectPointsPerComboPoint  = 119
	spEffectSpellClassMaskA      = 122 // 122..130: [3 effects][3 words], stored effect-major
	spSpellIconID                = 133
	spName                       = 136
	spRank                       = 153
	spDescription                = 170
	spToolTip                    = 187
	spManaCostPercentage         = 204
	spStartRecoveryCategory      = 205
	spStartRecoveryTime          = 206
	spMaxTargetLevel             = 207
	spSpellFamilyName            = 208
	spSpellFamilyFlags           = 209
	spMaxAffectedTargets         = 212
	spDmgClass                   = 213
	spPreventionType             = 214
	spEffectDamageMultiplier     = 216
	spSchoolMask                 = 225
	spEffectBonusMultiplier      = 229
	spSpellDescriptionVariableID = 232
	spSpellDifficultyID          = 233
)

type SpellEffect struct {
	Effect              uint32    `json:"effect"`
	EffectName          string    `json:"effectName,omitempty"`
	Aura                uint32    `json:"aura,omitempty"`
	AuraName            string    `json:"auraName,omitempty"`
	BasePoints          int32     `json:"basePoints"`
	DieSides            int32     `json:"dieSides,omitempty"`
	RealPointsPerLevel  float32   `json:"realPointsPerLevel,omitempty"`
	PointsPerComboPoint float32   `json:"pointsPerComboPoint,omitempty"`
	Mechanic            uint32    `json:"mechanic,omitempty"`
	ImplicitTargetA     uint32    `json:"implicitTargetA,omitempty"`
	ImplicitTargetB     uint32    `json:"implicitTargetB,omitempty"`
	RadiusIndex         uint32    `json:"radiusIndex,omitempty"`
	Amplitude           int32     `json:"amplitude,omitempty"`
	ValueMultiplier     float32   `json:"valueMultiplier,omitempty"`
	ChainTarget         uint32    `json:"chainTarget,omitempty"`
	ItemType            uint32    `json:"itemType,omitempty"`
	MiscValue           int32     `json:"miscValue,omitempty"`
	MiscValueB          int32     `json:"miscValueB,omitempty"`
	TriggerSpell        uint32    `json:"triggerSpell,omitempty"`
	SpellClassMask      [3]uint32 `json:"spellClassMask"`
	DamageMultiplier    float32   `json:"damageMultiplier,omitempty"`
	BonusMultiplier     float32   `json:"bonusMultiplier,omitempty"`
}

// Min/Max are the effect's value range at the spell's base level, as the server computes it.
func (e SpellEffect) Min() int32 {
	if e.DieSides >= 1 {
		return e.BasePoints + 1
	}
	return e.BasePoints
}
func (e SpellEffect) Max() int32 { return e.BasePoints + e.DieSides }
func (e SpellEffect) Avg() float64 {
	if e.DieSides <= 1 {
		return float64(e.BasePoints + e.DieSides)
	}
	return float64(e.Min()+e.Max()) / 2
}

type Spell struct {
	ID          uint32 `json:"id"`
	Name        string `json:"name"`
	Rank        string `json:"rank,omitempty"`
	Description string `json:"description,omitempty"`
	ToolTip     string `json:"tooltip,omitempty"`
	// Description with $s1/$d/$h-style tokens substituted, for reports.
	Text string `json:"text,omitempty"`
	Icon string `json:"icon,omitempty"`

	Category               uint32    `json:"category,omitempty"`
	Dispel                 uint32    `json:"dispel,omitempty"`
	Mechanic               uint32    `json:"mechanic,omitempty"`
	Attributes             [8]uint32 `json:"attributes"`
	CastingTimeIndex       uint32    `json:"castingTimeIndex,omitempty"`
	RecoveryTime           uint32    `json:"recoveryTimeMs,omitempty"`
	CategoryRecoveryTime   uint32    `json:"categoryRecoveryTimeMs,omitempty"`
	StartRecoveryCategory  uint32    `json:"startRecoveryCategory,omitempty"`
	StartRecoveryTime      uint32    `json:"gcdMs,omitempty"`
	ProcFlags              uint32    `json:"procFlags,omitempty"`
	ProcChance             uint32    `json:"procChance,omitempty"`
	ProcCharges            uint32    `json:"procCharges,omitempty"`
	BaseLevel              uint32    `json:"baseLevel,omitempty"`
	SpellLevel             uint32    `json:"spellLevel,omitempty"`
	MaxLevel               uint32    `json:"maxLevel,omitempty"`
	DurationIndex          uint32    `json:"durationIndex,omitempty"`
	DurationMs             int32     `json:"durationMs,omitempty"`
	PowerType              int32     `json:"powerType,omitempty"`
	ManaCost               uint32    `json:"manaCost,omitempty"`
	ManaCostPercentage     uint32    `json:"manaCostPct,omitempty"`
	RangeIndex             uint32    `json:"rangeIndex,omitempty"`
	Speed                  float32   `json:"speed,omitempty"`
	StackAmount            uint32    `json:"stackAmount,omitempty"`
	EquippedItemClass      int32     `json:"equippedItemClass"`
	EquippedItemSubClass   int32     `json:"equippedItemSubClassMask,omitempty"`
	EquippedItemInvType    int32     `json:"equippedItemInvTypeMask,omitempty"`
	SpellFamilyName        uint32    `json:"spellFamilyName,omitempty"`
	SpellFamilyFlags       [3]uint32 `json:"spellFamilyFlags"`
	MaxAffectedTargets     uint32    `json:"maxAffectedTargets,omitempty"`
	DmgClass               uint32    `json:"dmgClass,omitempty"`
	SchoolMask             uint32    `json:"schoolMask"`
	DescriptionVariablesID uint32    `json:"descriptionVariablesId,omitempty"`
	SpellIconID            uint32    `json:"spellIconId,omitempty"`

	Effects [3]SpellEffect `json:"effects"`
}

func decodeSpell(r Record) *Spell {
	s := &Spell{
		ID:                     r.Uint32(spId),
		Name:                   r.LocString(spName),
		Rank:                   r.LocString(spRank),
		Description:            r.LocString(spDescription),
		ToolTip:                r.LocString(spToolTip),
		Category:               r.Uint32(spCategory),
		Dispel:                 r.Uint32(spDispel),
		Mechanic:               r.Uint32(spMechanic),
		CastingTimeIndex:       r.Uint32(spCastingTimeIndex),
		RecoveryTime:           r.Uint32(spRecoveryTime),
		CategoryRecoveryTime:   r.Uint32(spCategoryRecoveryTime),
		StartRecoveryCategory:  r.Uint32(spStartRecoveryCategory),
		StartRecoveryTime:      r.Uint32(spStartRecoveryTime),
		ProcFlags:              r.Uint32(spProcFlags),
		ProcChance:             r.Uint32(spProcChance),
		ProcCharges:            r.Uint32(spProcCharges),
		BaseLevel:              r.Uint32(spBaseLevel),
		SpellLevel:             r.Uint32(spSpellLevel),
		MaxLevel:               r.Uint32(spMaxLevel),
		DurationIndex:          r.Uint32(spDurationIndex),
		PowerType:              r.Int32(spPowerType),
		ManaCost:               r.Uint32(spManaCost),
		ManaCostPercentage:     r.Uint32(spManaCostPercentage),
		RangeIndex:             r.Uint32(spRangeIndex),
		Speed:                  r.Float(spSpeed),
		StackAmount:            r.Uint32(spStackAmount),
		EquippedItemClass:      r.Int32(spEquippedItemClass),
		EquippedItemSubClass:   r.Int32(spEquippedItemSubClassMask),
		EquippedItemInvType:    r.Int32(spEquippedItemInvTypeMask),
		SpellFamilyName:        r.Uint32(spSpellFamilyName),
		MaxAffectedTargets:     r.Uint32(spMaxAffectedTargets),
		DmgClass:               r.Uint32(spDmgClass),
		SchoolMask:             r.Uint32(spSchoolMask),
		DescriptionVariablesID: r.Uint32(spSpellDescriptionVariableID),
		SpellIconID:            r.Uint32(spSpellIconID),
	}
	for i := 0; i < 8; i++ {
		s.Attributes[i] = r.Uint32(spAttributes + i)
	}
	for i := 0; i < 3; i++ {
		s.SpellFamilyFlags[i] = r.Uint32(spSpellFamilyFlags + i)
		e := &s.Effects[i]
		e.Effect = r.Uint32(spEffect + i)
		e.EffectName = EffectName(e.Effect)
		e.DieSides = r.Int32(spEffectDieSides + i)
		e.RealPointsPerLevel = r.Float(spEffectRealPointsPerLevel + i)
		e.BasePoints = r.Int32(spEffectBasePoints + i)
		e.Mechanic = r.Uint32(spEffectMechanic + i)
		e.ImplicitTargetA = r.Uint32(spEffectImplicitTargetA + i)
		e.ImplicitTargetB = r.Uint32(spEffectImplicitTargetB + i)
		e.RadiusIndex = r.Uint32(spEffectRadiusIndex + i)
		e.Aura = r.Uint32(spEffectApplyAuraName + i)
		e.AuraName = AuraName(e.Aura)
		e.Amplitude = r.Int32(spEffectAmplitude + i)
		e.ValueMultiplier = r.Float(spEffectValueMultiplier + i)
		e.ChainTarget = r.Uint32(spEffectChainTarget + i)
		e.ItemType = r.Uint32(spEffectItemType + i)
		e.MiscValue = r.Int32(spEffectMiscValue + i)
		e.MiscValueB = r.Int32(spEffectMiscValueB + i)
		e.TriggerSpell = r.Uint32(spEffectTriggerSpell + i)
		e.PointsPerComboPoint = r.Float(spEffectPointsPerComboPoint + i)
		for j := 0; j < 3; j++ {
			e.SpellClassMask[j] = r.Uint32(spEffectSpellClassMaskA + i*3 + j)
		}
		e.DamageMultiplier = r.Float(spEffectDamageMultiplier + i)
		e.BonusMultiplier = r.Float(spEffectBonusMultiplier + i)
	}
	return s
}

// SpellStore holds decoded spells and the small lookup tables needed to describe them.
type SpellStore struct {
	Spells    map[uint32]*Spell
	Durations map[uint32]int32  // SpellDuration.dbc: index -> base duration (ms)
	Icons     map[uint32]string // SpellIcon.dbc: id -> icon name (lowercase, no path)
}

func LoadSpells(d *Dir) (*SpellStore, error) {
	store := &SpellStore{Spells: map[uint32]*Spell{}, Durations: map[uint32]int32{}, Icons: map[uint32]string{}}

	if f, err := d.Open("SpellDuration.dbc"); err != nil {
		return nil, err
	} else if f != nil {
		if err := f.Expect(4); err != nil {
			return nil, err
		}
		for i := 0; i < f.RecordCount; i++ {
			r := f.Record(i)
			store.Durations[r.Uint32(0)] = r.Int32(1)
		}
	}

	if f, err := d.Open("SpellIcon.dbc"); err != nil {
		return nil, err
	} else if f != nil {
		if err := f.Expect(2); err != nil {
			return nil, err
		}
		for i := 0; i < f.RecordCount; i++ {
			r := f.Record(i)
			store.Icons[r.Uint32(0)] = IconName(r.String(1))
		}
	}

	f, err := d.Open("Spell.dbc")
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, fmt.Errorf("Spell.dbc not found")
	}
	if err := f.Expect(spellFieldCount); err != nil {
		return nil, err
	}
	for i := 0; i < f.RecordCount; i++ {
		s := decodeSpell(f.Record(i))
		s.DurationMs = store.Durations[s.DurationIndex]
		s.Icon = store.Icons[s.SpellIconID]
		store.Spells[s.ID] = s
	}
	for _, s := range store.Spells {
		s.Text = store.FormatText(s, s.Description)
	}
	return store, nil
}

// Spell.dbc fields holding floats; everything else is an integer except the localized strings.
var spellFloatFields = map[int]bool{spSpeed: true}

func init() {
	for i := 0; i < 3; i++ {
		for _, f := range []int{spEffectRealPointsPerLevel, spEffectValueMultiplier, spEffectPointsPerComboPoint, spEffectDamageMultiplier, spEffectBonusMultiplier} {
			spellFloatFields[f+i] = true
		}
	}
}

func isSpellStringField(field int) bool {
	for _, start := range []int{spName, spRank, spDescription, spToolTip} {
		if field >= start && field < start+16 {
			return true
		}
	}
	return false
}

// MergeTable merges server-side spells (AzerothCore/TrinityCore `spell_dbc`, whose columns follow
// the Spell.dbc field order) into the store. Server rows replace client rows with the same ID.
func (store *SpellStore) MergeTable(header []string, rows [][]string) (added, replaced int, err error) {
	if len(header) != spellFieldCount {
		return 0, 0, fmt.Errorf("expected %d columns in Spell.dbc order, got %d", spellFieldCount, len(header))
	}
	b := NewBuilder(spellFieldCount)
	for n, row := range rows {
		if len(row) != spellFieldCount {
			return 0, 0, fmt.Errorf("row %d has %d columns", n+1, len(row))
		}
		values := make(map[int]any, spellFieldCount)
		for i, v := range row {
			switch {
			case isSpellStringField(i):
				values[i] = v
			case spellFloatFields[i]:
				f, _ := strconv.ParseFloat(v, 32)
				values[i] = float32(f)
			default:
				num, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
				if err != nil && v != "" {
					f, ferr := strconv.ParseFloat(v, 64)
					if ferr != nil {
						return 0, 0, fmt.Errorf("row %d column %s: %q is not a number", n+1, header[i], v)
					}
					num = int64(f)
				}
				values[i] = uint32(num)
			}
		}
		b.Add(values)
	}
	f, err := Parse("spell_dbc", b.Bytes())
	if err != nil {
		return 0, 0, err
	}
	for i := 0; i < f.RecordCount; i++ {
		s := decodeSpell(f.Record(i))
		s.DurationMs = store.Durations[s.DurationIndex]
		s.Icon = store.Icons[s.SpellIconID]
		if _, ok := store.Spells[s.ID]; ok {
			replaced++
		} else {
			added++
		}
		store.Spells[s.ID] = s
	}
	// Client spells can reference server-only ones (e.g. set bonus texts quoting a spell_dbc value).
	for _, s := range store.Spells {
		s.Text = store.FormatText(s, s.Description)
	}
	return added, replaced, nil
}

// IconName turns "Interface\Icons\INV_Sword_39" into "inv_sword_39", the form wowhead/zamimg use.
func IconName(path string) string {
	path = strings.ReplaceAll(path, "/", "\\")
	if i := strings.LastIndex(path, "\\"); i >= 0 {
		path = path[i+1:]
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, ".blp"), ".BLP")
	return strings.ToLower(path)
}

var descTokenRegex = regexp.MustCompile(`\$(\d*)([a-zA-Z])(\d?)`)

// "$/1000;s1" (a token divided by a constant) and "${$m1/-1000}.1" (an expression, optionally
// followed by the number of decimals to show).
var descDivideRegex = regexp.MustCompile(`\$/(\d+);(\d*)([a-zA-Z])(\d?)`)
var descExprRegex = regexp.MustCompile(`\$\{([^{}]*)\}(?:\.(\d))?`)

// FormatText substitutes the description tokens the client uses in item and spell texts: $s1, $m1,
// $d, $h, $o1, $t1, $u, $n, $x1, cross-spell references like $12345s1, divisions like $/1000;s1 and
// arithmetic like ${$m1/-1000}.1. Anything else ($a1, $PL, max(), $?conditionals) is left as-is.
func (store *SpellStore) FormatText(s *Spell, text string) string {
	text = descExprRegex.ReplaceAllStringFunc(text, func(tok string) string {
		m := descExprRegex.FindStringSubmatch(tok)
		unresolved := false
		expr := descTokenRegex.ReplaceAllStringFunc(m[1], func(inner string) string {
			v, ok := store.tokenValue(s, descTokenRegex.FindStringSubmatch(inner))
			if !ok {
				unresolved = true
				return inner
			}
			return strconv.FormatFloat(v, 'f', -1, 64)
		})
		v, ok := evalExpr(expr)
		if unresolved || !ok {
			return tok
		}
		decimals := 0 // whole numbers unless the text asks for decimals
		if m[2] != "" {
			decimals, _ = strconv.Atoi(m[2])
		}
		return formatNumber(math.Abs(v), decimals)
	})
	text = descDivideRegex.ReplaceAllStringFunc(text, func(tok string) string {
		m := descDivideRegex.FindStringSubmatch(tok)
		div, _ := strconv.Atoi(m[1])
		v, ok := store.tokenValue(s, []string{"", m[2], m[3], m[4]})
		if !ok || div == 0 {
			return tok
		}
		return formatNumber(math.Abs(v)/float64(div), -1)
	})
	return descTokenRegex.ReplaceAllStringFunc(text, func(tok string) string {
		m := descTokenRegex.FindStringSubmatch(tok)
		target, eff, ok := store.tokenTarget(s, m)
		if !ok {
			return tok
		}
		switch strings.ToLower(m[2]) {
		case "s", "m":
			lo, hi := absInt(eff.Min()), absInt(eff.Max())
			if hi > lo {
				return fmt.Sprintf("%d to %d", lo, hi)
			}
			return strconv.Itoa(int(lo))
		case "d":
			if target.DurationMs > 0 {
				return formatSeconds(target.DurationMs) + " sec"
			}
			return tok
		}
		if v, ok := store.tokenValue(s, m); ok {
			return formatNumber(math.Abs(v), -1)
		}
		return tok
	})
}

// tokenTarget resolves the spell and effect a token refers to (m is a descTokenRegex match).
func (store *SpellStore) tokenTarget(s *Spell, m []string) (*Spell, SpellEffect, bool) {
	target := s
	if m[1] != "" {
		id, _ := strconv.Atoi(m[1])
		other, ok := store.Spells[uint32(id)]
		if !ok {
			return nil, SpellEffect{}, false
		}
		target = other
	}
	idx := 0
	if m[3] != "" {
		idx, _ = strconv.Atoi(m[3])
		idx--
	}
	if idx < 0 || idx > 2 {
		return nil, SpellEffect{}, false
	}
	return target, target.Effects[idx], true
}

// tokenValue is the signed numeric value of a token, as used in expressions: $d and $t in seconds.
func (store *SpellStore) tokenValue(s *Spell, m []string) (float64, bool) {
	target, eff, ok := store.tokenTarget(s, m)
	if !ok {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "s", "m":
		return float64(eff.Min()), true
	case "o":
		if eff.Amplitude > 0 && target.DurationMs > 0 {
			return float64(int(eff.Avg()) * int(target.DurationMs/eff.Amplitude)), true
		}
	case "t":
		if eff.Amplitude > 0 {
			return float64(eff.Amplitude) / 1000, true
		}
	case "d":
		if target.DurationMs > 0 {
			return float64(target.DurationMs) / 1000, true
		}
	case "h":
		return float64(target.ProcChance), true
	case "u":
		return float64(target.StackAmount), true
	case "n":
		return float64(target.ProcCharges), true
	case "x":
		return float64(eff.ChainTarget), true
	}
	return 0, false
}

// formatNumber shows at most 2 decimals, or exactly the given number of decimals when >= 0.
func formatNumber(v float64, decimals int) string {
	if decimals >= 0 {
		return strconv.FormatFloat(v, 'f', decimals, 64)
	}
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// evalExpr evaluates + - * / and parentheses over numbers.
func evalExpr(expr string) (float64, bool) {
	p := &exprParser{s: strings.ReplaceAll(expr, " ", "")}
	v, ok := p.sum()
	return v, ok && p.pos == len(p.s)
}

type exprParser struct {
	s   string
	pos int
}

func (p *exprParser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

func (p *exprParser) sum() (float64, bool) {
	v, ok := p.product()
	for ok && (p.peek() == '+' || p.peek() == '-') {
		op := p.peek()
		p.pos++
		var r float64
		if r, ok = p.product(); op == '+' {
			v += r
		} else {
			v -= r
		}
	}
	return v, ok
}

func (p *exprParser) product() (float64, bool) {
	v, ok := p.unary()
	for ok && (p.peek() == '*' || p.peek() == '/') {
		op := p.peek()
		p.pos++
		var r float64
		if r, ok = p.unary(); op == '*' {
			v *= r
		} else if r == 0 {
			return 0, false
		} else {
			v /= r
		}
	}
	return v, ok
}

func (p *exprParser) unary() (float64, bool) {
	switch p.peek() {
	case '-':
		p.pos++
		v, ok := p.unary()
		return -v, ok
	case '(':
		p.pos++
		v, ok := p.sum()
		if !ok || p.peek() != ')' {
			return 0, false
		}
		p.pos++
		return v, true
	}
	start := p.pos
	for p.pos < len(p.s) && (p.s[p.pos] >= '0' && p.s[p.pos] <= '9' || p.s[p.pos] == '.') {
		p.pos++
	}
	v, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	return v, err == nil
}

func absInt(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func formatSeconds(ms int32) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64)
}
