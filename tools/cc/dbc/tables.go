package dbc

import "fmt"

// ItemSet.dbc (53 fields): id, name[17], itemId[17], spellId[8], threshold[8], requiredSkill, requiredSkillRank.
type ItemSet struct {
	ID      uint32         `json:"id"`
	Name    string         `json:"name"`
	ItemIDs []uint32       `json:"itemIds"`
	Bonuses []ItemSetBonus `json:"bonuses"`
}

type ItemSetBonus struct {
	Pieces  uint32 `json:"pieces"`
	SpellID uint32 `json:"spellId"`
}

// SpellItemEnchantment.dbc (38 fields).
type SpellItemEnchantment struct {
	ID                 uint32    `json:"id"`
	Charges            uint32    `json:"charges,omitempty"`
	Type               [3]uint32 `json:"type"`      // 1=proc spell, 2=damage, 3=equip spell, 4=resistance, 5=stat, 6=totem, 7=use spell, 8=prismatic socket
	Amount             [3]int32  `json:"amount"`    // min
	AmountMax          [3]int32  `json:"amountMax"` // max
	Arg                [3]uint32 `json:"arg"`       // spell id / ITEM_MOD stat / resistance school
	Name               string    `json:"name"`
	ItemVisual         uint32    `json:"itemVisual,omitempty"`
	Flags              uint32    `json:"flags,omitempty"`
	GemItemID          uint32    `json:"gemItemId,omitempty"`
	ConditionID        uint32    `json:"conditionId,omitempty"` // SpellItemEnchantmentCondition.dbc (meta gem requirements)
	RequiredSkill      uint32    `json:"requiredSkill,omitempty"`
	RequiredSkillValue uint32    `json:"requiredSkillValue,omitempty"`
	RequiredLevel      uint32    `json:"requiredLevel,omitempty"`
}

const (
	EnchantTypeProcSpell  = 1
	EnchantTypeDamage     = 2
	EnchantTypeEquipSpell = 3
	EnchantTypeResistance = 4
	EnchantTypeStat       = 5
	EnchantTypeTotem      = 6
	EnchantTypeUseSpell   = 7
	EnchantTypePrismatic  = 8
)

// GemProperties.dbc (5 fields): id, enchantId, maxCountInv, maxCountItem, color mask (1 meta, 2 red, 4 yellow, 8 blue).
type GemProperties struct {
	ID        uint32
	EnchantID uint32
	Color     uint32
}

// Tables bundles the item-related DBCs. Missing optional files leave their map empty.
type Tables struct {
	ItemSets      map[uint32]*ItemSet
	Enchants      map[uint32]*SpellItemEnchantment
	GemProperties map[uint32]*GemProperties
	DisplayIcons  map[uint32]string // ItemDisplayInfo.dbc: displayId -> icon name
	// Item.dbc: id -> [class, subclass, inventoryType, displayId]. Useful to spot custom items even without server data.
	ClientItems map[uint32][4]uint32
}

func LoadTables(d *Dir) (*Tables, error) {
	t := &Tables{
		ItemSets:      map[uint32]*ItemSet{},
		Enchants:      map[uint32]*SpellItemEnchantment{},
		GemProperties: map[uint32]*GemProperties{},
		DisplayIcons:  map[uint32]string{},
		ClientItems:   map[uint32][4]uint32{},
	}

	load := func(name string, fields int, fn func(r Record)) error {
		f, err := d.Open(name)
		if err != nil {
			return err
		}
		if f == nil {
			return nil
		}
		if err := f.Expect(fields); err != nil {
			return err
		}
		for i := 0; i < f.RecordCount; i++ {
			fn(f.Record(i))
		}
		return nil
	}

	err := load("ItemSet.dbc", 53, func(r Record) {
		set := &ItemSet{ID: r.Uint32(0), Name: r.LocString(1)}
		for i := 0; i < 17; i++ {
			if id := r.Uint32(1 + locStringFields + i); id != 0 {
				set.ItemIDs = append(set.ItemIDs, id)
			}
		}
		for i := 0; i < 8; i++ {
			spell, pieces := r.Uint32(35+i), r.Uint32(43+i)
			if spell != 0 {
				set.Bonuses = append(set.Bonuses, ItemSetBonus{Pieces: pieces, SpellID: spell})
			}
		}
		t.ItemSets[set.ID] = set
	})
	if err != nil {
		return nil, err
	}

	err = load("SpellItemEnchantment.dbc", 38, func(r Record) {
		e := &SpellItemEnchantment{ID: r.Uint32(0), Charges: r.Uint32(1)}
		for i := 0; i < 3; i++ {
			e.Type[i] = r.Uint32(2 + i)
			e.Amount[i] = r.Int32(5 + i)
			e.AmountMax[i] = r.Int32(8 + i)
			e.Arg[i] = r.Uint32(11 + i)
		}
		e.Name = r.LocString(14)
		e.ItemVisual = r.Uint32(31)
		e.Flags = r.Uint32(32)
		e.GemItemID = r.Uint32(33)
		e.ConditionID = r.Uint32(34)
		e.RequiredSkill = r.Uint32(35)
		e.RequiredSkillValue = r.Uint32(36)
		e.RequiredLevel = r.Uint32(37)
		t.Enchants[e.ID] = e
	})
	if err != nil {
		return nil, err
	}

	err = load("GemProperties.dbc", 5, func(r Record) {
		g := &GemProperties{ID: r.Uint32(0), EnchantID: r.Uint32(1), Color: r.Uint32(4)}
		t.GemProperties[g.ID] = g
	})
	if err != nil {
		return nil, err
	}

	err = load("ItemDisplayInfo.dbc", 25, func(r Record) {
		if icon := r.String(5); icon != "" {
			t.DisplayIcons[r.Uint32(0)] = IconName(icon)
		}
	})
	if err != nil {
		return nil, err
	}

	err = load("Item.dbc", 8, func(r Record) {
		t.ClientItems[r.Uint32(0)] = [4]uint32{r.Uint32(1), r.Uint32(2), r.Uint32(6), r.Uint32(5)}
	})
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (e *SpellItemEnchantment) String() string {
	return fmt.Sprintf("enchant %d %q", e.ID, e.Name)
}
