package clop

import (
	"fmt"
	"strings"
	"time"
)

// nationLinks renders a user's nations as "Name (icon Region)" links.
func nationLinks(nations []Row) string {
	if len(nations) == 0 {
		return "Nationless"
	}
	links := make([]string, 0, len(nations))
	for _, n := range nations {
		links = append(links, fmt.Sprintf(`<a href="viewnation.php?nation_id=%s">%s (<img src="images/icons/%s.png"/>%s)</a>`,
			n.S("nation_id"), n.S("name"), regionIcons[n.I("region")], regionNames[n.I("region")]))
	}
	return strings.Join(links, ", ")
}

func (db *DB) nationsOf(userID int64) []Row {
	return db.Query("SELECT nation_id, name, region FROM nations WHERE user_id = ? ORDER BY name", userID)
}

// viewerHidesIcons reports the logged-in player's icon preference.
func (p *Page) viewerHidesIcons() bool {
	if p.UserID() == 0 {
		return false
	}
	return p.db.One("SELECT hideicons FROM users WHERE user_id = ?", p.UserID()).B("hideicons")
}

func (p *Page) userAllianceID() int64 {
	return p.db.One("SELECT alliance_id FROM users WHERE user_id = ?", p.UserID()).I("alliance_id")
}

func pageAlliances(p *Page) {
	p.title = "Alliances - "
	p.needsNation()
	db, uid := p.db, p.UserID()
	hasAlliance := p.userAllianceID() != 0
	p.csrf("alliances")
	if !p.HasErrors() {
		switch {
		case p.Post("action") == "Make New Alliance":
			if hasAlliance {
				p.Redirect("myalliance.php")
			}
			name := p.Post("alliancename")
			if !alnumSpace(name, true) {
				p.Err("Only English letters and numbers for the alliance name.")
			}
			if name == "" {
				p.Err("No name entered.")
			}
			if !p.HasErrors() {
				id := db.Exec("INSERT INTO alliances (name, description, public_description, owner_id, creationdate) VALUES (?, ?, '', ?, NOW())",
					name, p.Post("alliancedescription"), uid)
				db.Exec("UPDATE users SET alliance_id = ? WHERE user_id = ?", id, uid)
				db.Exec("DELETE FROM alliance_requests WHERE user_id = ?", uid)
				p.sess.Set("alliance_id", id)
				p.Redirect("myalliance.php")
			}
		case p.PostB("requestjoin"):
			if hasAlliance {
				p.Redirect("myalliance.php")
			}
			a := db.One("SELECT name FROM alliances WHERE alliance_id = ?", p.PostI("alliance_id"))
			if a == nil {
				p.Err("That alliance disappeared!")
			} else {
				db.Exec("INSERT INTO alliance_requests (alliance_id, user_id) VALUES (?, ?)", p.PostI("alliance_id"), uid)
				p.Info("You have requested to join %s.", a.S("name"))
			}
		case p.PostB("rescindrequest"):
			db.Exec("DELETE FROM alliance_requests WHERE alliance_id = ? AND user_id = ?", p.PostI("alliance_id"), uid)
		}
	}

	token := p.token("alliances")
	var b strings.Builder
	if hasAlliance {
		b.WriteString(`<center><h4><a href="myalliance.php">View My Alliance</a></h4></center>`)
	} else {
		fmt.Fprintf(&b, `<center><form name="alliance" method="post" action="alliances.php">
<input type="hidden" name="token_alliances" value="%s"/>
<table>
<tr><td>Alliance Name</td><td><input type="text" class="form-control" style="width:250px" name="alliancename" value="%s"/></td></tr>
<tr><td>Alliance Description</td><td><textarea class="form-control" name="alliancedescription">%s</textarea></td></tr>
</table>
<input type="submit" name="action" value="Make New Alliance" class="btn btn-success"/>
</form>
</center>`, token, esc(p.Post("alliancename")), esc(p.Post("alliancedescription")))
	}
	b.WriteString(`<center><table class="table table-striped table-bordered">
<tr><th>Alliance Name</th><th>Active<br/>Member Count</th><th>Active<br/>Nation Count</th><th>Leader</th>`)
	if !hasAlliance {
		b.WriteString("<th></th>")
	}
	b.WriteString("</tr>")
	for _, a := range db.Query(`SELECT a.alliance_id, a.name, a.owner_id, COUNT(DISTINCT u1.user_id) AS players, u2.username AS leader,
		COUNT(n.nation_id) AS nations, ar.user_id AS alliancerequested
		FROM alliances a
		INNER JOIN users u1 ON u1.alliance_id = a.alliance_id AND u1.stasismode = 0
		INNER JOIN users u2 ON a.owner_id = u2.user_id
		INNER JOIN nations n ON u1.user_id = n.user_id
		LEFT JOIN alliance_requests ar ON (ar.alliance_id = a.alliance_id AND ar.user_id = ?)
		GROUP BY a.alliance_id ORDER BY players DESC`, uid) {
		request := ""
		if !hasAlliance {
			button := `<input type="submit" name="requestjoin" value="Request to Join" class="btn btn-success btn-sm"/><br/>`
			if a.B("alliancerequested") {
				button = `<input type="submit" name="rescindrequest" value="Rescind Join Request" class="btn btn-danger btn-sm"/><br/>`
			}
			request = fmt.Sprintf(`<td><form action="alliances.php" method="post"><input type="hidden" name="token_alliances" value="%s"/>
<input type="hidden" name="alliance_id" value="%s"/>
%s</form></td>`, token, a.S("alliance_id"), button)
		}
		fmt.Fprintf(&b, `<tr><td><a href="viewalliance.php?alliance_id=%s">%s</a></td>
<td>%s</td><td>%s</td><td><a href="viewuser.php?user_id=%s">%s</a></td>%s</tr>
`, a.S("alliance_id"), a.S("name"), a.S("players"), a.S("nations"), a.S("owner_id"), a.S("leader"), request)
	}
	b.WriteString("</table></center>")
	p.Raw(b.String())
}

func pageAllianceAttacks(p *Page) {
	p.title = "Alliance Attacks - "
	nation := p.needsNation()
	gov := nation.S("government")
	switch {
	case gov != "Decentralization" && !ascendingGov(gov):
		p.Raw("<center>Attacks against alliance members can only be seen by Decentralized governments.</center>")
		return
	case !nation.B("alliance_id"):
		p.Raw("<center>You have no alliance.</center>")
		return
	}
	attacks := p.db.Query(`SELECT SUM(f.size) AS totalsize, n.name AS attackername, n.nation_id AS attackerid, u.username AS attackeruser, u.user_id AS attackeruserid,
		n2.name AS defendername, n2.nation_id AS defenderid, u2.username AS defenderuser, u2.user_id AS defenderuserid
		FROM nations n
		INNER JOIN forcegroups fg ON fg.nation_id = n.nation_id
		INNER JOIN forces f ON fg.forcegroup_id = f.forcegroup_id
		INNER JOIN users u ON u.user_id = n.user_id
		INNER JOIN nations n2 ON (fg.destination_id = n2.nation_id AND departuredate IS NOT NULL)
		INNER JOIN users u2 ON u2.user_id = n2.user_id
		WHERE fg.attack_mission = 1 AND u2.alliance_id = ?
		GROUP BY n.nation_id, n2.nation_id ORDER BY totalsize DESC`, nation.I("alliance_id"))
	if len(attacks) == 0 {
		p.Raw("<center>No one is attacking your alliance members.</center>")
		return
	}
	var b strings.Builder
	b.WriteString(`	<table class="table table-striped table-bordered">
	<tr><th>Total troops</th><th>Attacking<br/>Nation</th><th>Attacking<br/>User</th><th>Defending<br/>Nation</th><th>Defending<br/>User</th></tr>
`)
	for _, a := range attacks {
		b.WriteString(tpl(`<tr><td>{$a['totalsize']}</td>
<td><a href="viewnation.php?nation_id={$a['attackerid']}">{$a['attackername']}</a></td>
<td><a href="viewuser.php?user_id={$a['attackeruserid']}">{$a['attackeruser']}</a></td>
<td><a href="viewnation.php?nation_id={$a['defenderid']}">{$a['defendername']}</a></td>
<td><a href="viewuser.php?user_id={$a['defenderuserid']}">{$a['defenderuser']}</a></td>
</tr>
`, V{"a": a}))
	}
	b.WriteString("</table>")
	p.Raw(b.String())
}

func pageMyAlliance(p *Page) {
	p.title = "My Alliance - "
	p.needsUser()
	db, uid := p.db, p.UserID()
	allianceID := p.userAllianceID()
	if allianceID == 0 {
		p.Redirect("overview.php")
	}
	alliance := db.One("SELECT * FROM alliances WHERE alliance_id = ?", allianceID)
	if alliance == nil {
		db.Exec("UPDATE users SET alliance_id = 0 WHERE user_id = ?", uid)
		p.Redirect("overview.php")
	}
	p.csrf("myalliance")
	owner := uid == alliance.I("owner_id")
	if !p.HasErrors() && p.isPost {
		myAllianceAction(p, alliance, &owner)
		alliance = db.One("SELECT * FROM alliances WHERE alliance_id = ?", allianceID)
	}

	token := p.token("myalliance")
	form := func(inner string) string {
		return fmt.Sprintf(`<form method="post" action="myalliance.php"><input type="hidden" name="token_myalliance" value="%s"/>
%s</form>`, token, inner)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<center><h3>%s</h3></center><br/>\n<center>%s</center>\n", alliance.S("name"), nl2br(esc(alliance.S("description"))))
	if owner {
		fmt.Fprintf(&b, `    <center><h5>Public Description</h5></center>
    <center>%s</center>
    <center><h5>Private Description</h5></center>
    <center>%s</center>
`, form(`<textarea class="form-control" name="alliancepubdescription" style="width:300px;">`+esc(alliance.S("public_description"))+`</textarea><br/>
    <input type="submit" name="updatepubdescription" value="Update Public Description" class="btn btn-info"/>`),
			form(`<textarea class="form-control" name="alliancedescription" style="width:300px;">`+esc(alliance.S("description"))+`</textarea><br/>
    <input type="submit" name="updatedescription" value="Update Private Description" class="btn btn-info"/>`))
	}
	requests := db.Query(`SELECT u.username, u.user_id FROM alliance_requests ar INNER JOIN users u ON ar.user_id = u.user_id
		WHERE ar.alliance_id = ? ORDER BY u.username`, allianceID)
	if len(requests) > 0 {
		b.WriteString("    <center><h4>Requesting Members</h4></center>\n    <center><table class=\"table table-striped table-bordered\">\n")
		for _, r := range requests {
			fmt.Fprintf(&b, `    <tr><td><a href="viewuser.php?user_id=%s">%s</a></td>`, r.S("user_id"), r.S("username"))
			if owner {
				fmt.Fprintf(&b, "    <td>%s</td>\n", form(fmt.Sprintf(`    <input type="hidden" name="user_id" value="%s"/>
    <input type="submit" name="action" value="Accept User" class="btn btn-success btn-sm"/><br/>
    <input type="submit" name="action" value="Reject User" class="btn btn-danger btn-sm"/>`, r.S("user_id"))))
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</table></center>\n")
	}
	draft := ""
	if p.HasErrors() {
		draft = esc(p.Post("message"))
	}
	fmt.Fprintf(&b, `<center><h6>New Alliance Message</h6></center>
<form name="newmessage" method="post" action="myalliance.php">
<input type="hidden" name="token_myalliance" value="%s"/>
<center><textarea name="message" class="form-control" style="width:300px;">%s</textarea></center>
<center><input type="submit" name="sendmessage" value="Post Message" class="btn btn-info"/></center>
</form>
`, token, draft)
	if owner {
		fmt.Fprintf(&b, `<form name="bulkdelete" method="post" action="myalliance.php">
<input type="hidden" name="token_myalliance" value="%s"/>
<center><div class="input-group">Delete all messages more than <input name="deletedays" type="text" style="width:25px;" maxlength="2"/> days old
<input type="submit" name="bulkdelete" value="Bulk Delete" class="btn btn-sm btn-danger" onclick="return confirm('Really bulk delete messages?')"/></div></center>
</form>
`, token)
	}
	messages := db.Query(`SELECT u.username, u.user_id, am.message, am.posted, am.message_id FROM alliance_messages am
		INNER JOIN users u ON am.user_id = u.user_id WHERE am.alliance_id = ? ORDER BY am.posted DESC`, allianceID)
	if len(messages) > 0 {
		b.WriteString("    <center><h4>Messages</h4></center>\n    <center><table class=\"table table-striped table-bordered\">\n")
		for _, m := range messages {
			fmt.Fprintf(&b, `    <tr><td>%s</td>
    <td><a href="viewuser.php?user_id=%s">%s</a></td>
    <td>%s</td>
`, nl2br(esc(m.S("message"))), m.S("user_id"), m.S("username"), m.S("posted"))
			if owner || m.I("user_id") == uid {
				fmt.Fprintf(&b, "        <td>%s</td>\n", form(fmt.Sprintf(`    <input type="hidden" name="message_id" value="%s"/>
    <input type="submit" name="deletemessage" value="Delete Message" class="btn btn-danger btn-sm"/>`, m.S("message_id"))))
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</table></center>\n")
	}
	b.WriteString("    <center><h4>Current Members</h4></center>\n    <center><table class=\"table table-striped table-bordered\">\n    <tr><th>Member</th><th>Stasis</th><th>Nations</th>")
	if owner {
		b.WriteString("<th></th>")
	}
	b.WriteString("</tr>")
	for _, m := range db.Query("SELECT username, user_id, stasismode FROM users WHERE alliance_id = ? ORDER BY username", allianceID) {
		stasis := ""
		if m.B("stasismode") {
			stasis = "<center>X</center>"
		}
		fmt.Fprintf(&b, `<tr><td><a href="viewuser.php?user_id=%s">%s</a></td>
<td>%s</td><td>%s</td>
`, m.S("user_id"), m.S("username"), stasis, nationLinks(db.nationsOf(m.I("user_id"))))
		if owner && m.I("user_id") != uid {
			fmt.Fprintf(&b, "    <td>%s</td>\n", form(fmt.Sprintf(`    <input type="hidden" name="user_id" value="%s"/>
    <input type="submit" name="action" value="Eject User" class="btn btn-danger btn-sm"/>`, m.S("user_id"))))
		} else if owner {
			b.WriteString("<td></td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table></center>\n<br/>\n<br/>\n<br/>\n")
	if owner {
		fmt.Fprintf(&b, `    <center>%s</center>
    <br/>
    <center>%s</center>
`, form(`    <center>Give Alliance To: <input name="giveto" type="text" class="form-control" style="width:200px;"/></center>
    <input type="submit" name="givealliance" value="Give Away Alliance" class="btn btn-danger btn-sm" onclick="return confirm('Really give away your alliance?')"/>`),
			form(`    <input type="submit" name="action" value="Disband Alliance" class="btn btn-danger btn-sm" onclick="return confirm('Really disband your alliance?')"/>`))
	} else {
		fmt.Fprintf(&b, "    <center>%s</center>\n", form(`    <input type="submit" name="action" value="Leave Alliance" class="btn btn-danger btn-sm" onclick="return confirm('Really leave your alliance?')"/>`))
	}
	b.WriteString(`    <div class="col-md-6 col-md-offset-3">
    `)
	b.WriteString(flowTable("Alliance Resources", db.resourceFlows("u.alliance_id = ? AND u.stasismode = 0", allianceID), p.viewerHidesIcons(), ""))
	b.WriteString("    </div>\n")
	p.Raw(b.String())

	db.Exec("UPDATE users SET alliance_messages_last_checked = NOW() WHERE user_id = ?", uid)
	p.sess.Set("alliance_messages_last_checked", time.Now().UTC().Format("2006-01-02 15:04:05"))
}

func myAllianceAction(p *Page, alliance Row, owner *bool) {
	db, uid := p.db, p.UserID()
	allianceID := alliance.I("alliance_id")
	if *owner {
		target := p.PostI("user_id")
		switch {
		case p.PostB("updatedescription"):
			db.Exec("UPDATE alliances SET description = ? WHERE alliance_id = ?", p.Post("alliancedescription"), allianceID)
		case p.PostB("updatepubdescription"):
			db.Exec("UPDATE alliances SET public_description = ? WHERE alliance_id = ?", p.Post("alliancepubdescription"), allianceID)
		case p.Post("action") == "Accept User":
			if db.One("SELECT user_id FROM alliance_requests WHERE user_id = ? AND alliance_id = ?", target, allianceID) == nil {
				p.Err("The user disappeared before you could accept!")
				break
			}
			db.Exec("DELETE FROM alliance_requests WHERE user_id = ?", target)
			db.Exec("UPDATE users SET alliance_id = ? WHERE user_id = ?", allianceID, target)
			db.systemMessage(target, "Your application to "+alliance.S("name")+" was accepted.")
		case p.Post("action") == "Reject User":
			if db.One("SELECT user_id FROM alliance_requests WHERE user_id = ? AND alliance_id = ?", target, allianceID) == nil {
				p.Err("The member disappeared before you could reject!")
				break
			}
			db.Exec("DELETE FROM alliance_requests WHERE user_id = ? AND alliance_id = ?", target, allianceID)
			db.systemMessage(target, "Your application to "+alliance.S("name")+" was rejected.")
		case p.Post("action") == "Eject User":
			if target == uid {
				break
			}
			db.Exec("UPDATE users SET alliance_id = 0 WHERE user_id = ? AND alliance_id = ?", target, allianceID)
			db.systemMessage(target, "You were thrown out of "+alliance.S("name")+"!")
		case p.PostB("givealliance"):
			u := db.One("SELECT user_id, alliance_id FROM users WHERE username = ?", p.Post("giveto"))
			switch {
			case u == nil:
				p.Err("User not found.")
			case u.I("alliance_id") != allianceID:
				p.Err("That user is not in your alliance!")
			case u.I("user_id") == uid:
				p.Err("Silly.")
			default:
				db.Exec("UPDATE alliances SET owner_id = ? WHERE alliance_id = ?", u.I("user_id"), allianceID)
				*owner = false
			}
		case p.Post("action") == "Disband Alliance":
			db.Exec("UPDATE users SET alliance_id = 0 WHERE alliance_id = ?", allianceID)
			db.Exec("DELETE FROM alliance_requests WHERE alliance_id = ?", allianceID)
			db.Exec("DELETE FROM alliance_messages WHERE alliance_id = ?", allianceID)
			db.Exec("DELETE FROM alliances WHERE alliance_id = ?", allianceID)
			p.Redirect("overview.php")
		case p.PostB("bulkdelete"):
			if !isDigits(p.Post("deletedays")) {
				p.Err("Enter a number of days.")
				break
			}
			db.Exec("DELETE FROM alliance_messages WHERE alliance_id = ? AND posted < DATE_SUB(NOW(), INTERVAL ? DAY)", allianceID, p.PostI("deletedays"))
		}
	}
	switch {
	case p.Post("action") == "Leave Alliance" && !*owner:
		db.Exec("UPDATE users SET alliance_id = 0 WHERE user_id = ?", uid)
		p.Redirect("overview.php")
	case p.PostB("sendmessage"):
		if strings.TrimSpace(p.Post("message")) == "" {
			p.Err("No message entered.")
			break
		}
		db.Exec("INSERT INTO alliance_messages (alliance_id, user_id, posted, message) VALUES (?, ?, NOW(), ?)", allianceID, uid, p.Post("message"))
	case p.PostB("deletemessage"):
		q := "SELECT user_id FROM alliance_messages WHERE alliance_id = ? AND message_id = ?"
		args := []any{allianceID, p.PostI("message_id")}
		if !*owner {
			q += " AND user_id = ?"
			args = append(args, uid)
		}
		if db.One(q, args...).B("user_id") {
			db.Exec("DELETE FROM alliance_messages WHERE message_id = ?", p.PostI("message_id"))
		} else {
			p.Err("You can't delete that message.")
		}
	}
}

func pageViewAlliance(p *Page) {
	p.title = "View Alliance - "
	db, uid := p.db, p.UserID()
	allianceID := p.PostI("alliance_id")
	if allianceID == 0 {
		allianceID = toInt(p.Get("alliance_id"))
	}
	p.csrf("viewalliance")
	alliance := db.One(`SELECT a.name, a.alliance_id, a.public_description, ar.user_id AS alliancerequested
		FROM alliances a LEFT JOIN alliance_requests ar ON (ar.alliance_id = a.alliance_id AND ar.user_id = ?)
		WHERE a.alliance_id = ?`, uid, allianceID)
	if alliance == nil {
		p.Raw("<center>Alliance not found.</center> ")
		return
	}
	myAlliance := p.userAllianceID()
	if uid != 0 && !p.HasErrors() && p.isPost {
		members := db.Query("SELECT user_id FROM users WHERE alliance_id = ?", allianceID)
		bulk := func(insert, self, done string, add bool) {
			for _, m := range members {
				if m.I("user_id") == uid {
					if add {
						p.Err("%s", self)
					}
					continue
				}
				db.Exec(insert, uid, m.I("user_id"))
			}
			p.Info("%s", done)
		}
		switch {
		case p.PostB("requestjoin"):
			if myAlliance != 0 {
				p.Redirect("myalliance.php")
			}
			db.Exec("INSERT INTO alliance_requests (alliance_id, user_id) VALUES (?, ?)", allianceID, uid)
			alliance["alliancerequested"] = str(uid)
			p.Info("You have requested to join %s.", alliance.S("name"))
		case p.PostB("rescindrequest"):
			db.Exec("DELETE FROM alliance_requests WHERE alliance_id = ? AND user_id = ?", allianceID, uid)
			alliance["alliancerequested"] = ""
		case p.PostB("embargoalliance"):
			bulk("INSERT INTO embargoes (embargoer, embargoee) VALUES (?, ?)", "You can embargo your own alliance if you like, but you can't embargo yourself!", "Alliance embargoed.", true)
		case p.PostB("friendalliance"):
			bulk("INSERT INTO friends (friender, friendee) VALUES (?, ?)", "You can friend your own alliance if you like, but you can't friend yourself!", "Alliance friended.", true)
		case p.PostB("enemyalliance"):
			bulk("INSERT INTO enemies (enemier, enemiee) VALUES (?, ?)", "You can enemy your own alliance if you like, but you can't enemy yourself!", "Alliance enemied.", true)
		case p.PostB("unembargoalliance"):
			bulk("DELETE FROM embargoes WHERE embargoer = ? AND embargoee = ?", "", "Alliance unembargoed.", false)
		case p.PostB("unfriendalliance"):
			bulk("DELETE FROM friends WHERE friender = ? AND friendee = ?", "", "Alliance unfriended.", false)
		case p.PostB("unenemyalliance"):
			bulk("DELETE FROM enemies WHERE enemier = ? AND enemiee = ?", "", "Alliance unenemied.", false)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<center><h3>%s</h3></center>\n", alliance.S("name"))
	if uid != 0 {
		join := ""
		if myAlliance == 0 {
			join = `<input type="submit" name="requestjoin" value="Request to Join" class="btn btn-success btn-sm"/>`
			if alliance.B("alliancerequested") {
				join = `<input type="submit" name="rescindrequest" value="Rescind Join Request" class="btn btn-danger btn-sm"/>`
			}
		}
		fmt.Fprintf(&b, `<center><form action="viewalliance.php" method="post"><input type="hidden" name="token_viewalliance" value="%s"/>
<input type="hidden" name="alliance_id" value="%s"/>
%s <input type="submit" onclick="return confirm('Really add everyone in %s to your embargo list? (Depending on the alliance, a large portion of the market may disappear for you!)')"
name="embargoalliance" value="Embargo Alliance" class="btn btn-danger btn-sm"/>
<input type="submit" name="unembargoalliance" value="Unembargo Alliance" class="btn btn-success btn-sm"/>
<input type="submit" name="enemyalliance" value="Enemy Alliance" class="btn btn-danger btn-sm"/>
<input type="submit" name="unenemyalliance" value="UnEnemy Alliance" class="btn btn-success btn-sm"/>
<input type="submit" name="friendalliance" value="Friend Alliance" class="btn btn-success btn-sm"/>
<input type="submit" name="unfriendalliance" value="UnFriend Alliance" class="btn btn-danger btn-sm"/>
</form>
</center>
</br>
`, p.token("viewalliance"), alliance.S("alliance_id"), join, alliance.S("name"))
	}
	fmt.Fprintf(&b, "<center>%s</center>\n<center><table class=\"table table-striped table-bordered\">\n", nl2br(esc(alliance.S("public_description"))))
	hideFlags := truthy(p.sess.Get("hideflags"))
	for _, m := range db.Query("SELECT user_id, flag, username, stasismode FROM users WHERE alliance_id = ? ORDER BY username", allianceID) {
		flag := ""
		if m.B("flag") && !hideFlags {
			flag = fmt.Sprintf(`<img src="%s" height="20" width="20">`, esc(m.S("flag")))
		}
		name := m.S("username")
		if m.B("stasismode") {
			name = "<i>" + name + " (Stasis)</i>"
		}
		fmt.Fprintf(&b, `<tr><td style="width:25px">%s</td><td><a href="viewuser.php?user_id=%s">%s</a></td><td>%s</td></tr>
`, flag, m.S("user_id"), name, nationLinks(db.nationsOf(m.I("user_id"))))
	}
	b.WriteString("</table></center>\n")
	gdp := db.One(`SELECT SUM(n.gdp_last_turn) AS gdp FROM nations n INNER JOIN users u ON u.user_id = n.user_id
		WHERE u.alliance_id = ? AND u.stasismode = 0`, allianceID).S("gdp")
	hideIcons := p.viewerHidesIcons()
	icon := "<td></td>"
	if hideIcons {
		icon = ""
	}
	gdpRow := fmt.Sprintf(`        <tr>%s
        <td style="text-align: right;">GDP</td>
        <td style="text-align: center;"><span class="text-success"></span></td>
        <td style="text-align: center;"><span class="text-danger"></span></td>
        <td style="text-align: center;"><span class="text-success">%s</span></td>
        </tr>
`, icon, commas(gdp))
	b.WriteString(`    <div class="col-md-6 col-md-offset-3">
    `)
	b.WriteString(flowTable("Alliance Resources", db.resourceFlows("u.alliance_id = ? AND u.stasismode = 0", allianceID), hideIcons, gdpRow))
	b.WriteString("    </div>\n")
	p.Raw(b.String())
}
