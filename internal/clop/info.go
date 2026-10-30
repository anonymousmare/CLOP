package clop

import (
	"fmt"
	"strings"
)

// pager prints page links 1..n with the current page unlinked.
func pager(url string, current, total int64) string {
	pages := (total + 19) / 20
	var b strings.Builder
	for i := int64(1); i <= pages; i++ {
		if i == current {
			fmt.Fprintf(&b, "%d ", i)
		} else {
			fmt.Fprintf(&b, "<a href=\"%s%d\">%d</a> \n", url, i, i)
		}
	}
	return b.String()
}

func (p *Page) pageNumber() int64 {
	n := toInt(p.Get("page"))
	if n < 1 {
		n = 1
	}
	return n
}

func (p *Page) flagCell(flag string) string {
	if flag == "" || truthy(p.sess.Get("hideflags")) {
		return ""
	}
	return fmt.Sprintf(`<img src="%s" height="20" width="20">`, esc(flag))
}

var rankingRegions = map[string]int64{"saddle": 1, "zebrica": 2, "burrozil": 3, "przewalskia": 4}

func pageRankings(p *Page) {
	p.title = "Rankings - "
	db := p.db
	page := p.pageNumber()
	offset := (page - 1) * 20
	mode := p.Get("mode")
	base := "FROM nations n INNER JOIN users u ON u.user_id = n.user_id WHERE u.stasismode = 0 AND u.user_id != ?"
	args := []any{p.app.cfg.AdminUserID}
	var order, topline, second string
	switch mode {
	case "statues":
		base = "FROM nations n INNER JOIN users u ON u.user_id = n.user_id INNER JOIN resources r ON r.nation_id = n.nation_id WHERE r.resource_id = 38 AND u.stasismode = 0 AND u.user_id != ?"
		order = "ORDER BY r.amount DESC, n.nation_id ASC"
		topline, second = "Here, you can see who has the biggest ego and the most money to spend.", "Statues"
	case "longevity":
		order = "ORDER BY n.age DESC, n.creationdate ASC, n.nation_id ASC"
		topline, second = "A nation's age has nothing to do with how long its owner has been playing &gt;CLOP.", "Age</th><th>Creation Date"
	case "allianceless":
		base += " AND u.alliance_id = 0"
		order = "ORDER BY n.gdp_last_turn DESC, n.nation_id ASC"
		topline, second = "These are the nations without an alliance.", "User"
	default:
		if region, ok := rankingRegions[mode]; ok {
			base += " AND n.region = ?"
			args = append(args, region)
			topline, second = "These are the "+mode+" nations.", "User</th><th>Subregion"
		} else {
			mode = "gdp"
			topline, second = "These rankings are only for GDP made from factories and satisfaction! There are plenty of other ways to make money in &gt;CLOP. Take these rankings with a grain of salt.", "GDP Last Turn"
		}
		order = "ORDER BY n.gdp_last_turn DESC, n.nation_id ASC"
	}
	count := db.One("SELECT COUNT(*) AS count "+base, args...).I("count")
	cols := "u.flag, u.username, u.user_id, n.name, n.nation_id, n.region, n.subregion, n.government, n.economy, n.gdp_last_turn, n.age, n.creationdate"
	if mode == "statues" {
		cols += ", r.amount"
	}
	nations := db.Query("SELECT "+cols+" "+base+" "+order+" LIMIT ?, 20", append(args, offset)...)

	var b strings.Builder
	fmt.Fprintf(&b, `<center>%s</center>
<table class="table table-striped table-bordered">
<tr><th style="width:25px"></th><th>Nation</th><th>%s</th><th>Government</th><th>Economy</th></tr>
`, topline, second)
	for _, n := range nations {
		var cell string
		switch mode {
		case "statues":
			cell = str(n.I("amount"))
		case "longevity":
			cell = n.S("age") + "</td><td>" + n.S("creationdate")
		case "gdp":
			cell = commas(n.S("gdp_last_turn"))
		case "allianceless":
			cell = fmt.Sprintf(`<a href="viewuser.php?user_id=%s">%s</a>`, n.S("user_id"), n.S("username"))
		default:
			cell = fmt.Sprintf(`<a href="viewuser.php?user_id=%s">%s</a></td><td>%s`, n.S("user_id"), n.S("username"), subregionNames[n.I("subregion")])
		}
		fmt.Fprintf(&b, `<tr><td style="width:25px">%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>
`, p.flagCell(n.S("flag")), nationLinks([]Row{n}), cell, n.S("government"), n.S("economy"))
	}
	b.WriteString("</table>")
	b.WriteString(pager("rankings.php?mode="+mode+"&page=", page, count))
	p.Raw(b.String())
}

func pageEmpireRankings(p *Page) {
	p.title = "Empire Rankings - "
	db := p.db
	page := p.pageNumber()
	count := db.One("SELECT COUNT(DISTINCT u.user_id) AS count FROM users u INNER JOIN nations n ON u.user_id = n.user_id WHERE u.stasismode = 0").I("count")
	var b strings.Builder
	b.WriteString(`<center>Empires are ranked by number of nations first and total GDP second.</center>
<table class="table table-striped table-bordered">
<tr><th style="width:25px"></th><th>User</th><th>Nations</th><th>Total GDP</th></tr>
`)
	for _, u := range db.Query(`SELECT u.flag, u.user_id, u.username, COUNT(n.nation_id) AS nationcount, SUM(n.gdp_last_turn) AS totalgdp
		FROM users u INNER JOIN nations n ON u.user_id = n.user_id WHERE u.stasismode = 0
		GROUP BY u.user_id ORDER BY nationcount DESC, totalgdp DESC LIMIT ?, 20`, (page-1)*20) {
		fmt.Fprintf(&b, `<tr><td style="width:25px">%s</td><td><a href="viewuser.php?user_id=%s">%s</a></td><td>%s</td><td>%s</td></tr>
`, p.flagCell(u.S("flag")), u.S("user_id"), u.S("username"), u.S("nationcount"), commas(u.S("totalgdp")))
	}
	b.WriteString("</table>")
	b.WriteString(pager("empirerankings.php?page=", page, count))
	p.Raw(b.String())
}

func pageStatistics(p *Page) {
	p.title = "Statistics - "
	db := p.db
	v := db.census()
	for _, w := range []struct {
		key  string
		days int
	}{{"24h", 1}, {"week", 7}, {"month", 30}} {
		v["nations_new_"+w.key] = db.One("SELECT COUNT(*) AS c FROM nations WHERE creationdate >= NOW() - INTERVAL ? DAY", w.days).I("c")
		v["players_active_"+w.key] = db.One("SELECT COUNT(*) AS c FROM users WHERE lastactive >= NOW() - INTERVAL ? DAY", w.days).I("c")
	}
	p.Echo(`<center>New Nations</center>
<table class="table table-striped table-bordered">
<tr><th>Last 24 Hours:</th><th>$nations_new_24h</th></tr>
<tr><th>Last 7 Days:</th><th>$nations_new_week</th></tr>
<tr><th>Last 30 Days:</th><th>$nations_new_month</th></tr>
</table>
<center>Players Active</center>
<table class="table table-striped table-bordered">
<tr><th>Last 24 Hours:</th><th>$players_active_24h</th></tr>
<tr><th>Last 7 Days:</th><th>$players_active_week</th></tr>
<tr><th>Last 30 Days:</th><th>$players_active_month</th></tr>
</table>
<center>Census</center>
<table class="table table-striped table-bordered">
<tr><th>REGION:</th><th>NORTH</th><th>CENTRAL</th><th>SOUTH</th><th>TOTAL</th></tr>
<tr><th>Burrozil:</th><th>$census_burrozil_north</th><th>$census_burrozil_central</th><th>$census_burrozil_south</th><th>$census_burrozil_total</th></tr>
<tr><th>Zebrica:</th><th>$census_zebrica_north</th><th>$census_zebrica_central</th><th>$census_zebrica_south</th><th>$census_zebrica_total</th></tr>
<tr><th>Saddle Arabia:</th><th>$census_saddle_north</th><th>$census_saddle_central</th><th>$census_saddle_south</th><th>$census_saddle_total</th></tr>
<tr><th>Przewalskia:</th><th>$census_prze_north</th><th>$census_prze_central</th><th>$census_prze_south</th><th>$census_prze_total</th></tr>
</table>
<div class="row">
  <div class="col-md-6">
   `, v)
	p.Raw(flowTable("Global Resources", db.resourceFlows("u.stasismode = 0"), p.viewerHidesIcons(), ""))
	p.Raw("  </div>\n</div>\n")
}

func pageNews(p *Page) {
	p.title = "News - "
	db := p.db
	page := p.pageNumber()
	p.Raw("<center><h3>News</h3></center>\n")
	news := db.Query("SELECT message, posted FROM news ORDER BY posted DESC LIMIT ?, 20", (page-1)*20)
	if len(news) == 0 && page == 1 {
		p.Raw("<center>No news yet.</center>")
		return
	}
	var b strings.Builder
	b.WriteString("<table class=\"table table-striped table-bordered\">\n")
	for _, n := range news {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td></tr>\n", n.S("message"), n.S("posted"))
	}
	b.WriteString("</table>")
	b.WriteString(pager("news.php?page=", page, db.One("SELECT COUNT(*) AS count FROM news").I("count")))
	p.Raw(b.String())
}

func pageReports(p *Page) {
	p.title = "Reports - "
	p.needsNation()
	var b strings.Builder
	b.WriteString("<center><h3>Reports</h3></center>\n<table class=\"table table-striped table-bordered\">\n")
	// Reports filed in the same second are shown together.
	var times []string
	byTime := map[string][]string{}
	for _, r := range p.db.Query("SELECT report, time FROM reports WHERE nation_id = ? ORDER BY time DESC", p.NationID()) {
		t := r.S("time")
		if _, ok := byTime[t]; !ok {
			times = append(times, t)
		}
		byTime[t] = append(byTime[t], r.S("report"))
	}
	for _, t := range times {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td></tr>\n", joinBR(byTime[t]), t)
	}
	b.WriteString("</table>")
	p.Raw(b.String())
}

func pageTopMessage(p *Page) {
	p.title = "Top Message - "
	var b strings.Builder
	b.WriteString("<table class=\"table table-striped table-bordered\">\n")
	for _, m := range p.db.Query("SELECT time, message FROM topmessage ORDER BY time DESC") {
		fmt.Fprintf(&b, "<tr><td>%s</td></tr>\n", esc(m.S("message")))
	}
	b.WriteString("</table>")
	p.Raw(b.String())
}

func pageGraveyard(p *Page) {
	p.title = "Graveyard - "
	db := p.db
	page := p.pageNumber()
	p.Raw("<center>The Graveyard only holds nations which have been in the game for a month before ceasing to exist.</center>\n")
	nations := db.Query("SELECT graveyard_id, name, killer, deathdate FROM graveyard ORDER BY graveyard_id DESC LIMIT ?, 20", (page-1)*20)
	if len(nations) == 0 && page == 1 {
		p.Raw("<center>No nations are in the graveyard yet. (Don't worry- there will be!)</center>")
		return
	}
	var b strings.Builder
	b.WriteString("<table class=\"table table-striped table-bordered\">\n<tr><th>Name</th><th>Killer</th><th>Died On</th></tr>\n")
	for _, n := range nations {
		fmt.Fprintf(&b, "<tr><td><a href=\"viewgraveyard.php?graveyard_id=%s\">%s</a></td><td>%s</td><td>%s</td></tr>\n",
			n.S("graveyard_id"), n.S("name"), esc(n.S("killer")), n.S("deathdate"))
	}
	b.WriteString("</table>")
	b.WriteString(pager("graveyard.php?page=", page, db.One("SELECT COUNT(*) AS count FROM graveyard").I("count")))
	p.Raw(b.String())
}

func pageViewGraveyard(p *Page) {
	p.title = "View Graveyard - "
	n := p.db.One("SELECT * FROM graveyard WHERE graveyard_id = ?", toInt(p.Get("graveyard_id")))
	if n == nil {
		p.Raw("<center>Nation not found.</center>")
		return
	}
	p.Raw(fmt.Sprintf("<center><h2>%s</h2></center>\n%s", n.S("name"), n.S("details")))
}

func pageAscensions(p *Page) {
	p.title = "Ascensions - "
	db := p.db
	page := p.pageNumber()
	nations := db.Query(`SELECT an.user_id, an.name, an.date, u.username FROM ascendednations an INNER JOIN users u ON u.user_id = an.user_id
		ORDER BY an.date DESC LIMIT ?, 20`, (page-1)*20)
	if len(nations) == 0 && page == 1 {
		p.Raw("<center>No nations have yet ascended.</center>")
		return
	}
	var b strings.Builder
	b.WriteString(`<center>These are the players whose nations have ascended into alicorns and gone on to pursue new pleasures.</center>
<table class="table table-striped table-bordered">
<tr><th>Nation</th><th>User</th><th>Ascended On</th></tr>
`)
	for _, n := range nations {
		fmt.Fprintf(&b, "<tr><td><a href=\"viewascension.php?user_id=%s\">%s</a></td><td>%s</td><td>%s</td></tr>\n",
			n.S("user_id"), n.S("name"), n.S("username"), n.S("date"))
	}
	b.WriteString("</table>")
	b.WriteString(pager("ascensions.php?page=", page, db.One("SELECT COUNT(*) AS count FROM ascendednations").I("count")))
	p.Raw(b.String())
}

func pageViewAscension(p *Page) {
	p.title = "View Ascension - "
	db := p.db
	uid := toInt(p.Get("user_id"))
	u := db.One("SELECT username FROM users WHERE user_id = ?", uid)
	if u == nil {
		p.Raw("<center>User not found.</center>")
		return
	}
	nations := db.Query("SELECT * FROM ascendednations WHERE user_id = ? ORDER BY date DESC", uid)
	if len(nations) == 0 {
		p.Raw(fmt.Sprintf("<center>%s has not ascended.</center> ", u.S("username")))
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<center><h3>%s has ascended!</h3></center>
<br/><br/>
<center><h4>Nations</h4></center>
<table class="table table-striped table-bordered">
<th>Nation</th><th>Ascended on</th>
`, u.S("username"))
	for _, n := range nations {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td></tr>\n", n.S("name"), n.S("date"))
	}
	funds := db.One("SELECT amount FROM ascendedresources WHERE user_id = ? AND resource_id = 0", uid).S("amount")
	fmt.Fprintf(&b, `</table>
<br/><br/>
<center><h4>Resources and Buildings</h4></center>
<table class="table table-striped table-bordered">
<tr><td>Bits</td><td>%s</td></tr>
`, commas(funds))
	for _, r := range db.Query(`SELECT ar.amount, rd.name FROM ascendedresources ar INNER JOIN resourcedefs rd ON rd.resource_id = ar.resource_id
		WHERE ar.user_id = ? ORDER BY rd.name`, uid) {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td></tr>\n", r.S("name"), commas(r.S("amount")))
	}
	b.WriteString("</table>")
	p.Raw(b.String())
}

func pageSearch(p *Page) {
	p.title = "Search - "
	if p.PostB("search") {
		name := p.Post("name")
		if n := p.db.One("SELECT nation_id FROM nations WHERE name = ?", name); n != nil {
			p.Redirect("viewnation.php?nation_id=" + n.S("nation_id"))
		}
		if u := p.db.One("SELECT user_id FROM users WHERE username = ?", name); u != nil {
			p.Redirect("viewuser.php?user_id=" + u.S("user_id"))
		}
		p.Err("Name not found.")
	}
	p.Raw(`<center><form action="search.php" method="post">
Search individual User or Nation:
<input name="name" class="form-control" placeholder="User or Nation Name" style="width:300px"/>
<input type="submit" name="search" value="Search" class="btn btn-success"/>
</form></center>`)
}

// userList is one of the per-user lists of other players: blocks,
// embargoes, friends and enemies.
type userList struct {
	table, subject, object  string // table and its "who did it" / "to whom" columns
	page, token             string
	field, addLabel         string // form field and button for adding a name
	removeField, removeBtn  string
	selfErr                 string
	listTitle, emptyMine    string
	theirsTitle, noneTheirs string
	formLabel               string
}

func (l userList) handle(p *Page) {
	db, uid := p.db, p.UserID()
	switch p.Post("action") {
	case l.addLabel:
		id := db.findUser(p.Post(l.field))
		switch {
		case id != 0 && id == uid:
			p.Err("%s", l.selfErr)
		case id == 0:
			p.Err("Name not found.")
		default:
			if db.One("SELECT "+l.subject+" FROM "+l.table+" WHERE "+l.subject+" = ? AND "+l.object+" = ?", uid, id) == nil {
				db.Exec("INSERT INTO "+l.table+" ("+l.object+", "+l.subject+") VALUES (?, ?)", id, uid)
			}
		}
	case l.removeBtn:
		db.Exec("DELETE FROM "+l.table+" WHERE "+l.object+" = ? AND "+l.subject+" = ?", p.PostI(l.removeField), uid)
	}
}

func (l userList) render(p *Page) string {
	db, uid := p.db, p.UserID()
	token := p.token(l.token)
	var b strings.Builder
	fmt.Fprintf(&b, `<center><form action="%s" method="post">
<input type="hidden" name="token_%s" value="%s"/>
%s <input name="%s" size="25" type="text" class="form-control" style="width:250px"/>
<input name="action" type="submit" value="%s" class="btn btn-danger"/>
</form></center>
`, l.page, l.token, token, l.formLabel, l.field, l.addLabel)
	mine := db.Query("SELECT u.user_id, u.username FROM "+l.table+" e INNER JOIN users u ON e."+l.object+" = u.user_id WHERE e."+l.subject+" = ?", uid)
	if len(mine) == 0 {
		fmt.Fprintf(&b, "    <center>%s</center>\n    <br>\n", l.emptyMine)
	} else {
		fmt.Fprintf(&b, "    <center>%s</center>\n    <center><table class=\"table table-striped table-bordered\" style=\"width:350px\">\n", l.listTitle)
		for _, u := range mine {
			fmt.Fprintf(&b, `<tr><td><a href="viewuser.php?user_id=%s">%s</a></td><td><form action="%s" method="post">
<input type="hidden" name="token_%s" value="%s"/>
<input type="hidden" name="%s" value="%s"/><input name="action" type="submit" value="%s" class="btn btn-success btn-sm"/></form></td></tr>
`, u.S("user_id"), u.S("username"), l.page, l.token, token, l.removeField, u.S("user_id"), l.removeBtn)
		}
		b.WriteString("</table></center>\n")
	}
	if l.theirsTitle == "" {
		return b.String()
	}
	theirs := db.Query("SELECT u.user_id, u.username FROM "+l.table+" e INNER JOIN users u ON e."+l.subject+" = u.user_id WHERE e."+l.object+" = ?", uid)
	if len(theirs) == 0 {
		fmt.Fprintf(&b, "    <center>%s</center>\n", l.noneTheirs)
	} else {
		fmt.Fprintf(&b, "    <center>%s</center>\n    <center><table class=\"table table-striped table-bordered\" style=\"width:250px\">\n", l.theirsTitle)
		for _, u := range theirs {
			fmt.Fprintf(&b, "<tr><td><a href=\"viewuser.php?user_id=%s\">%s</a></td></tr>\n", u.S("user_id"), u.S("username"))
		}
		b.WriteString("    </table></center>\n")
	}
	return b.String()
}

const crackPipe = "Your senior advisors formally request that you put down the crack pipe."

var (
	blockList = userList{table: "blocklist", subject: "blocker", object: "blockee", page: "blocklist.php", token: "blocklist",
		field: "block", addLabel: "Block", removeField: "unblock", removeBtn: "Unblock", selfErr: crackPipe,
		listTitle: "Users you are blocking", emptyMine: "You are not blocking any users.",
		theirsTitle: "Users blocking you:", noneTheirs: "No users are blocking you.", formLabel: "User or Nation Name to block:"}
	embargoList = userList{table: "embargoes", subject: "embargoer", object: "embargoee", page: "embargoes.php", token: "embargoes",
		field: "embargo", addLabel: "Embargo", removeField: "unembargo", removeBtn: "Unembargo", selfErr: crackPipe,
		listTitle: "Users you are embargoing", emptyMine: "You are not embargoing any users.",
		theirsTitle: "Users embargoing you:", noneTheirs: "No users are embargoing you.", formLabel: "User or Nation Name to Embargo:"}
	friendList = userList{table: "friends", subject: "friender", object: "friendee", page: "friends.php", token: "friends",
		field: "Friend", addLabel: "Friend", removeField: "unfriend", removeBtn: "unfriend", selfErr: crackPipe,
		listTitle: "Users you are Friends with", emptyMine: "You have no friends.", formLabel: "User or Nation Name to Friend:"}
	enemyList = userList{table: "enemies", subject: "enemier", object: "enemiee", page: "friends.php", token: "friends",
		field: "Enemy", addLabel: "Enemy", removeField: "unenemy", removeBtn: "unenemy", selfErr: crackPipe,
		listTitle: "Users you are Enemies with", emptyMine: "You have no enemies.", formLabel: "User or Nation Name to Enemy:"}
)

func pageBlocklist(p *Page) {
	p.title = "blocklist - "
	p.needsUser()
	p.csrf("blocklist")
	if !p.HasErrors() {
		blockList.handle(p)
	}
	p.Raw(blockList.render(p))
}

func pageEmbargoes(p *Page) {
	p.title = "Embargoes - "
	p.needsNation()
	p.csrf("embargoes")
	if !p.HasErrors() {
		embargoList.handle(p)
	}
	p.Raw(embargoList.render(p))
}

func pageFriends(p *Page) {
	p.title = "friends - "
	p.needsNation()
	p.csrf("friends")
	if !p.HasErrors() {
		friendList.handle(p)
		enemyList.handle(p)
	}
	p.Raw(friendList.render(p))
	p.Raw(enemyList.render(p))
}

func pageMyOffers(p *Page) {
	p.title = "My Offers - "
	nation := p.needsNation()
	db, nid := p.db, p.NationID()
	p.csrf("myoffers")
	kinds := []struct {
		key   string
		buy   bool
		label string
		k     marketKind
	}{
		{"buyitems", true, "Items", marketKindFor("")}, {"sellitems", false, "Items", marketKindFor("")},
		{"buyarmor", true, "Armor", marketKindFor("armor")}, {"sellarmor", false, "Armor", marketKindFor("armor")},
		{"buyweapons", true, "Weapons", marketKindFor("weapons")}, {"sellweapons", false, "Weapons", marketKindFor("weapons")},
	}
	if !p.HasErrors() {
		for _, kind := range kinds {
			if !p.PostB("remove" + kind.key) {
				continue
			}
			table := kind.k.sellTable
			if kind.buy {
				table = kind.k.buyTable
			}
			itemID, price := p.PostI("resource_id"), p.PostI("price")
			o := db.One("SELECT m.price, m.amount, rd.name FROM "+table+" m INNER JOIN "+kind.k.defs+" rd ON m."+kind.k.idcol+" = rd."+kind.k.idcol+
				" WHERE m.nation_id = ? AND m."+kind.k.idcol+" = ? AND m.price = ?", nid, itemID, price)
			if o == nil {
				p.Err("Eeeeenope.")
				break
			}
			db.Exec("DELETE FROM "+table+" WHERE nation_id = ? AND "+kind.k.idcol+" = ? AND price = ?", nid, itemID, price)
			if kind.buy {
				refund := floor(db.buyingMultiplier(nid) * float64(o.I("price")*o.I("amount")))
				db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", refund, nid)
				p.Info("You have removed your request of %d %s from the market.", o.I("amount"), o.S("name"))
			} else {
				kind.k.addStock(db, nid, itemID, o.I("amount"))
				p.Info("You have removed %d %s from the market.", o.I("amount"), o.S("name"))
			}
			nation = db.One(nationInfoSQL, p.UserID(), nid)
		}
	}

	token := p.token("myoffers")
	var b strings.Builder
	fmt.Fprintf(&b, "<center><div class=\"well\">Funds: <span class=\"text-success\">%s Bits</span></div></center>\n", commas(nation.S("funds")))
	for _, kind := range kinds {
		table, verb := kind.k.sellTable, "Sell"
		if kind.buy {
			table, verb = kind.k.buyTable, "Buy"
		}
		offers := db.Query("SELECT m.*, m."+kind.k.idcol+" AS item_id, rd.name FROM "+table+" m INNER JOIN "+kind.k.defs+" rd ON rd."+kind.k.idcol+
			" = m."+kind.k.idcol+" WHERE m.nation_id = ?", nid)
		if len(offers) == 0 {
			fmt.Fprintf(&b, "<center>You have no offers to %s %s.</center>\n", strings.ToLower(verb), strings.ToLower(kind.label))
			continue
		}
		fmt.Fprintf(&b, `<center><h4>%sing %s</h4>
<center>
<table class="table table-striped table-bordered">
<thead><tr><td><div class="row">
<div class="col-md-1">%sing for</div>
<div class="col-md-1">Amount</div>
<div class="col-md-5">Resource</div>
<div class="col-md-5">Actions</div>
</div></td></tr></thead><tbody>
`, verb, kind.label, verb)
		for _, o := range offers {
			fmt.Fprintf(&b, `  <tr><td><div class="row">
  <div class="col-md-1"><p class="text-danger">%s</p></div>
  <div class="col-md-1"><p class="text-success">%s</p></div>
  <div class="col-md-5"><p>%s</p></div>
  <div class="col-md-5"><form action="myoffers.php" method="post">
  <input type="hidden" name="token_myoffers" value="%s"/>
  <input type="hidden" name="resource_id" value="%s"/>
  <input type="hidden" name="price" value="%s"/>
  <div class="row">
  <div class="col-xs-6">
  <input type="submit" name="remove%s" value="Remove from Marketplace" class="btn btn-danger btn-sm btn-block"/>
  </div>
  </div>
</form></div>
</div></td></tr>
`, commas(o.S("price")), commas(o.S("amount")), o.S("name"), token, o.S("item_id"), o.S("price"), kind.key)
		}
		b.WriteString("</tbody></table></center>\n")
	}
	p.Raw(b.String())
}

func pageEmpireOverview(p *Page, net bool) {
	nation := p.needsNation()
	db, uid := p.db, p.UserID()
	p.title = "Empire Overview STOCK - "
	heading, totalLabel := "Resources - Stockpiles", "TOTAL<br>STOCKS"
	if net {
		p.title = "Empire Overview NET - "
		heading, totalLabel = "Resources - Net", "TOTAL<br>NET"
	}
	resources := db.Query("SELECT resource_id, name FROM resourcedefs WHERE is_building = 0")
	nations := db.Query("SELECT * FROM nations WHERE user_id = ?", uid)
	type column struct {
		sat, se, nlr    string // display values
		satN, seN, nlrN float64
		funds           int64
		res             map[int64]int64
	}
	cols := make([]column, len(nations))
	for i, n := range nations {
		c := column{res: map[int64]int64{}}
		if net {
			proj := db.project(n)
			c.sat, c.se, c.nlr = str(proj.Sat), proj.SE, proj.NLR
			c.satN, c.seN, c.nlrN = float64(proj.Sat), toFloat(proj.SE), toFloat(proj.NLR)
			c.funds = n.I("gdp_last_turn")
			flows := db.resourceFlows("n.nation_id = ?", n.I("nation_id"))
			for _, r := range resources {
				c.res[r.I("resource_id")] = flows.Generated[r.S("name")] - flows.Used[r.S("name")]
			}
		} else {
			c.satN, c.seN, c.nlrN = n.F("satisfaction"), n.F("se_relation"), n.F("nlr_relation")
			c.sat, c.se, c.nlr = numberFormat(c.satN), numberFormat(c.seN), numberFormat(c.nlrN)
			c.funds = n.I("funds")
			for _, r := range db.Query("SELECT resource_id, amount FROM resources WHERE nation_id = ?", n.I("nation_id")) {
				c.res[r.I("resource_id")] = r.I("amount")
			}
		}
		cols[i] = c
	}

	hideIcons := nation.B("hideicons")
	var b strings.Builder
	fmt.Fprintf(&b, `<style>
button[name="switchnation_id"] {
  white-space: nowrap; /* Prevent wrapping i.e. force to a single line */
  text-overflow: ellipsis; /* ellipsis where text gets cut off */
  overflow-x: hidden; /* Required for prior two to work */
  width: 80px; /* Or however wide you want the columns to be */
}
</style>

<div class="row">
  <div class="col-md-6">
   <div class="panel panel-default">
     <div class="panel-heading">%s</div>
     <table class="table">
      <thead>
        <tr>
`, heading)
	if !hideIcons {
		b.WriteString("            <td style=\"width: 16px;\"></td>\n")
	}
	b.WriteString("        <td style=\"text-align: left;\">Resource</td>\n")
	for _, n := range nations {
		fmt.Fprintf(&b, `    <td style="text-align: left;"><div title="%s"><form action="overview.php" method="post"><button name="switchnation_id" type="submit" value="%s">%s</button></form></div></td>
`, n.S("name"), n.S("nation_id"), n.S("name"))
	}
	fmt.Fprintf(&b, "<td>%s</td></tr></thead><tbody>\n", totalLabel)
	cell := func(class, text string) {
		fmt.Fprintf(&b, "    <td style=\"text-align: left;\"><span class=\"%s\">%s</span></td>\n", class, text)
	}
	iconCell := "<td></td>"
	if hideIcons {
		iconCell = ""
	}
	for _, row := range []struct {
		label string
		get   func(column) (string, float64)
	}{
		{"Satisfaction", func(c column) (string, float64) { return c.sat, c.satN }},
		{"NLR Rep", func(c column) (string, float64) { return c.nlr, c.nlrN }},
		{"SE Rep", func(c column) (string, float64) { return c.se, c.seN }},
	} {
		fmt.Fprintf(&b, "<tr>%s<td style=\"text-align: left;\">%s</td>\n", iconCell, row.label)
		for _, c := range cols {
			text, n := row.get(c)
			cell(numberClass(n), text)
		}
		b.WriteString("<td></td></tr>\n")
	}
	fmt.Fprintf(&b, "<tr>%s<td style=\"text-align: left;\">Funds</td>\n", iconCell)
	var total int64
	for _, c := range cols {
		total += c.funds
		class := numberClass(float64(c.funds))
		if !net && c.funds > 500000000 {
			class = "text-warning"
		} else if !net && c.funds <= 0 {
			class = "text-danger"
		}
		cell(class, commas(c.funds))
	}
	cell(numberClass(float64(total)), commas(total))
	b.WriteString("</tr>\n")
	for _, r := range resources {
		id := r.I("resource_id")
		b.WriteString("    <tr>\n")
		if !hideIcons {
			fmt.Fprintf(&b, "    <td style=\"width: 16px;\"><img src=\"images/icons/%s.png\"/></td>\n", r.S("name"))
		}
		fmt.Fprintf(&b, "    <td style=\"text-align: left;\">%s</td>\n", r.S("name"))
		var total int64
		for _, c := range cols {
			v := c.res[id]
			total += v
			class := numberClass(float64(v))
			if !net {
				switch {
				case v > 50000:
					class = "text-warning"
				case v > 0:
					class = "text-success"
				default:
					class = "text-danger"
				}
			}
			cell(class, commas(v))
		}
		cell(numberClass(float64(total)), commas(total))
		b.WriteString("</tr>\n")
	}
	b.WriteString("    </tbody></table>\n   </div>\n  </div>\n</div>\n")
	p.Raw(b.String())
}

func pageStatic(title, html string) func(*Page) {
	return func(p *Page) {
		p.title = title
		p.Raw(html)
	}
}

func pageWarGuide(p *Page) {
	p.title = "War Guide - "
	db := p.db
	var b strings.Builder
	b.WriteString(contentHTML("warguide_intro"))
	b.WriteString(`<center><h3>Weapons</h3></center>
<center><table class="table table-striped table-bordered">
<tr><th>Name</th><th>Type</th><th>Vs. Cavalry</th><th>Vs. Tanks</th><th>Vs. Pegasi</th><th>Vs. Unicorns</th><th>Vs. Naval</th></tr>
`)
	for _, w := range db.Query("SELECT * FROM weapondefs") {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td>\n<td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			w.S("name"), forceTypeNames[w.I("type")], w.S("dmg_cavalry"), w.S("dmg_tanks"), w.S("dmg_pegasi"), w.S("dmg_unicorns"), w.S("dmg_naval"))
	}
	b.WriteString(`</table></center>
<center><h3>Armor</h3></center>
<center><table class="table table-striped table-bordered">
<tr><th>Name</th><th>Type</th><th>Vs. Cavalry</th><th>Vs. Tanks</th><th>Vs. Pegasi</th><th>Vs. Unicorns</th><th>Vs. Naval</th><th>Carrying Capacity</th></tr>
`)
	for _, a := range db.Query("SELECT * FROM armordefs") {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td>\n<td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			a.S("name"), forceTypeNames[a.I("type")], a.S("arm_cavalry"), a.S("arm_tanks"), a.S("arm_pegasi"), a.S("arm_unicorns"), a.S("arm_naval"), a.S("carrying"))
	}
	b.WriteString("</table></center>")
	p.Raw(b.String())
}

func pagePurchase(p *Page) {
	if p.PostB("purchase") {
		p.Raw(`<center><h2>April Fool's!</h2></center>
<center>Hahaha, wow, do you pay to win in EVERY game you play?</center>`)
		return
	}
	p.Raw(`<table class="table table-striped table-bordered">
<tr><td>Basic starter kit with 1 million bits and 1,000 oil, energy, copper, and apples.</td><td><span class="text-success">$5.00</span></td><td><input type="radio" name="lol"/></td></tr>
<tr><td>For players who want to get ahead quickly. 5 million bits, 5,000 copper, 5,000 energy, and 5 factories.</td><td><span class="text-success">$10.00</span></td><td><input type="radio" name="lol"/></td></tr>
<tr><td>This kit will get you to the top of the rankings in no time at all. 50 million bits, 10,000 of every resource (except DNA) in the game, and 10 malls.<br/>
Includes composites and precision parts- this package has it all.</td><td><span class="text-success">$20.00</span></td><td><input type="radio" name="lol"/></td></tr>
<tr><td>The ultimate. For a mere $50 USD, I will clone the nation (including forces) of any player you choose, bit for bit and resource for resource.<br/>
Make sure you can handle it before you buy!</td><td><span class="text-success">$50.00</span></td><td><input type="radio" name="lol"/></td></tr>
</table>
<center><form action="purchase.php" method="post"><input type="submit" name="purchase" value="Continue to Checkout" class="btn btn-success"/></form></center>`)
}
