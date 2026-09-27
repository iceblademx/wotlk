package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
)

// LoadItemCacheWDB reads the 3.3.5a client item cache (Cache/WDB/<locale>/itemcache.wdb).
//
// The cache holds SMSG_ITEM_QUERY_SINGLE_RESPONSE payloads for every item the client has seen,
// so it only contains items you've encountered in game, and it does not include server-only
// fields such as proc PPM rates. Prefer an item_template export when available; items loaded
// from SQL/CSV take precedence over cache entries.
func (d *Data) LoadItemCacheWDB(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < 24 || string(data[:4]) != "BDIW" {
		return fmt.Errorf("%s: not an itemcache.wdb file (expected 'BDIW' signature)", path)
	}
	build := binary.LittleEndian.Uint32(data[4:])
	if build != 12340 {
		d.warn("%s: cache is from client build %d, expected 12340 (3.3.5a); parsing anyway", path, build)
	}
	// Header is signature, build, locale, then 2 or 3 uint32s depending on client; pick whichever
	// offset yields a cleanly parsing first record.
	pos := 24
	for _, hdr := range []int{20, 24} {
		if hdr+8 > len(data) {
			continue
		}
		entry := binary.LittleEndian.Uint32(data[hdr:])
		size := int(binary.LittleEndian.Uint32(data[hdr+4:]))
		if entry != 0 && size > 0 && hdr+8+size <= len(data) {
			if _, err := parseWDBItem(entry, data[hdr+8:hdr+8+size]); err == nil {
				pos = hdr
				break
			}
		}
	}
	loaded, failed := 0, 0
	for pos+8 <= len(data) {
		entry := binary.LittleEndian.Uint32(data[pos:])
		size := int(binary.LittleEndian.Uint32(data[pos+4:]))
		pos += 8
		if entry == 0 && size == 0 {
			break
		}
		if pos+size > len(data) {
			return fmt.Errorf("%s: truncated record for item %d", path, entry)
		}
		it, err := parseWDBItem(entry, data[pos:pos+size])
		pos += size
		if err != nil {
			failed++
			if failed <= 5 {
				d.warn("%s: item %d: %v", path, entry, err)
			}
			continue
		}
		// SQL/CSV data is authoritative; the cache only fills gaps.
		if _, ok := d.rows["item_template"][entry]; ok {
			continue
		}
		if _, ok := d.Items[entry]; !ok {
			d.Items[entry] = it
			loaded++
		}
	}
	if failed > 0 {
		d.warn("%s: %d of %d cached items could not be parsed", path, failed, loaded+failed)
	}
	return nil
}

type wdbReader struct {
	r   *bytes.Reader
	err error
}

func (w *wdbReader) u32() uint32 {
	var v uint32
	if w.err == nil {
		w.err = binary.Read(w.r, binary.LittleEndian, &v)
	}
	return v
}
func (w *wdbReader) i32() int32   { return int32(w.u32()) }
func (w *wdbReader) f32() float64 { return float64(math.Float32frombits(w.u32())) }
func (w *wdbReader) cstr() string {
	var buf []byte
	for w.err == nil {
		b, err := w.r.ReadByte()
		if err != nil {
			w.err = err
			break
		}
		if b == 0 {
			break
		}
		buf = append(buf, b)
	}
	return string(buf)
}

// parseWDBItem decodes the 3.3.5a item query response layout (TrinityCore HandleItemQuerySingleOpcode).
func parseWDBItem(entry uint32, payload []byte) (*ItemTemplate, error) {
	w := &wdbReader{r: bytes.NewReader(payload)}
	it := &ItemTemplate{Entry: entry}
	it.Class = w.u32()
	it.SubClass = w.u32()
	w.i32() // SoundOverrideSubclass
	it.Name = strings.TrimSpace(w.cstr())
	w.cstr()
	w.cstr()
	w.cstr()
	it.DisplayID = w.u32()
	it.Quality = w.u32()
	it.Flags = w.u32()
	w.u32() // Flags2
	w.u32() // BuyPrice
	w.u32() // SellPrice
	it.InventoryType = w.u32()
	it.AllowableClass = w.i32()
	it.AllowableRace = w.i32()
	it.ItemLevel = w.u32()
	it.RequiredLevel = w.u32()
	it.RequiredSkill = w.u32()
	it.RequiredSkillRank = w.u32()
	it.RequiredSpell = w.u32()
	w.u32() // RequiredHonorRank
	w.u32() // RequiredCityRank
	w.u32() // RequiredReputationFaction
	w.u32() // RequiredReputationRank
	it.MaxCount = w.i32()
	w.i32() // Stackable
	w.u32() // ContainerSlots
	statsCount := w.u32()
	if statsCount > 10 {
		return nil, fmt.Errorf("implausible stat count %d (layout mismatch?)", statsCount)
	}
	for i := uint32(0); i < statsCount; i++ {
		t, v := w.u32(), w.i32()
		if v != 0 {
			it.Stats = append(it.Stats, ItemStat{Type: t, Value: v})
		}
	}
	it.ScalingStatDistribution = w.u32()
	it.ScalingStatValue = w.u32()
	for i := 0; i < 2; i++ {
		it.DamageMin[i] = w.f32()
		it.DamageMax[i] = w.f32()
		it.DamageType[i] = w.u32()
	}
	it.Armor = w.u32()
	for i := 0; i < 6; i++ {
		it.Resistances[i] = w.i32()
	}
	it.Delay = w.u32()
	w.u32() // AmmoType
	w.f32() // RangedModRange
	for i := 0; i < 5; i++ {
		sp := ItemSpell{
			SpellID:          w.u32(),
			Trigger:          w.u32(),
			Charges:          w.i32(),
			Cooldown:         w.i32(),
			Category:         w.u32(),
			CategoryCooldown: w.i32(),
		}
		if sp.SpellID != 0 {
			it.Spells = append(it.Spells, sp)
		}
	}
	it.Bonding = w.u32()
	it.Description = w.cstr()
	w.u32() // PageText
	w.u32() // LanguageID
	w.u32() // PageMaterial
	w.u32() // StartQuest
	w.u32() // LockID
	w.i32() // Material
	w.u32() // Sheath
	it.RandomProperty = w.i32()
	it.RandomSuffix = w.u32()
	it.Block = w.u32()
	it.ItemSet = w.u32()
	w.u32() // MaxDurability
	w.u32() // Area
	w.u32() // Map
	w.u32() // BagFamily
	it.TotemCategory = w.u32()
	for i := 0; i < 3; i++ {
		it.SocketColors[i] = w.u32()
		w.u32() // SocketContent
	}
	it.SocketBonus = w.u32()
	it.GemProperties = w.u32()
	w.i32() // RequiredDisenchantSkill
	it.ArmorDamageModifier = w.f32()
	w.u32() // Duration
	it.ItemLimitCategory = w.u32()
	w.u32() // HolidayId
	if w.err != nil {
		return nil, fmt.Errorf("record too short: %v", w.err)
	}
	if w.r.Len() != 0 {
		return nil, fmt.Errorf("%d unread bytes at end of record (layout mismatch?)", w.r.Len())
	}
	return it, nil
}
