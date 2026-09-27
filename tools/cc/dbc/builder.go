package dbc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Builder writes WDBC files. It's used to build test fixtures with the exact 3.3.5a layouts.
type Builder struct {
	fields  int
	records [][]uint32
	strings bytes.Buffer
	offsets map[string]uint32
}

func NewBuilder(fields int) *Builder {
	b := &Builder{fields: fields, offsets: map[string]uint32{}}
	b.strings.WriteByte(0) // offset 0 = empty string
	return b
}

func (b *Builder) str(s string) uint32 {
	if s == "" {
		return 0
	}
	if off, ok := b.offsets[s]; ok {
		return off
	}
	off := uint32(b.strings.Len())
	b.strings.WriteString(s)
	b.strings.WriteByte(0)
	b.offsets[s] = off
	return off
}

// Add appends a record. Values may be uint32, int32, int, float32 or string (stored in the string block).
func (b *Builder) Add(values map[int]any) {
	rec := make([]uint32, b.fields)
	for field, v := range values {
		switch x := v.(type) {
		case uint32:
			rec[field] = x
		case int32:
			rec[field] = uint32(x)
		case int:
			rec[field] = uint32(int32(x))
		case float32:
			rec[field] = math.Float32bits(x)
		case string:
			rec[field] = b.str(x)
		default:
			panic(fmt.Sprintf("unsupported DBC value %T", v))
		}
	}
	b.records = append(b.records, rec)
}

func (b *Builder) Bytes() []byte {
	var out bytes.Buffer
	out.WriteString("WDBC")
	for _, v := range []uint32{uint32(len(b.records)), uint32(b.fields), uint32(b.fields * 4), uint32(b.strings.Len())} {
		binary.Write(&out, binary.LittleEndian, v)
	}
	for _, rec := range b.records {
		binary.Write(&out, binary.LittleEndian, rec)
	}
	out.Write(b.strings.Bytes())
	return out.Bytes()
}

// Field offsets exported for fixture builders in other packages.
const (
	SpellFieldCount         = spellFieldCount
	SpellFieldID            = spId
	SpellFieldProcFlags     = spProcFlags
	SpellFieldProcChance    = spProcChance
	SpellFieldDurationIndex = spDurationIndex
	SpellFieldRecoveryTime  = spRecoveryTime
	SpellFieldStackAmount   = spStackAmount
	SpellFieldEquippedClass = spEquippedItemClass
	SpellFieldEquippedSub   = spEquippedItemSubClassMask
	SpellFieldEquippedInv   = spEquippedItemInvTypeMask
	SpellFieldEffect        = spEffect
	SpellFieldDieSides      = spEffectDieSides
	SpellFieldBasePoints    = spEffectBasePoints
	SpellFieldTargetA       = spEffectImplicitTargetA
	SpellFieldAura          = spEffectApplyAuraName
	SpellFieldMiscValue     = spEffectMiscValue
	SpellFieldTriggerSpell  = spEffectTriggerSpell
	SpellFieldSpellIconID   = spSpellIconID
	SpellFieldName          = spName
	SpellFieldDescription   = spDescription
	SpellFieldSpellFamily   = spSpellFamilyName
	SpellFieldSchoolMask    = spSchoolMask
)
