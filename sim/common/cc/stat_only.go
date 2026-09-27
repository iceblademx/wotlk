package cc

import (
	"fmt"

	"github.com/wowsims/wotlk/sim/core"
)

const statOnlyIDOffset = 10_000_000

// StatOnlyCopy registers a copy of an item under a new ID, with the same stats but without its item
// effect or set membership, and returns the new ID. Tests use it to measure what a custom item's
// effect is worth on its own.
func StatOnlyCopy(itemID int32) int32 {
	item, ok := core.ItemsByID[itemID]
	if !ok {
		panic(fmt.Sprintf("StatOnlyCopy: unknown item %d", itemID))
	}
	item.ID = itemID + statOnlyIDOffset
	item.Name += " (stats only)"
	item.SetName = ""
	core.ItemsByID[item.ID] = item
	return item.ID
}

// StatOnlyCopies applies StatOnlyCopy to every item of a gear map.
func StatOnlyCopies[K comparable](items map[K]int32) map[K]int32 {
	out := make(map[K]int32, len(items))
	for k, id := range items {
		out[k] = StatOnlyCopy(id)
	}
	return out
}
