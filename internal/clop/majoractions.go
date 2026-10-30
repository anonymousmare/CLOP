package clop

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed content/*.html
var content embed.FS

func contentHTML(name string) string {
	b, err := content.ReadFile("content/" + name + ".html")
	if err != nil {
		panic(err)
	}
	return string(b)
}

// requireResources records an error for every resource the nation is short of.
func (p *Page) requireResources(nationID int64, needs []upkeep, message string) {
	for _, n := range needs {
		if p.db.resourceAmount(nationID, n.resourceID) < n.amount {
			p.Err(message, strings.ToLower(n.name))
		}
	}
}

func (p *Page) spendResources(nationID int64, needs []upkeep) {
	for _, n := range needs {
		p.db.Exec("UPDATE resources SET amount = amount - ? WHERE nation_id = ? AND resource_id = ?", n.amount, nationID, n.resourceID)
	}
	p.db.Exec("DELETE FROM resources WHERE amount = 0 AND nation_id = ?", nationID)
}

// hasBuyerOffers reports whether a nation has any open buy orders.
func (db *DB) hasBuyerOffers(nationID int64) bool {
	for _, t := range []string{"buyermarketplace", "weaponsbuyermarketplace", "armorbuyermarketplace"} {
		if db.One("SELECT amount FROM "+t+" WHERE nation_id = ? LIMIT 1", nationID).B("amount") {
			return true
		}
	}
	return false
}

func (db *DB) hasMarketListings(nationID int64) bool {
	for _, t := range []string{"marketplace", "weaponsmarketplace", "armormarketplace"} {
		if db.One("SELECT amount FROM "+t+" WHERE nation_id = ? LIMIT 1", nationID).B("amount") {
			return true
		}
	}
	return db.hasBuyerOffers(nationID)
}

func pageMajorActions(p *Page) {
	p.title = "Major Actions - "
	nation := p.needsNation()
	db, nid := p.db, p.NationID()
	buildings := db.One(`SELECT SUM(r.amount) AS buildings FROM resources r INNER JOIN resourcedefs rd ON r.resource_id = rd.resource_id
		WHERE r.nation_id = ? AND rd.is_building = 1 AND rd.resource_id != 43`, nid).I("buildings")
	newNamePrice := buildings * 250000
	p.csrf("majoractions")

	if !p.HasErrors() && p.isPost {
		majorAction(p, nation, newNamePrice)
	}

	gov := nation.S("government")
	v := V{"displaynewnameprice": commas(newNamePrice), "nationinfo": nation}
	p.Echo(contentHTML("majoractions_top"), v)
	if nation.B("seesecrets") && gov != "Alicorn Elite" && gov != "Transponyism" {
		p.Echo(contentHTML("majoractions_alicornify"), v)
	}
	block := map[string]string{
		"Alicorn Elite":    "majoractions_transponyism",
		"Transponyism":     "majoractions_ascend",
		"Independence":     "majoractions_independence_revert",
		"Decentralization": "majoractions_decentralization_revert",
		"Authoritarianism": "majoractions_authoritarianism_revert",
		"Oppression":       "majoractions_oppression_revert",
		"Democracy":        "majoractions_democracy",
		"Repression":       "majoractions_repression",
	}[gov]
	if block == "" {
		block = "majoractions_default"
	}
	p.Echo(contentHTML(block), v)
	if nation.I("se_relation") == 1000 && gov != "Solar Vassal" {
		p.Echo(contentHTML("majoractions_solarvassal"), v)
	}
	if nation.I("nlr_relation") == 1000 && gov != "Lunar Client" {
		p.Echo(contentHTML("majoractions_lunarclient"), v)
	}
	switch nation.S("economy") {
	case "Free Market":
		p.Echo(contentHTML("majoractions_freemarket_revert"), v)
	case "State Controlled":
		p.Echo(contentHTML("majoractions_statecontrolled_revert"), v)
	default:
		p.Echo(contentHTML("majoractions_poorlydefined"), v)
	}
}

func majorAction(p *Page, nation Row, newNamePrice int64) {
	db, nid, uid := p.db, p.NationID(), p.UserID()
	gov, funds := nation.S("government"), nation.I("funds")
	noMoney := func(need int64, msg string) {
		if funds < need {
			p.Err(msg)
		}
	}

	if p.PostB("topmessage") {
		noMoney(5000000, "You don't have the money!")
		if !p.HasErrors() {
			db.Exec("INSERT INTO topmessage (message, user_id, time) VALUES (?, ?, NOW())", p.Post("message"), uid)
			db.Exec("UPDATE nations SET funds = funds - 5000000 WHERE nation_id = ?", nid)
			p.Info("Message set.")
		}
	}

	if p.PostB("changename") {
		noMoney(newNamePrice, "You don't have the money!")
		if !alnumSpace(p.Post("nationname"), false) {
			p.Err("Only English letters and numbers for the nation name.")
		}
		newName := strings.TrimSpace(p.Post("nationname"))
		if newName == "" {
			p.Err("No nation name entered.")
		}
		if newName == nation.S("name") {
			p.Err("Name's the same. Lame.")
		}
		if db.One("SELECT COUNT(*) AS count FROM users WHERE username = ? AND user_id != ?", newName, uid).I("count") > 0 {
			p.Err("To avoid confusion, you can't name your nation after someone else's username.")
		}
		if db.One("SELECT COUNT(*) AS count FROM nations WHERE name = ? AND nation_id != ?", newName, nid).I("count") > 0 {
			p.Err("Name already taken.")
		}
		if !p.HasErrors() {
			db.report(nid, fmt.Sprintf("Nation name changed from %s to %s", nation.S("name"), newName))
			db.Exec("UPDATE nations SET funds = funds - ?, name = ?, satisfaction = satisfaction - 500 WHERE nation_id = ?", newNamePrice, newName, nid)
			p.Redirect("overview.php")
		}
	}

	const missing = "You don't have the %s!"
	switch {
	case p.PostB("alicornelite"):
		if !nation.B("seesecrets") {
			p.Err("No.")
			return
		}
		noMoney(3000000000, "You don't have the money!")
		needs := []upkeep{{resMachineryParts, "Machinery Parts", 1000}, {resTungsten, "Tungsten", 1000}, {resPrecisionParts, "Precision Parts", 1000}}
		p.requireResources(nid, needs, missing)
		if p.HasErrors() {
			return
		}
		db.Exec("UPDATE nations SET funds = funds - 3000000000 WHERE nation_id = ?", nid)
		db.Exec("UPDATE nations SET government = 'Alicorn Elite' WHERE user_id = ?", uid)
		db.Exec("UPDATE users SET empiremax = -10 WHERE user_id = ?", uid)
		db.Exec("UPDATE nations SET se_relation = -10 WHERE se_relation > -10 AND user_id = ?", uid)
		db.Exec("UPDATE nations SET nlr_relation = -10 WHERE nlr_relation > -10 AND user_id = ?", uid)
		p.spendResources(nid, needs)
		db.Exec("INSERT INTO resources (resource_id, nation_id, amount, disabled) VALUES (?, ?, 1, 0)", resAlicornFacility, nid)
		db.reportAllNations(uid, "Government changed to Alicorn Elite. This happened in all your nations.")
		p.Info("You have switched to Alicorn Elite. This happened in all your nations. Check this page to see what you will need for the next step of ascension.")
		nation["government"] = "Alicorn Elite"

	case p.PostB("transponyism"):
		if gov != "Alicorn Elite" {
			p.Err("No.")
			return
		}
		noMoney(5000000000, "You don't have the money!")
		needs := []upkeep{{resMachineryParts, "Machinery Parts", 2000}, {resTungsten, "Tungsten", 2000},
			{resPrecisionParts, "Precision Parts", 2000}, {resApotheosis, "Apotheosis Serum", 30}}
		p.requireResources(nid, needs, missing)
		if p.HasErrors() {
			return
		}
		db.Exec("UPDATE nations SET funds = funds - 5000000000 WHERE nation_id = ?", nid)
		db.Exec("UPDATE nations SET government = 'Transponyism' WHERE user_id = ?", uid)
		p.spendResources(nid, needs)
		db.Exec("UPDATE resources SET amount = amount + 2 WHERE nation_id = ? AND resource_id = ?", nid, resAlicornFacility)
		db.reportAllNations(uid, "Government changed to Transponyism. This happened in all your nations.")
		p.Info("You have switched to Transponyism. This happened in all your nations. Check this page to see what you will need for the final step of ascension.")
		nation["government"] = "Transponyism"

	case p.PostB("ascend"):
		if gov != "Transponyism" {
			p.Err("No.")
			return
		}
		noMoney(8000000000, "You don't have the money!")
		needs := []upkeep{{resMachineryParts, "Machinery Parts", 5000}, {resComposites, "Composites", 10000},
			{resPrecisionParts, "Precision Parts", 5000}, {resApotheosis, "Apotheosis Serum", 100}}
		p.requireResources(nid, needs, missing)
		if p.HasErrors() {
			return
		}
		p.spendResources(nid, needs)
		db.Exec("UPDATE nations SET funds = funds - 8000000000 WHERE nation_id = ?", nid)
		ascend(p)

	case p.PostB("loosedespotism"):
		switch gov {
		case "Democracy":
			if funds < 1000000 {
				p.Err("You don't have the money to revert!")
			} else {
				db.Exec("UPDATE nations SET satisfaction = satisfaction - 700, government = 'Loose Despotism', funds = funds - 1000000 WHERE nation_id = ?", nid)
			}
		case "Repression":
			if funds < 5000000 {
				p.Err("You don't have the money to revert!")
			} else {
				db.Exec("UPDATE nations SET government = 'Loose Despotism', funds = funds - 5000000 WHERE nation_id = ?", nid)
			}
		default:
			return
		}
		if !p.HasErrors() {
			db.Exec("UPDATE nations SET satisfaction = LEAST(satisfaction, 1000) WHERE nation_id = ?", nid)
			db.report(nid, "Government changed to Loose Despotism.")
			p.Redirect("overview.php")
		}

	case p.PostB("democracy"):
		switch gov {
		case "Loose Despotism", "Lunar Client", "Solar Vassal":
			p.requireResources(nid, []upkeep{{resCopper, "Copper", 300}, {resGasoline, "Gasoline", 60}, {resVehicleParts, "Vehicle Parts", 30}},
				"You don't have the %s to switch!")
			noMoney(2000000, "You don't have the money to switch!")
			if !p.HasErrors() {
				db.Exec("UPDATE nations SET government = 'Democracy', funds = funds - 2000000 WHERE nation_id = ?", nid)
				p.spendResources(nid, []upkeep{{resVehicleParts, "", 20}, {resGasoline, "", 60}, {resCopper, "", 300}})
				db.report(nid, "Government changed to Democracy.")
				p.Redirect("overview.php")
			}
		case "Independence", "Decentralization":
			noMoney(5000000, "You don't have the money to switch!")
			if !p.HasErrors() {
				loss, from := int64(2000), "Independence"
				if gov == "Decentralization" {
					loss, from = 1000, "Decentralized"
				}
				db.Exec("UPDATE nations SET funds = funds - 5000000 WHERE nation_id = ?", nid)
				db.Exec("UPDATE nations SET government = 'Democracy', satisfaction = LEAST(satisfaction - ?, 1500) WHERE user_id = ?", loss, uid)
				db.reportAllNations(uid, "Government reverted from "+from+" to Democracy. This happened in all your nations.")
				p.Redirect("overview.php")
			}
		}

	case p.PostB("repression"):
		switch gov {
		case "Loose Despotism", "Lunar Client", "Solar Vassal":
			p.requireResources(nid, []upkeep{{resCopper, "Copper", 200}, {resGasoline, "Gasoline", 20}, {resVehicleParts, "Vehicle Parts", 10}},
				"You don't have the %s to switch!")
			noMoney(1000000, "You don't have the money to switch!")
			if !p.HasErrors() {
				db.Exec("UPDATE nations SET government = 'Repression', funds = funds - 1000000 WHERE nation_id = ?", nid)
				p.spendResources(nid, []upkeep{{resVehicleParts, "", 10}, {resGasoline, "", 20}, {resCopper, "", 200}})
				db.report(nid, "Government changed to Repression.")
				p.Redirect("overview.php")
			}
		case "Authoritarianism", "Oppression":
			price := int64(10000000)
			if gov == "Oppression" {
				price = 20000000
			}
			noMoney(price, "You don't have the money to switch back!")
			if !p.HasErrors() {
				db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", price, nid)
				db.Exec("UPDATE nations SET government = 'Repression' WHERE user_id = ?", uid)
				db.reportAllNations(uid, "Government reverted from "+gov+" to Repression. This happened in all your nations.")
				p.Redirect("overview.php")
			}
		}

	case p.PostB("independence"):
		if gov != "Democracy" {
			return
		}
		noMoney(20000000, "You don't have the money to switch!")
		if db.One("SELECT COUNT(*) AS number FROM nations WHERE user_id = ?", uid).I("number") > 1 {
			p.Err("You cannot become independent; you have more than one nation.")
		}
		if !p.HasErrors() {
			db.Exec("UPDATE nations SET government = 'Independence', funds = funds - 20000000 WHERE nation_id = ?", nid)
			db.report(nid, "Government changed to Independence.")
			p.Redirect("overview.php")
		}

	case p.PostB("decentralization"):
		if gov != "Democracy" {
			return
		}
		noMoney(50000000, "You don't have the money to switch!")
		needs := []upkeep{{resCopper, "Copper", 500}, {resComposites, "Composites", 20}, {resMachineryParts, "Machinery Parts", 50}}
		p.requireResources(nid, needs, "You don't have the %s to switch!")
		if !p.HasErrors() {
			db.Exec("UPDATE nations SET funds = funds - 50000000 WHERE nation_id = ?", nid)
			db.Exec("UPDATE nations SET government = 'Decentralization' WHERE user_id = ?", uid)
			p.spendResources(nid, needs)
			db.reportAllNations(uid, "Government changed to Decentralization. This happened in all your nations.")
			p.Redirect("overview.php")
		}

	case p.PostB("authoritarianism"):
		if gov != "Repression" {
			return
		}
		noMoney(20000000, "You don't have the money to switch!")
		needs := []upkeep{{resMachineryParts, "Machinery Parts", 70}}
		p.requireResources(nid, needs, "You don't have the %s to switch!")
		if !p.HasErrors() {
			db.Exec("UPDATE nations SET funds = funds - 20000000 WHERE nation_id = ?", nid)
			db.Exec("UPDATE nations SET government = 'Authoritarianism' WHERE user_id = ?", uid)
			p.spendResources(nid, needs)
			db.reportAllNations(uid, "Government changed from Repression to Authoritarianism. This happened in all your nations.")
			p.Redirect("overview.php")
		}

	case p.PostB("oppression"):
		needs := []upkeep{{resMachineryParts, "Machinery Parts", 100}}
		p.requireResources(nid, needs, "You don't have the %s to switch!")
		noMoney(30000000, "You don't have the money to switch!")
		if gov != "Repression" {
			return
		}
		for _, n := range db.Query("SELECT nation_id, name FROM nations WHERE user_id = ?", uid) {
			if db.hasMarketListings(n.I("nation_id")) {
				p.Err("Remove everything from the marketplaces for %s before switching to Oppression.", n.S("name"))
			}
		}
		if !p.HasErrors() {
			db.Exec("UPDATE nations SET funds = funds - 30000000 WHERE nation_id = ?", nid)
			db.Exec("UPDATE nations SET government = 'Oppression' WHERE user_id = ?", uid)
			p.spendResources(nid, needs)
			db.reportAllNations(uid, "Government changed to Oppression. This happened in all your nations.")
			p.Redirect("overview.php")
		}

	case p.PostB("poorlydefined"):
		if nation.S("economy") == "Poorly Defined" {
			p.Err("Your economy is already Poorly Defined.")
		}
		noMoney(1000000, "You don't have the money to revert!")
		if db.hasBuyerOffers(nid) {
			p.Err("Remove everything from the buyer's marketplaces before altering your economy type.")
		}
		if !p.HasErrors() {
			db.Exec(`UPDATE nations SET satisfaction = satisfaction - 500, economy = 'Poorly Defined', active_economy = 1, funds = funds - 1000000
				WHERE nation_id = ?`, nid)
			db.report(nid, "Economy changed to Poorly Defined")
			p.Redirect("overview.php")
		}

	case p.PostB("solarvassal"), p.PostB("lunarclient"):
		becomePatronState(p, nation)

	case p.PostB("freemarket"), p.PostB("statecontrolled"):
		if db.hasBuyerOffers(nid) {
			p.Err("Remove everything from the buyer's marketplaces before altering your economy type.")
		}
		if nation.S("economy") != "Poorly Defined" {
			p.Err("You already made your choice.")
		}
		name, drink, short := "Free Market", int64(resCoffee), "We're not doing this without enough coffee, boss!"
		if p.PostB("statecontrolled") {
			name, drink, short = "State Controlled", resCider, "You will need more cider for us to do this, comrade!"
		}
		if db.resourceAmount(nid, resCopper) < 500 {
			p.Err("You don't have the copper to switch!")
		}
		noMoney(1000000, "You don't have the money to switch!")
		if db.resourceAmount(nid, drink) < 25 {
			p.Err(short)
		}
		if !p.HasErrors() {
			db.Exec("UPDATE nations SET active_economy = 1, economy = ?, funds = funds - 1000000 WHERE nation_id = ?", name, nid)
			p.spendResources(nid, []upkeep{{drink, "", 25}, {resCopper, "", 500}})
			db.report(nid, "Economy changed to "+name)
			p.Redirect("overview.php")
		}
	}
}

// becomePatronState turns a nation into a Solar Vassal or Lunar Client.
// Empire-wide governments convert every nation the player owns.
func becomePatronState(p *Page, nation Row) {
	db, nid, uid := p.db, p.NationID(), p.UserID()
	gov := nation.S("government")
	target, rel, article := "Solar Vassal", nation.I("se_relation"), "a Solar Vassal"
	if p.PostB("lunarclient") {
		target, rel, article = "Lunar Client", nation.I("nlr_relation"), "a Lunar Client"
	}
	empireWide := gov == "Oppression" || gov == "Decentralization" || gov == "Authoritarianism"
	contraband := func(id int64) bool {
		return db.One("SELECT amount FROM resources WHERE nation_id = ? AND (resource_id = ? OR resource_id = ?) AND amount > 0 LIMIT 1",
			id, resDrugFarm, resForbiddenLab) != nil
	}
	if empireWide {
		for _, n := range db.Query("SELECT nation_id, name FROM nations WHERE user_id = ?", uid) {
			if contraband(n.I("nation_id")) {
				p.Err("Destroy your drug farms and/or forbidden research facilities in %s before attempting to become %s.", n.S("name"), article)
			}
		}
	} else if contraband(nid) {
		p.Err("Destroy your drug farms and/or forbidden research facilities before attempting to become %s.", article)
	}
	if gov == target {
		p.Err("You already are %s.", article)
	}
	if rel < 1000 {
		p.Err("No.")
	}
	if p.HasErrors() {
		return
	}
	if empireWide {
		db.Exec("UPDATE nations SET government = ? WHERE user_id = ?", target, uid)
	} else {
		db.Exec("UPDATE nations SET government = ? WHERE nation_id = ?", target, nid)
	}
	db.Exec("UPDATE nations SET satisfaction = LEAST(satisfaction, 1250) WHERE nation_id = ?", nid)
	db.reportAllNations(uid, "Government changed to "+target+". This happened in all your nations.")
	p.Redirect("overview.php")
}

// ascend removes every nation the player owns, records the totals of
// everything they held, and announces it.
func ascend(p *Page) {
	db, uid := p.db, p.UserID()
	var funds int64
	totals := map[int64]int64{}
	nations := db.Query("SELECT nation_id, name, funds FROM nations WHERE user_id = ?", uid)
	for _, n := range nations {
		funds += n.I("funds")
		for _, r := range db.Query("SELECT resource_id, amount FROM resources WHERE nation_id = ?", n.I("nation_id")) {
			totals[r.I("resource_id")] += r.I("amount")
		}
		db.deleteNation(n.I("nation_id"))
	}
	for _, n := range nations {
		db.Exec("INSERT INTO ascendednations (user_id, name, date) VALUES (?, ?, NOW())", uid, n.S("name"))
	}
	db.Exec(`INSERT INTO ascendedresources (user_id, resource_id, amount) VALUES (?, 0, ?)
		ON DUPLICATE KEY UPDATE amount = amount + ?`, uid, funds, funds)
	for id, amount := range totals {
		db.Exec(`INSERT INTO ascendedresources (user_id, resource_id, amount) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE amount = amount + ?`, uid, id, amount, amount)
	}
	db.Exec("UPDATE users SET empiremax = NULL, seesecrets = 0 WHERE user_id = ?", uid)
	username := db.One("SELECT username FROM users WHERE user_id = ?", uid).S("username")
	db.news(username + " has ascended!")
	if p.app.compounds != nil {
		p.app.compounds.Exec("UPDATE users SET ascended = 1 WHERE username = ?", username)
	}
	p.Redirect(fmt.Sprintf("viewascension.php?user_id=%d", uid))
}
