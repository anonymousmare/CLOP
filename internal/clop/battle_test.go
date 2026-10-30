package clop

import "testing"

func unit(id, nation, size, training, typ int64, attacking bool, dmg, arm float64) *Unit {
	u := &Unit{ForceID: id, NationID: nation, Name: "u", Size: size, Training: training, Type: typ, Attacking: attacking}
	for t := 1; t <= 5; t++ {
		u.Dmg[t], u.Arm[t] = dmg, arm
	}
	return u
}

func TestBattleHomeDefense(t *testing.T) {
	// Seven untrained, unarmed cavalry against two at home: .25 * .75 per hit.
	att := unit(1, 1, 7, 0, 1, true, .25, 1)
	def := unit(2, 3, 2, 0, 1, false, .25, 1)
	b := &Battle{Units: []*Unit{att, def}, Shielded: func(u *Unit) bool { return u.NationID == 3 }}
	b.Run()
	if def.damage != 1.3125 || def.Losses() != 1 {
		t.Errorf("defender damage %v losses %d", def.damage, def.Losses())
	}
	if att.damage != .5 || att.Losses() != 0 {
		t.Errorf("attacker damage %v losses %d", att.damage, att.Losses())
	}
	hits := b.Hits()
	if len(hits) != 2 || hits[0].Attacker != att || hits[0].Hits != 7 {
		t.Errorf("unexpected hits %+v", hits)
	}
}

func TestBattleNoDefendersMeansNoFight(t *testing.T) {
	att := unit(1, 1, 10, 0, 1, true, 1, 1)
	b := &Battle{Units: []*Unit{att}}
	b.Run()
	if len(b.Hits()) != 0 {
		t.Error("attackers fought nobody")
	}
}

func TestBattleTargetPriority(t *testing.T) {
	// The defender with the best armor against the attacker absorbs hits
	// first, and overkill spills to the next one.
	att := unit(1, 1, 3, 0, 1, true, 1, 1)
	tough := unit(2, 2, 1, 0, 1, false, 0, .5)
	soft := unit(3, 2, 5, 0, 1, false, 0, 1)
	b := &Battle{Units: []*Unit{att, soft, tough}}
	b.Run()
	if tough.damage != 1 || soft.damage != 1 {
		t.Errorf("tough %v soft %v", tough.damage, soft.damage)
	}
	if h := b.Hits(); h[0].Defender != tough || h[0].Hits != 2 {
		t.Errorf("first target %+v", h[0])
	}
}

func TestBattleTrainingCurve(t *testing.T) {
	// Fully trained units do 1.5x to untrained ones (rounded to 3 places).
	att := unit(1, 1, 1, 20, 2, true, 1, 1)
	def := unit(2, 2, 10, 0, 2, false, 0, 1)
	b := &Battle{Units: []*Unit{att, def}}
	b.Run()
	if def.damage != 1.5 {
		t.Errorf("trained damage %v", def.damage)
	}
}
