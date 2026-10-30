package clop

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

func pageIndex(p *Page) {
	p.Raw(`<center><h4>Welcome to &gt;CLOP!</h4>
&gt;CLOP is a game of war, politics, markets, deals, and ponies. Join an alliance, build an empire, and rule your equines how you like.<br/>
Be warned- what is easily established is easily overthrown, and if you displease or neglect your nation, you will no longer have it.<br/>
Featuring frequent ticks and a robust trading system, &gt;CLOP is an addictive, maddening experience for aspiring politicians and horse lovers alike.
</center>`)
}

func pageLogout(p *Page) {
	p.app.sessions.Destroy(p.sess)
	p.Redirect("index.php")
}

// checkBanned stops banned addresses at the door.
func (p *Page) checkBanned() {
	ips := []string{p.clientIP(), p.r.Header.Get("X-Forwarded"), p.r.Header.Get("X-Forwarded-For")}
	for _, ip := range ips {
		if ip == "" {
			continue
		}
		if b := p.db.One("SELECT ip, reason FROM banlist WHERE ip = ?", ip); b != nil {
			p.Die("You're banned. " + esc(b.S("reason")))
		}
	}
}

func pageLogin(p *Page) {
	p.checkBanned()
	username := p.Post("username")
	if username == "" {
		return
	}
	password := p.Post("password")
	if p.PostB("isopass") {
		password = toLatin1(password)
	}
	ip, ip2 := p.clientIP(), p.remoteAddr()
	fwd, fwdFor := p.r.Header.Get("X-Forwarded"), p.r.Header.Get("X-Forwarded-For")
	failed := p.db.One("SELECT COUNT(*) AS count FROM logins WHERE ip = ? AND failed = true AND logindate > DATE_SUB(NOW(), INTERVAL 2 HOUR)", ip)
	if failed.I("count") > 20 {
		p.Err("Go bruteforce somewhere else.")
		return
	}
	u := p.db.One(`SELECT user_id, alliance_id, stasisdate, stasismode, css, hidebanners, hidereports, hideflags, alliance_messages_last_checked
		FROM users WHERE username = ? AND password = ?`, username, passwordHash(password))
	if u == nil {
		p.db.Exec(`INSERT INTO logins (username, user_id, ip, ip2, forwarded, forwarded_for, logindate, failed)
			VALUES (?, 0, ?, ?, ?, ?, NOW(), true)`, username, ip, ip2, fwd, fwdFor)
		p.Err("Login incorrect.")
		return
	}
	if u.B("stasismode") && stasisRecent(u.S("stasisdate")) {
		p.Err("You have entered stasis less than 24 hours ago.")
		return
	}
	p.db.Exec(`INSERT INTO logins (username, user_id, ip, ip2, forwarded, forwarded_for, logindate, failed)
		VALUES (?, ?, ?, ?, ?, ?, NOW(), false)`, username, u.I("user_id"), ip, ip2, fwd, fwdFor)
	p.app.sessions.Regenerate(p.w, p.sess)
	for _, k := range []string{"css", "hidebanners", "hidereports", "hideflags", "user_id", "alliance_id", "alliance_messages_last_checked"} {
		p.sess.Set(k, u.S(k))
	}
	n := p.db.One("SELECT nation_id, name FROM nations WHERE user_id = ?", u.I("user_id"))
	p.sess.Set("nation_id", n.S("nation_id"))
	p.sess.Set("nation_name", n.S("name"))
	p.Redirect("overview.php")
}

// stasisRecent reports whether a stasis date is less than 24 hours old.
func stasisRecent(date string) bool {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", date, time.UTC)
	return err == nil && t.After(time.Now().Add(-24*time.Hour))
}

// shuffledOptions renders <option>s for an id->name map in random order,
// which is how region pickers have always been shown (so nobody just takes
// the first one).
func shuffledOptions(names map[int64]string, ids []int64, selected string) string {
	order := append([]int64(nil), ids...)
	rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	var b strings.Builder
	for _, id := range order {
		sel := ""
		if selected == str(id) {
			sel = "selected"
		}
		fmt.Fprintf(&b, `<option value="%d" %s>%s</option>`, id, sel, names[id])
	}
	return b.String()
}

var baseRegions = map[int64]string{1: "Saddle Arabia", 2: "Zebrica", 3: "Burrozil", 4: "Przewalskia"}
var baseSubregions = map[int64]string{1: "North", 2: "Central", 3: "South"}

// census counts active nations per region and subregion.
func (db *DB) census() V {
	v := V{}
	regions := map[string]int64{"saddle": 1, "zebrica": 2, "burrozil": 3, "prze": 4}
	subs := map[string]int64{"north": 1, "central": 2, "south": 3}
	counts := map[[2]int64]int64{}
	for _, r := range db.Query(`SELECT n.region, n.subregion, COUNT(*) AS c FROM nations n
		INNER JOIN users u ON n.user_id = u.user_id WHERE u.stasismode = 0 GROUP BY n.region, n.subregion`) {
		counts[[2]int64{r.I("region"), r.I("subregion")}] = r.I("c")
	}
	for rn, rid := range regions {
		var total int64
		for sn, sid := range subs {
			c := counts[[2]int64{rid, sid}]
			v["census_"+rn+"_"+sn] = c
			total += c
		}
		v["census_"+rn+"_total"] = total
	}
	return v
}

// validNewNation checks a requested nation name and region, recording errors.
func (p *Page) validNewNation(name, region string, userID int64) {
	if !alnumSpace(name, false) {
		p.Err("Only English letters and numbers for the nation name.")
	}
	if strings.TrimSpace(name) == "" {
		p.Err("No nation name entered.")
	}
	if !isNumeric(region) || toFloat(region) < 1 || toFloat(region) > 4 {
		p.Err("I'm on to your game, buster!")
	}
	if p.db.One("SELECT COUNT(*) AS count FROM nations WHERE name = ?", strings.TrimSpace(name)).I("count") > 0 {
		p.Err("Nation name already taken.")
	}
	if p.db.One("SELECT COUNT(*) AS count FROM users WHERE username = ? AND user_id != ?", strings.TrimSpace(name), userID).I("count") > 0 {
		p.Err("To avoid confusion, you can't name your nation after someone else's username.")
	}
}

func pageNewUser(p *Page) {
	if p.UserID() != 0 {
		p.Err(`The "multiple accounts will get you banned" thing isn't a joke. Don't do it.`)
	}
	ip := p.clientIP()
	if b := p.db.One("SELECT ip, reason FROM banlist WHERE ip = ?", ip); b != nil {
		p.Die("You're banned. " + esc(b.S("reason")))
	}
	if p.db.One("SELECT ip FROM creation_banlist WHERE ip = ?", ip) != nil {
		p.Err("This IP belongs to a public system, such as a school or a library. You can play from here, but to prevent easy multi abuse, new users have to make accounts at home.")
	}
	p.csrf("newuser")
	display := map[string]string{}
	for k := range p.form {
		display[k] = esc(strings.TrimSpace(p.Post(k)))
	}

	if !p.HasErrors() && p.isPost {
		username := strings.TrimSpace(p.Post("realusername"))
		nationName := strings.TrimSpace(p.Post("nationname"))
		if !alnumSpace(p.Post("realusername"), false) {
			p.Err("Only English letters and numbers for the username.")
		}
		if p.Post("username") != "" {
			p.Err("First field must be blank.")
		}
		if username == "" {
			p.Err("No username entered.")
		}
		if strings.TrimSpace(p.Post("password")) == "" {
			p.Err("No password entered.")
		}
		if p.Post("confirmpassword") != p.Post("password") {
			p.Err("Passwords do not match.")
		}
		if p.db.One("SELECT COUNT(*) AS count FROM users WHERE username = ?", username).I("count") > 0 {
			p.Err("Username already taken.")
		}
		if p.db.One("SELECT COUNT(*) AS count FROM nations WHERE name = ?", username).I("count") > 0 {
			p.Err("To avoid confusion, you can't use someone else's nation name as your username.")
		}
		p.validNewNation(p.Post("nationname"), p.Post("region"), 0)
		if !p.HasErrors() {
			userID := p.db.Exec(`INSERT INTO users (username, password, email, creation_ip, creation_ip2, creation_forwarded_ip, creation_forwarded_for_ip, description, flag)
				VALUES (?, ?, ?, ?, ?, ?, ?, '', '')`, username, passwordHash(strings.TrimSpace(p.Post("password"))),
				strings.TrimSpace(p.Post("asdf")), ip, p.remoteAddr(), p.r.Header.Get("X-Forwarded"), p.r.Header.Get("X-Forwarded-For"))
			if userID == 0 {
				p.Err("Something went wrong creating your account. Try again.")
			} else {
				p.createUserNation(userID, username, nationName, ip)
			}
		}
	}
	v := p.db.census()
	v["display"] = display
	v["mysql"] = map[string]string{"asdf": esc(p.Post("asdf"))}
	v["regionlist"] = shuffledOptions(baseRegions, []int64{1, 2, 3, 4}, p.Post("region"))
	v["subregionlist"] = shuffledOptions(baseSubregions, []int64{1, 2, 3}, p.Post("subregion"))
	p.Echo(newUserHTML, v)
}

// createUserNation finishes signing up: the first nation, the session and
// the news item.
func (p *Page) createUserNation(userID int64, username, nationName, ip string) {
	nationID := p.db.Exec(`INSERT INTO nations (name, description, user_id, region, subregion, creationdate)
		VALUES (?, ?, ?, ?, ?, NOW())`, nationName, strings.TrimSpace(p.Post("nationdescription")), userID, p.PostI("region"), p.PostI("subregion"))
	p.app.sessions.Regenerate(p.w, p.sess)
	p.sess.Set("user_id", userID)
	p.sess.Set("nation_id", nationID)
	p.db.Exec("INSERT INTO logins (username, user_id, ip, ip2, forwarded, forwarded_for, logindate, failed) VALUES (?, ?, ?, '', '', '', NOW(), false)", username, userID, ip)
	p.db.news(fmt.Sprintf(`The user <a href="viewuser.php?user_id=%d">%s</a> has joined the game with the nation of <a href="viewnation.php?nation_id=%d">%s</a>.`,
		userID, username, nationID, nationName))
	if p.app.cfg.TestServer() {
		giveTestServerResources(p.db, nationID)
	}
	p.Redirect("overview.php")
}

// toLatin1 re-encodes a UTF-8 string as ISO-8859-1 bytes, for accounts
// whose password was set from a Latin-1 form.
func toLatin1(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 256 {
			b = append(b, byte(r))
		} else {
			b = append(b, '?')
		}
	}
	return string(b)
}

// giveTestServerResources makes test-server accounts rich enough to try anything.
func giveTestServerResources(db *DB, nationID int64) {
	for _, id := range []int64{1, 2, 3, 4, 9, 10, 13, 18, 20, 25, 26, 27, 28, 29, 30, 42, 47, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 75, 77} {
		db.Exec("INSERT INTO resources (nation_id, resource_id, amount, disabled) VALUES (?, ?, 1000000, 0)", nationID, id)
	}
	db.Exec("UPDATE nations SET age = 30, funds = 100000000000000 WHERE nation_id = ?", nationID)
}

const newUserHTML = `Hold it! Have you read the <a href="rules.php">Rules</a> and the <a href="https://docs.google.com/document/u/0/d/1jSinNyYJCHkoDvQgaJkD_z2g9SV5G6v9gsKI-nwmrYM/mobilebasic">Guide</a>?
<form name="newuser" method="post" action="newuser.php" role="form">
<input type="hidden" name="token_newuser" value="{$_SESSION['token_newuser']}"/>
Leave this field blank (stops crawler spambots) <input name="username" maxlength="25"/>
  <div class="form-group">
    <label for="realusername">Username</label>
    <p class="help-block">Other people can see this and it can't be changed. Choose wisely.</p>
    <input type="text" class="form-control" id="realusername" placeholder="Username" name="realusername" maxlength="25" value="{$display['realusername']}"/>
  </div>
  <div class="form-group">
    <label for="password">Password</label>
    <input type="password" class="form-control" id="password" placeholder="Password" name="password" maxlength="250"/>
  </div>
  <div class="form-group">
    <label for="confirmpassword">Confirm Password</label>
    <input type="password" class="form-control" id="confirmpassword" placeholder="Confirm Password" name="confirmpassword" maxlength="250"/>
  </div>
  <div class="form-group">
    <label for="asdf">Email</label>
    <p class="help-block">This is completely optional; there's no email confirmation.<br/>If you're under 13, leave this blank.</p>
    <input type="email" class="form-control" id="asdf" placeholder="Email" name="asdf" maxlength="128" value="{$mysql['asdf']}"/>
  </div>
  <div class="form-group">
    <label for="nationname">Nation Name</label>
    <input type="text" class="form-control" id="nationname" placeholder="Nation Name" name="nationname" maxlength="40" value="{$display['nationname']}">
  </div>
  <div class="form-group">
    <label for="nationdescription">Nation Description</label>
    <textarea class="form-control" id="nationdescription" name="nationdescription">{$display['nationdescription']}</textarea>
  </div>

  <div>
  <center>EXISTING NATIONS:</center>
  <table class="table table-striped table-bordered">
  <tr><th>REGION:</th><th>NORTH</th><th>CENTRAL</th><th>SOUTH</th><th>TOTAL</th></tr>
  <tr><th>Burrozil:</th><th>$census_burrozil_north</th><th>$census_burrozil_central</th><th>$census_burrozil_south</th><th>$census_burrozil_total</th></tr>
  <tr><th>Zebrica:</th><th>$census_zebrica_north</th><th>$census_zebrica_central</th><th>$census_zebrica_south</th><th>$census_zebrica_total</th></tr>
  <tr><th>Saddle Arabia:</th><th>$census_saddle_north</th><th>$census_saddle_central</th><th>$census_saddle_south</th><th>$census_saddle_total</th></tr>
  <tr><th>Przewalskia:</th><th>$census_prze_north</th><th>$census_prze_central</th><th>$census_prze_south</th><th>$census_prze_total</th></tr>
  </table>
  </div>

  Your Nation:
  <div class="form-group">
    <label for="region">Region</label>
    <select name="region">
			{$regionlist}
		</select>
  </div>
  <div class="form-group">
    <label for="subregion">Subregion</label>
    <select name="subregion">
			{$subregionlist}
		</select>
  </div>
<input type="submit" onclick="if (document.getElementsByName('region')[0].options[document.getElementsByName('region')[0].selectedIndex].text == 'Przewalskia') return confirm('Are you sure? Przewalskia nations are typically only needed 1 per alliance, at most. Only choose this nation type if you know what you are doing.'); else return;" class="btn btn-success" value="Join >CLOP"/>
</form>
`

func pageNoNation(p *Page) {
	p.needsUser()
	if n := p.db.One("SELECT nation_id FROM nations WHERE user_id = ?", p.UserID()); n != nil {
		p.sess.Set("nation_id", n.I("nation_id"))
		p.Redirect("overview.php")
	}
	p.csrf("nonation")
	if p.HasErrors() && p.isPost {
		p.errors = []string{"Try again at trying again."}
	}
	display := map[string]string{}
	for k := range p.form {
		display[k] = esc(p.Post(k))
	}
	if !p.HasErrors() && p.isPost {
		name := strings.TrimSpace(p.Post("nationname"))
		p.validNewNation(p.Post("nationname"), p.Post("region"), p.UserID())
		if !p.HasErrors() {
			nationID := p.db.Exec(`INSERT INTO nations (name, description, user_id, region, subregion, creationdate)
				VALUES (?, ?, ?, ?, ?, NOW())`, name, strings.TrimSpace(p.Post("nationdescription")), p.UserID(), p.PostI("region"), p.PostI("subregion"))
			p.sess.Set("nation_id", nationID)
			u := p.db.One("SELECT username FROM users WHERE user_id = ?", p.UserID())
			p.db.news(fmt.Sprintf(`The user <a href="viewuser.php?user_id=%d">%s</a> has created the new nation of <a href="viewnation.php?nation_id=%d">%s</a>.`,
				p.UserID(), u.S("username"), nationID, name))
			p.Redirect("overview.php")
		}
	}
	p.Echo(`<center><h4>You have lost your last nation! Try again!</h4></center><br/>
<center><form name="newuser" method="post" action="nonation.php">
<input type="hidden" name="token_nonation" value="{$_SESSION['token_nonation']}"/>
<table>
<tr><td>Nation Name</td><td><input name="nationname" class="form-control" maxlength="40" value="{$display['nationname']}"/></td></tr>
<tr><td>Nation Description</td><td><textarea name="nationdescription" class="form-control">{$display['nationdescription']}</textarea></td></tr>
<tr><td>Region</td><td><select name="region" class="form-control">
{$regions}
</select></td></tr>
<tr><td>Subregion</td><td><select name="subregion" class="form-control">
{$subregions}
</select></td></tr>
</table>
<input type="submit" class="btn btn-success" value="Rise From the Ashes"/>
</form></center>`, V{
		"display":    display,
		"regions":    shuffledOptions(baseRegions, []int64{1, 2, 3, 4}, p.Post("region")),
		"subregions": shuffledOptions(baseSubregions, []int64{1, 2, 3}, p.Post("subregion")),
	})
}
