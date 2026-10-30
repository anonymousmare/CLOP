package clop

import (
	"fmt"
	"math"
	"strings"
)

// Resource ids the rules refer to directly.
const (
	resCopper          = 2
	resApples          = 3
	resFactory         = 5
	resVehicleParts    = 9
	resMachineryParts  = 10
	resCider           = 18
	resCoffee          = 20
	resGasoline        = 25
	resGems            = 26
	resTungsten        = 27
	resPrecisionParts  = 29
	resComposites      = 30
	resBarracks        = 36
	resStatue          = 38
	resDrugFarm        = 41
	resSolarEnviro     = 44
	resLunarEnviro     = 45
	resForbiddenLab    = 74
	resForbiddenDone   = 75
	resAlicornFacility = 76
	resApotheosis      = 77
	resSunWorship      = 78
	resMoonWorship     = 79
)

// satMultiplier scales the satisfaction cap and the over-satisfaction decay.
func satMultiplier(gov string) float64 {
	switch gov {
	case "Transponyism":
		return 7
	case "Alicorn Elite":
		return 5
	case "Independence":
		return 2.5
	case "Decentralization":
		return 2
	case "Democracy":
		return 1.5
	case "Solar Vassal", "Lunar Client":
		return 1.25
	}
	return 1
}

func satCap(gov string) int64 { return int64(1000 * satMultiplier(gov)) }

// applyGovernmentGDP turns base GDP into what the nation actually earns.
func applyGovernmentGDP(gdp float64, gov string, satisfaction int64) float64 {
	switch gov {
	case "Transponyism":
		return gdp + gdp*7
	case "Alicorn Elite":
		return gdp + gdp*5
	case "Authoritarianism":
		return gdp + gdp*1.5
	case "Oppression":
		return gdp + gdp*2
	case "Repression":
		return gdp + gdp
	}
	return gdp + gdp*(float64(satisfaction)/1000)
}

// baseGDP is a nation's GDP from its enabled resources, before government.
func (db *DB) baseGDP(nationID int64) float64 {
	r := db.One(`SELECT SUM(rd.gdp * (r.amount - r.disabled)) AS totalgdp FROM resources r
		INNER JOIN resourcedefs rd ON rd.resource_id = r.resource_id WHERE r.nation_id = ?`, nationID)
	return r.F("totalgdp") + 50000
}

func chooseWord(current, alteration int64) string {
	if current >= 0 {
		if alteration > 0 {
			return "improved"
		}
		return "dwindled"
	}
	if alteration > 0 {
		return "recovered"
	}
	return "worsened"
}

func empireColumn(empire string) string {
	if empire == "Solar Empire" {
		return "se_relation"
	}
	return "nlr_relation"
}

// affectEmpireRelations changes a nation's standing with an empire, capped
// at 1000 (or the owner's ascension maximum) and floored at -1000.
func (db *DB) affectEmpireRelations(nationID, start, effect int64, affector, empire string) string {
	u := db.One("SELECT u.empiremax FROM users u INNER JOIN nations n ON n.user_id = u.user_id WHERE n.nation_id = ?", nationID)
	ascending := u.S("empiremax") != "" && u.S("empiremax") != "0"
	max := int64(1000)
	if ascending {
		max = u.I("empiremax")
	}
	if start+effect > max {
		effect = max - start
		if effect == 0 {
			return fmt.Sprintf("Your relationship with the %s can't get any better, despite the effects of your %s.", empire, affector)
		}
	} else if start+effect < -1000 && !ascending {
		effect = -1000 - start
		if effect == 0 {
			return fmt.Sprintf("Your relationship with the %s can't get any worse- your %s sure tried, though!", empire, affector)
		}
	}
	col := empireColumn(empire)
	db.Exec("UPDATE nations SET "+col+" = "+col+" + ? WHERE nation_id = ?", effect, nationID)
	return fmt.Sprintf("Your relationship with the %s has %s due to your %s. (%d)", empire, chooseWord(start, effect), affector, effect)
}

// affectSatisfaction changes satisfaction, capped by government type.
func (db *DB) affectSatisfaction(nationID, start, effect int64, affector, gov string) string {
	max := satCap(gov)
	if start+effect > max {
		effect = max - start
		if effect == 0 {
			return fmt.Sprintf("Your population can't be any more satisfied, despite the effects of your %s.", affector)
		}
	}
	db.Exec("UPDATE nations SET satisfaction = satisfaction + ? WHERE nation_id = ?", effect, nationID)
	return fmt.Sprintf("Your population's satisfaction has %s due to your %s. (%d)", chooseWord(start, effect), affector, effect)
}

// affectSatisfactionSilent is affectSatisfaction returning the applied change.
func (db *DB) affectSatisfactionSilent(nationID, start, effect int64, gov string) int64 {
	if max := satCap(gov); start+effect > max {
		effect = max - start
	}
	db.Exec("UPDATE nations SET satisfaction = satisfaction + ? WHERE nation_id = ?", effect, nationID)
	return effect
}

// Marketplace fees by economy: what a seller keeps and what a buyer pays.
func (db *DB) sellingMultiplier(nationID int64) float64 {
	n := db.One("SELECT economy, active_economy FROM nations WHERE nation_id = ?", nationID)
	switch {
	case n.S("economy") == "State Controlled":
		return .85
	case n.S("economy") == "Free Market" && n.B("active_economy"):
		return .97
	}
	return .9
}

func (db *DB) buyingMultiplier(nationID int64) float64 {
	n := db.One("SELECT economy, active_economy FROM nations WHERE nation_id = ?", nationID)
	switch {
	case n.S("economy") == "State Controlled":
		return 1.2
	case n.S("economy") == "Free Market" && n.B("active_economy"):
		return 1.03
	}
	return 1.1
}

type upkeep struct {
	resourceID int64
	name       string
	amount     int64
}

// governmentUpkeep is what a government consumes every tick.
func governmentUpkeep(gov string) []upkeep {
	gas := func(n int64) upkeep { return upkeep{resGasoline, "Gasoline", n} }
	vp := func(n int64) upkeep { return upkeep{resVehicleParts, "Vehicle Parts", n} }
	mp := func(n int64) upkeep { return upkeep{resMachineryParts, "Machinery Parts", n} }
	switch gov {
	case "Democracy":
		return []upkeep{gas(20), vp(2)}
	case "Repression":
		return []upkeep{gas(10)}
	case "Independence":
		return []upkeep{gas(40), vp(4)}
	case "Decentralization":
		return []upkeep{gas(50), vp(5)}
	case "Authoritarianism":
		return []upkeep{gas(10), mp(3)}
	case "Oppression":
		return []upkeep{gas(10), mp(5)}
	}
	return nil
}

func economyUpkeep(economy string) []upkeep {
	switch economy {
	case "Free Market":
		return []upkeep{{resCoffee, "Coffee", 6}}
	case "State Controlled":
		return []upkeep{{resCider, "Cider", 6}}
	}
	return nil
}

// Flows is per-tick production and consumption keyed by resource name.
type Flows struct {
	Generated map[string]int64
	Used      map[string]int64
}

// resourceFlows totals production and consumption for every nation matched
// by where (a SQL condition on n/u), including government and economy upkeep.
func (db *DB) resourceFlows(where string, args ...any) Flows {
	f := Flows{Generated: map[string]int64{}, Used: map[string]int64{}}
	from := `INNER JOIN resources r ON r.resource_id = rr.resource_id
		INNER JOIN nations n ON r.nation_id = n.nation_id
		INNER JOIN users u ON n.user_id = u.user_id`
	for _, r := range db.Query(`SELECT rd.name, SUM((r.amount - r.disabled) * rr.amount) AS total FROM resourceeffects rr `+from+`
		INNER JOIN resourcedefs rd ON rd.resource_id = rr.affectedresource_id WHERE `+where+` GROUP BY rd.name`, args...) {
		f.Generated[r.S("name")] = r.I("total")
	}
	for _, r := range db.Query(`SELECT rd.name, SUM((r.amount - r.disabled) * rr.amount) AS total FROM resourcerequirements rr `+from+`
		INNER JOIN resourcedefs rd ON rd.resource_id = rr.requiredresource_id WHERE `+where+` GROUP BY rd.name`, args...) {
		f.Used[r.S("name")] = r.I("total")
	}
	for _, r := range db.Query(`SELECT n.government, n.economy, COUNT(*) AS count FROM nations n
		INNER JOIN users u ON u.user_id = n.user_id WHERE `+where+` GROUP BY n.government, n.economy`, args...) {
		for _, up := range append(governmentUpkeep(r.S("government")), economyUpkeep(r.S("economy"))...) {
			f.Used[up.name] += up.amount * r.I("count")
		}
	}
	return f
}

// flowTable renders a Generated/Used/Net panel body for a set of flows.
// extraRows is inserted before the resource rows (used for alliance GDP).
func flowTable(title string, f Flows, hideIcons bool, extraRows string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="panel panel-default">
     <div class="panel-heading">%s</div>
     <table class="table">
      <thead>
        <tr>
`, title)
	if !hideIcons {
		b.WriteString("          <td></td>\n")
	}
	b.WriteString(`          <td style="text-align: right;">Resource</td>
          <td>Generated</td>
          <td>Used</td>
          <td>Net</td>
        </tr>
      </thead>
      <tbody>
`)
	b.WriteString(extraRows)
	names := map[string]bool{}
	for n := range f.Generated {
		names[n] = true
	}
	for n := range f.Used {
		names[n] = true
	}
	for _, name := range sortedKeys(names) {
		net := f.Generated[name] - f.Used[name]
		b.WriteString("    <tr>\n")
		if !hideIcons {
			fmt.Fprintf(&b, "    <td style=\"width: 16px;\"><img src=\"images/icons/%s.png\"/></td>\n", name)
		}
		fmt.Fprintf(&b, `    <td style="text-align: right;">%s</td>
    <td style="text-align: center;"><span class="text-success">%s</span></td>
    <td style="text-align: center;"><span class="text-danger">%s</span></td>
    <td style="text-align: center;"><span class="%s">%s</span></td>
    </tr>
`, name, commas(f.Generated[name]), commas(f.Used[name]), numberClass(float64(net)), commas(net))
	}
	b.WriteString("      </tbody>\n     </table>\n   </div>\n")
	return b.String()
}

// Projection is the expected change of a nation's standing over one tick.
type Projection struct {
	Sat int64
	SE  string // a number, or "Fixed"/"Ascending"
	NLR string
}

// relationDecay is the per-tick drift of an empire relation towards zero.
func relationDecay(rel int64) int64 {
	r := float64(rel)
	var d int64
	if rel > 250 {
		d -= floor((r - 250) / 50)
		if rel > 400 {
			d -= floor((r - 400) / 50)
			if rel > 800 {
				d -= floor((r - 800) / 50)
			}
		}
	}
	if rel < -450 {
		d -= ceil((r + 450) / 50)
		if rel < -700 {
			d -= ceil((r + 700) / 50)
			if rel < -900 {
				d -= ceil((r + 900) / 50)
			}
		}
	}
	return d
}

// oversatisfactionLoss is how much satisfaction a happy population sheds per tick.
func oversatisfactionLoss(sat int64, gov string) int64 {
	m := satMultiplier(gov)
	s := float64(sat)
	var loss int64
	if s > 250*m {
		loss += floor((s - 250*m) / (50 * m))
		if s > 500*m {
			loss += floor((s - 500*m) / (50 * m))
			if s > 750*m {
				loss += floor((s - 750*m) / (50 * m))
			}
		}
	}
	return loss
}

// empireSatLoss is the per-tick satisfaction cost of owning several nations.
func empireSatLoss(empireSize int64, gov string) int64 {
	if empireSize <= 1 {
		return 0
	}
	loss := float64((empireSize - 1) * (empireSize - 1) * 20)
	if gov == "Oppression" || gov == "Alicorn Elite" || gov == "Transponyism" {
		return ceil(loss / 3)
	}
	return int64(loss)
}

// project estimates a nation's satisfaction and relation changes next tick.
func (db *DB) project(n Row) Projection {
	gov, econ := n.S("government"), n.S("economy")
	se, nlr := n.I("se_relation"), n.I("nlr_relation")
	seTurn, nlrTurn := relationDecay(se), relationDecay(nlr)
	if t := se + seTurn; t > 0 {
		nlrTurn -= floor(float64(t) / 50)
	}
	if t := nlr + nlrTurn; t > 0 {
		seTurn -= floor(float64(t) / 50)
	}
	switch econ {
	case "Free Market":
		seTurn -= 3
		nlrTurn += 1
	case "State Controlled":
		seTurn += 1
		nlrTurn -= 3
	}
	seLabel, nlrLabel := "", ""
	switch gov {
	case "Democracy":
		seTurn, nlrTurn = seTurn-3, nlrTurn+2
	case "Decentralization":
		seTurn, nlrTurn = seTurn-3, nlrTurn+4
	case "Independence":
		seTurn, nlrTurn = seTurn-3, nlrTurn+6
	case "Repression":
		seTurn, nlrTurn = seTurn+2, nlrTurn-3
	case "Authoritarianism":
		seTurn, nlrTurn = seTurn+4, nlrTurn-3
	case "Oppression":
		seTurn, nlrTurn = seTurn+6, nlrTurn-3
	case "Solar Vassal":
		seLabel = "Fixed"
	case "Lunar Client":
		nlrLabel = "Fixed"
	case "Alicorn Elite", "Transponyism":
		seLabel, nlrLabel = "Ascending", "Ascending"
	}

	var sat, envDamage, cleaners int64
	for _, r := range db.Query(`SELECT rd.*, r.amount, r.disabled FROM resourcedefs rd
		INNER JOIN resources r ON r.resource_id = rd.resource_id WHERE r.nation_id = ?`, n.I("nation_id")) {
		active := r.I("amount") - r.I("disabled")
		sat += r.I("satisfaction")*active - r.I("disabled")
		if badMin := r.I("bad_min"); badMin != 0 && active > badMin {
			loss := ceil(math.Pow(float64(active-badMin), 2) / r.F("bad_div"))
			sat -= loss
			envDamage += loss
		}
		if id := r.I("resource_id"); id == resSolarEnviro || id == resLunarEnviro {
			cleaners += active
		}
		seTurn += active * r.I("se_relation")
		nlrTurn += active * r.I("nlr_relation")
	}
	if cleaners > 0 {
		sat += envDamage - ceil(float64(envDamage)*math.Pow(.9, float64(cleaners)))
	}
	if size := db.One("SELECT SUM(size) AS totalsize FROM forces WHERE nation_id = ?", n.I("nation_id")).I("totalsize"); size > 20 {
		sat -= ceil(float64(size-20) / 2)
	}
	empire := db.One("SELECT COUNT(*) AS empiresize FROM nations WHERE user_id = ?", n.I("user_id")).I("empiresize")
	sat -= empireSatLoss(empire, gov)
	switch gov {
	case "Democracy":
		sat += 15
	case "Decentralization":
		sat += 30
	case "Independence":
		sat += 50
	}
	sat -= oversatisfactionLoss(n.I("satisfaction"), gov)

	p := Projection{Sat: sat, SE: str(seTurn), NLR: str(nlrTurn)}
	if seLabel != "" {
		p.SE = seLabel
	}
	if nlrLabel != "" {
		p.NLR = nlrLabel
	}
	return p
}
