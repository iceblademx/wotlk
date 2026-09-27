package cc_test

import (
	"testing"
	"time"

	"github.com/wowsims/wotlk/sim/common/cc"
	"github.com/wowsims/wotlk/sim/core"
	"github.com/wowsims/wotlk/sim/core/proto"
	"github.com/wowsims/wotlk/sim/core/stats"
	dpsWarrior "github.com/wowsims/wotlk/sim/warrior/dps"
)

// Synthetic custom items registered through the same helpers zz_generated.go uses.
const (
	testDagger  = 990001
	testTrinket = 990002
	testHelm    = 990003
	testSetName = "Battlegear of Testing"
)

func init() {
	dpsWarrior.RegisterDpsWarrior()

	core.ItemsByID[testDagger] = core.Item{ID: testDagger, Name: "Dagger of Testing", Type: proto.ItemType_ItemTypeWeapon,
		WeaponType: proto.WeaponType_WeaponTypeDagger, HandType: proto.HandType_HandTypeOneHand,
		WeaponDamageMin: 300, WeaponDamageMax: 500, SwingSpeed: 1.8, SetName: testSetName}
	core.ItemsByID[testTrinket] = core.Item{ID: testTrinket, Name: "Trinket of Testing", Type: proto.ItemType_ItemTypeTrinket}
	core.ItemsByID[testHelm] = core.Item{ID: testHelm, Name: "Helm of Testing", Type: proto.ItemType_ItemTypeHead,
		ArmorType: proto.ArmorType_ArmorTypePlate, SetName: testSetName}

	cc.RegisterProcStat(cc.ProcStat{ItemID: testDagger, Name: "Dagger of Testing", AuraSpellID: 990101,
		Bonus: stats.Stats{stats.MeleeHaste: 100}, Duration: 10 * time.Second, MaxStacks: 3,
		ProcFlags: 0x14, ProcChance: 0.5, ICD: 30 * time.Second})
	cc.RegisterUseStat(testTrinket, stats.Stats{stats.AttackPower: 500}, 20*time.Second, 120*time.Second, true, false)
	cc.RegisterStatSet(testSetName, map[int32]stats.Stats{2: {stats.Strength: 77}})
}

func warriorRequest(items ...int32) *proto.RaidSimRequest {
	equipment := &proto.EquipmentSpec{Items: make([]*proto.ItemSpec, 17)}
	for i := range equipment.Items {
		equipment.Items[i] = &proto.ItemSpec{}
	}
	for _, id := range items {
		switch id {
		case testDagger:
			equipment.Items[proto.ItemSlot_ItemSlotMainHand].Id = id
		case testTrinket:
			equipment.Items[proto.ItemSlot_ItemSlotTrinket1].Id = id
		case testHelm:
			equipment.Items[proto.ItemSlot_ItemSlotHead].Id = id
		}
	}
	player := core.WithSpec(&proto.Player{
		Class:     proto.Class_ClassWarrior,
		Race:      proto.Race_RaceHuman,
		Equipment: equipment,
		Rotation:  core.GetAplRotation("../../../ui/warrior/apls", "fury").Rotation,
	}, &proto.Player_Warrior{Warrior: &proto.Warrior{Options: &proto.Warrior_Options{}}})

	return &proto.RaidSimRequest{
		Raid:       core.SinglePlayerRaidProto(player, &proto.PartyBuffs{}, &proto.RaidBuffs{}, &proto.Debuffs{}),
		Encounter:  core.MakeSingleTargetEncounter(0),
		SimOptions: &proto.SimOptions{Iterations: 50, RandomSeed: 101, IsTest: true},
	}
}

func auraMetrics(t *testing.T, result *proto.RaidSimResult, match func(*proto.ActionID) bool) *proto.AuraMetrics {
	t.Helper()
	if result.ErrorResult != "" {
		t.Fatal(result.ErrorResult)
	}
	for _, a := range result.RaidMetrics.Parties[0].Players[0].Auras {
		if match(a.Id) {
			return a
		}
	}
	return nil
}

func TestRegisterProcStatRespectsICD(t *testing.T) {
	result := core.RunRaidSim(warriorRequest(testDagger))
	aura := auraMetrics(t, result, func(id *proto.ActionID) bool { return id.GetSpellId() == 990101 })
	if aura == nil {
		t.Fatal("proc aura never registered/activated")
	}
	duration := float64(core.LongDuration)
	// 50% on every hit would proc ~every other swing; the 30s ICD caps it at one proc per 30s.
	maxProcs := duration/30 + 1
	if aura.ProcsAvg < maxProcs*0.7 || aura.ProcsAvg > maxProcs {
		t.Errorf("procs avg = %.2f, expected close to but not above %.1f (ICD-limited)", aura.ProcsAvg, maxProcs)
	}
	if aura.UptimeSecondsAvg <= 0 || aura.UptimeSecondsAvg > duration {
		t.Errorf("uptime = %.1fs", aura.UptimeSecondsAvg)
	}
}

func TestRegisterUseStatIsUsed(t *testing.T) {
	result := core.RunRaidSim(warriorRequest(testTrinket))
	aura := auraMetrics(t, result, func(id *proto.ActionID) bool { return id.GetItemId() == testTrinket })
	if aura == nil || aura.ProcsAvg < 1 {
		t.Fatalf("on-use trinket was never activated: %+v", aura)
	}
	// 20s buff on a 2 min cooldown, used on cooldown.
	duration := float64(core.LongDuration)
	if want := float64(int(duration/120) + 1); aura.ProcsAvg != want {
		t.Errorf("uses = %.2f, want %.0f", aura.ProcsAvg, want)
	}
}

func TestRegisterStatSet(t *testing.T) {
	statsFor := func(items ...int32) float64 {
		req := warriorRequest(items...)
		res := core.ComputeStats(&proto.ComputeStatsRequest{Raid: req.Raid})
		if res.ErrorResult != "" {
			t.Fatal(res.ErrorResult)
		}
		return res.RaidStats.Parties[0].Players[0].GearStats.Stats[stats.Strength]
	}
	with, without := statsFor(testDagger, testHelm), statsFor(testDagger)
	if with-without != 77 {
		t.Errorf("2pc bonus added %.0f strength, want 77", with-without)
	}
	if !core.HasItemSet(testSetName) {
		t.Error("set not registered")
	}
	// A second registration of the same set name must be ignored rather than duplicated.
	cc.RegisterStatSet(testSetName, map[int32]stats.Stats{2: {stats.Strength: 1000}})
	if again := statsFor(testDagger, testHelm); again != with {
		t.Errorf("duplicate registration changed stats: %.0f -> %.0f", with, again)
	}
}
