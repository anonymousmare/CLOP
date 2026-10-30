package clop

import (
	"fmt"
	"strings"
)

// dealKind is one of the three kinds of goods a deal can carry.
type dealKind struct {
	table    string // deal<table>_offered / deal<table>_requested
	typ      string // value of the "type" form field
	singular string
	plural   string
	market   marketKind
}

var dealKinds = []dealKind{
	{"items", "item", "Item", "Items", marketKindFor("")},
	{"weapons", "weapon", "Weapon", "Weapons", marketKindFor("weapons")},
	{"armor", "armor", "Armor", "Armor", marketKindFor("armor")},
}

func dealKindFor(typ string) dealKind {
	for _, k := range dealKinds {
		if k.typ == typ {
			return k
		}
	}
	return dealKinds[0]
}

// dealGoods is everything offered and requested in a deal, per kind.
type dealGoods struct {
	offer, ask [3][]Row // rows have id, name, amount
}

func (g dealGoods) empty() bool {
	for i := range dealKinds {
		if len(g.offer[i]) > 0 || len(g.ask[i]) > 0 {
			return false
		}
	}
	return true
}

func (db *DB) dealGoods(dealID int64) dealGoods {
	var g dealGoods
	for i, k := range dealKinds {
		q := "SELECT rd.name, d.amount, d." + k.market.idcol + " AS id FROM deal" + k.table + "_%s d INNER JOIN " +
			k.market.defs + " rd ON rd." + k.market.idcol + " = d." + k.market.idcol + " WHERE d.deal_id = ?"
		g.offer[i] = db.Query(fmt.Sprintf(q, "offered"), dealID)
		g.ask[i] = db.Query(fmt.Sprintf(q, "requested"), dealID)
	}
	return g
}

// refundDeal hands everything an originator escrowed in a deal back to them
// and deletes the deal.
func (db *DB) refundDeal(deal Row, goods dealGoods) {
	from := deal.I("fromnation")
	for i, k := range dealKinds {
		for _, it := range goods.offer[i] {
			k.market.addStock(db, from, it.I("id"), it.I("amount"))
		}
	}
	refund := deal.I("paid")
	if !deal.B("askingformoney") {
		refund += deal.I("amount")
	}
	db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", refund, from)
	db.deleteDealItems(deal.I("deal_id"))
	db.Exec("DELETE FROM deals WHERE deal_id = ?", deal.I("deal_id"))
}

func pageDeals(p *Page) {
	p.title = "Deals - "
	nation := p.needsNation()
	db, nid := p.db, p.NationID()
	p.csrf("deals")

	if !p.HasErrors() {
		dealID := p.PostI("deal_id")
		switch {
		case p.PostB("makedeal"):
			dealsMake(p, nation)
		case p.PostB("acceptdeal"):
			deal := db.One("SELECT d.*, n.name FROM deals d INNER JOIN nations n ON d.fromnation = n.nation_id WHERE d.deal_id = ? AND d.tonation = ? AND d.finalized = '1'", dealID, nid)
			if deal == nil {
				p.Err("Deal not found!")
			} else {
				dealsAccept(p, nation, deal)
			}
		case p.PostB("rejectdeal"):
			deal := db.One("SELECT * FROM deals WHERE deal_id = ? AND tonation = ? AND finalized = '1'", dealID, nid)
			if deal == nil {
				p.Err("Deal not found!")
				break
			}
			db.report(deal.I("fromnation"), fmt.Sprintf("Your deal with %s was rejected.", nation.S("name")))
			db.refundDeal(deal, db.dealGoods(dealID))
			p.Info("Deal rejected.")
		case p.PostB("canceldeal"):
			deal := db.One("SELECT d.*, n.name FROM deals d INNER JOIN nations n ON d.tonation = n.nation_id WHERE d.deal_id = ? AND d.fromnation = ?", dealID, nid)
			if deal == nil {
				p.Err("Deal not found!")
				break
			}
			db.refundDeal(deal, db.dealGoods(dealID))
			p.Info("Your deal with %s was canceled.", deal.S("name"))
		}
	}

	if nation.S("economy") == "Free Market" {
		p.Raw(`<center>Your economy type is unable to make deals.</center>`)
		return
	}
	if nation.S("government") == "Oppression" {
		p.Raw(`<center>You cannot deal with nations outside of your empire.</center>`)
		return
	}
	var b strings.Builder
	if nation.S("economy") == "State Controlled" && nation.B("active_economy") {
		b.WriteString(tpl(`<center>Make a deal with the nation of
<form action="deals.php" method="post">
<input type="hidden" name="token_deals" value="{$token}"/>
<input type="text" class="form-control" name="nationname" style="width:200px;"/>
<input type="submit" name="makedeal" value="Make Deal" class="btn btn-success btn-primary"/>
</form>
`, V{"token": p.token("deals")}))
		outgoing := db.Query(`SELECT d.deal_id, d.amount, d.askingformoney, d.finalized, d.paid, n.name, n.nation_id
			FROM deals d INNER JOIN nations n ON d.tonation = n.nation_id WHERE d.fromnation = ?`, nid)
		if len(outgoing) == 0 {
			b.WriteString("<center>You have no outgoing deal requests.</center>\n")
		}
		for _, d := range outgoing {
			moneyline := "You do not involve money in this deal"
			if d.B("amount") {
				if d.B("askingformoney") {
					moneyline = fmt.Sprintf("You request %s bits for this deal", commas(d.S("amount")))
				} else {
					moneyline = fmt.Sprintf("You offer %s bits for this deal", commas(d.S("amount")))
				}
			}
			fmt.Fprintf(&b, `    <center><h4><a href="viewnation.php?nation_id=%s">%s</a></h4></center>
    <center>%s</center>
`, d.S("nation_id"), d.S("name"), moneyline)
			b.WriteString(dealGoodsTables(db.dealGoods(d.I("deal_id"))))
			fmt.Fprintf(&b, `	<form action="deals.php" method="post">
    <input type="hidden" name="token_deals" value="%s"/>
	<input type="hidden" name="deal_id" value="%s"/>
`, p.token("deals"), d.S("deal_id"))
			if !d.B("finalized") {
				fmt.Fprintf(&b, `	<center><a href="makedeal.php?deal_id=%s">Edit Deal</a></center>
`, d.S("deal_id"))
			} else {
				b.WriteString("	<center>This deal is finalized and sent; you cannot edit it.</center>\n")
			}
			b.WriteString(`	<input type="submit" name="canceldeal" value="Cancel Deal" class="btn btn-danger btn-primary"/>
	</form>
`)
		}
	}

	incoming := db.Query(`SELECT d.deal_id, d.amount, d.fromnation, d.askingformoney, d.paid, n.name
		FROM deals d INNER JOIN nations n ON d.fromnation = n.nation_id WHERE d.tonation = ? AND d.finalized = '1'`, nid)
	if len(incoming) == 0 {
		b.WriteString("<center>You have no incoming deal requests.</center>\n")
	}
	for _, d := range incoming {
		moneyline := "does not involve money in this deal"
		if d.B("amount") {
			if d.B("askingformoney") {
				moneyline = fmt.Sprintf("requests %s bits from you for this deal", commas(d.S("amount")))
			} else {
				moneyline = fmt.Sprintf("offers %s bits for this deal", commas(d.S("amount")))
			}
		}
		fmt.Fprintf(&b, `    <center><h4><a href="viewnation.php?nation_id=%s">%s</a></h4></center>
    <center>%s</center>
`, d.S("fromnation"), d.S("name"), moneyline)
		b.WriteString(dealGoodsTables(db.dealGoods(d.I("deal_id"))))
		fmt.Fprintf(&b, `	<form action="deals.php" method="post">
    <input type="hidden" name="token_deals" value="%s"/>
	<input type="hidden" name="deal_id" value="%s"/>
	<center><input type="submit" name="acceptdeal" value="Accept Deal" class="btn btn-success btn-primary"/>
	<input type="submit" name="rejectdeal" value="Reject Deal" class="btn btn-danger btn-primary"/></center>
	</form>
`, p.token("deals"), d.S("deal_id"))
	}
	p.Raw(b.String())
}

func dealGoodsTables(g dealGoods) string {
	var b strings.Builder
	section := func(title string, rows []Row) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(&b, `    <center><h4>%s</h4></center>
    <center><table class="table table-striped table-bordered" style="width:400px;">
`, title)
		for _, r := range rows {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td></tr>\n", r.S("name"), commas(r.S("amount")))
		}
		b.WriteString("</table></center>")
	}
	for i, k := range dealKinds {
		section("Offered "+k.plural, g.offer[i])
	}
	for i, k := range dealKinds {
		section("Requested "+k.plural, g.ask[i])
	}
	return b.String()
}

func dealsMake(p *Page, nation Row) {
	db := p.db
	if !(nation.S("economy") == "State Controlled" && nation.B("active_economy") && nation.S("government") != "Oppression") {
		p.Err("Your economy can't initiate deals right now.")
		return
	}
	other := db.One(`SELECT n.economy, n.nation_id, n.government, u.user_id, u.alliance_id, u.stasismode
		FROM nations n INNER JOIN users u ON n.user_id = u.user_id WHERE n.name = ?`, p.Post("nationname"))
	myAlliance := nation.I("alliance_id")
	switch {
	case other == nil:
		p.Err("Nation not found.")
	case other.B("stasismode"):
		p.Err("That nation's owner is in stasis.")
	case nation.S("government") == "Authoritarianism" && (other.I("alliance_id") != myAlliance || myAlliance == 0):
		p.Err("That nation's owner is not in your alliance, Authoritarian.")
	case other.S("government") == "Authoritarianism" && (other.I("alliance_id") != myAlliance || myAlliance == 0):
		p.Err("That Authoritarian nation is not in your alliance.")
	case other.I("user_id") == p.UserID():
		p.Err("Use Empire Transfers instead of deals to trade with yourself.")
	case other.S("economy") == "Free Market":
		p.Err("That Free Market nation is unable to accept deals!")
	case other.S("government") == "Oppression":
		p.Err("That Oppressive government is unable to accept deals!")
	default:
		id := db.Exec("INSERT INTO deals (fromnation, tonation, amount, askingformoney, finalized, paid) VALUES (?, ?, 0, 0, 0, 0)",
			p.NationID(), other.I("nation_id"))
		p.Redirect(fmt.Sprintf("makedeal.php?deal_id=%d", id))
	}
}

func dealsAccept(p *Page, nation Row, deal Row) {
	db, nid := p.db, p.NationID()
	dealID, from := deal.I("deal_id"), deal.I("fromnation")
	if nation.S("government") == "Oppression" {
		p.Err("Your Oppressive government cannot accept deals.")
	}
	other := db.One("SELECT u.alliance_id FROM users u INNER JOIN nations n ON u.user_id = n.user_id WHERE n.nation_id = ?", from)
	same := other.I("alliance_id") != 0 && other.I("alliance_id") == nation.I("alliance_id")
	if nation.S("government") == "Authoritarianism" && !same {
		p.Err("You cannot make deals with someone not in your alliance.")
	}
	if p.HasErrors() {
		return
	}
	if deal.B("askingformoney") && nation.I("funds") < deal.I("amount") {
		p.Err("You don't have enough money to do this deal!")
	}
	goods := db.dealGoods(dealID)
	for i, k := range dealKinds {
		for _, it := range goods.ask[i] {
			have := db.One("SELECT amount FROM "+k.market.stock+" WHERE nation_id = ? AND "+k.market.idcol+" = ?", nid, it.I("id")).I("amount")
			if have < it.I("amount") {
				p.Err("You don't have enough %s to do this deal!", it.S("name"))
			}
		}
	}
	if p.HasErrors() {
		return
	}

	theirName, myName := deal.S("name"), nation.S("name")
	if same {
		theirName = `<span class="text-success">` + theirName + `</span>`
		myName = `<span class="text-success">` + myName + `</span>`
	}
	infos := []string{fmt.Sprintf(`You accepted a deal with <a href="viewnation.php?nation_id=%d">%s</a>.`, from, theirName)}
	messages := []string{fmt.Sprintf(`Your deal with <a href="viewnation.php?nation_id=%d">%s</a> was accepted.`, nid, myName)}
	for i, k := range dealKinds {
		for _, it := range goods.ask[i] {
			db.Exec("UPDATE "+k.market.stock+" SET amount = amount - ? WHERE "+k.market.idcol+" = ? AND nation_id = ?", it.I("amount"), it.I("id"), nid)
			k.market.addStock(db, from, it.I("id"), it.I("amount"))
			infos = append(infos, fmt.Sprintf("You dealt away %s %s.", commas(it.S("amount")), it.S("name")))
			messages = append(messages, fmt.Sprintf("You received %s %s as part of your deal.", commas(it.S("amount")), it.S("name")))
		}
		db.Exec("DELETE FROM "+k.market.stock+" WHERE nation_id = ? AND amount = 0", nid)
		for _, it := range goods.offer[i] {
			k.market.addStock(db, nid, it.I("id"), it.I("amount"))
			infos = append(infos, fmt.Sprintf("You received %s %s.", commas(it.S("amount")), it.S("name")))
			messages = append(messages, fmt.Sprintf("You dealt away %s %s.", commas(it.S("amount")), it.S("name")))
		}
	}
	if amount := deal.I("amount"); amount != 0 {
		bits := commas(amount)
		if deal.B("askingformoney") {
			db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", amount, nid)
			db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", amount, from)
			infos = append(infos, "You dealt away "+bits+" bits.")
			messages = append(messages, "You received "+bits+" bits as part of your deal.")
		} else {
			// The originator paid into the deal when they made it.
			db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", amount, nid)
			infos = append(infos, "You received "+bits+" bits.")
			messages = append(messages, "You dealt away "+bits+" bits.")
		}
	}
	db.report(from, joinBR(messages))
	db.report(nid, joinBR(infos))
	db.deleteDealItems(dealID)
	db.Exec("DELETE FROM deals WHERE deal_id = ?", dealID)
	p.infos = append(p.infos, infos...)
}

func pageMakeDeal(p *Page) {
	p.title = "Make Deal - "
	nation := p.needsNation()
	db, nid := p.db, p.NationID()
	if nation.S("economy") != "State Controlled" || !nation.B("active_economy") {
		p.Redirect("overview.php")
	}
	dealID := toInt(p.GP("deal_id"))
	loadDeal := func() Row {
		return db.One(`SELECT d.*, n.name FROM deals d INNER JOIN nations n ON d.tonation = n.nation_id
			WHERE d.deal_id = ? AND d.fromnation = ? AND d.finalized = '0'`, dealID, nid)
	}
	deal := loadDeal()
	if dealID == 0 || deal == nil {
		p.Redirect("deals.php")
	}
	goods := db.dealGoods(dealID)
	p.csrf("makedeal")

	if !p.HasErrors() && p.isPost {
		makeDealAction(p, nation, deal, goods)
		if !p.HasErrors() {
			if deal = loadDeal(); deal == nil {
				p.Redirect("deals.php")
			}
			goods = db.dealGoods(dealID)
		}
	}
	renderMakeDeal(p, deal, goods)
}

func makeDealAction(p *Page, nation Row, deal Row, goods dealGoods) {
	db, nid := p.db, p.NationID()
	dealID := deal.I("deal_id")
	k := dealKindFor(p.Post("type"))
	kindIdx := 0
	for i := range dealKinds {
		if dealKinds[i].typ == k.typ {
			kindIdx = i
		}
	}
	itemID, amount := p.PostI("resource_id"), p.PostI("amount")
	funds := nation.I("funds")
	max := 10000 + funds/100000
	if k.typ != "item" {
		max = 5000 + funds/200000
	}
	lineAmount := func(rows []Row) int64 {
		for _, r := range rows {
			if r.I("id") == itemID {
				return r.I("amount")
			}
		}
		return 0
	}
	validItem := func() bool {
		return db.One("SELECT "+k.market.idcol+" FROM "+k.market.defs+" rd WHERE "+k.market.idcol+" = ?"+k.market.tradeable, itemID) != nil
	}

	switch {
	case p.PostB("offeritem"):
		if !validItem() {
			p.Err("That item can't be traded.")
		}
		current := db.One("SELECT SUM(dio.amount) AS totalamount FROM deal"+k.table+"_offered dio INNER JOIN deals d ON dio.deal_id = d.deal_id WHERE d.fromnation = ? AND dio."+k.market.idcol+" = ?",
			nid, itemID).I("totalamount")
		if amount+current > max {
			p.Err("You may only have %d of this kind of item in deals.", max)
		}
		have := db.One("SELECT amount FROM "+k.market.stock+" WHERE nation_id = ? AND "+k.market.idcol+" = ?", nid, itemID).I("amount")
		fee := amount * 100
		if funds < fee {
			p.Err("You don't have the money to include that many items!")
		}
		if have < amount {
			p.Err("You don't have enough to offer!")
		} else if amount < 1 {
			p.Err("No amount entered.")
		}
		if p.HasErrors() {
			return
		}
		k.market.takeStock(db, nid, itemID, amount, have)
		db.Exec("INSERT INTO deal"+k.table+"_offered (deal_id, "+k.market.idcol+", amount) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?",
			dealID, itemID, amount, amount)
		db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", fee, nid)
		db.Exec("UPDATE deals SET paid = paid + ? WHERE deal_id = ?", fee, dealID)
		p.Info("Item added to offerings.")

	case p.PostB("removeoffer"):
		n := lineAmount(goods.offer[kindIdx])
		if n == 0 {
			return
		}
		k.market.addStock(db, nid, itemID, n)
		db.Exec("DELETE FROM deal"+k.table+"_offered WHERE deal_id = ? AND "+k.market.idcol+" = ?", dealID, itemID)
		db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", n*100, nid)
		db.Exec("UPDATE deals SET paid = paid - ? WHERE deal_id = ?", n*100, dealID)
		p.Info("Item removed from offerings.")

	case p.PostB("askitem"):
		if !validItem() {
			p.Err("That item can't be traded.")
		}
		if amount == 0 {
			p.Err("No amount entered.")
		} else if amount < 1 {
			p.Err("Nope.")
		}
		fee := amount * 100
		if funds < fee {
			p.Err("You don't have the money to include that many items!")
		}
		if p.HasErrors() {
			return
		}
		db.Exec("INSERT INTO deal"+k.table+"_requested (deal_id, "+k.market.idcol+", amount) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?",
			dealID, itemID, amount, amount)
		db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", fee, nid)
		db.Exec("UPDATE deals SET paid = paid + ? WHERE deal_id = ?", fee, dealID)
		p.Info("Item added to requests.")

	case p.PostB("removeask"):
		n := lineAmount(goods.ask[kindIdx])
		if n == 0 {
			return
		}
		db.Exec("DELETE FROM deal"+k.table+"_requested WHERE deal_id = ? AND "+k.market.idcol+" = ?", dealID, itemID)
		db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", n*100, nid)
		db.Exec("UPDATE deals SET paid = paid - ? WHERE deal_id = ?", n*100, dealID)
		p.Info("Item removed from requests.")

	case p.PostB("offermoney"):
		if amount <= 0 {
			p.Err("No amount entered.")
		}
		bitsMax := 100000000 + funds/10
		if amount+db.One("SELECT SUM(amount) AS totalamount FROM deals WHERE fromnation = ?", nid).I("totalamount") > bitsMax {
			p.Err("You may only offer %s bits total in deals.", commas(bitsMax))
		}
		if funds < amount {
			p.Err("You do not have that much money.")
		}
		if deal.B("amount") {
			p.Err("There's already money in this deal!")
		}
		if !p.HasErrors() {
			db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", amount, nid)
			db.Exec("UPDATE deals SET amount = ?, askingformoney = 0 WHERE deal_id = ?", amount, dealID)
		}

	case p.PostB("requestmoney"):
		if amount <= 0 {
			p.Err("No amount entered.")
		}
		if deal.B("amount") {
			p.Err("There's already money in this deal!")
		}
		if !p.HasErrors() {
			db.Exec("UPDATE deals SET amount = ?, askingformoney = 1 WHERE deal_id = ?", amount, dealID)
		}

	case p.PostB("removemoney"):
		if !deal.B("askingformoney") {
			db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", deal.I("amount"), nid)
		}
		db.Exec("UPDATE deals SET amount = 0, askingformoney = 0 WHERE deal_id = ?", dealID)

	case p.PostB("canceldeal"):
		db.refundDeal(deal, goods)
		p.Redirect("deals.php")

	case p.PostB("finalizedeal"):
		if !deal.B("amount") && goods.empty() {
			p.Err("There's nothing to finalize!")
		} else {
			db.Exec("UPDATE deals SET finalized = 1 WHERE deal_id = ?", dealID)
		}
	}
}

func renderMakeDeal(p *Page, deal Row, goods dealGoods) {
	var b strings.Builder
	token := p.token("makedeal")
	dealID := deal.S("deal_id")
	hidden := fmt.Sprintf(`<input type="hidden" name="token_makedeal" value="%s"/>
                <input type="hidden" name="deal_id" value="%s"/>`, token, dealID)
	fmt.Fprintf(&b, `  <center>Making a deal with</center>
  <center><h4>%s</h4></center>
  <center><h5>Deal Cost: <span class="text-danger">%s</span></h5></center>
`, deal.S("name"), commas(deal.S("paid")))

	moneyRow := fmt.Sprintf(`              <tr>
                <td style="text-align: right;">Bits</td>
                <td>%s</td>
                <td>
              <form action="makedeal.php" method="post">
                %s
                <input type="submit" name="removemoney" value="Remove Money" class="btn btn-warning btn-block"/>
              </form>
              </td></tr>
`, commas(deal.S("amount")), hidden)
	side := func(heading, panel string, lines [3][]Row, removeName string, showMoney bool, verb, submitName, moneyName, moneyLabel string) {
		fmt.Fprintf(&b, `    <div class="col-md-6">
      <div class="panel panel-%s">
        <div class="panel-heading">%s</div>
`, panel, heading)
		has := showMoney
		for i := range dealKinds {
			has = has || len(lines[i]) > 0
		}
		if has {
			b.WriteString("          <table class=\"table\">\n            <tbody>\n")
			if showMoney {
				b.WriteString(moneyRow)
			}
			for i, k := range dealKinds {
				for _, it := range lines[i] {
					fmt.Fprintf(&b, `              <tr>
                <td style="text-align: right;">%s</td>
                <td>%s</td>
                <td>
                  <form action="makedeal.php" method="post">
                    %s
                    <input type="hidden" name="type" value="%s"/>
                    <input type="hidden" name="resource_id" value="%s"/>
                    <input type="submit" name="%s" value="Remove %s" class="btn btn-warning btn-block"/>
                  </form>
                </td>
              </tr>
`, it.S("name"), commas(it.S("amount")), hidden, k.typ, it.S("id"), removeName, k.singular)
				}
			}
			b.WriteString("</tbody></table>")
		}
		for _, k := range dealKinds {
			fmt.Fprintf(&b, `        <div class="panel-footer">
          <form action="makedeal.php" method="post" class="form-inline" role="form">
            %s
            <input type="hidden" name="type" value="%s"/>
            %s this %s:
            <select name="resource_id" class="form-control" style="width:210px;">
`, hidden, k.typ, verb, strings.ToLower(k.singular))
			for _, o := range k.market.itemOptions(p.db, p.NationID(), false) {
				fmt.Fprintf(&b, "              <option value=\"%s\">%s</option>\n", o.S("id"), o.S("label"))
			}
			fmt.Fprintf(&b, `            </select>
            in this quantity:
            <div class="input-group">
              <input type="text" class="form-control" name="amount" placeholder="Qty"/>
              <span class="input-group-btn">
                  <input type="submit" name="%s" value="%s %s" class="btn btn-primary"/>
               </span>
            </div>
          </form>
        </div>
`, submitName, verb, k.singular)
		}
		if !deal.B("amount") {
			fmt.Fprintf(&b, `          <form action="makedeal.php" method="post">
            %s
            <div class="input-group">
              <input type="text" class="form-control" name="amount"/>
              <span class="input-group-btn">
                  <input type="submit" name="%s" value="%s" class="btn btn-primary"/>
               </span>
            </div>
          </form>
`, hidden, moneyName, moneyLabel)
		}
		b.WriteString("        </div>\n      </div>\n")
	}
	b.WriteString("  <div class=\"row\">\n")
	offering := deal.B("amount") && !deal.B("askingformoney")
	asking := deal.B("amount") && deal.B("askingformoney")
	side("Offer", "warning", goods.offer, "removeoffer", offering, "Offer", "offeritem", "offermoney", "Offer This Much Money")
	side("Request", "success", goods.ask, "removeask", asking, "Request", "askitem", "requestmoney", "Request This Much Money")
	fmt.Fprintf(&b, `      </div>
  <div class="row">
    <form action="makedeal.php" method="post">
      %s
      <div class="col-md-6">
        <input type="submit" name="canceldeal" value="Cancel Deal" class="btn btn-danger btn-block"/>
      </div>
      <div class="col-md-6">
        <input type="submit" name="finalizedeal" value="Finalize Deal" class="btn btn-success btn-block"/>
      </div>
    </form>
  </div>
`, hidden)
	p.Raw(b.String())
}
