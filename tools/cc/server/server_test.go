package server

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const dumpSQL = `-- MySQL dump (trimmed)
/*!40101 SET NAMES utf8 */;
DROP TABLE IF EXISTS ` + "`item_template`" + `;
CREATE TABLE ` + "`item_template`" + ` (
  ` + "`entry`" + ` mediumint unsigned NOT NULL DEFAULT '0',
  ` + "`class`" + ` tinyint unsigned NOT NULL DEFAULT '0',
  ` + "`subclass`" + ` tinyint unsigned NOT NULL DEFAULT '0',
  ` + "`name`" + ` varchar(255) NOT NULL DEFAULT '',
  ` + "`InventoryType`" + ` tinyint unsigned NOT NULL DEFAULT '0',
  ` + "`stat_type1`" + ` tinyint unsigned NOT NULL DEFAULT '0',
  ` + "`stat_value1`" + ` smallint NOT NULL DEFAULT '0',
  ` + "`spellid_1`" + ` mediumint NOT NULL DEFAULT '0',
  ` + "`spelltrigger_1`" + ` tinyint unsigned NOT NULL DEFAULT '0',
  ` + "`spellppmRate_1`" + ` float NOT NULL DEFAULT '0',
  ` + "`description`" + ` varchar(255) NOT NULL DEFAULT '',
  PRIMARY KEY (` + "`entry`" + `),
  KEY ` + "`idx_name`" + ` (` + "`name`" + `(250))
) ENGINE=MyISAM DEFAULT CHARSET=utf8;

INSERT INTO ` + "`item_template`" + ` VALUES (900100,4,0,'Band of O\'Testing',11,7,30,90001,1,0,'Line1\nsemi;colon'),(900101,2,15,'Dagger',13,3,-5,0,0,1.5,'');
INSERT INTO item_template (entry, class, name, InventoryType) VALUES (900199, 4, 'Doomed', 1);
UPDATE item_template SET stat_value1 = 35, name='Band of Testing' WHERE entry = 900100;
UPDATE ` + "`item_template`" + ` SET spellppmRate_1=2.5 WHERE entry IN (900101, 123);
DELETE FROM item_template WHERE entry BETWEEN 900150 AND 900199;
INSERT INTO other_table VALUES (1,2,3);
REPLACE INTO spell_proc_event (entry, procFlags, procEx, ppmRate, CustomChance, Cooldown) VALUES (90002, 20, 2, 0, 0, 45);
INSERT INTO spell_proc (SpellId, ProcFlags, HitMask, ProcsPerMinute, Chance, Cooldown) VALUES (90009, 4, 0, 1.5, 0, 50000);
INSERT INTO spell_bonus_data (entry, direct_bonus, dot_bonus, ap_bonus, ap_dot_bonus, comments) VALUES (90020, 0.8571, -1, 0, 0, 'Custom Bolt');
`

func TestSQLDump(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "01_world.sql")
	if err := os.WriteFile(path, []byte(dumpSQL), 0666); err != nil {
		t.Fatal(err)
	}
	d := NewData()
	if err := d.LoadSQLFile(path); err != nil {
		t.Fatal(err)
	}
	d.Finalize()
	for _, w := range d.Warnings {
		t.Errorf("unexpected warning: %s", w)
	}
	if !d.CompleteItemList {
		t.Error("SQL item data should count as complete")
	}

	ring := d.Items[900100]
	if ring == nil {
		t.Fatal("ring missing")
	}
	if ring.Name != "Band of Testing" || ring.InventoryType != 11 || ring.Description != "Line1\nsemi;colon" {
		t.Errorf("ring: %+v", ring)
	}
	if len(ring.Stats) != 1 || ring.Stats[0] != (ItemStat{Type: 7, Value: 35}) {
		t.Errorf("ring stats after UPDATE: %+v", ring.Stats)
	}
	if len(ring.Spells) != 1 || ring.Spells[0].SpellID != 90001 || ring.Spells[0].Trigger != 1 {
		t.Errorf("ring spells: %+v", ring.Spells)
	}
	if ring.AllowableClass != -1 {
		t.Errorf("missing AllowableClass column should mean 'all classes', got %d", ring.AllowableClass)
	}

	dagger := d.Items[900101]
	if dagger == nil || dagger.Stats[0].Value != -5 {
		t.Fatalf("dagger: %+v", dagger)
	}
	if len(dagger.Spells) != 0 {
		t.Errorf("spell id 0 should be dropped: %+v", dagger.Spells)
	}
	if d.rows["item_template"][900101]["spellppmrate1"] != "2.5" {
		t.Errorf("UPDATE ... IN () not applied")
	}
	if _, ok := d.Items[900199]; ok {
		t.Error("DELETE ... BETWEEN not applied")
	}

	if p := d.SpellProcs[90002]; p == nil || p.CooldownMs != 45000 || p.ProcFlags != 20 || p.HitMask != 2 {
		t.Errorf("spell_proc_event: %+v", p)
	}
	if p := d.SpellProcs[90009]; p == nil || p.CooldownMs != 50000 || p.PPM != 1.5 {
		t.Errorf("spell_proc: %+v", p)
	}
	if b := d.SpellBonus[90020]; b == nil || b.Direct != 0.8571 || b.Dot != -1 {
		t.Errorf("spell_bonus_data: %+v", b)
	}
}

func TestSQLWithoutCreateTable(t *testing.T) {
	// A 3.3.5 TrinityCore row using the default column order (no CREATE TABLE in the file).
	values := make([]string, len(defaultItemTemplateColumns))
	for i := range values {
		values[i] = "0"
	}
	set := func(col, v string) {
		for i, c := range defaultItemTemplateColumns {
			if c == col {
				values[i] = v
				return
			}
		}
		t.Fatalf("no column %s", col)
	}
	set("entry", "5")
	set("name", "'Implicit'")
	set("ItemLevel", "264")
	set("socketColor_2", "8")
	sql := "INSERT INTO `item_template` VALUES ("
	for i, v := range values {
		if i > 0 {
			sql += ","
		}
		sql += v
	}
	sql += ");"

	d := NewData()
	for _, stmt := range splitStatements(sql) {
		d.execStatement(stmt)
	}
	d.Finalize()
	it := d.Items[5]
	if it == nil || it.Name != "Implicit" || it.ItemLevel != 264 || it.SocketColors[1] != 8 {
		t.Fatalf("implicit columns: %+v (warnings %v)", it, d.Warnings)
	}
}

func TestCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom_item_template.tsv")
	csv := "entry\tclass\tsubclass\tname\tInventoryType\tstat_type1\tstat_value1\n" +
		"900300\t4\t4\tPlate \"Helm\"\t1\t4\t50\n"
	if err := os.WriteFile(path, []byte(csv), 0666); err != nil {
		t.Fatal(err)
	}
	d := NewData()
	if err := d.LoadCSVFile(path); err != nil {
		t.Fatal(err)
	}
	d.Finalize()
	it := d.Items[900300]
	if it == nil || it.SubClass != 4 || it.Stats[0].Value != 50 || it.Name != `Plate "Helm"` {
		t.Fatalf("csv item: %+v", it)
	}
}

func TestItemCacheWDB(t *testing.T) {
	var rec bytes.Buffer
	u := func(v uint32) { binary.Write(&rec, binary.LittleEndian, v) }
	f := func(v float32) { u(math.Float32bits(v)) }
	s := func(v string) { rec.WriteString(v); rec.WriteByte(0) }

	u(2)
	u(7)
	u(0xFFFFFFFF) // class, subclass, sound override
	s("Cached Sword")
	s("")
	s("")
	s("") // names
	u(1234)
	u(4)
	u(8)
	u(0) // display, quality, flags, flags2
	u(0)
	u(0)
	u(13) // buy, sell, inventory type
	u(0xFFFFFFFF)
	u(0xFFFFFFFF)
	u(264)
	u(80)                    // classes, races, ilvl, req level
	for i := 0; i < 9; i++ { // req skill.. stackable, container slots
		u(0)
	}
	u(0) // container slots
	u(2) // stats count
	u(4)
	u(50)
	u(32)
	u(40)
	u(0)
	u(0) // scaling
	f(500)
	f(900)
	u(0)
	f(0)
	f(0)
	u(0)
	u(0)                     // armor
	for i := 0; i < 6; i++ { // resistances
		u(0)
	}
	u(2600)
	u(0)
	f(0) // delay, ammo, range mod
	u(71519)
	u(2)
	u(0)
	u(0xFFFFFFFF)
	u(0)
	u(0xFFFFFFFF) // spell 1
	for i := 0; i < 4; i++ {
		u(0)
		u(0)
		u(0)
		u(0xFFFFFFFF)
		u(0)
		u(0xFFFFFFFF)
	}
	u(2)
	s("flavor")              // bonding, description
	for i := 0; i < 7; i++ { // page text .. sheath
		u(0)
	}
	u(0)
	u(0)
	u(0)
	u(0)                     // random property/suffix, block, itemset
	for i := 0; i < 5; i++ { // durability, area, map, bag family, totem category
		u(0)
	}
	u(2)
	u(0)
	u(8)
	u(0)
	u(0)
	u(0) // sockets
	u(3312)
	u(0)
	u(0) // socket bonus, gem properties, disenchant skill
	f(0)
	u(0)
	u(0)
	u(0) // armor dmg mod, duration, limit category, holiday

	var file bytes.Buffer
	file.WriteString("BDIW")
	binary.Write(&file, binary.LittleEndian, []uint32{12340, 0x656e5553, 0, 0}) // build, locale, 2 header fields
	binary.Write(&file, binary.LittleEndian, []uint32{900400, uint32(rec.Len())})
	file.Write(rec.Bytes())
	binary.Write(&file, binary.LittleEndian, []uint32{0, 0})

	path := filepath.Join(t.TempDir(), "itemcache.wdb")
	if err := os.WriteFile(path, file.Bytes(), 0666); err != nil {
		t.Fatal(err)
	}
	d := NewData()
	if err := d.LoadItemCacheWDB(path); err != nil {
		t.Fatal(err)
	}
	for _, w := range d.Warnings {
		t.Errorf("warning: %s", w)
	}
	it := d.Items[900400]
	if it == nil {
		t.Fatal("cached item missing")
	}
	if it.Name != "Cached Sword" || it.ItemLevel != 264 || it.Delay != 2600 || it.DamageMax[0] != 900 ||
		len(it.Stats) != 2 || it.Spells[0].SpellID != 71519 || it.Spells[0].Trigger != 2 || it.SocketColors[1] != 8 || it.SocketBonus != 3312 {
		t.Errorf("cached item: %+v", it)
	}
	if d.CompleteItemList {
		t.Error("a client cache is not a complete item list")
	}
}
