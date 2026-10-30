package clop

import (
	"fmt"
	"strings"
)

func pageRequestFeature(p *Page) {
	p.needsUser()
	db, uid := p.db, p.UserID()
	keep := false
	if p.Post("action") == "Submit" {
		isBug := p.Post("isbug") == "on"
		switch {
		case p.Post("token_featureform") != p.token("featureform"):
			p.Err("Try Again.")
			keep = true
		case p.Post("title") == "":
			p.Err("Must specify a title.")
		}
		recent := db.One(`SELECT COUNT(*) AS count FROM requests WHERE submitter = ? AND submitdate >= DATE_SUB(NOW(), INTERVAL 1 WEEK) AND isbug = ?`,
			uid, isBug).I("count")
		if recent >= 15 && !p.isAdmin() {
			p.Err("Wait wait wait. That's a lot of requests for one week.")
		}
		if !p.HasErrors() {
			if db.ExecOK("INSERT INTO requests (submitter, title, description, isbug, submitdate) VALUES (?, ?, ?, ?, NOW())",
				uid, p.Post("title"), p.Post("description"), isBug) {
				p.Info("Request submitted.")
			} else {
				p.Err("Something went wrong saving your request.")
				keep = true
			}
		}
	}
	p.sess.Set("token_featureform", newToken())
	title, desc, checked := "", "", ""
	if keep {
		title, desc = esc(p.Post("title")), esc(p.Post("description"))
		if p.Post("isbug") == "on" {
			checked = "checked"
		}
	}
	p.Echo(`<style>
#submitbtn {
padding-left: 20px;
padding-right: 20px;
}
#isbug {
margin-left: 10px;
margin-right: 10px;
}
</style>
<center>
<h3>Feature Requests</h3>
</center>
Your request will be reviewed, and if it's OK, it will be made into a poll, where users can vote if they would want to see your idea implemented in game or not.</br>
Submit your ideas about looks, game mechanics, rules, bugs, users, admins and everything!
</br></br></br>
<div class="row">
<form name="feature-form" action="requestfeature.php" method="post" class="form">
<input type="hidden" name="token_featureform" value="{$token}">
<div class="form-group">
<div class="form-group">
<label>Poll Title</label>
<input type="text" name="title" placeholder="General idea of your feature" class="form-control" value="{$title}">
</div>
<div class="form-group">
<label>Description</label>
<textarea rows="5" name="description" placeholder="More elaborate description" class="form-control">{$desc}</textarea>
</div>
<div class="form-group">
<p class="pull-left">Poll voting options will be chosen accordingly. If you have a preference, outline them in description.</p>
<div class="pull-right">
<label for="isbug">Bug?</label>
<input type="checkbox" name="isbug" id="isbug" {$checked}>
<input type="submit" name="action" value="Submit" class="btn btn-success" id="submitbtn">
</div>
</div>
</div>
</form>
</div>
`, V{"token": p.token("featureform"), "title": title, "desc": desc, "checked": checked})
}

var pollColors = []string{"#FF0000", "#00FF00", "#0000FF"}

func pagePolls(p *Page) {
	p.title = "Polls - "
	p.needsUser()
	db, uid := p.db, p.UserID()
	if pollID := p.PostI("vote"); p.Post("vote") != "" {
		key := fmt.Sprintf("tokenfor_%d", pollID)
		option := p.PostI("option")
		switch {
		case p.Post(key) == "" || p.Post(key) != p.sess.Get(key):
			p.Err("Try Again.")
		case db.One("SELECT request_id FROM requests WHERE request_id = ? AND voteable = 1", pollID) == nil,
			db.One("SELECT optid FROM poll_options WHERE optid = ? AND poll_id = ?", option, pollID) == nil:
			p.Err("That poll isn't open.")
		default:
			if db.ExecOK("INSERT INTO votes (`poll_id`, `user_id`, `option`, `date`) VALUES (?, ?, ?, NOW())", pollID, uid, option) {
				p.Info("Thank you for voting.")
			} else {
				p.Err("You already voted in that poll.")
			}
		}
	}
	allUsers := db.One("SELECT COUNT(*) AS numusers FROM users WHERE stasismode = '0'").I("numusers")

	var b strings.Builder
	b.WriteString(pollsHeadHTML)
	polls := db.Query(`SELECT rq.request_id, rq.submitter, rq.title, rq.description, rq.submitdate, rq.voteable, u.username
		FROM requests rq INNER JOIN users u ON rq.submitter = u.user_id WHERE rq.visible = '1' AND rq.isbug = '0' ORDER BY rq.submitdate DESC`)
	if len(polls) == 0 {
		b.WriteString("</br>There are currently no active polls.")
		p.Raw(b.String())
		return
	}
	fmt.Fprintf(&b, `<div style="display: none;" id="allusers" data-allusers="%d"></div>`, allUsers)
	for _, r := range polls {
		fmt.Fprintf(&b, `<div class="row well">
<span>Suggested by: <a href="viewuser.php?user_id=%s">%s</a></span><span class="pull-right">Submit date: %s</span></br>
<span><b>%s</b></span><span class="pull-right"><span name="totalvotes-here"></span> Votes</span></br><span class="pull-right">Voter turnout: <b><span name="turnout-here"></span>%%</b></span></br>
<button class="btn btn-success" onclick="this.nextElementSibling.className = 'well visible'; this.style.display = 'none';">Show More</button>
<div class="well invisible" name="poll-content">
<div>%s</div></br>
`, r.S("submitter"), r.S("username"), r.S("submitdate"), esc(r.S("title")), nl2br(esc(r.S("description"))))
		b.WriteString(pollBody(p, r.I("request_id"), r.B("voteable")))
		b.WriteString("</div></div></br>")
	}
	p.Raw(b.String())
}

// pollBody shows the voting form if the player hasn't voted on an open
// poll, and the results otherwise.
func pollBody(p *Page, pollID int64, voteable bool) string {
	db, uid := p.db, p.UserID()
	var b strings.Builder
	if voteable && db.One("SELECT 1 AS x FROM votes WHERE user_id = ? AND poll_id = ?", uid, pollID) == nil {
		key := fmt.Sprintf("tokenfor_%d", pollID)
		p.sess.Set(key, newToken())
		fmt.Fprintf(&b, `<div><form action="polls.php" method="post" name="vote_poll_%d">
<input type="hidden" name="%s" value="%s">
<input type="hidden" name="vote" value="%d">
`, pollID, key, p.sess.Get(key), pollID)
		for _, o := range db.Query("SELECT optid, opttext FROM poll_options WHERE poll_id = ?", pollID) {
			fmt.Fprintf(&b, `<input type="radio" name="option" value="%s" onclick="this.form.submit()">
<span>%s</span></br>
`, o.S("optid"), esc(o.S("opttext")))
		}
		b.WriteString("</form></div>")
		fmt.Fprintf(&b, `<div style="display: none;" name="poll-results" data-totalvotes="%d"></div>`,
			db.One("SELECT COUNT(*) AS numvotes FROM votes WHERE poll_id = ?", pollID).I("numvotes"))
		return b.String()
	}
	if !voteable {
		b.WriteString("<b>Poll closed.</b></br>")
	}
	results := db.Query(`SELECT v.option, COUNT(*) AS num, o.opttext AS text, MAX(v.user_id = ?) AS mine
		FROM votes v INNER JOIN poll_options o ON v.option = o.optid WHERE v.poll_id = ?
		GROUP BY v.option, o.opttext ORDER BY num DESC, v.option ASC`, uid, pollID)
	var total float64
	for _, r := range results {
		total += r.F("num")
	}
	fmt.Fprintf(&b, `<div name="poll-results" data-totalvotes="%s">`, phpFloat(total))
	var sum float64
	for i, r := range results {
		color := pollColors[i%len(pollColors)]
		width := r.F("num") / total * 100
		offset := sum / width * -100
		sum += width
		vote := ""
		if r.B("mine") {
			vote = "(Voted)"
		}
		fmt.Fprintf(&b, `<div style="width: %s%%; height: 20px; display: inline-block;" data-desc="%s" data-vote="%s" data-numvotes="%s" data-percentage="%s" onmouseover="extrainfo(event)" onmouseleave="hideinfo(event)">
<div class="growable" style="background: %s; color: %s; --my-width: %s; --left-offset: %s%%;">
<p style="text-align: center;">%d%%</p></div></div>
`, phpFloat(width), esc(r.S("text")), vote, r.S("num"), phpFloat(width), color, color, phpFloat(width), phpFloat(offset), int(width))
	}
	b.WriteString(`</div></br><div name="extended-desc"></div>`)
	return b.String()
}

const pollsHeadHTML = `<style>
.growable {
	position: relative;
}
.growable:hover {
	width: calc((100 / var(--my-width)) * 100%);
	left: var(--left-offset) !important;
	color: #000000 !important;
	z-index: 2;
	position: relative;
	left: 0px;
}
#option-tooltip {
	position: absolute;
	z-index: 2;
	color: white;
}
.invisible {
	display: none;
}
.visible {
	display: block;
}
div[name="poll-results"] {
	border: solid 1px;
	border-radius: 3px;
	border-color: white;
}
</style>
<script>
function extrainfo(e) {
var dat = e.target.parentElement.parentElement.dataset;
var elem = $('div[name="extended-desc"]', e.target.parentElement.parentElement.parentElement.parentElement)[0];
suffix = (dat.numvotes == 1) ? ' Vote</b></span>' : ' Votes</b></span>';
elem.innerHTML = dat.desc + ' <b>' + dat.vote + '</b><span class="pull-right"><b>' + dat.numvotes + suffix;
}

function hideinfo(e) {
var elem = $('div[name="extended-desc"]', e.target.parentElement.parentElement.parentElement)[0];
elem.innerText = '';
}

window.addEventListener('load', function () {
var allusers = document.getElementById('allusers');
if (!allusers) return;
allusers = allusers.dataset.allusers;
var polls = $$('div.row.well');
var i, votes, el, t;
for (i = 0; i < polls.length; ++i) {
	votes = $('div[name="poll-results"]', polls[i])[0].dataset.totalvotes;
	el = $('span[name="totalvotes-here"]', polls[i])[0];
	el.innerText = votes;
	(votes == 1) ? el.nextSibling.textContent = " Vote" : 0;
	t = $('span[name="turnout-here"]', polls[i])[0];
	t.innerText = parseInt(((votes / allusers) * 100), 10);
}
});

function $$ (selector, el) {
    if (!el) {el = document;}
    return el.querySelectorAll(selector);
}
</script>
<center>
<h3>Polls</h3>
</center>
<b>You are allowed to vote once per each poll, and you cannot change your vote. Your vote is cast automatically when you select appropriate option, so be careful!</b>
</br>Also note that the poll result isn't binding. The polls may last for indefinite time period.
</br>I will try and implement features based on how many people want them. So when there are multiple polls for features, try and only vote on what you want done the most
</br>
`

// pageMakePolls is the admin page for turning feature requests into polls.
func pageMakePolls(p *Page) {
	if !p.isAdmin() {
		p.Redirect("index.php")
	}
	db := p.db
	var b strings.Builder
	pollID := toInt(p.GP("poll_id"))
	switch {
	case p.Post("newpoll") == "New Poll":
		for _, opt := range p.form["options[]"] {
			if strings.TrimSpace(opt) != "" {
				db.Exec("INSERT INTO poll_options (poll_id, opttext) VALUES (?, ?)", pollID, opt)
			}
		}
		q, args := "UPDATE requests SET visible = '1', voteable = '1'", []any{}
		if p.Post("unchanged") != "on" {
			q += ", title = ?, description = ?"
			args = append(args, p.Post("title"), p.Post("desc"))
			if p.Post("preservedate") != "on" {
				if p.Post("newdate") != "" {
					q += ", submitdate = ?"
					args = append(args, p.Post("newdate"))
				} else {
					q += ", submitdate = NOW()"
				}
			}
		}
		db.Exec(q+" WHERE request_id = ?", append(args, pollID)...)
		db.systemMessage(toInt(p.Post("submitter")), "Your Request was accepted and made into a poll.")
		b.WriteString("Success.")
	case p.Get("edit") == "Pollify":
		r := db.One("SELECT * FROM requests WHERE request_id = ?", pollID)
		fmt.Fprintf(&b, `<div><hr><form action="makepolls.php" method="post">
By: %s<input type="hidden" name="submitter" value="%s"></br>
On: <input type="text" name="newdate" value="%s" placeholder="leave empty for NOW()"> <label for="preservedate">Preserve: </label><input type="checkbox" name="preservedate" checked id="preservedate"></br>
Title: <input type="text" name="title" value="%s"></br>
Desc:
<textarea name="desc">%s</textarea></br>
Options:
<div id="options-container">
<input type="text" name="options[]"></br>
<button onclick="addoption(event)">Add Option</button></br>
</div>
<input type="hidden" name="poll_id" value="%s">
<label for="unchanged">Unchanged: </label><input type="checkbox" name="unchanged" checked id="unchanged">
<input type="submit" name="newpoll" value="New Poll">
</form>
<script>
function addoption(e) {
e.preventDefault();
var field = document.createElement('input');
field.type = 'text';
field.name = 'options[]';
var eol = document.createElement('br');
e.target.parentElement.insertBefore(field, e.target);
e.target.parentElement.insertBefore(eol, e.target);
}
</script>
<hr></div>
`, r.S("submitter"), r.S("submitter"), esc(r.S("submitdate")), esc(r.S("title")), esc(r.S("description")), r.S("request_id"))
	case p.Get("takedown") == "Deactivate":
		db.Exec("UPDATE requests SET visible = 0, voteable = 0 WHERE request_id = ?", pollID)
		b.WriteString("Voting closed.</br>Poll deactivated.")
	case p.Get("close") == "Close Voting":
		db.Exec("UPDATE requests SET voteable = 0 WHERE request_id = ?", pollID)
		b.WriteString("Voting closed.")
	default:
		for _, r := range db.Query("SELECT r.*, u.username FROM requests r INNER JOIN users u ON r.submitter = u.user_id ORDER BY r.request_id DESC") {
			fmt.Fprintf(&b, `<div><hr>
By: <a href="viewuser.php?user_id=%s">%s</a></br>
On: %s</br>
Title: %s</br>
Desc:
<p style="white-space: pre;">%s</p>
Isbug: %s</br>
<form action="makepolls.php" method="get">
<input type="hidden" name="poll_id" value="%s">
<input type="submit" name="edit" value="Pollify">
`, r.S("submitter"), esc(r.S("username")), esc(r.S("submitdate")), esc(r.S("title")), esc(r.S("description")), r.S("isbug"), r.S("request_id"))
			if r.B("visible") {
				b.WriteString(`<input type="submit" name="takedown" value="Deactivate">`)
			}
			if r.B("voteable") {
				b.WriteString(`<input type="submit" name="close" value="Close Voting">`)
			}
			b.WriteString("</form>\n<hr></div></br>\n")
		}
	}
	p.Raw(b.String())
}

// pageAdminKill deletes a player outright.
func pageAdminKill(p *Page) {
	if !p.isAdmin() {
		return
	}
	db := p.db
	p.Raw(`<form action="adminkilleveryone.php" method="post">
<input name="user_id"/>
<input type="submit" value="Kill"/>
</form>
`)
	target := p.PostI("user_id")
	if target == 0 {
		return
	}
	u := db.One("SELECT username FROM users WHERE user_id = ?", target)
	if u == nil {
		p.Raw("No such user.")
		return
	}
	db.news("The admin has disposed of " + u.S("username") + "!")
	for _, n := range db.Query("SELECT nation_id FROM nations WHERE user_id = ?", target) {
		db.deleteNation(n.I("nation_id"))
	}
	db.Exec("DELETE FROM embargoes WHERE embargoer = ? OR embargoee = ?", target, target)
	db.Exec("DELETE FROM messages WHERE fromuser = ? OR touser = ?", target, target)
	db.Exec("DELETE FROM users WHERE user_id = ?", target)
	p.Raw(fmt.Sprintf("User %d has been killed.", target))
}

func pageChat(p *Page) {
	p.title = "Chat - "
	p.sess.Set("token_chat", newToken())
	p.Raw(fmt.Sprintf(`<script type="text/javascript" src="js/chat.js"></script>
<div id="messages" style="height: 400px; overflow: auto;"><input type="hidden" id="lastmessage" value="0"/></div>
<form class="form-inline" role="form" action="" onsubmit="addmessage(); return false;">
<input type="hidden" id="token" class="form-control" value="%s"/>
<div class="input-group">
<input type="text" id="message" class="form-control"/>
<span class="input-group-btn">
<input type="submit" value="Send" class="btn btn-success"/>
</span>
</div>
</form>
<script>getmessages(false); setInterval("getmessages(false);", 5000);</script>`, p.token("chat")))
}

func pageGetMessages(p *Page) {
	db := p.db
	if p.Get("token") == "" || p.Get("token") != p.token("chat") {
		p.Raw("Log in to chat, and only have one chat window open at a time!")
		return
	}
	if uid := p.UserID(); uid != 0 {
		db.Exec("UPDATE users SET lastactive = NOW() WHERE user_id = ?", uid)
	}
	last := db.One("SELECT MAX(message_id) AS lastmessage FROM chat").I("lastmessage")
	since := toInt(p.Get("lastmessage"))
	if since == last {
		p.w.WriteHeader(304)
		return
	}
	var b strings.Builder
	for _, m := range db.Query(`SELECT c.*, u.username, DATE_FORMAT(c.posted, '%a %H:%i:%s') AS shortdate FROM chat c
		INNER JOIN users u ON u.user_id = c.user_id WHERE c.message_id > ? ORDER BY posted`, since) {
		fmt.Fprintf(&b, "[%s] <a href=\"viewuser.php?user_id=%s\">%s</a>: %s<br/>\n", m.S("shortdate"), m.S("user_id"), m.S("username"), esc(m.S("message")))
	}
	fmt.Fprintf(&b, `<input type="hidden" id="lastmessage" value="%d"/>`, last)
	p.Raw(b.String())
}

func pageAddMessage(p *Page) {
	db, uid := p.db, p.UserID()
	msg := p.Get("message")
	if uid == 0 || msg == "" || p.Get("token") != p.token("chat") {
		return
	}
	if db.One("SELECT message_id FROM chat WHERE user_id = ? AND posted > DATE_SUB(NOW(), INTERVAL 1 SECOND)", uid) != nil {
		return
	}
	db.Exec("INSERT INTO chat (message, posted, user_id) VALUES (?, NOW(), ?)", msg, uid)
	db.Exec("DELETE FROM chat WHERE posted <= DATE_SUB(NOW(), INTERVAL 1 WEEK)")
}
