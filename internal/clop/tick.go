package clop

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

// RunScheduler runs the tick at every even hour (UTC), forever.
func (a *App) RunScheduler() {
	for {
		now := time.Now().UTC()
		next := now.Truncate(2 * time.Hour).Add(2 * time.Hour)
		time.Sleep(time.Until(next))
		a.RunTick(time.Now().UTC())
	}
}

// RunTick advances the game by one tick. Ticks happen every two hours;
// the ones at midnight and noon are also war ticks.
func (a *App) RunTick(now time.Time) {
	a.gameMu.Lock()
	defer a.gameMu.Unlock()
	start := time.Now()
	t := &ticker{db: a.db, now: now.UTC(), hour: now.UTC().Hour(), test: a.cfg.TestServer()}
	t.loadDefs()
	t.economy()
	a.db.Exec("DELETE FROM reports WHERE time < DATE_SUB(NOW(), INTERVAL 1 DAY)")
	t.uprisings()
	t.rebels()
	t.empireStrikes()
	t.ascensionDecay()
	if t.hour == 0 || t.hour == 12 || t.test {
		t.war()
	}
	if t.hour == 0 {
		a.db.Exec("UPDATE forces SET training = LEAST(training + 1, 20)")
	}
	a.db.Exec("DELETE FROM forcegroups WHERE forcegroup_id NOT IN (SELECT forcegroup_id FROM forces)")
	a.db.Exec(`UPDATE users SET stasismode = 1
		WHERE (stasismode = 0 AND lastactive < DATE_SUB(NOW(), INTERVAL 3 DAY)) OR lastactive IS NULL`)
	log.Printf("tick for %s done in %s", now.Format("2006-01-02 15:04"), time.Since(start).Round(time.Millisecond))
}

type resourceDef struct {
	Row
	effects      [][2]int64 // affected resource, amount per unit
	requirements [][2]int64 // required resource, amount per unit
}

type ticker struct {
	db   *DB
	now  time.Time
	hour int
	test bool
	defs map[int64]*resourceDef
}

func (t *ticker) loadDefs() {
	t.defs = map[int64]*resourceDef{}
	for _, r := range t.db.Query("SELECT * FROM resourcedefs") {
		t.defs[r.I("resource_id")] = &resourceDef{Row: r}
	}
	for _, r := range t.db.Query("SELECT * FROM resourceeffects") {
		if d := t.defs[r.I("resource_id")]; d != nil {
			d.effects = append(d.effects, [2]int64{r.I("affectedresource_id"), r.I("amount")})
		}
	}
	for _, r := range t.db.Query("SELECT * FROM resourcerequirements") {
		if d := t.defs[r.I("resource_id")]; d != nil {
			d.requirements = append(d.requirements, [2]int64{r.I("requiredresource_id"), r.I("amount")})
		}
	}
}

func (t *ticker) name(id int64) string {
	if d := t.defs[id]; d != nil {
		return d.S("name")
	}
	return ""
}

// nationTick is the running state of one nation during the economy pass.
type nationTick struct {
	t        *ticker
	id       int64
	gov      string
	sat      int64
	se, nlr  int64
	messages []string
	owned    map[int64]int64 // live enabled stock, updated as it is used
}

func (n *nationTick) say(format string, args ...any) {
	n.messages = append(n.messages, fmt.Sprintf(format, args...))
}

func (n *nationTick) addSat(delta int64) {
	n.t.db.Exec("UPDATE nations SET satisfaction = satisfaction + ? WHERE nation_id = ?", delta, n.id)
	n.sat += delta
}

func (n *nationTick) addRelation(empire string, delta int64, affector string) {
	col := empireColumn(empire)
	n.t.db.Exec("UPDATE nations SET "+col+" = "+col+" + ? WHERE nation_id = ?", delta, n.id)
	cur := &n.se
	if col == "nlr_relation" {
		cur = &n.nlr
	}
	n.say("Your relationship with the %s has %s due to your %s. (%d)", empire, chooseWord(*cur, delta), affector, delta)
	*cur += delta
}

// spend takes resources from stock if there are enough, reporting success.
func (n *nationTick) spend(resourceID, amount int64) bool {
	if n.owned[resourceID] < amount {
		return false
	}
	n.t.db.Exec("UPDATE resources SET amount = amount - ? WHERE nation_id = ? AND resource_id = ?", amount, n.id, resourceID)
	n.owned[resourceID] -= amount
	return true
}

// economy runs production, upkeep, satisfaction and income for every
// nation whose owner isn't in stasis.
func (t *ticker) economy() {
	db := t.db
	empireSize := map[int64]int64{}
	for _, r := range db.Query("SELECT COUNT(*) AS empiresize, user_id FROM nations GROUP BY user_id") {
		empireSize[r.I("user_id")] = r.I("empiresize")
	}
	reportID := fmt.Sprintf("report%d", t.now.Unix())
	for _, nation := range db.Query("SELECT n.*, u.empiremax, u.stasismode FROM nations n INNER JOIN users u ON u.user_id = n.user_id WHERE u.stasismode = 0") {
		n := &nationTick{t: t, id: nation.I("nation_id"), gov: nation.S("government"), sat: nation.I("satisfaction"),
			se: nation.I("se_relation"), nlr: nation.I("nlr_relation"), owned: map[int64]int64{}}
		ascending := nation.S("empiremax") != "" && nation.S("empiremax") != "0"
		startSat, startSE, startNLR := n.sat, n.se, n.nlr
		if t.hour == 0 {
			db.Exec("UPDATE nations SET age = age + 1 WHERE nation_id = ?", n.id)
		}
		t.moodSwings(n, empireSize[nation.I("user_id")], ascending)
		gdp, buildings := t.production(n)
		t.equipmentDecay(n)
		if t.hour == 0 || t.hour == 12 {
			t.militaryUpkeep(n)
		}
		if n.owned[resForbiddenDone] >= 200 {
			msg := "You have completed the forbidden research, and the facility has been automatically dismantled. A new Major Action is available to you."
			n.say("%s", msg)
			db.systemMessage(nation.I("user_id"), msg)
			db.Exec("DELETE FROM resources WHERE (resource_id = ? OR resource_id = ?) AND nation_id = ?", resForbiddenLab, resForbiddenDone, n.id)
			db.Exec("UPDATE users SET seesecrets = 1 WHERE user_id = ?", nation.I("user_id"))
		}
		t.governmentUpkeep(n)
		t.economyUpkeep(n, nation.S("economy"))
		if buildings == 0 {
			n.say("You lost 5 satisfaction for not having any buildings!")
			n.addSat(-5)
		}
		if size := db.One("SELECT SUM(size) AS totalsize FROM forces WHERE nation_id = ?", n.id).I("totalsize"); size > 20 {
			penalty := ceil(float64(size-20) / 2)
			n.addSat(-penalty)
			n.say("You lost %d satisfaction for having a military of total size %d.", penalty, size)
		}
		t.applyCaps(n, ascending)

		gdp = applyGovernmentGDP(gdp, n.gov, n.sat)
		changes := fmt.Sprintf("\tChange in SE Relation: %d<br/>\n\tChange in NLR Relation: %d<br/>\n", n.se-startSE, n.nlr-startNLR)
		if ascending {
			changes = "\tYou're ascending; your relationships with the Solar Empire and New Lunar Republic can only go down.<br/>\n"
		}
		report := fmt.Sprintf(`	<div id="%[1]s" class="report-showbutton"><a href="javascript:;" onclick="document.getElementById('%[1]s').style.display = 'none';
document.getElementById('%[1]sx').style.display = 'block';">Show Details</a></div>
	<div id="%[1]sx" class="report-details"><a href="javascript:;" onclick="document.getElementById('%[1]s').style.display = 'block';
document.getElementById('%[1]sx').style.display = 'none';">Hide Details</a><br/>
%[2]s
	</div>
	<b>
	Change in Satisfaction: %[3]d<br/>
%[4]s	</b>`, reportID, joinBR(n.messages), n.sat-startSat, changes)
		db.report(n.id, report)
		db.Exec("UPDATE nations SET funds = funds + ?, gdp_last_turn = ? WHERE nation_id = ?", math.Round(gdp), math.Max(0, math.Round(gdp)), n.id)
		db.Exec("DELETE FROM resources WHERE amount = 0 AND nation_id = ?", n.id)
	}
}

// moodSwings applies the drift of satisfaction and empire relations that
// happens regardless of what a nation owns.
func (t *ticker) moodSwings(n *nationTick, empireSize int64, ascending bool) {
	db := t.db
	if loss := oversatisfactionLoss(n.sat, n.gov); loss > 0 {
		m := satMultiplier(n.gov)
		desc := "A satisfied population"
		if float64(n.sat) > 750*m {
			desc = "A loving population"
		} else if float64(n.sat) > 500*m {
			desc = "A very satisfied population"
		}
		db.Exec("UPDATE nations SET satisfaction = satisfaction - ? WHERE nation_id = ?", loss, n.id)
		n.sat -= loss
		n.say("%s is hard to keep. (-%d sat)", desc, loss)
	}
	if loss := empireSatLoss(empireSize, n.gov); loss > 0 {
		n.say("You lose %d sat for having an empire of %d nations.", loss, empireSize)
		n.addSat(-loss)
	}

	type drift struct {
		empire string
		rel    *int64
	}
	for _, d := range []drift{{"Solar Empire", &n.se}, {"New Lunar Republic", &n.nlr}} {
		rel := *d.rel
		decay := relationDecay(rel)
		col := empireColumn(d.empire)
		switch {
		case decay < 0:
			desc := "A good friend"
			if rel > 800 {
				desc = "An extremely good friend"
			} else if rel > 400 {
				desc = "A very good friend"
			}
			db.Exec("UPDATE nations SET "+col+" = "+col+" + ? WHERE nation_id = ?", decay, n.id)
			*d.rel += decay
			n.say("%s is hard to keep; you lose %d relationship with the %s.", desc, -decay, d.empire)
		case decay > 0 && !ascending:
			desc := "A bad enemy"
			if rel < -900 {
				desc = "An extremely bad enemy"
			} else if rel < -700 {
				desc = "A very bad enemy"
			}
			db.Exec("UPDATE nations SET "+col+" = "+col+" + ? WHERE nation_id = ?", decay, n.id)
			*d.rel += decay
			n.say("%s forgets eventually; you gain %d relationship with the %s.", desc, decay, d.empire)
		}
	}

	// Each empire resents friendship with the other.
	var seJealousy, nlrJealousy int64
	if n.se > 0 {
		nlrJealousy = n.se / 50
	}
	if n.nlr > 0 {
		seJealousy = n.nlr / 50
	}
	if seJealousy > 0 {
		n.say("The Solar Empire doesn't like your good relations with the New Lunar Republic. (-%d)", seJealousy)
		db.Exec("UPDATE nations SET se_relation = se_relation - ? WHERE nation_id = ?", seJealousy, n.id)
		n.se -= seJealousy
	}
	if nlrJealousy > 0 {
		n.say("The New Lunar Republic doesn't like your good relations with the Solar Empire. (-%d)", nlrJealousy)
		db.Exec("UPDATE nations SET nlr_relation = nlr_relation - ? WHERE nation_id = ?", nlrJealousy, n.id)
		n.nlr -= nlrJealousy
	}
}

// production runs every building and resource: requirements, outputs,
// relation and satisfaction effects, pollution and stockpile siphoning.
// It returns the base GDP earned and how many buildings are running.
func (t *ticker) production(n *nationTick) (float64, int64) {
	db := t.db
	type stock struct{ id, amount int64 }
	var stocks []stock
	var disabled int64
	for _, r := range db.Query("SELECT * FROM resources WHERE nation_id = ? ORDER BY resource_id", n.id) {
		active := r.I("amount") - r.I("disabled")
		stocks = append(stocks, stock{r.I("resource_id"), active})
		n.owned[r.I("resource_id")] = active
		disabled += r.I("disabled")
	}
	if disabled > 0 {
		n.addSat(-disabled)
		n.say("You lose %d satisfaction for having %d disabled buildings.", disabled, disabled)
	}
	gdp := 50000.0
	var buildings, envDamage, cleaners int64
	for _, s := range stocks {
		id, amount := s.id, s.amount
		def := t.defs[id]
		if def == nil {
			continue
		}
		name := def.S("name")
		if amount != 0 {
			if def.B("is_building") && id != resSunWorship && id != resMoonWorship {
				buildings += amount
			}
			if badMin := def.I("bad_min"); badMin != 0 && amount > badMin {
				loss := ceil(math.Pow(float64(amount-badMin), 2) / def.F("bad_div"))
				n.addSat(-loss)
				n.say("Too many %s cause environmental damage! (-%d sat)", name, loss)
				envDamage += loss
			}
			enough := true
			for _, req := range def.requirements {
				if n.owned[req[0]] < req[1]*amount {
					if enough {
						n.say("You don't have enough %s to run your %d %s! (-%d sat)", t.name(req[0]), amount, name, amount)
						n.addSat(-amount)
					} else {
						n.say("You don't have enough %s to run your %d %s!", t.name(req[0]), amount, name)
					}
					enough = false
				}
			}
			if enough {
				label := fmt.Sprintf("%d %s", amount, name)
				if id == resSolarEnviro || id == resLunarEnviro {
					cleaners += amount
				}
				if eff := def.I("se_relation"); eff != 0 {
					n.addRelation("Solar Empire", eff*amount, label)
				}
				if eff := def.I("nlr_relation"); eff != 0 {
					n.addRelation("New Lunar Republic", eff*amount, label)
				}
				if eff := def.I("satisfaction"); eff != 0 {
					delta := eff * amount
					db.Exec("UPDATE nations SET satisfaction = satisfaction + ? WHERE nation_id = ?", delta, n.id)
					n.say("Your population's satisfaction has %s due to your %s. (%d)", chooseWord(n.sat, delta), label, delta)
					n.sat += delta
				}
				gdp += def.F("gdp") * float64(amount)
				for _, e := range def.effects {
					added := e[1] * amount
					n.say("You gained %d %s from your %d %s.", added, t.name(e[0]), amount, name)
					db.Exec(`INSERT INTO resources (nation_id, resource_id, amount, disabled) VALUES (?, ?, ?, 0)
						ON DUPLICATE KEY UPDATE amount = amount + ?`, n.id, e[0], added, added)
				}
				for _, req := range def.requirements {
					used := req[1] * amount
					n.say("Your %d %s used up %d %s.", amount, name, used, t.name(req[0]))
					db.Exec("UPDATE resources SET amount = amount - ? WHERE nation_id = ? AND resource_id = ?", used, n.id, req[0])
					n.owned[req[0]] -= used
				}
			}
		}
		if tax := stockpileTax(amount, 50000); tax > 0 {
			db.Exec("UPDATE resources SET amount = amount - ? WHERE nation_id = ? AND resource_id = ?", tax, n.id, id)
			n.say("As you have more than 50,000 %s, %d was siphoned off.", name, tax)
		}
	}
	if cleaners > 0 {
		fixed := envDamage - ceil(float64(envDamage)*math.Pow(.9, float64(cleaners)))
		n.addSat(fixed)
		n.say("Some of the environmental damage has been repaired. (%d sat)", fixed)
	}
	return gdp, buildings
}

// equipmentDecay siphons off weapon and armor stockpiles over 1,000.
func (t *ticker) equipmentDecay(n *nationTick) {
	for _, k := range []equipmentKind{weaponEquip, armorEquip} {
		for _, r := range t.db.Query("SELECT s."+k.idcol+" AS id, s.amount, d.name FROM "+k.stock+" s INNER JOIN "+k.defs+
			" d ON d."+k.idcol+" = s."+k.idcol+" WHERE s.nation_id = ?", n.id) {
			if tax := stockpileTax(r.I("amount"), 1000); tax > 0 {
				t.db.Exec("UPDATE "+k.stock+" SET amount = amount - ? WHERE nation_id = ? AND "+k.idcol+" = ?", tax, n.id, r.I("id"))
				n.say("As you have more than 1,000 %s, %d were siphoned off.", r.S("name"), tax)
			}
		}
	}
}

// forceUpkeep is what each unit type eats every war tick, per unit of size.
var forceUpkeep = map[int64]struct {
	resource, perUnit int64
	name              string
}{
	1: {resApples, 5, "apples"},
	2: {resGasoline, 5, "gasoline"},
	3: {resCoffee, 5, "coffee"},
	4: {resGems, 5, "gems"},
	5: {resGasoline, 5, "gasoline"},
	6: {resGems, 10, "gems"},
}

func (t *ticker) militaryUpkeep(n *nationTick) {
	for _, f := range t.db.Query("SELECT size, type, name, force_id FROM forces WHERE nation_id = ?", n.id) {
		up, ok := forceUpkeep[f.I("type")]
		if !ok {
			continue
		}
		need := f.I("size") * up.perUnit
		if n.spend(up.resource, need) {
			n.say("Your %s used up %d %s.", f.S("name"), need, up.name)
		} else {
			t.db.Exec("DELETE FROM forces WHERE force_id = ?", f.I("force_id"))
			n.say("You couldn't pay the upkeep for your %s and it's gone!", f.S("name"))
		}
	}
}

func (t *ticker) governmentUpkeep(n *nationTick) {
	type rule struct {
		label    string // how the government is named in messages
		failSat  int64  // satisfaction lost when upkeep can't be paid
		failMsg  string
		paidSat  int64 // satisfaction gained when it can
		se, nlr  int64 // relation change every tick
		relLabel string
	}
	rules := map[string]rule{
		"Independence":     {"Your Independence", 100, "Your Independence lacks the gasoline and vehicle parts to function properly!", 50, -3, 6, "Independence"},
		"Decentralization": {"Your decentralized government", 100, "Your decentralized government lacks the gasoline and vehicle parts to function properly!", 30, -3, 4, "Decentralization"},
		"Democracy":        {"Your Democracy", 20, "Your government lacks the gasoline and vehicle parts to function properly!", 15, -3, 2, "Democracy"},
		"Repression":       {"Your machinery of Repression", 50, "Your government lacks the gasoline to function properly!", 0, 2, -3, "Repression"},
		"Authoritarianism": {"Your Authoritarian government", 400, "Your government lacks the gasoline and machinery parts to function properly!", 0, 4, -3, "Authoritarianism"},
		"Oppression":       {"Your machinery of Oppression", 500, "Your government lacks the gasoline and machinery parts to function properly!", 0, 6, -3, "Oppression"},
	}
	switch n.gov {
	case "Lunar Client":
		n.addRelation("New Lunar Republic", 60, "Lunar Client status")
		return
	case "Solar Vassal":
		n.addRelation("Solar Empire", 60, "Solar Vassal status")
		return
	}
	r, ok := rules[n.gov]
	if !ok {
		return
	}
	needs := governmentUpkeep(n.gov)
	paid := true
	for _, u := range needs {
		if n.owned[u.resourceID] < u.amount {
			paid = false
		}
	}
	if !paid {
		n.say("%s (-%d sat)", r.failMsg, r.failSat)
		n.addSat(-r.failSat)
	} else {
		var used []string
		for _, u := range needs {
			n.spend(u.resourceID, u.amount)
			used = append(used, fmt.Sprintf("%d %s", u.amount, strings.ToLower(u.name)))
		}
		n.say("%s used %s.", r.label, strings.Join(used, " and "))
		if r.paidSat != 0 {
			delta := r.paidSat
			db := t.db
			db.Exec("UPDATE nations SET satisfaction = satisfaction + ? WHERE nation_id = ?", delta, n.id)
			n.say("Your population's satisfaction has %s due to your %s. (%d)", chooseWord(n.sat, delta), r.relLabel, delta)
			n.sat += delta
		}
	}
	// Left-leaning governments please the NLR first; the others the SE.
	if r.se < 0 {
		n.addRelation("Solar Empire", r.se, r.relLabel)
		n.addRelation("New Lunar Republic", r.nlr, r.relLabel)
	} else {
		n.addRelation("New Lunar Republic", r.nlr, r.relLabel)
		n.addRelation("Solar Empire", r.se, r.relLabel)
	}
}

func (t *ticker) economyUpkeep(n *nationTick, economy string) {
	db := t.db
	switch economy {
	case "State Controlled":
		if n.spend(resCider, 6) {
			n.say("Your State Controllers drank 6 cider.")
			db.Exec("UPDATE nations SET active_economy = 1 WHERE nation_id = ?", n.id)
		} else {
			n.say("Your economy lacks the cider to function properly! (-25 sat, unable to make deals)")
			n.addSat(-25)
			db.Exec("UPDATE nations SET active_economy = 0 WHERE nation_id = ?", n.id)
		}
		n.addRelation("New Lunar Republic", -3, "State Controlled economy")
		n.addRelation("Solar Empire", 1, "State Controlled economy")
	case "Free Market":
		if n.spend(resCoffee, 6) {
			n.say("Your Free Marketeers drank 6 coffee.")
			db.Exec("UPDATE nations SET active_economy = 1 WHERE nation_id = ?", n.id)
		} else {
			n.say("Your economy lacks the coffee to function properly! (-25 sat, trading efficiency eliminated)")
			n.addSat(-25)
			db.Exec("UPDATE nations SET active_economy = 0 WHERE nation_id = ?", n.id)
		}
		n.addRelation("Solar Empire", -3, "Free Market")
		n.addRelation("New Lunar Republic", 1, "Free Market")
	}
}

// applyCaps clamps satisfaction and relations to their limits.
func (t *ticker) applyCaps(n *nationTick, ascending bool) {
	db := t.db
	if cap := satCap(n.gov); n.sat > cap {
		label := "the " + n.gov + " satisfaction cap"
		if satMultiplier(n.gov) == 1 {
			label = "the satisfaction cap"
		}
		n.say("You hit %s of %d. (-%d)", label, cap, n.sat-cap)
		db.Exec("UPDATE nations SET satisfaction = ? WHERE nation_id = ?", cap, n.id)
		n.sat = cap
	}
	for _, e := range []struct {
		empire string
		rel    *int64
	}{{"Solar Empire", &n.se}, {"New Lunar Republic", &n.nlr}} {
		col := empireColumn(e.empire)
		if *e.rel > 1000 {
			n.say("You hit the %s relationship cap of 1000. (-%d)", e.empire, *e.rel-1000)
			db.Exec("UPDATE nations SET "+col+" = 1000 WHERE nation_id = ?", n.id)
			*e.rel = 1000
		}
		if !ascending && *e.rel < -1000 {
			n.say("Even for the %s, there are limits to hate. (+%d)", e.empire, -1000-*e.rel)
			db.Exec("UPDATE nations SET "+col+" = -1000 WHERE nation_id = ?", n.id)
			*e.rel = -1000
		}
	}
}

// bury records a dead nation in the graveyard (if it lived at least four
// weeks), deletes it, and tells its owner and the world.
func (t *ticker) bury(nation Row, killer, ownerMessage, news string) {
	db := t.db
	id := nation.I("nation_id")
	created, err := time.ParseInLocation("2006-01-02 15:04:05", nation.S("creationdate"), time.UTC)
	if err == nil && created.Before(t.now.Add(-28*24*time.Hour)) {
		list := func(building int) string {
			var b strings.Builder
			for _, r := range db.Query(`SELECT rd.name, r.amount FROM resources r INNER JOIN resourcedefs rd ON rd.resource_id = r.resource_id
				WHERE rd.is_building = ? AND r.nation_id = ?`, building, id) {
				fmt.Fprintf(&b, "<td>%s</td><td>%s</td></tr>\n", r.S("name"), r.S("amount"))
			}
			return b.String()
		}
		var reports strings.Builder
		for _, r := range db.Query("SELECT * FROM reports WHERE nation_id = ? ORDER BY time DESC", id) {
			fmt.Fprintf(&reports, "<tr><td>%s</td><td>%s</td></tr>\n", r.S("report"), r.S("time"))
		}
		details := fmt.Sprintf(`<center>Killed by %s<br/>
%s Bits<br/>
%s<br/>
%s</center>
<center><h4 class="graveyardresourcesheading">Resources</h4></center>
<table class="graveyardresourcestable table table-striped table-bordered">%s</table>
<center><h4 class="graveyardbuildingsheading">Buildings</h4></center>
<table class="graveyardbuildingstable table table-striped table-bordered">%s</table>
<center><h4 class="graveyardreportsheading">Reports</h4></center>
<table class="graveyardreportstable table table-striped table-bordered">%s</table>`,
			esc(killer), commas(nation.S("funds")), nation.S("government"), nation.S("economy"), list(0), list(1), reports.String())
		db.Exec("INSERT INTO graveyard (name, details, killer, deathdate) VALUES (?, ?, ?, NOW())", nation.S("name"), details, killer)
	}
	db.deleteNation(id)
	db.systemMessage(nation.I("user_id"), ownerMessage)
	db.news(news)
}

// uprisings destroy nations whose satisfaction has collapsed completely.
func (t *ticker) uprisings() {
	for _, n := range t.db.Query(`SELECT n.*, u.username FROM nations n INNER JOIN users u ON u.user_id = n.user_id WHERE n.satisfaction < -5000`) {
		t.bury(n, "Massive Uprising",
			fmt.Sprintf("Congratulations! Your nation of %s got so low in satisfaction that it collapsed instantly.", n.S("name")),
			fmt.Sprintf(`The nation of %s (<a href="viewuser.php?user_id=%s">%s</a>) has exploded from a massive uprising!`, n.S("name"), n.S("user_id"), n.S("username")))
	}
}

// revoltThreshold is the satisfaction below which a government's ponies
// revolt, and how much of the unrest it suppresses.
var revoltThreshold = map[string][2]int64{
	"Loose Despotism":  {-100, 25},
	"Solar Vassal":     {-100, 25},
	"Lunar Client":     {-100, 25},
	"Democracy":        {0, 0},
	"Independence":     {0, 0},
	"Decentralization": {0, 0},
	"Repression":       {-300, 75},
	"Authoritarianism": {-400, 100},
	"Oppression":       {-500, 125},
	"Alicorn Elite":    {-500, 125},
	"Transponyism":     {-500, 125},
}

// rebels raise rebel armies in unhappy nations.
func (t *ticker) rebels() {
	db := t.db
	for _, n := range db.Query("SELECT nation_id, satisfaction, government FROM nations") {
		th, ok := revoltThreshold[n.S("government")]
		if !ok || n.I("satisfaction") >= th[0] {
			continue
		}
		strength := ceil(float64(-n.I("satisfaction"))/4) - th[1]
		if strength < 1 {
			continue
		}
		group := db.Exec("INSERT INTO forcegroups (name, location_id, destination_id, attack_mission, nation_id) VALUES ('Rebels', ?, 0, 1, -3)", n.I("nation_id"))
		db.Exec(`INSERT INTO forces (weapon_id, armor_id, training, nation_id, type, size, name, forcegroup_id)
			VALUES (0, 0, 0, -3, 1, ?, ?, ?)`, strength, fmt.Sprintf("Rebel Group %d", group), group)
		db.Exec("UPDATE nations SET satisfaction = satisfaction + ? WHERE nation_id = ?", strength, n.I("nation_id"))
		db.report(n.I("nation_id"), fmt.Sprintf("Your satisfaction is below the minimum - your ponies are revolting!\n(You gain %d sat among the rest of your nation as the subversives stop participating in it.)", strength))
	}
}

// empireStrikes has the empires send airstrikes against nations they hate
// (or that are ascending).
func (t *ticker) empireStrikes() {
	db := t.db
	for _, n := range db.Query(`SELECT n.se_relation, n.nlr_relation, n.nation_id, u.empiremax FROM nations n
		INNER JOIN users u ON n.user_id = u.user_id WHERE n.nlr_relation + n.se_relation < -50`) {
		ascending := n.S("empiremax") != "" && n.S("empiremax") != "0"
		for _, e := range []struct {
			npc  int64
			name string
			col  string
		}{{-1, "Solar Empire", "se_relation"}, {-2, "New Lunar Republic", "nlr_relation"}} {
			rel := n.I(e.col)
			if rel >= -25 {
				continue
			}
			strength := ceil(float64(-rel) / 4)
			group := db.Exec("INSERT INTO forcegroups (name, location_id, destination_id, attack_mission, nation_id) VALUES (?, ?, 0, 1, ?)", e.name, n.I("nation_id"), e.npc)
			// Canopy lights, dragon armor, maximum training.
			db.Exec(`INSERT INTO forces (weapon_id, armor_id, training, nation_id, type, size, name, forcegroup_id)
				VALUES (13, 11, 20, ?, 3, ?, ?, ?)`, e.npc, strength, fmt.Sprintf("%s %d", e.name, group), group)
			var report string
			if !ascending {
				db.Exec("UPDATE nations SET "+e.col+" = "+e.col+" + ? WHERE nation_id = ?", strength, n.I("nation_id"))
				report = fmt.Sprintf("The %s hates you enough to send an airstrike and you don't have enough support from its opponent!\n(Wishing to avoid a protracted war, it has reduced its hate for you by %d.)", e.name, strength)
			} else {
				report = fmt.Sprintf("The %s has attacked you for daring to ascend! (%d)", e.name, strength)
			}
			db.report(n.I("nation_id"), report)
		}
	}
}

// ascensionDecay lowers the relation ceiling of ascending players each tick.
func (t *ticker) ascensionDecay() {
	db := t.db
	db.Exec("UPDATE users SET empiremax = empiremax - 1 WHERE empiremax < 0")
	for _, u := range db.Query("SELECT empiremax, user_id FROM users WHERE empiremax IS NOT NULL") {
		db.Exec("UPDATE nations SET se_relation = ? WHERE se_relation > ? AND user_id = ?", u.I("empiremax"), u.I("empiremax"), u.I("user_id"))
		db.Exec("UPDATE nations SET nlr_relation = ? WHERE nlr_relation > ? AND user_id = ?", u.I("empiremax"), u.I("empiremax"), u.I("user_id"))
	}
}

// war moves arriving forces into place and fights every battle.
func (t *ticker) war() {
	db := t.db
	if t.test {
		db.Exec("UPDATE forcegroups SET location_id = destination_id, destination_id = 0, departuredate = NULL WHERE departuredate IS NOT NULL")
	} else {
		tick := time.Date(t.now.Year(), t.now.Month(), t.now.Day(), t.hour, 0, 0, 0, time.UTC)
		before := func(h int) string { return tick.Add(-time.Duration(h) * time.Hour).Format("2006-01-02 15:04:05") }
		for _, g := range db.Query(`SELECT fg.forcegroup_id FROM forcegroups fg
			LEFT JOIN nations n ON fg.location_id = n.nation_id
			LEFT JOIN nations n2 ON fg.destination_id = n2.nation_id
			WHERE fg.departuredate IS NOT NULL AND (
			(fg.departuredate < ? AND n.region = n2.region AND fg.attack_mission = 0) OR
			(fg.departuredate < ? AND n.region = n2.region AND fg.attack_mission = 1) OR
			(fg.departuredate < ? AND fg.attack_mission = 0) OR
			(fg.departuredate < ? AND fg.attack_mission = 1))`, before(12), before(24), before(36), before(48)) {
			db.Exec("UPDATE forcegroups SET location_id = destination_id, destination_id = 0, departuredate = NULL WHERE forcegroup_id = ?", g.I("forcegroup_id"))
		}
	}

	stasis := map[int64]bool{}
	var battlegrounds []int64
	for _, r := range db.Query(`SELECT DISTINCT fg.location_id, u.stasismode FROM forcegroups fg
		LEFT JOIN nations n ON n.nation_id = fg.location_id LEFT JOIN users u ON u.user_id = n.user_id
		WHERE fg.departuredate IS NULL AND fg.attack_mission = 1 ORDER BY fg.location_id`) {
		if _, seen := stasis[r.I("location_id")]; !seen {
			battlegrounds = append(battlegrounds, r.I("location_id"))
		}
		stasis[r.I("location_id")] = r.B("stasismode")
	}
	for _, bg := range battlegrounds {
		t.battle(bg, stasis[bg])
	}
}

func (t *ticker) battle(bg int64, ownerInStasis bool) {
	db := t.db
	b := &Battle{Shielded: func(u *Unit) bool { return u.NationID == bg && !ownerInStasis }}
	for _, r := range db.Query(`SELECT f.*, fg.attack_mission, wd.dmg_cavalry, wd.dmg_tanks, wd.dmg_pegasi, wd.dmg_unicorns, wd.dmg_naval,
		ad.arm_cavalry, ad.arm_tanks, ad.arm_pegasi, ad.arm_unicorns, ad.arm_naval, n.name AS nationname
		FROM forces f
		LEFT JOIN nations n ON f.nation_id = n.nation_id
		INNER JOIN forcegroups fg ON f.forcegroup_id = fg.forcegroup_id
		LEFT JOIN armordefs ad ON f.armor_id = ad.armor_id
		LEFT JOIN weapondefs wd ON f.weapon_id = wd.weapon_id
		WHERE fg.location_id = ? AND fg.departuredate IS NULL ORDER BY f.force_id`, bg) {
		u := newUnit(r, r.B("attack_mission"))
		if u.NationID < 0 {
			u.NationName = npcName(u.NationID)
		}
		b.Units = append(b.Units, u)
	}
	b.Run()

	reports := map[int64][]string{}
	var order []int64
	say := func(nation int64, msg string) {
		if _, ok := reports[nation]; !ok {
			order = append(order, nation)
		}
		reports[nation] = append(reports[nation], msg)
	}
	for _, h := range b.Hits() {
		a, d := h.Attacker, h.Defender
		dmg := phpFloat(round(h.Damage, 6))
		say(a.NationID, fmt.Sprintf("Your %s (size %d) hit\n%s's %s (size %d)\nfor %s damage (%d hits)", a.Name, a.Size, nationRef(d), d.Name, d.Size, dmg, h.Hits))
		say(d.NationID, fmt.Sprintf("Your %s (size %d) were hit by\n%s's %s (size %d)\nfor %s damage (%d hits)", d.Name, d.Size, nationRef(a), a.Name, a.Size, dmg, h.Hits))
	}
	for _, u := range b.Units {
		loss := u.Losses()
		switch {
		case loss <= 0:
		case loss < u.Size:
			db.Exec("UPDATE forces SET size = size - ? WHERE force_id = ?", loss, u.ForceID)
			say(u.NationID, fmt.Sprintf("Your %s lost %d size!", u.Name, loss))
		default:
			db.Exec("DELETE FROM forces WHERE force_id = ?", u.ForceID)
			say(u.NationID, fmt.Sprintf("Your %s scattered to the four winds!", u.Name))
		}
	}
	for _, nation := range order {
		if nation > 0 {
			db.report(nation, joinBR(reports[nation]))
		}
	}
	db.Exec("DELETE FROM forcegroups WHERE forcegroup_id NOT IN (SELECT forcegroup_id FROM forces)")

	if db.One("SELECT forcegroup_id FROM forcegroups WHERE location_id = ? AND attack_mission = 0 AND departuredate IS NULL LIMIT 1", bg) != nil {
		return
	}
	// The biggest attacking army (summed per player) takes the nation.
	// Players beat the empires and rebels regardless of size; those all
	// count as one ownerless army, credited to whoever arrived first.
	attackers := `FROM forces f INNER JOIN forcegroups fg ON f.forcegroup_id = fg.forcegroup_id %s JOIN nations n ON f.nation_id = n.nation_id
		WHERE fg.location_id = ? AND fg.attack_mission = 1 AND fg.departuredate IS NULL %s`
	biggest := "SELECT SUM(f.size) AS sumsize, n.user_id, MIN(f.force_id) AS first_force " + attackers + " GROUP BY n.user_id ORDER BY sumsize DESC, n.user_id DESC LIMIT 1"
	army := db.One(fmt.Sprintf(biggest, "INNER", "AND n.nation_id > 0"), bg)
	if army == nil {
		army = db.One(fmt.Sprintf(biggest, "LEFT", ""), bg)
	}
	if army == nil {
		return // everyone died
	}
	winner := db.One(`SELECT f.nation_id, n.user_id, n.name, n.government FROM forces f
		LEFT JOIN nations n ON f.nation_id = n.nation_id WHERE f.force_id = ?`, army.I("first_force"))
	loser := db.One("SELECT n.*, u.username FROM nations n INNER JOIN users u ON u.user_id = n.user_id WHERE n.nation_id = ?", bg)
	if loser == nil {
		return
	}
	if !winner.B("user_id") || winner.S("government") == "Independence" {
		t.destroyed(loser, winner)
	} else {
		t.conquered(loser, winner)
	}
	// The empires and the rebels go home once a nation falls.
	for _, g := range db.Query("SELECT forcegroup_id FROM forcegroups WHERE (location_id = ? OR location_id < 0) AND nation_id < 0", bg) {
		db.Exec("DELETE FROM forcegroups WHERE forcegroup_id = ?", g.I("forcegroup_id"))
		db.Exec("DELETE FROM forces WHERE forcegroup_id = ?", g.I("forcegroup_id"))
	}
}

// destroyed handles a nation overrun by an empire, rebels or an
// independent nation: it is wiped out rather than conquered.
func (t *ticker) destroyed(loser, winner Row) {
	name, owner := loser.S("name"), fmt.Sprintf(`<a href="viewuser.php?user_id=%s">%s</a>`, loser.S("user_id"), loser.S("username"))
	switch winner.I("nation_id") {
	case -1, -2:
		empire := "The " + npcName(winner.I("nation_id"))
		t.bury(loser, empire, fmt.Sprintf("%s has destroyed %s!", empire, name), fmt.Sprintf("%s has destroyed %s (%s)!", empire, name, owner))
	case -3:
		t.bury(loser, "Rebels", fmt.Sprintf("The ponies of %s have successfully revolted against you!", name),
			fmt.Sprintf("The ponies of %s have successfully revolted against %s!", name, owner))
	default:
		t.bury(loser, winner.S("name"),
			fmt.Sprintf("Your nation of %s has been destroyed by the independent nation of %s!", name, winner.S("name")),
			fmt.Sprintf(`The nation of %s (%s) has been destroyed by the independent nation of <a href="viewnation.php?nation_id=%s">%s</a>!`,
				name, owner, winner.S("nation_id"), winner.S("name")))
	}
}

// conquered hands a defeated nation to the player with the biggest army there.
func (t *ticker) conquered(loser, winner Row) {
	db := t.db
	bg := loser.I("nation_id")
	oldest := db.One("SELECT n.government, n.economy, u.username FROM nations n INNER JOIN users u ON u.user_id = n.user_id WHERE u.user_id = ? ORDER BY n.nation_id ASC LIMIT 1",
		winner.I("user_id"))
	db.systemMessage(loser.I("user_id"), fmt.Sprintf("Your nation of %s has been conquered by the armies of %s!", loser.S("name"), oldest.S("username")))
	db.news(fmt.Sprintf(`The nation of <a href="viewnation.php?nation_id=%d">%s</a> (<a href="viewuser.php?user_id=%s">%s</a>)
has been conquered by the armies of <a href="viewuser.php?user_id=%s">%s</a>!`,
		bg, loser.S("name"), loser.S("user_id"), loser.S("username"), winner.S("user_id"), oldest.S("username")))
	db.Exec("UPDATE nations SET user_id = ?, government = ?, economy = ? WHERE nation_id = ?", winner.I("user_id"), oldest.S("government"), oldest.S("economy"), bg)
	db.Exec("DELETE FROM forces WHERE nation_id = ?", bg)
	db.Exec("DELETE FROM forcegroups WHERE nation_id = ?", bg)
	// Anyone coming to attack it is now coming to defend it, and vice versa.
	db.Exec(`UPDATE forcegroups SET attack_mission = !attack_mission
		WHERE (destination_id = ? AND departuredate IS NOT NULL) OR (location_id = ? AND departuredate IS NULL)`, bg, bg)
	db.Exec("UPDATE forcegroups SET oldmission = !oldmission WHERE location_id = ? AND departuredate IS NOT NULL", bg)
	switch oldest.S("government") {
	case "Solar Vassal":
		db.Exec("DELETE FROM resources WHERE nation_id = ? AND resource_id = ?", bg, resDrugFarm)
		db.Exec("UPDATE nations SET se_relation = 1000 WHERE nation_id = ?", bg)
	case "Lunar Client":
		db.Exec("DELETE FROM resources WHERE nation_id = ? AND resource_id = ?", bg, resDrugFarm)
		db.Exec("UPDATE nations SET nlr_relation = 1000 WHERE nation_id = ?", bg)
	}
}
