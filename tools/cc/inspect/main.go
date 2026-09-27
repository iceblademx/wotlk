// Command inspect prints decoded 3.3.5a data from cc_data/ for implementing custom effects.
//
//	go run ./tools/cc/inspect spell 71519        # decoded Spell.dbc row (+ server proc/bonus data)
//	go run ./tools/cc/inspect item 50363         # item_template row, converted sim item, and its spells
//	go run ./tools/cc/inspect set 883            # ItemSet.dbc row and bonus spells
//	go run ./tools/cc/inspect enchant 3789       # SpellItemEnchantment.dbc row
//	go run ./tools/cc/inspect search "deathbringer"  # spells/items/sets whose name contains the text
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/wowsims/wotlk/sim/core/stats"
	"github.com/wowsims/wotlk/tools/cc"
	"github.com/wowsims/wotlk/tools/cc/convert"
)

var inDir = flag.String("in", "cc_data", "Directory with raw 3.3.5a client/server files")

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: inspect [-in cc_data] spell|item|set|enchant <id> | search <text>")
		os.Exit(2)
	}
	in, err := cc.Load(*inDir)
	if err != nil {
		log.Fatal(err)
	}
	src := in.Source

	if args[0] == "search" {
		search(in, strings.ToLower(strings.Join(args[1:], " ")))
		return
	}
	id64, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil {
		log.Fatalf("invalid id %q", args[1])
	}
	id := uint32(id64)

	switch args[0] {
	case "spell":
		sp := src.Spells.Spells[id]
		if sp == nil {
			log.Fatalf("spell %d not in Spell.dbc", id)
		}
		dump(sp)
		if p := src.Server.SpellProcs[id]; p != nil {
			fmt.Println("server proc:")
			dump(p)
		}
		if b := src.Server.SpellBonus[id]; b != nil {
			fmt.Println("server spell_bonus_data:")
			dump(b)
		}
		if s, ok, why := convert.SpellStats(sp); ok {
			fmt.Printf("as passive stats: %s\n", statString(s))
		} else {
			fmt.Printf("not plain stats: %s\n", why)
		}
		if base := in.Baseline; base != nil {
			if old := base.Spells[id]; old != nil {
				fmt.Println("stock (dbc_baseline) version:")
				dump(old)
			}
		}
	case "item":
		it := src.Server.Items[id]
		if it == nil {
			log.Fatalf("item %d not in server data (cc_data/server)", id)
		}
		dump(it)
		if convert.IsEquippable(it) {
			item, issues := src.ToUIItem(it, in.Config.Phase)
			fmt.Printf("sim item: type=%s armor=%s weapon=%s hand=%s ranged=%s set=%q icon=%s\n  stats: %s\n  sockets: %v bonus: %s\n",
				item.Type, item.ArmorType, item.WeaponType, item.HandType, item.RangedWeaponType, item.SetName, item.Icon,
				floatStatString(item.Stats), item.GemSockets, floatStatString(item.SocketBonus))
			for _, is := range issues {
				fmt.Printf("  ! %s %s spell=%d: %s\n", is.Kind, is.Trigger, is.SpellID, is.Detail)
			}
		} else if convert.IsGem(it) {
			gem, issues := src.ToUIGem(it, in.Config.Phase)
			if gem != nil {
				fmt.Printf("sim gem: color=%s stats: %s\n", gem.Color, floatStatString(gem.Stats))
			}
			for _, is := range issues {
				fmt.Printf("  ! %s: %s\n", is.Kind, is.Detail)
			}
		}
		for _, s := range it.Spells {
			if sp := src.Spells.Spells[s.SpellID]; sp != nil {
				fmt.Printf("\nspell %d (trigger %d): %s - %s\n", s.SpellID, s.Trigger, sp.Name, sp.Text)
			}
		}
	case "set":
		set := src.Tables.ItemSets[id]
		if set == nil {
			log.Fatalf("set %d not in ItemSet.dbc", id)
		}
		dump(set)
		for _, b := range set.Bonuses {
			sp := src.Spells.Spells[b.SpellID]
			if sp == nil {
				fmt.Printf("%dpc: spell %d missing\n", b.Pieces, b.SpellID)
				continue
			}
			fmt.Printf("%dpc: spell %d %s - %s\n", b.Pieces, b.SpellID, sp.Name, sp.Text)
		}
	case "enchant":
		e := src.Tables.Enchants[id]
		if e == nil {
			log.Fatalf("enchant %d not in SpellItemEnchantment.dbc", id)
		}
		dump(e)
		s, problems := src.EnchantStats(e)
		fmt.Printf("stats: %s\n", statString(s))
		for _, p := range problems {
			fmt.Printf("  ! %s\n", p)
		}
	default:
		log.Fatalf("unknown kind %q", args[0])
	}
}

func search(in *cc.Inputs, text string) {
	src := in.Source
	var lines []string
	for id, sp := range src.Spells.Spells {
		if strings.Contains(strings.ToLower(sp.Name), text) {
			lines = append(lines, fmt.Sprintf("spell %d\t%s %s", id, sp.Name, sp.Rank))
		}
	}
	for id, it := range src.Server.Items {
		if strings.Contains(strings.ToLower(it.Name), text) {
			lines = append(lines, fmt.Sprintf("item  %d\t%s (ilvl %d)", id, it.Name, it.ItemLevel))
		}
	}
	for id, set := range src.Tables.ItemSets {
		if strings.Contains(strings.ToLower(set.Name), text) {
			lines = append(lines, fmt.Sprintf("set   %d\t%s", id, set.Name))
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Println(l)
	}
}

func dump(v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func statString(s stats.Stats) string {
	return floatStatString(s[:])
}

func floatStatString(s []float64) string {
	var parts []string
	for i, v := range s {
		if v != 0 {
			parts = append(parts, fmt.Sprintf("%s=%g", stats.Stat(i).StatName(), v))
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}
