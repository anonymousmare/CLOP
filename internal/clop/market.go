package clop

import (
	"fmt"
	"strings"
)

// marketKind describes one of the three goods markets.
type marketKind struct {
	mode      string // "", "weapons" or "armor", as used in URLs
	stock     string // table holding what nations own
	defs      string // table of item definitions
	idcol     string // item id column
	tradeable string // extra SQL condition on rd for resources
	sellTable string
	buyTable  string
	plural    string // for messages
}

func marketKindFor(mode string) marketKind {
	switch mode {
	case "weapons":
		return marketKind{"weapons", "weapons", "weapondefs", "weapon_id", "", "weaponsmarketplace", "weaponsbuyermarketplace", "weapons"}
	case "armor":
		return marketKind{"armor", "armor", "armordefs", "armor_id", "", "armormarketplace", "armorbuyermarketplace", "armor"}
	}
	return marketKind{"", "resources", "resourcedefs", "resource_id", " AND rd.is_tradeable = 1", "marketplace", "buyermarketplace", "resources"}
}

// addStock gives a nation items of this kind.
func (k marketKind) addStock(db *DB, nationID, itemID, amount int64) {
	extra, extraVal := "", ""
	if k.stock == "resources" {
		extra, extraVal = ", disabled", ", 0"
	}
	db.Exec("INSERT INTO "+k.stock+" (nation_id, "+k.idcol+", amount"+extra+") VALUES (?, ?, ?"+extraVal+") ON DUPLICATE KEY UPDATE amount = amount + ?",
		nationID, itemID, amount, amount)
}

// takeStock removes items, deleting the row when it reaches zero.
func (k marketKind) takeStock(db *DB, nationID, itemID, amount, have int64) {
	if have == amount {
		db.Exec("DELETE FROM "+k.stock+" WHERE "+k.idcol+" = ? AND nation_id = ?", itemID, nationID)
	} else {
		db.Exec("UPDATE "+k.stock+" SET amount = amount - ? WHERE "+k.idcol+" = ? AND nation_id = ?", amount, itemID, nationID)
	}
}

// itemOptions lists every item of this kind with how many the nation has.
func (k marketKind) itemOptions(db *DB, nationID int64, ownedOnly bool) []Row {
	join, where := "LEFT JOIN", "WHERE 1 = 1"+k.tradeable
	if ownedOnly {
		join = "INNER JOIN"
	}
	q := "SELECT rd." + k.idcol + " AS id, rd.name, r.amount FROM " + k.defs + " rd " + join + " " + k.stock +
		" r ON r." + k.idcol + " = rd." + k.idcol + " AND r.nation_id = ? " + where
	var opts []Row
	if k.stock == "resources" {
		// DNA goes at the end of the list.
		opts = db.Query(q+" AND rd.name NOT LIKE 'DNA%' ORDER BY rd.name", nationID)
		opts = append(opts, db.Query(q+" AND rd.name LIKE 'DNA%' ORDER BY rd.name", nationID)...)
	} else {
		opts = db.Query(q+" ORDER BY rd.name", nationID)
	}
	for _, o := range opts {
		o["label"] = o.S("name")
		if o.B("amount") {
			o["label"] = fmt.Sprintf("%s (Have %s)", o.S("name"), o.S("amount"))
		}
	}
	return opts
}

// tradePartners holds who the player embargoes, befriends and hates.
type tradePartners struct {
	embargoed, friends, enemies map[int64]bool
}

func (db *DB) tradePartners(userID int64) tradePartners {
	t := tradePartners{map[int64]bool{}, map[int64]bool{}, map[int64]bool{}}
	for _, r := range db.Query("SELECT embargoee AS id FROM embargoes WHERE embargoer = ? UNION SELECT embargoer FROM embargoes WHERE embargoee = ?", userID, userID) {
		t.embargoed[r.I("id")] = true
	}
	for _, r := range db.Query("SELECT friendee AS id FROM friends WHERE friender = ?", userID) {
		t.friends[r.I("id")] = true
	}
	for _, r := range db.Query("SELECT enemiee AS id FROM enemies WHERE enemier = ?", userID) {
		t.enemies[r.I("id")] = true
	}
	return t
}

// colourName marks friends, enemies and alliance mates in listings.
func (t tradePartners) colourName(text string, userID, allianceID, myAlliance int64) string {
	switch {
	case t.friends[userID]:
		return `<span class="text-info">` + text + `</span>`
	case t.enemies[userID]:
		return `<span class="text-danger">` + text + `</span>`
	case allianceID != 0 && allianceID == myAlliance:
		return `<span class="text-success">` + text + `</span>`
	}
	return text
}

// tradeBlocked explains why two nations may not trade, or returns "".
func tradeBlocked(nation Row, other Row, partners tradePartners, sameAlliance bool, buying bool) string {
	verb, otherVerb := "buy from", "sell to"
	if !buying {
		verb, otherVerb = "sell to", "buy from"
	}
	switch {
	case partners.embargoed[other.I("user_id")]:
		return "There's an embargo prohibiting that!"
	case nation.S("government") == "Oppression":
		return "Your Oppressive government cannot buy nor sell."
	case nation.S("government") == "Authoritarianism" && !sameAlliance:
		return "Your Authoritarian government cannot " + verb + " someone not in your alliance!"
	case other.S("government") == "Authoritarianism" && !sameAlliance:
		return "That Authoritarian government will not " + otherVerb + " you, as you are not in its alliance!"
	case other.I("user_id") == nation.I("user_id"):
		return "You cannot " + verb + " another of your nations. Use Empire Transfers instead."
	}
	return ""
}

func marketTitle(mode, kind string) string {
	switch mode {
	case "weapons":
		return "Weapons " + kind + " - "
	case "armor":
		return "Armor " + kind + " - "
	}
	return kind + " - "
}

func pageMarketplace(p *Page) {
	nation := p.needsNation()
	k := marketKindFor(p.GP("mode"))
	p.title = marketTitle(k.mode, "Marketplace")
	db, nid := p.db, p.NationID()
	max := 50000 + nation.I("funds")/100000
	if k.mode != "" {
		max = 10000 + nation.I("funds")/200000
	}
	buyMult, sellMult := db.buyingMultiplier(nid), db.sellingMultiplier(nid)
	rawAmount, rawPrice := p.GP("amount"), p.GP("price")
	if truthy(rawAmount) && truthy(rawPrice) && (!isDigits(rawAmount) || !isDigits(rawPrice)) {
		p.Err("Digits only- no commas, periods, or other markers.")
	}
	amount, price, itemID := toInt(rawAmount), toInt(rawPrice), toInt(p.GP("resource_id"))
	p.csrf(k.sellTable)

	var listings []Row
	var partners tradePartners
	if !p.HasErrors() {
		partners = db.tradePartners(p.UserID())
		switch p.Post("action") {
		case "Place on Market":
			if nation.S("government") == "Oppression" {
				p.Err("Your Oppressive government cannot buy nor sell.")
			}
			current := db.One("SELECT SUM(amount) AS totalamount FROM "+k.sellTable+" WHERE nation_id = ? AND "+k.idcol+" = ?", nid, itemID)
			if amount+current.I("totalamount") > max {
				p.Err("You may only have %d of this kind of item on the market.", max)
			}
			have := db.One("SELECT r.amount, rd.name FROM "+k.stock+" r INNER JOIN "+k.defs+" rd ON r."+k.idcol+" = rd."+k.idcol+
				" WHERE r.nation_id = ? AND r."+k.idcol+" = ?"+k.tradeable, nid, itemID)
			if price < 1000 {
				p.Err("Price must be at least 1000.")
			}
			if amount < 1 {
				p.Err("Amount must be above zero.")
			}
			if have.I("amount") < amount {
				p.Err("You do not have that much %s to sell.", have.S("name"))
			}
			if !p.HasErrors() {
				k.takeStock(db, nid, itemID, amount, have.I("amount"))
				db.Exec("INSERT INTO "+k.sellTable+" (nation_id, "+k.idcol+", amount, price) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?",
					nid, itemID, amount, price, amount)
				p.Info("You have put %d %s on the market.", amount, have.S("name"))
			}
		case "Remove from Marketplace":
			if p.PostI("buyingfrom_id") != nid {
				p.Err("C'mon, you really didn't think I'd check for THIS?")
				break
			}
			l := db.One("SELECT m.amount, rd.name FROM "+k.sellTable+" m INNER JOIN "+k.defs+" rd ON m."+k.idcol+" = rd."+k.idcol+
				" WHERE m.nation_id = ? AND m."+k.idcol+" = ? AND m.price = ?", nid, itemID, price)
			if l == nil {
				p.Err("Too late, somepony bought it all.")
				break
			}
			db.Exec("DELETE FROM "+k.sellTable+" WHERE nation_id = ? AND "+k.idcol+" = ? AND price = ?", nid, itemID, price)
			k.addStock(db, nid, itemID, l.I("amount"))
			p.Info("You have removed %d %s from the market.", l.I("amount"), l.S("name"))
		case "Buy One", "Buy All", "Buy:":
			marketBuy(p, nation, k, partners, itemID, price, buyMult)
		}
		if p.isPost {
			for _, l := range db.Query("SELECT m.*, n.nation_id, n.name, n.region, u.user_id, u.alliance_id FROM "+k.sellTable+
				" m INNER JOIN nations n ON n.nation_id = m.nation_id INNER JOIN users u ON u.user_id = n.user_id WHERE m."+k.idcol+
				" = ? ORDER BY m.price ASC, n.nation_id DESC", itemID) {
				if !partners.embargoed[l.I("user_id")] {
					l["resource_id"] = l.S(k.idcol)
					listings = append(listings, l)
				}
			}
		}
	}
	nation = db.One(nationInfoSQL, p.UserID(), nid)
	renderMarket(p, nation, k, false, buyMult, sellMult, itemID, listings, partners)
}

func marketBuy(p *Page, nation Row, k marketKind, partners tradePartners, itemID, price int64, buyMult float64) {
	db, nid := p.db, p.NationID()
	sellerID := p.PostI("buyingfrom_id")
	seller := db.One("SELECT u.user_id, u.alliance_id, n.government FROM nations n INNER JOIN users u ON u.user_id = n.user_id WHERE n.nation_id = ?", sellerID)
	same := seller.I("alliance_id") != 0 && seller.I("alliance_id") == nation.I("alliance_id")
	if msg := tradeBlocked(nation, seller, partners, same, true); msg != "" {
		p.Err("%s", msg)
		return
	}
	l := db.One("SELECT m.amount, rd.name, n.name AS nationname FROM "+k.sellTable+" m INNER JOIN "+k.defs+" rd ON m."+k.idcol+" = rd."+k.idcol+
		" INNER JOIN nations n ON n.nation_id = m.nation_id WHERE m.nation_id = ? AND m."+k.idcol+" = ? AND m.price = ?", sellerID, itemID, price)
	if !l.B("amount") {
		p.Err("Somepony else bought the last one!")
		return
	}
	var n int64
	switch p.Post("action") {
	case "Buy One":
		n = 1
	case "Buy All":
		n = l.I("amount")
	default:
		n = p.PostI("buyingamount")
		if n < 1 {
			p.Err("Whole numbers greater than 0.")
		} else if n > l.I("amount") {
			p.Err("That vendor doesn't have that many for sale!")
		}
	}
	if p.HasErrors() {
		return
	}
	cost := floor(float64(price*n) * buyMult)
	if nation.I("funds") < cost {
		p.Err("You can't afford it!")
		return
	}
	if n < l.I("amount") {
		db.Exec("UPDATE "+k.sellTable+" SET amount = amount - ? WHERE nation_id = ? AND "+k.idcol+" = ? AND price = ?", n, sellerID, itemID, price)
	} else {
		db.Exec("DELETE FROM "+k.sellTable+" WHERE nation_id = ? AND "+k.idcol+" = ? AND price = ?", sellerID, itemID, price)
	}
	k.addStock(db, nid, itemID, n)
	earned := floor(float64(price*n) * db.sellingMultiplier(sellerID))
	db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", cost, nid)
	db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", earned, sellerID)

	sellerName, buyerName := l.S("nationname"), nation.S("name")
	if same {
		sellerName = `<span class="text-success">` + sellerName + `</span>`
		buyerName = `<span class="text-success">` + buyerName + `</span>`
	}
	p.Info(`You bought %d %s from <a href="viewnation.php?nation_id=%d">%s</a> for %s bits.`, n, l.S("name"), sellerID, l.S("nationname"), commas(cost))
	db.report(nid, fmt.Sprintf(`You bought %d %s from <a href="viewnation.php?nation_id=%d">%s</a> for %s bits.`, n, l.S("name"), sellerID, sellerName, commas(cost)))
	db.report(sellerID, fmt.Sprintf(`You sold %d %s to <a href="viewnation.php?nation_id=%d">%s</a> and made %s bits.`, n, l.S("name"), nid, buyerName, commas(earned)))
}

func pageBuyerMarketplace(p *Page) {
	nation := p.needsNation()
	k := marketKindFor(p.GP("mode"))
	p.title = marketTitle(k.mode, "Buyer's Marketplace")
	db, nid := p.db, p.NationID()
	max := 200000000 + nation.I("funds")/10
	if k.mode != "" {
		max = 100000000 + nation.I("funds")/10
	}
	buyMult, sellMult := db.buyingMultiplier(nid), db.sellingMultiplier(nid)
	rawAmount, rawPrice := p.GP("amount"), p.GP("price")
	if truthy(rawAmount) && truthy(rawPrice) && (!isDigits(rawAmount) || !isDigits(rawPrice)) {
		p.Err("Digits only- no commas, periods, or other markers.")
	}
	amount, price, itemID := toInt(rawAmount), toInt(rawPrice), toInt(p.GP("resource_id"))
	p.csrf(k.buyTable)

	var listings []Row
	var partners tradePartners
	if !p.HasErrors() {
		partners = db.tradePartners(p.UserID())
		switch {
		case p.PostB("offer"):
			if nation.S("government") == "Oppression" {
				p.Err("Your Oppressive government cannot buy nor sell.")
			}
			if price < 1000 {
				p.Err("Price must be at least 1000.")
			}
			if amount < 1 {
				p.Err("Amount must be above zero.")
			}
			current := db.One("SELECT SUM(price * amount) AS totalamount FROM "+k.buyTable+" WHERE nation_id = ?", nid).I("totalamount")
			if price*amount+current > max {
				remaining := "No more"
				if max > current {
					remaining = commas(max - current)
				}
				p.Err("You may only offer %s bits total for all %s on the buyer's marketplace. (%s available)", commas(max), k.plural, remaining)
			}
			cost := floor(float64(price*amount) * buyMult)
			if nation.I("funds") < cost {
				p.Err("You cannot afford to make that offer.")
			}
			item := db.One("SELECT name FROM "+k.defs+" rd WHERE "+k.idcol+" = ?"+k.tradeable, itemID)
			if !item.B("name") {
				p.Err("No item selected.")
			}
			if !p.HasErrors() {
				db.Exec("INSERT INTO "+k.buyTable+" (nation_id, "+k.idcol+", amount, price) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?",
					nid, itemID, amount, price, amount)
				db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", cost, nid)
				p.Info("You have requested to buy %d %s for %s bits.", amount, item.S("name"), commas(cost))
			}
		case p.PostB("remove"):
			if p.PostI("sellingto_id") != nid {
				p.Err("C'mon, you really didn't think I'd check for THIS?")
				break
			}
			o := db.One("SELECT m.price, m.amount, rd.name FROM "+k.buyTable+" m INNER JOIN "+k.defs+" rd ON m."+k.idcol+" = rd."+k.idcol+
				" WHERE m.nation_id = ? AND m."+k.idcol+" = ? AND m.price = ?", nid, itemID, price)
			if o == nil {
				p.Err("Too late, somepony sold it to you already.")
				break
			}
			db.Exec("DELETE FROM "+k.buyTable+" WHERE nation_id = ? AND "+k.idcol+" = ? AND price = ?", nid, itemID, price)
			refund := floor(buyMult * float64(o.I("price")*o.I("amount")))
			db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", refund, nid)
			p.Info("You have removed your request of %d %s from the market.", o.I("amount"), o.S("name"))
		case p.PostB("sellone"), p.PostB("sellall"), p.PostB("sellamount"):
			marketSell(p, nation, k, partners, itemID, price)
		}
		if p.isPost {
			for _, l := range db.Query("SELECT m.*, n.nation_id, n.name, n.region, u.user_id, u.alliance_id FROM "+k.buyTable+
				" m INNER JOIN nations n ON n.nation_id = m.nation_id INNER JOIN users u ON u.user_id = n.user_id WHERE m."+k.idcol+
				" = ? ORDER BY m.price DESC, n.nation_id DESC", itemID) {
				if !partners.embargoed[l.I("user_id")] {
					l["resource_id"] = l.S(k.idcol)
					listings = append(listings, l)
				}
			}
		}
	}
	nation = db.One(nationInfoSQL, p.UserID(), nid)
	renderMarket(p, nation, k, true, buyMult, sellMult, itemID, listings, partners)
}

func marketSell(p *Page, nation Row, k marketKind, partners tradePartners, itemID, price int64) {
	db, nid := p.db, p.NationID()
	buyerID := p.PostI("sellingto_id")
	buyer := db.One("SELECT u.user_id, u.alliance_id, n.government FROM nations n INNER JOIN users u ON u.user_id = n.user_id WHERE n.nation_id = ?", buyerID)
	same := buyer.I("alliance_id") != 0 && buyer.I("alliance_id") == nation.I("alliance_id")
	if msg := tradeBlocked(nation, buyer, partners, same, false); msg != "" {
		p.Err("%s", msg)
		return
	}
	o := db.One("SELECT m.amount, rd.name, n.name AS nationname FROM "+k.buyTable+" m INNER JOIN "+k.defs+" rd ON m."+k.idcol+" = rd."+k.idcol+
		" INNER JOIN nations n ON n.nation_id = m.nation_id WHERE m.nation_id = ? AND m."+k.idcol+" = ? AND m.price = ?", buyerID, itemID, price)
	if !o.B("amount") {
		p.Err("Somepony else fulfilled this order!")
		return
	}
	var n int64
	switch {
	case p.PostB("sellone"):
		n = 1
	case p.PostB("sellall"):
		n = o.I("amount")
	default:
		n = p.PostI("sellingamount")
		if n < 1 {
			p.Err("Whole numbers greater than 0.")
		} else if n > o.I("amount") {
			p.Err("That buyer doesn't want that many!")
		}
	}
	if p.HasErrors() {
		return
	}
	have := db.One("SELECT r.amount FROM "+k.stock+" r WHERE r.nation_id = ? AND r."+k.idcol+" = ?", nid, itemID).I("amount")
	if have < n {
		p.Err("You don't have that many to sell!")
		return
	}
	if n < o.I("amount") {
		db.Exec("UPDATE "+k.buyTable+" SET amount = amount - ? WHERE nation_id = ? AND "+k.idcol+" = ? AND price = ?", n, buyerID, itemID, price)
	} else {
		db.Exec("DELETE FROM "+k.buyTable+" WHERE nation_id = ? AND "+k.idcol+" = ? AND price = ?", buyerID, itemID, price)
	}
	k.addStock(db, buyerID, itemID, n)
	k.takeStock(db, nid, itemID, n, have)
	earned := floor(float64(price*n) * db.sellingMultiplier(nid))
	cost := floor(float64(price*n) * db.buyingMultiplier(buyerID))
	db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", earned, nid)

	sellerName, buyerName := nation.S("name"), o.S("nationname")
	if same {
		sellerName = `<span class="text-success">` + sellerName + `</span>`
		buyerName = `<span class="text-success">` + buyerName + `</span>`
	}
	db.report(buyerID, fmt.Sprintf(`You bought %d %s from <a href="viewnation.php?nation_id=%d">%s</a> for %s bits.`, n, o.S("name"), nid, sellerName, commas(cost)))
	p.Info(`You sold %d %s to <a href="viewnation.php?nation_id=%d">%s</a> and made %s bits.`, n, o.S("name"), buyerID, o.S("nationname"), commas(earned))
	db.report(nid, fmt.Sprintf(`You sold %d %s to <a href="viewnation.php?nation_id=%d">%s</a> and made %s bits.`, n, o.S("name"), buyerID, buyerName, commas(earned)))
}

// renderMarket draws either marketplace: the listing form and the offers
// for the selected item.
func renderMarket(p *Page, nation Row, k marketKind, buyer bool, buyMult, sellMult float64, itemID int64, listings []Row, partners tradePartners) {
	page, table := "marketplace.php", k.sellTable
	if buyer {
		page, table = "buyermarketplace.php", k.buyTable
	}
	token := p.token(table)
	hidden := func(l Row, idName string) string {
		return tpl(`<input type="hidden" name="token_{$table}" value="{$token}"/>
  <input type="hidden" name="mode" value="{$mode}"/>
  <input type="hidden" name="resource_id" value="{$resource_id}"/>
  <input type="hidden" name="{$idname}" value="{$nation_id}"/>
  <input type="hidden" name="price" value="{$price}"/>
`, V{"table": table, "token": token, "mode": k.mode, "resource_id": l.S("resource_id"), "idname": idName,
			"nation_id": l.S("nation_id"), "price": l.S("price")})
	}

	var b strings.Builder
	if buyer {
		fmt.Fprintf(&b, `<div class="alert alert-info">Due to your economic type, you will pay %s%% extra when offering to buy these items
and receive %s%% less than their listed price when selling.</div>
`, phpFloat((buyMult-1)*100), phpFloat((1-sellMult)*100))
	} else {
		fmt.Fprintf(&b, `<div class="alert alert-info">Due to your economic type, you will pay %s%% more than these listed prices
and receive %s%% less than your listed price when selling.</div>
`, phpFloat((buyMult-1)*100), phpFloat((1-sellMult)*100))
	}
	fmt.Fprintf(&b, `<center><div class="well">Funds: <span class="text-success">%s Bits</span></div></center>
<center>
  <form action="%s" method="post" class="form-inline" role="form">
    <input type="hidden" name="token_%s" value="%s"/>
    <input type="hidden" name="mode" value="%s"/>
    <p>
      <select name="resource_id" class="form-control" style="width:210px;" onchange="this.form.submit()">
      <option value=""></option>
`, commas(nation.S("funds")), page, table, token, k.mode)
	for _, o := range k.itemOptions(p.db, p.NationID(), false) {
		sel := ""
		if o.I("id") == itemID {
			sel = " selected "
		}
		fmt.Fprintf(&b, "        <option value=\"%s\"%s>%s</option>\n", o.S("id"), sel, o.S("label"))
	}
	if buyer {
		b.WriteString(`      </select>
    </p>
    <p>
      Offer to buy
      <input name="amount" class="form-control" placeholder="Qty" style="width:100px;"/>
      of the selected item for
      <input name="price" class="form-control" placeholder="Bits" style="width:100px;"/>
      <input type="submit" name="offer" value="Offer to Buy" class="btn btn-success"/>
    </p>
  </form>
</center>
`)
	} else {
		b.WriteString(`      </select>
    </p>
    <p>
      Place
      <input name="amount" class="form-control" placeholder="Qty" style="width:100px;"/>
      of the selected item on the market for
      <input name="price" class="form-control" placeholder="Bits" style="width:100px;"/>
      <input type="submit" name="action" value="Place on Market" class="btn btn-success"/>
    </p>
  </form>
</center>
`)
	}

	if len(listings) == 0 {
		if p.PostB("resource_id") && !p.HasErrors() {
			if buyer {
				b.WriteString(`<div class="alert alert-warning">Nobody wants to buy that item.</div>`)
			} else {
				b.WriteString(`<div class="alert alert-warning">That item is not on the market.</div>`)
			}
		}
		p.Raw(b.String())
		return
	}
	cols := [3]string{"Unit Price", "Units Available", "Seller"}
	if buyer {
		cols = [3]string{"Offering", "Amount", "Buyer"}
	}
	fmt.Fprintf(&b, `<center>
<table class="table table-striped table-bordered">
<thead><tr><td><div class="row">
  <div class="col-md-1">%s</div>
  <div class="col-md-1">%s</div>
  <div class="col-md-5">%s</div>
  <div class="col-md-5">Actions</div>
</div></td></tr></thead><tbody>
`, cols[0], cols[1], cols[2])
	for _, l := range listings {
		name := partners.colourName(l.S("name"), l.I("user_id"), l.I("alliance_id"), nation.I("alliance_id"))
		region := partners.colourName(regionNames[l.I("region")], l.I("user_id"), l.I("alliance_id"), nation.I("alliance_id"))
		fmt.Fprintf(&b, `<tr><td><div class="row">
  <div class="col-md-1"><p class="text-danger">%s</p></div>
  <div class="col-md-1"><p class="text-success">%s</p></div>
  <div class="col-md-5"><p><a href="viewnation.php?nation_id=%s">%s (<img src="images/icons/%s.png"/>%s)</a></p></div>
  <div class="col-md-5">
`, commas(l.S("price")), l.S("amount"), l.S("nation_id"), name, regionIcons[l.I("region")], region)
		idName := "buyingfrom_id"
		if buyer {
			idName = "sellingto_id"
		}
		switch {
		case l.I("nation_id") == p.NationID():
			removeName := `name="action" value="Remove from Marketplace"`
			if buyer {
				removeName = `name="remove" value="Remove from Marketplace"`
			}
			fmt.Fprintf(&b, `  <div class="row">
    <div class="col-xs-6">
      <form action="%s" method="post">
  %s      <input type="submit" %s class="btn btn-danger btn-sm btn-block"/>
  </form>
    </div>
    <div class="col-xs-6">

    </div>
  </div>
`, page, hidden(l, idName), removeName)
		case buyer:
			fmt.Fprintf(&b, `  <div class="row">
    <div class="col-xs-6">
<form action="%s" method="post">
%s<div class="btn-group btn-block">
<input type="submit" name="sellone" value="Sell One" class="btn btn-primary" style="width:50%%"/>
<input type="submit" name="sellall" value="Sell All" class="btn btn-warning" style="width:50%%"/>
</div>
</form>
</div>
<div class="col-xs-6">
<form action="%s" method="post">
%s<div class="input-group">
<span class="input-group-btn">
<input type="submit" name="sellamount" value="Sell:" class="btn btn-success"/>
</span>
<input name="sellingamount" value="1" class="form-control" type="text"/>
</div>
</form>
</div>
</div>
`, page, hidden(l, idName), page, hidden(l, idName))
		default:
			fmt.Fprintf(&b, `  <div class="row">
    <div class="col-xs-6">
  <form action="%s" method="post">
  %s      <div class="btn-group btn-block">
        <input type="submit" name="action" value="Buy One" class="btn btn-primary" style="width:50%%"/>
        <input type="submit" name="action" value="Buy All" class="btn btn-warning" style="width:50%%"/>
      </div>
      </form>
    </div>
    <div class="col-xs-6">
   <form action="%s" method="post">
  %s      <div class="input-group">
          <span class="input-group-btn">
  <input type="submit" name="action" value="Buy:" class="btn btn-success"/>
           </span>
  <input name="buyingamount" value="1" class="form-control" type="text"/>
      </div>
  </form>
    </div>
  </div>

`, page, hidden(l, idName), page, hidden(l, idName))
		}
		b.WriteString("</div>\n</div></td></tr>\n")
	}
	b.WriteString("</tbody></table>\n")
	p.Raw(b.String())
}
