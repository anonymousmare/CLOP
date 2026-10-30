package clop

import (
	"fmt"
	"math"
	"sort"
)

// unitTypes are the five combat classes, in the order targets are attacked.
// Alicorns (type 6) fight as naval.
var unitTypes = []string{1: "cavalry", 2: "tanks", 3: "pegasi", 4: "unicorns", 5: "naval"}

// Unit is one force taking part in a battle.
type Unit struct {
	ForceID    int64
	NationID   int64
	NationName string
	Name       string
	Size       int64
	Training   int64
	Type       int64 // 1-5
	Attacking  bool
	Dmg        [6]float64 // damage per hit against each target type
	Arm        [6]float64 // damage multiplier when hit by each attacker type

	hits   int64
	damage float64
}

// newUnit builds a battle unit from a forces row joined with its weapon
// and armor definitions (dmg_* and arm_* columns, NULL when scrounged).
func newUnit(r Row, attacking bool) *Unit {
	u := &Unit{
		ForceID: r.I("force_id"), NationID: r.I("nation_id"), NationName: r.S("nationname"), Name: r.S("name"),
		Size: r.I("size"), Training: r.I("training"), Type: r.I("type"), Attacking: attacking,
	}
	for t := 1; t <= 5; t++ {
		switch {
		case u.Type == 6:
			u.Dmg[t], u.Arm[t] = 10, .1
		default:
			u.Dmg[t], u.Arm[t] = .25, 1
			if r.I("weapon_id") != 0 && r.S("dmg_"+unitTypes[t]) != "" {
				u.Dmg[t] = r.F("dmg_" + unitTypes[t])
			}
			if r.I("armor_id") != 0 && r.S("arm_"+unitTypes[t]) != "" {
				u.Arm[t] = r.F("arm_" + unitTypes[t])
			}
		}
	}
	if u.Type == 6 {
		u.Type = 5
	}
	return u
}

// Hit totals what one unit did to another during a battle.
type Hit struct {
	Attacker, Defender *Unit
	Hits               int64
	Damage             float64
}

// Battle resolves one round of combat at a location. Invaders (Attacking)
// and repellers strike simultaneously: both sides get their full number of
// hits regardless of losses taken in the same round.
type Battle struct {
	Units []*Unit
	// Shielded reports whether a defending unit takes reduced (.75x)
	// damage from invaders, e.g. because it is fighting at home.
	Shielded func(*Unit) bool

	hits    []*Hit
	hitByID map[[2]int64]*Hit
}

func (b *Battle) side(attacking bool) []*Unit {
	var out []*Unit
	for _, u := range b.Units {
		if u.Attacking == attacking {
			out = append(out, u)
		}
	}
	return out
}

// Run fights the battle. Nothing happens unless both sides are present.
func (b *Battle) Run() {
	b.hitByID = map[[2]int64]*Hit{}
	for _, u := range b.Units {
		u.hits = u.Size
	}
	invaders, repellers := b.side(true), b.side(false)
	if len(repellers) == 0 {
		return
	}
	b.strike(invaders, repellers, b.Shielded)
	b.strike(repellers, invaders, nil)
}

type damageGroup struct {
	dmg    float64
	byType [6][]*Unit
}

// strike lets one side hit the other. Units doing the most damage go
// first; within that, targets are taken in type order and attackers by
// size. Defenders with the best armor against the attacker soak hits
// first, then the largest.
func (b *Battle) strike(attackers, defenders []*Unit, shielded func(*Unit) bool) {
	groupsByDmg := map[float64]*damageGroup{}
	for _, u := range attackers {
		for t := 1; t <= 5; t++ {
			g, ok := groupsByDmg[u.Dmg[t]]
			if !ok {
				g = &damageGroup{dmg: u.Dmg[t]}
				groupsByDmg[u.Dmg[t]] = g
			}
			g.byType[t] = append(g.byType[t], u)
		}
	}
	groups := make([]*damageGroup, 0, len(groupsByDmg))
	for _, g := range groupsByDmg {
		for t := 1; t <= 5; t++ {
			sort.SliceStable(g.byType[t], func(i, j int) bool { return bySizeNationForce(g.byType[t][i], g.byType[t][j]) })
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].dmg > groups[j].dmg })

	// targets[defType][attType]: defenders of defType, best armor vs attType first.
	var targets [6][6][]*Unit
	for _, d := range defenders {
		for a := 1; a <= 5; a++ {
			targets[d.Type][a] = append(targets[d.Type][a], d)
		}
	}
	for t := 1; t <= 5; t++ {
		for a := 1; a <= 5; a++ {
			list := targets[t][a]
			sort.SliceStable(list, func(i, j int) bool {
				if list[i].Arm[a] != list[j].Arm[a] {
					return list[i].Arm[a] < list[j].Arm[a]
				}
				return bySizeNationForce(list[i], list[j])
			})
		}
	}

	for _, g := range groups {
	nextType:
		for t := 1; t <= 5; t++ {
			for _, att := range g.byType[t] {
				for att.hits > 0 {
					defs := targets[t][att.Type]
					if len(defs) == 0 {
						break
					}
					fought := false
				defenderLoop:
					for _, def := range defs {
						dmg := round(g.dmg*def.Arm[att.Type]*math.Pow(1.5, float64(att.Training-def.Training)/20), 3)
						if shielded != nil && shielded(def) {
							dmg *= .75
						}
						for float64(def.Size) > def.damage {
							fought = true
							def.damage += dmg
							b.record(att, def, dmg)
							att.hits--
							if att.hits == 0 {
								break defenderLoop
							}
						}
					}
					if !fought {
						// Every target of this type is already destroyed.
						continue nextType
					}
				}
			}
		}
	}
}

func bySizeNationForce(a, b *Unit) bool {
	if a.Size != b.Size {
		return a.Size > b.Size
	}
	if a.NationID != b.NationID {
		return a.NationID < b.NationID
	}
	return a.ForceID < b.ForceID
}

func (b *Battle) record(att, def *Unit, dmg float64) {
	key := [2]int64{att.ForceID, def.ForceID}
	h, ok := b.hitByID[key]
	if !ok {
		h = &Hit{Attacker: att, Defender: def}
		b.hitByID[key] = h
		b.hits = append(b.hits, h)
	}
	h.Hits++
	h.Damage += dmg
}

// Hits lists who hit whom, grouped by attacker in the order fighting began.
func (b *Battle) Hits() []*Hit {
	order := map[int64]int{}
	for _, h := range b.hits {
		if _, ok := order[h.Attacker.ForceID]; !ok {
			order[h.Attacker.ForceID] = len(order)
		}
	}
	out := append([]*Hit(nil), b.hits...)
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Attacker.ForceID] < order[out[j].Attacker.ForceID] })
	return out
}

// Losses is the whole number of size a unit lost.
func (u *Unit) Losses() int64 {
	return int64(math.Floor(round(u.damage, 6)))
}

// npcName names the empire or rebel faction behind negative nation ids.
func npcName(nationID int64) string {
	switch nationID {
	case -1:
		return "Solar Empire"
	case -2:
		return "New Lunar Republic"
	case -3:
		return "Rebel Forces"
	}
	return ""
}

// nationRef links to a player nation; NPC factions are plain text.
func nationRef(u *Unit) string {
	if u.NationID > 0 {
		return fmt.Sprintf(`<a href="viewnation.php?nation_id=%d">%s</a>`, u.NationID, u.NationName)
	}
	return u.NationName
}
