package main

import "testing"

func TestWowheadSpellTexts(t *testing.T) {
	// Trimmed from wowhead's Comet's Trail and VanCleef's Breastplate of Triumph tooltips.
	tooltip := `<span class="q2">Equip: Increases attack power by <!--rtg38-->271.</span><br>` +
		`<span class="q2">Equip: <a href="/wotlk/spell=64786/comets-trail" class="q2">Your melee and ranged attacks have a chance to increase your haste rating by 819 for 10 sec. (Proc chance: 15%, 45s cooldown)</a></span>` +
		`<span>(2) Set : <a href="/wotlk/spell=67209/item-rogue-t9-2p-bonus-rupture">Reduce the cost of your next ability by 40&nbsp;energy.</a></span>` +
		`<span class="q2">Use: <a href="/wotlk/spell=433/food" class="q2">Restores 61.2 health over 18 sec.</a></span>`
	got := wowheadSpellTexts(tooltip)
	want := map[uint32]string{
		64786: "Your melee and ranged attacks have a chance to increase your haste rating by 819 for 10 sec.",
		67209: "Reduce the cost of your next ability by 40 energy.",
		433:   "Restores 61.2 health over 18 sec.",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d spells, want %d: %v", len(got), len(want), got)
	}
	for id, text := range want {
		if got[id] != text {
			t.Errorf("spell %d: got %q, want %q", id, got[id], text)
		}
	}
}

func TestSameValues(t *testing.T) {
	if !sameValues("Increases haste rating by 726 for 10 sec.", "Your haste rating increases by 726 for 10 sec.") {
		t.Error("rewording alone should not count as a change")
	}
	if sameValues("increase your haste rating by 819 for 10 sec.", "increase your haste rating by 726 for 10 sec.") {
		t.Error("a changed value should count as a change")
	}
}
