package clop

import (
	"fmt"
	"regexp"
	"strings"
)

// findUser resolves a username or nation name to a user id (0 if none).
func (db *DB) findUser(name string) int64 {
	if u := db.One("SELECT user_id FROM users WHERE username = ?", name); u != nil {
		return u.I("user_id")
	}
	return db.One("SELECT u.user_id FROM users u INNER JOIN nations n ON u.user_id = n.user_id WHERE n.name = ?", name).I("user_id")
}

// sendMessage delivers a player message unless the recipient blocks the sender.
func (p *Page) sendMessage(to int64, message string) bool {
	if p.db.One("SELECT blocker FROM blocklist WHERE blocker = ? AND blockee = ?", to, p.UserID()) != nil {
		p.Err("That user has you blocked from sending messages.")
		return false
	}
	p.db.Exec("INSERT INTO messages (touser, fromuser, message, sent) VALUES (?, ?, ?, NOW())", to, p.UserID(), message)
	return true
}

func pageMessages(p *Page) {
	p.title = "Messages - "
	p.needsUser()
	db, uid := p.db, p.UserID()
	p.csrf("messages")
	draft := p.Post("message")
	if !p.HasErrors() {
		switch {
		case p.Post("action") == "Send Message":
			to := db.findUser(p.Post("sendto"))
			switch {
			case to != 0 && to == uid:
				p.Err("Just use a piece of paper or something.")
			case draft == "":
				p.Err("No message entered.")
			case to == 0:
				p.Err("Name not found.")
			case p.sendMessage(to, draft):
				draft = ""
				p.Info("Message sent to %s.", esc(p.Post("sendto")))
			}
		case p.PostB("bulkdelete"):
			if !isDigits(p.Post("deletedays")) {
				p.Err("Enter a number of days.")
				break
			}
			mine, theirs, who := mailboxColumns(p.Post("bulkdeletebox") == "inbox")
			for _, m := range db.Query("SELECT message_id, "+theirs+" AS other FROM messages WHERE "+who+" = ? AND sent < DATE_SUB(NOW(), INTERVAL ? DAY)",
				uid, p.PostI("deletedays")) {
				deleteMessageCopy(db, m, mine)
			}
		case p.Post("action") == "Delete Message":
			mine, theirs, who := mailboxColumns(p.Post("messagetype") == "inbox")
			m := db.One("SELECT message_id, "+theirs+" AS other FROM messages WHERE "+who+" = ? AND message_id = ?", uid, p.PostI("message_id"))
			if m == nil {
				p.Err("Something's gone wrong. You're not trying to delete someone else's message, are you?")
			} else {
				deleteMessageCopy(db, m, mine)
			}
		case p.PostB("markread"), p.PostB("markunread"):
			from, to, msg := 0, 1, "Message already marked as read."
			if p.PostB("markunread") {
				from, to, msg = 1, 0, "Message already marked as unread."
			}
			if db.One("SELECT message_id FROM messages WHERE touser = ? AND message_id = ? AND is_read = ?", uid, p.PostI("message_id"), from) == nil {
				p.Err(msg)
			} else {
				db.Exec("UPDATE messages SET is_read = ? WHERE message_id = ?", to, p.PostI("message_id"))
			}
		}
	}

	token := p.token("messages")
	var b strings.Builder
	fmt.Fprintf(&b, `<center>New Message</center>
<form name="newmessage" method="post" action="messages.php">
<input type="hidden" name="token_messages" value="%s"/>
<center><div class="input-group">To: <input name="sendto" type="text" class="form-control" style="width:200px;"/></div></center>
<center><textarea name="message" class="form-control" style="width:300px;">%s</textarea></center>
<center><input type="submit" name="action" value="Send Message" class="btn btn-info"/></center>
</form>
<form name="bulkdelete" method="post" action="messages.php">
<input type="hidden" name="token_messages" value="%s"/>
<center><div class="input-group">Delete all messages more than <input name="deletedays" type="text" style="width:25px;" maxlength="2"/> days old
from the <select name="bulkdeletebox">
<option value="inbox">Inbox</option>
<option value="sentbox">Sentbox</option>
</select>
<input type="submit" name="bulkdelete" value="Bulk Delete" class="btn btn-sm btn-danger" onclick="return confirm('Really bulk delete messages?')"/></div></center>
</form>
`, token, esc(draft), token)

	inbox := db.Query(`SELECT m.*, u.username, u.user_id FROM messages m LEFT JOIN users u ON u.user_id = m.fromuser
		WHERE m.touser = ? AND m.todeleted = 0 ORDER BY sent DESC`, uid)
	if len(inbox) == 0 {
		b.WriteString("<center><h4>No messages in inbox</h4></center>\n")
	} else {
		b.WriteString(`<center>Inbox</center>
<center><table class="table table-striped table-bordered">
<tr><th>Message</th><th>From</th><th>Sent</th><th></th></tr>
`)
		for _, m := range inbox {
			from := "<b>*Automated System Message*</b>"
			if m.I("fromuser") != 0 {
				from = fmt.Sprintf(`<a href="viewuser.php?user_id=%s">%s</a>`, m.S("user_id"), m.S("username"))
			}
			read := `<input type="submit" name="markread" value="Mark as Read" class="btn btn-primary"/>`
			if m.B("is_read") {
				read = `<input type="submit" name="markunread" value="Mark as Unread" class="btn btn-warning"/>`
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td>
<td><form name="deletemessage" method="post" action="messages.php"><input type="hidden" name="messagetype" value="inbox"/>
<input type="hidden" name="token_messages" value="%s"/>
<input type="hidden" name="message_id" value="%s"/>
%s
<input type="submit" name="action" value="Delete Message" class="btn btn-danger"/>
</form></td></tr>
`, nl2br(esc(m.S("message"))), from, m.S("sent"), token, m.S("message_id"), read)
		}
		b.WriteString("</table></center>\n")
	}
	sent := db.Query(`SELECT m.*, u.username, u.user_id FROM messages m LEFT JOIN users u ON u.user_id = m.touser
		WHERE m.fromuser = ? AND m.fromdeleted = 0 ORDER BY sent DESC`, uid)
	if len(sent) == 0 {
		b.WriteString("<center><h4>No messages in sentbox</h4></center>\n")
	} else {
		b.WriteString(`<center>Sentbox</center>
<center><table class="table table-striped table-bordered">
<tr><th>Message</th><th>To</th><th>Sent</th><th></th></tr>
`)
		for _, m := range sent {
			fmt.Fprintf(&b, `<tr><td>%s</td><td><a href="viewuser.php?user_id=%s">%s</a></td><td>%s</td>
<td><form name="deletemessage" method="post" action="messages.php"><input type="hidden" name="messagetype" value="sentbox"/>
<input type="hidden" name="token_messages" value="%s"/>
<input type="hidden" name="message_id" value="%s"/>
<input type="submit" name="action" value="Delete Message" class="btn btn-danger"/>
</form></td></tr>
`, nl2br(esc(m.S("message"))), m.S("user_id"), m.S("username"), m.S("sent"), token, m.S("message_id"))
		}
		b.WriteString("</table></center>\n")
	}
	p.Raw(b.String())
}

// mailboxColumns names the deletion flags for a mailbox: the flag for our
// copy, the other party's flag, and the column identifying us.
func mailboxColumns(inbox bool) (mine, theirs, who string) {
	if inbox {
		return "todeleted", "fromdeleted", "touser"
	}
	return "fromdeleted", "todeleted", "fromuser"
}

// deleteMessageCopy hides a message from one side, removing it entirely
// once both sides have deleted it.
func deleteMessageCopy(db *DB, m Row, mine string) {
	if m.B("other") {
		db.Exec("DELETE FROM messages WHERE message_id = ?", m.I("message_id"))
	} else {
		db.Exec("UPDATE messages SET "+mine+" = 1 WHERE message_id = ?", m.I("message_id"))
	}
}

func pageViewUser(p *Page) {
	p.title = "View User - "
	db := p.db
	userID := p.PostI("user_id")
	if userID == 0 {
		userID = toInt(p.Get("user_id"))
	}
	draft := p.Post("message")
	if p.Post("action") == "Send Message" && p.UserID() != 0 {
		p.csrf("viewuser")
		switch {
		case p.HasErrors():
		case db.One("SELECT user_id FROM users WHERE user_id = ?", userID) == nil:
			p.Err("Something went badly wrong.")
		case userID == p.UserID():
			p.Err("Use a piece of paper or something.")
		case draft == "":
			p.Err("No message entered.")
		case p.sendMessage(userID, draft):
			draft = ""
			p.Info("Message sent.")
		}
	} else if p.token("viewuser") == "" {
		p.sess.Set("token_viewuser", newToken())
	}
	u := db.One(`SELECT u.username, u.user_id, u.flag, u.donator, u.stasismode, u.description, u.alliance_id, a.name AS alliancename
		FROM users u LEFT JOIN alliances a ON u.alliance_id = a.alliance_id WHERE u.user_id = ?`, userID)
	if u == nil {
		p.Raw("<center><h3>User not found!</h3></center>")
		return
	}
	var b strings.Builder
	if u.B("donator") {
		b.WriteString("<center><b>This user has donated to >CLOP!</b></center>\n")
	}
	if u.B("stasismode") {
		b.WriteString("<center><b>This user is in stasis.</b></center>\n")
	}
	fmt.Fprintf(&b, "<center>%s</center>\n", u.S("username"))
	if u.B("flag") && !truthy(p.sess.Get("hideflags")) {
		fmt.Fprintf(&b, "    <center>    <img src=\"%s\" height=\"150\" width=\"250\"></center>\n", esc(u.S("flag")))
	}
	if u.B("alliancename") {
		fmt.Fprintf(&b, "<center><a href=\"viewalliance.php?alliance_id=%s\">%s</a></center>\n", u.S("alliance_id"), u.S("alliancename"))
	}
	fmt.Fprintf(&b, "<center>%s</center>\n", nl2br(esc(u.S("description"))))
	if p.UserID() != 0 && p.UserID() != userID {
		fmt.Fprintf(&b, `<center>Send Message</center>
<form name="newmessage" method="post" action="viewuser.php">
<input type="hidden" name="token_viewuser" value="%s"/>
<input type="hidden" name="user_id" value="%d"/>
<center><textarea name="message">%s</textarea></center>
<center><input type="submit" name="action" value="Send Message" class="btn btn-sm btn-success"/></center>
</form>
`, p.token("viewuser"), userID, esc(draft))
	}
	nations := db.Query("SELECT nation_id, name, region FROM nations WHERE user_id = ?", userID)
	if len(nations) == 0 {
		b.WriteString("<center>This user is nationless!</center>\n")
	} else {
		b.WriteString("<center>Nations</center>\n<center><table>\n")
		for _, n := range nations {
			fmt.Fprintf(&b, "<tr><td>%s</td></tr>\n", nationLinks([]Row{n}))
		}
		b.WriteString("</table></center>\n")
	}
	p.Raw(b.String())
}

func pageViewNation(p *Page) {
	p.title = "View Nation - "
	db := p.db
	nationID := toInt(p.GP("nation_id"))
	n := db.One(`SELECT n.*, u.user_id, u.username, u.alliance_id, u.flag, a.name AS alliance_name FROM nations n
		INNER JOIN users u ON u.user_id = n.user_id LEFT JOIN alliances a ON a.alliance_id = u.alliance_id WHERE n.nation_id = ?`, nationID)
	if n == nil {
		p.Raw("<center><h3>Nation not found!</h3></center>")
		return
	}
	var b strings.Builder
	b.WriteString(tpl(`<center><h3>{$n['name']}</h3></center>
<center><h4>{$region}</h4></center>
<center><h4>Government: {$n['government']}</h4></center>
<center><h4>Economy: {$n['economy']}</h4></center>
<center><h4>Leader: <a href="viewuser.php?user_id={$n['user_id']}">{$n['username']}</a></h4></center>
`, V{"n": n, "region": subregionNames[n.I("subregion")] + regionNames[n.I("region")]}))
	if n.B("flag") && !truthy(p.sess.Get("hideflags")) {
		fmt.Fprintf(&b, "  <center>  <img src=\"%s\" height=\"150\" width=\"250\"></center>\n", esc(n.S("flag")))
	}
	fmt.Fprintf(&b, `<center><h4>Alliance: <a href="viewalliance.php?alliance_id=%s">%s</a></h4></center>
<center><h4>Created: %s (Age: %s)</h4></center>
<center>%s</center><br/>
<center>GDP: %s bits every 2 hours</center>
<center><b>Buildings</b></center>
<center><table id="buildings">
`, n.S("alliance_id"), n.S("alliance_name"), n.S("creationdate"), n.S("age"), nl2br(esc(n.S("description"))), commas(db.baseGDP(nationID)))
	for _, r := range db.Query(`SELECT r.amount, rd.name FROM resources r INNER JOIN resourcedefs rd ON r.resource_id = rd.resource_id
		WHERE r.nation_id = ? AND rd.is_building = 1 ORDER BY rd.name`, nationID) {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td></tr>\n", r.S("name"), r.S("amount"))
	}
	b.WriteString("</table></center>")
	attackers, defenders := forcesAt(db, nationID)
	b.WriteString(forceColumns(attackers, "Attackers", "No attackers are in this nation."))
	b.WriteString(forceColumns(defenders, "Defenders", "No defenders are in this nation."))

	hideIcons := p.viewerHidesIcons()
	b.WriteString("<div class=\"row\">\n  <div class=\"col-md-6\">\n   ")
	b.WriteString(flowTable("Nation Resources", db.resourceFlows("n.nation_id = ?", nationID), hideIcons, ""))
	b.WriteString("  </div>\n")
	if n.I("alliance_id") != 0 {
		b.WriteString("  <div class=\"col-md-6\">\n   ")
		b.WriteString(flowTable("Alliance Resources", db.resourceFlows("u.alliance_id = ? AND u.stasismode = 0", n.I("alliance_id")), hideIcons, ""))
		b.WriteString("  </div>\n")
	}
	b.WriteString("</div>\n")
	p.Raw(b.String())
}

// forceColumns renders force groups in bootstrap columns, as on viewnation.
func forceColumns(units []Row, heading, empty string) string {
	if len(units) == 0 {
		return fmt.Sprintf(`<div class="row"><center><h5>%s</h5></center></div>`, empty)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="row"><center><h4>%s</h4></center>
`, heading)
	group := ""
	for _, f := range units {
		if f.S("forcegroup_id") != group {
			if group != "" {
				b.WriteString("</table></div></div>")
			}
			owner := f.S("ownername")
			if f.I("ownernation_id") > 0 {
				owner = fmt.Sprintf(`<a href="viewnation.php?nation_id=%s">%s</a> (%s)`, f.S("ownernation_id"), f.S("ownername"), f.S("ownerregionname"))
			}
			fmt.Fprintf(&b, `<div class="col-md-4">
	<div class="panel panel-default">
	    <div class="panel-heading">%s<br/>
Owned by %s</div>
<table class="table table-striped table-bordered">
`, f.S("groupname"), owner)
			group = f.S("forcegroup_id")
		}
		b.WriteString(forceUnitCell(f, true))
	}
	b.WriteString("</table></div></div></div>")
	return b.String()
}

var flagURL = regexp.MustCompile(`(?i)^(https?://)?([\w\-\.]+):?([0-9]*)/([^ \?&=#"\n\r\t<]*?(\.(jpg|jpeg|gif|png)))$`)

func pageUserInfo(p *Page) {
	p.title = "User Info - "
	p.needsUser()
	db, uid := p.db, p.UserID()
	p.csrf("userinfo")
	if !p.HasErrors() && p.isPost {
		userInfoAction(p)
	}
	u := db.One(`SELECT username, email, flag, donator, description, stasismode, hidebanners, hideicons, hideflags, hidereports
		FROM users WHERE user_id = ?`, uid)
	token := p.token("userinfo")
	var b strings.Builder
	if u.B("donator") {
		b.WriteString("<center><b>Thank you for your donation!</b></center>\n")
	}
	if u.B("stasismode") {
		b.WriteString("<center><b>You are in stasis mode! Leave stasis to play >CLOP.</b></center>\n")
	}
	b.WriteString(tpl(userInfoHTML, V{"u": u, "token": token, "email": esc(u.S("email")), "flag": esc(u.S("flag")), "description": esc(u.S("description"))}))
	for i, label := range []string{"Grey", "Black", "White"} {
		fmt.Fprintf(&b, "        <option value=\"%d\"%s>%s</option>\n", i, selected(toInt(p.sess.Get("css")) == int64(i)), label)
	}
	fmt.Fprintf(&b, `</select><input type="submit" name="changecolor" value="Change Background Color" class="btn btn-sm btn-success"/></form></center><br/>
<form name="banners" method="post" action="userinfo.php">
<input type="hidden" name="token_userinfo" value="%s"/>
`, token)
	for _, t := range []struct{ col, hide, show, hideLabel, showLabel string }{
		{"hidebanners", "hidebanners", "showbanners", "Hide Top Banners", "Show Top Banners"},
		{"hidereports", "hidereports", "showreports", "Hide Report Details", "Show Report Details"},
		{"hideicons", "hideicons", "showicons", "Hide Overview Icons", "Show Overview Icons"},
		{"hideflags", "hideflags", "showflags", "Hide Flags", "Show Flags"},
	} {
		name, label := t.hide, t.hideLabel
		if u.B(t.col) {
			name, label = t.show, t.showLabel
		}
		fmt.Fprintf(&b, "<center><input type=\"submit\" name=\"%s\" value=\"%s\" class=\"btn btn-success\"/></center>\n", name, label)
	}
	fmt.Fprintf(&b, `</form>
<form name="stasis" method="post" action="userinfo.php">
<input type="hidden" name="token_userinfo" value="%s"/>
`, token)
	if !u.B("stasismode") {
		b.WriteString(`<center><h5>Stasis Mode</h5></center>
If you enter stasis mode, you will be unable to log in at all for 24 hours. Ticks will not happen for you, and you will not be able to be attacked.<br/>
<center><input type="submit" name="enterstasis" onclick="return confirm('Really enter stasis for at least 24 hours?')"  value="Enter Stasis" class="btn btn-danger"/></center>
`)
	} else {
		b.WriteString(`<center><input type="submit" name="leavestasis" value="Leave Stasis" class="btn btn-success"/></center>
`)
	}
	b.WriteString("</form>\n")
	p.Raw(b.String())
}

const userInfoHTML = `<center>{$u['username']}</center>
<form name="newpassword" method="post" action="userinfo.php">
<input type="hidden" name="token_userinfo" value="{$token}"/>
<center><table>
<tr><td>Current Password</td><td><input type="password" class="form-control" name="currentpassword" maxlength="250" style="width:200px;" autocomplete="off"/></td></tr>
<tr><td>New Password</td><td><input type="password" name="password" class="form-control" style="width:200px;" autocomplete="off"/></td></tr>
<tr><td>Confirm New Password</td><td><input type="password" class="form-control" style="width:200px;" name="confirm_password"/></td></tr>
</table></center>
<center><input type="submit" name="action" value="New Password" class="btn btn-sm btn-warning"/></center>
</form>
<br/>
<form name="newemail" method="post" action="userinfo.php">
<input type="hidden" name="token_userinfo" value="{$token}"/>
<center><table>
<tr><td>Email</td><td><input name="email" class="form-control" style="width:200px;" value="{$email}"/></td></tr>
</table></center>
<center><input type="submit" name="changeemail" value="Change Email" class="btn btn-sm btn-warning"/></center>
<br/>
</form>
<form name="newflag" method="post" action="userinfo.php">
<input type="hidden" name="token_userinfo" value="{$token}"/>
<center><table>
<tr><td>Flag Image URL</td><td><input name="flag" class="form-control" style="width:200px;" value="{$flag}"/></td></tr>
</table></center>
<center><input type="submit" name="changeflag" value="Change Flag" class="btn btn-sm btn-warning"/></center></form>
<br/>
<center><form name="description" method="post" action="userinfo.php" class="form" role="form">
<textarea name="description" class="form-control" style="width:400px;">{$description}</textarea>
<input type="hidden" name="token_userinfo" value="{$token}"/>
</br>
<input name="changedescription" type="submit" value="Change Description" class="btn btn-success"/>
</form></center>
<br/>
<center><form name="cssoptions" method="post" action="userinfo.php">
<input type="hidden" name="token_userinfo" value="{$token}"/>
<select name="css" class="form-control" style="width:210px;">
`

func userInfoAction(p *Page) {
	db, uid := p.db, p.UserID()
	toggle := func(col string, on bool, sessKey string) {
		v := 0
		if on {
			v = 1
		}
		db.Exec("UPDATE users SET "+col+" = ? WHERE user_id = ?", v, uid)
		if sessKey != "" {
			p.sess.Set(sessKey, v)
		}
	}
	switch {
	case p.PostB("changedescription"):
		db.Exec("UPDATE users SET description = ? WHERE user_id = ?", p.Post("description"), uid)
		p.Info("Description changed.")
	case p.Post("action") == "New Password":
		if db.One("SELECT user_id FROM users WHERE user_id = ? AND password = ?", uid, passwordHash(p.Post("currentpassword"))) == nil {
			p.Err("Incorrect current password.")
		}
		if p.Post("password") != p.Post("confirm_password") {
			p.Err("Passwords do not match.")
		}
		if p.Post("password") == "" {
			p.Err("No password entered.")
		}
		if !p.HasErrors() {
			db.Exec("UPDATE users SET password = ? WHERE user_id = ?", passwordHash(p.Post("password")), uid)
			p.Info("Password changed.")
		}
	case p.PostB("changeflag"):
		flag := p.Post("flag")
		if flag != "" && !flagURL.MatchString(flag) {
			p.Err("Heeeeyyy... are you SURE that's an image? (JPGs, GIFs, and PNGs only.)")
			break
		}
		db.Exec("UPDATE users SET flag = ? WHERE user_id = ?", flag, uid)
		p.Info("Flag changed.")
	case p.PostB("changeemail"):
		db.Exec("UPDATE users SET email = ? WHERE user_id = ?", p.Post("email"), uid)
		p.Info("Email address changed.")
	case p.PostB("changecolor"):
		css := p.PostI("css")
		if css < 0 || css > 2 {
			p.Err("What do you think would even happen, smart guy?")
			break
		}
		db.Exec("UPDATE users SET css = ? WHERE user_id = ?", css, uid)
		p.sess.Set("css", css)
	case p.PostB("enterstasis"):
		if stasisRecent(db.One("SELECT stasisdate FROM users WHERE user_id = ?", uid).S("stasisdate")) {
			p.Err("You have left stasis less than 24 hours ago.")
			break
		}
		for _, n := range db.Query("SELECT nation_id FROM nations WHERE user_id = ?", uid) {
			id := n.I("nation_id")
			db.Exec(`UPDATE forcegroups SET attack_mission = 0, oldmission = 0, departuredate = NULL, destination_id = 0
				WHERE nation_id = ? AND departuredate IS NOT NULL`, id)
			db.Exec(`UPDATE forcegroups SET attack_mission = 0, oldmission = 0, destination_id = ?, departuredate = NOW()
				WHERE nation_id = ? AND location_id != ?`, id, id, id)
		}
		username := db.One("SELECT username FROM users WHERE user_id = ?", uid).S("username")
		db.news(fmt.Sprintf(`<a href="viewuser.php?user_id=%d">%s</a> has gone into stasis.`, uid, username))
		db.Exec("UPDATE users SET stasisdate = NOW(), stasismode = 1 WHERE user_id = ?", uid)
		p.app.sessions.Destroy(p.sess)
		p.Redirect("index.php")
	case p.PostB("leavestasis"):
		db.Exec("UPDATE users SET stasisdate = NOW(), stasismode = 0 WHERE user_id = ?", uid)
		p.Info("You have left stasis mode.")
		username := db.One("SELECT username FROM users WHERE user_id = ?", uid).S("username")
		db.news(fmt.Sprintf(`<a href="viewuser.php?user_id=%d">%s</a> has left stasis.`, uid, username))
	case p.PostB("hidebanners"), p.PostB("showbanners"):
		toggle("hidebanners", p.PostB("hidebanners"), "hidebanners")
	case p.PostB("hidereports"), p.PostB("showreports"):
		toggle("hidereports", p.PostB("hidereports"), "hidereports")
	case p.PostB("hideicons"), p.PostB("showicons"):
		toggle("hideicons", p.PostB("hideicons"), "")
	case p.PostB("hideflags"), p.PostB("showflags"):
		toggle("hideflags", p.PostB("hideflags"), "hideflags")
	}
}

func pageTransfer(p *Page) {
	p.title = "Empire Transfers - "
	nation := p.needsNation()
	db, nid, uid := p.db, p.NationID(), p.UserID()
	p.csrf("transfer")
	target := p.PostI("nation_id")
	if p.isPost && target == nid {
		p.Err("You just tried to send something to the nation you're sending it from. Watch your tab switching!")
	}
	if !p.HasErrors() && p.isPost {
		transferAction(p, nation, target)
		nation = db.One(nationInfoSQL, uid, nid)
	}

	nations := db.Query("SELECT nation_id, name FROM nations WHERE user_id = ?", uid)
	switch {
	case len(nations) <= 1:
		p.Raw("<center>You only have one nation.</center>")
		return
	case nation.S("government") == "Decentralization":
		p.Raw("<center>Your Decentralized government cannot perform transfers.</center>")
		return
	}
	description := "With your Poorly Defined economy, you transfer items for 50 bits each and pay a 3% fee to transfer bits."
	switch nation.S("economy") {
	case "State Controlled":
		description = "With your State Controlled economy, you transfer both bits and items for free."
	case "Free Market":
		description = "With your Free Market economy, you transfer items for 100 bits each and pay a 6% fee to transfer bits."
	}
	var b strings.Builder
	fmt.Fprintf(&b, `	<center><div class="well">Funds: <span class="text-success">%s Bits</span></div></center>
	<center>%s</center>
	<center><div style="width:400px;">
<div class="panel-footer">
Transfer to: <select name="determinenation_id" class="form-control"
onchange="document.getElementById('select1').value = this.value;
document.getElementById('select2').value = this.value;
document.getElementById('select3').value = this.value;
document.getElementById('select4').value = this.value;">
`, commas(nation.S("funds")), description)
	var defaultID int64
	for _, n := range nations {
		if n.I("nation_id") == nid {
			continue
		}
		if defaultID == 0 || target == n.I("nation_id") {
			defaultID = n.I("nation_id")
		}
		fmt.Fprintf(&b, "<option value=\"%s\"%s>%s</option>\n", n.S("nation_id"), selected(target == n.I("nation_id")), n.S("name"))
	}
	b.WriteString("		</select>\n		</div>\n")
	token := p.token("transfer")
	for i, k := range dealKinds {
		field := map[string]string{"item": "resource", "weapon": "weapon", "armor": "armor"}[k.typ]
		fmt.Fprintf(&b, `        <div class="panel-footer">
        <form action="transfer.php" method="post">
<input type="hidden" name="token_transfer" value="%s"/>
<input type="hidden" id="select%d" name="nation_id" value="%d"/>
            Transfer this %s:
<select name="%s_id" class="form-control" style="width:210px;">
`, token, i+1, defaultID, field, field)
		for _, o := range k.market.itemOptions(db, nid, true) {
			fmt.Fprintf(&b, "<option value=\"%s\"%s>%s</option>\n", o.S("id"), selected(p.Post(field+"_id") == o.S("id")), o.S("label"))
		}
		fmt.Fprintf(&b, `            </select>
            in this quantity:
            <div class="input-group">
              <input type="text" class="form-control" name="%samount" placeholder="Qty"/>
              <span class="input-group-btn">
                  <input type="submit" name="transfer%s" value="Transfer %s" class="btn btn-primary"/>
               </span>
            </div>
        </form>
        </div>
`, field, field, map[string]string{"resource": "Resource", "weapon": "Weapon", "armor": "Armor"}[field])
	}
	fmt.Fprintf(&b, `		  <div class="panel-footer">
          <form action="transfer.php" method="post">
<input type="hidden" name="token_transfer" value="%s"/>
<input type="hidden" id="select4" name="nation_id" value="%d"/>
            <div class="input-group">
              <input type="text" class="form-control" name="money"/>
              <span class="input-group-btn">
                  <input type="submit" name="transfermoney" value="Transfer This Much Money" class="btn btn-primary"/>
               </span>
            </div>
		</form>
			</div>
		</div></center>
`, token, defaultID)
	p.Raw(b.String())
}

func transferAction(p *Page, nation Row, target int64) {
	db, nid := p.db, p.NationID()
	t := db.One("SELECT nation_id, government, name FROM nations WHERE user_id = ? AND nation_id = ?", p.UserID(), target)
	switch {
	case t == nil:
		p.Err("No.")
		return
	case t.S("government") == "Decentralization" || nation.S("government") == "Decentralization":
		p.Err("Decentralized governments cannot perform transfers.")
		return
	}
	econ := nation.S("economy")
	if p.PostB("transfermoney") {
		raw := p.Post("money")
		money := toInt(raw)
		if !isDigits(raw) {
			p.Err("Digits only- no commas, periods, or other markers.")
		}
		if money <= 0 {
			p.Err("No amount entered.")
		}
		if nation.I("funds") < money {
			p.Err("You do not have that much money.")
		}
		if p.HasErrors() {
			return
		}
		mult := .97
		switch econ {
		case "State Controlled":
			mult = 1
		case "Free Market":
			mult = .94
		}
		received := int64(round(mult*float64(money), 0))
		db.Exec("UPDATE nations SET funds = funds + ? WHERE nation_id = ?", received, target)
		db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", money, nid)
		info := fmt.Sprintf("This nation paid %s bits and %s received %s bits.", commas(money), t.S("name"), commas(received))
		p.Info("%s", info)
		db.report(nid, info)
		db.report(target, fmt.Sprintf("This nation received %s bits from %s.", commas(received), nation.S("name")))
		return
	}
	var k marketKind
	var field string
	switch {
	case p.PostB("transferweapon"):
		k, field = marketKindFor("weapons"), "weapon"
	case p.PostB("transferarmor"):
		k, field = marketKindFor("armor"), "armor"
	case p.PostB("transferresource"):
		k, field = marketKindFor(""), "resource"
	default:
		return
	}
	raw := p.Post(field + "amount")
	amount, itemID := toInt(raw), p.PostI(field+"_id")
	def := db.One("SELECT name FROM "+k.defs+" rd WHERE "+k.idcol+" = ?"+k.tradeable, itemID)
	if def == nil {
		p.Err("That can't be transferred.")
	}
	have := db.One("SELECT amount FROM "+k.stock+" WHERE nation_id = ? AND "+k.idcol+" = ?", nid, itemID).I("amount")
	var fee int64
	switch econ {
	case "Free Market":
		fee = amount * 100
	case "State Controlled":
	default:
		fee = amount * 50
	}
	if nation.I("funds") < fee {
		p.Err("You don't have the money to transfer that many items!")
	}
	if have < amount {
		p.Err("You don't have enough to transfer!")
	} else if amount < 1 {
		p.Err("No amount entered.")
	}
	if !isDigits(raw) {
		p.Err("Digits only- no commas, periods, or other markers.")
	}
	if p.HasErrors() {
		return
	}
	k.addStock(db, target, itemID, amount)
	k.takeStock(db, nid, itemID, amount, have)
	if fee != 0 {
		db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", fee, nid)
	}
	info := fmt.Sprintf("You transferred %d %s to %s for %s bits.", amount, def.S("name"), t.S("name"), commas(fee))
	p.Info("%s", info)
	db.report(nid, info)
	db.report(target, fmt.Sprintf("This nation received %d %s from %s.", amount, def.S("name"), nation.S("name")))
}
