package clop

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

const footerHTML = `</div>
</div>
</body>
</html>`

// header renders the page head, banner, navbar and any error/info boxes.
func (p *Page) header() string {
	db := p.db
	v := V{"extratitle": p.title}

	top := db.One("SELECT message FROM topmessage ORDER BY time DESC LIMIT 1")
	v["topmessage"] = esc(top.S("message"))

	uid := p.UserID()
	if uid != 0 {
		v["messagenumber"] = countBadge(db.One("SELECT COUNT(*) AS number FROM messages WHERE touser = ? AND todeleted = 0 AND is_read = 0", uid).I("number"))
		polls := db.One(`SELECT COUNT(*) AS pollcount FROM requests WHERE visible = '1' AND voteable = '1' AND isbug = '0'
			AND request_id NOT IN (SELECT poll_id FROM votes WHERE user_id = ?)`, uid).I("pollcount")
		if polls > 0 {
			v["pollcount"] = fmt.Sprintf("(%d)", polls)
		}
		user := db.One("SELECT alliance_id, alliance_messages_last_checked FROM users WHERE user_id = ?", uid)
		v["alliancemessagenumber"] = countBadge(db.One("SELECT COUNT(*) AS number FROM alliance_messages WHERE alliance_id = ? AND posted > ?",
			user.I("alliance_id"), user.S("alliance_messages_last_checked")).I("number"))
		db.Exec("UPDATE users SET lastactive = NOW() WHERE user_id = ?", uid)

		if nid := p.NationID(); nid != 0 {
			v["dealnumber"] = countBadge(db.One("SELECT COUNT(*) AS number FROM deals WHERE tonation = ? AND finalized = '1'", nid).I("number"))
			v["incomingnumber"] = countBadge(db.One(`SELECT COUNT(*) AS number FROM forcegroups
				WHERE destination_id = ? AND attack_mission = 1 AND departuredate IS NOT NULL`, nid).I("number"))
			nations := db.Query("SELECT nation_id, name FROM nations WHERE user_id = ?", uid)
			v["nationline"] = nationPicker(nations, nid)
		}
	}

	headernumber := rand.Intn(16) + 1
	ext := "png"
	if headernumber == 2 || headernumber == 4 {
		ext = "gif"
	}
	v["headernumber"] = headernumber
	v["headerextension"] = ext

	now := time.Now().UTC()
	v["currenttime"] = now.Format("2006-01-02 15:04:05")
	timer := 7200 - now.Unix()%7200
	v["timer"] = timer
	v["countdowntimer"] = fmt.Sprintf("%d:%02d:%02d", timer/3600, timer%3600/60, timer%60)

	switch p.sess.Get("css") {
	case "", "0":
		v["stylesheets"] = `<link href="css/slate.min.css" rel="stylesheet" media="Screen">`
	case "1":
		v["stylesheets"] = `<link href="css/cyborg.min.css" rel="stylesheet" media="Screen">`
		v["internal"] = `.form-control{
    background-color: #555;
    color: #eee;
    }`
	default:
		v["stylesheets"] = `<link href="css/bootstrap.min.css" rel="stylesheet" media="Screen">
    <link rel="stylesheet" href="css/bootstrap-theme.min.css">`
	}
	if truthy(p.sess.Get("hidereports")) {
		v["reportcss"] = `.report-showbutton{display: block;}
    .report-details{display: none;}`
	} else {
		v["reportcss"] = `.report-showbutton{display: none;}
    .report-details{display: block;}`
	}
	v["errormessages"] = alertBox("danger", "error", p.errors)
	v["infomessages"] = alertBox("info", "info", p.infos)

	var b strings.Builder
	b.WriteString(tpl(headHTML, v))
	if !truthy(p.sess.Get("hidebanners")) {
		b.WriteString(tpl(`<center><img src="images/{$headernumber}.{$headerextension}" height="250"/></center>`, v))
	}
	b.WriteString(tpl(communityHTML, v))
	if uid != 0 {
		b.WriteString(tpl(navLoggedInHTML, v))
	} else {
		b.WriteString(tpl(navLoggedOutHTML, v))
	}
	return b.String()
}

func countBadge(n int64) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d)", n)
}

func alertBox(kind, class string, msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<div class=\"alert alert-%s\">\n", kind)
	for _, m := range msgs {
		fmt.Fprintf(&b, "<div class=\"%s\">%s</div>\n", class, m)
	}
	b.WriteString("</div>")
	return b.String()
}

func nationPicker(nations []Row, current int64) string {
	if len(nations) == 0 {
		return ""
	}
	if len(nations) == 1 {
		return "<li><a>" + nations[0].S("name") + "</a></li>"
	}
	var b strings.Builder
	b.WriteString(`<li><a><form action="" method="post"><select name="switchnation_id" onchange='this.form.submit()'>`)
	for _, n := range nations {
		sel := ""
		if n.I("nation_id") == current {
			sel = " selected "
		}
		fmt.Fprintf(&b, `<option value="%d"%s>%s</option>`, n.I("nation_id"), sel, n.S("name"))
	}
	b.WriteString(`</select></form></a></li>`)
	return b.String()
}

const headHTML = `<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">
<html>
<head>
    <link rel="icon" type="image/png" href="favicon.png" />
    {$stylesheets}
    <script src="js/jquery-1.10.2.min.js"></script>
    <script src="js/bootstrap.min.js"></script>
    <script src="js/masonry.pkgd.min.js"></script>
    <style type="text/css">

ul.nav li.dropdown:hover > ul.dropdown-menu {
display: block;
}
span.spoiler {
color: #000000;
background: #000000;
}

span.spoiler:hover {
color: #FFFFFF;
}
        {$internal}
        {$reportcss}
        #content{
            min-width: 500px;
            max-width: 95%;
            margin-left: auto;
            margin-right: auto;
            padding: 40px
        }
        #content-outer{

        }
        .text-width-micro{
            width: 64px;
        }
    </style>

    <script type="text/javascript">
      function doCountdownTick(countdown_seconds){
        if(countdown_seconds == 0)
          countdown_seconds = 60 * 60 * 2;

        var hours = Math.floor(countdown_seconds / 60 / 60);
        var mins = Math.floor(countdown_seconds / 60) - (hours * 60);
        var secs = countdown_seconds % 60;

        var countdownstring =
          hours + ":" +
          ("0" + mins).slice(-2) + ":" +
          ("0" + secs).slice(-2);
        document.getElementById("countdown").innerHTML = countdownstring;

        countdown_seconds--;
        setTimeout("doCountdownTick(" + countdown_seconds + ")", 1000);
      }
      window.onload = function(){
        doCountdownTick({$timer});
      }
    </script>

    <title>{$extratitle}&gt;CLOP, the game of geoponitics</title>
</head>
<body alink="#00ae0e" link="#00ae0e" vlink="#1f8001">
<meta content="text/html; charset=utf-8" http-equiv="content-type">
<div id="content-outer">
`

const communityHTML = `
<center><span id="topmessage"><a href="topmessage.php">{$topmessage}</a></span></center>
<center>&gt;4CLOP Community: <a href="https://boards.4channel.org/mlp/catalog#s=4clop" target="_new_win">/MLP/ Thread</a> | <a href="https://irc.4clop.com" target="_new_win">IRC Chat (join without a password initially)</a> | <a href="https://github.com/ergochat/ergo/blob/stable/docs/USERGUIDE.md#account-registration" target="_new_win">IRC Guide</a> &nbsp;&nbsp;&nbsp;&nbsp;</center>
`

const navLoggedInHTML = `<nav class="navbar navbar-default" role="navigation">
<div class="navbar-header">
</div>
<ul class="nav navbar-nav">
  <li><a class="dropdown-toggle" href="messages.php">Messages <span class="badge">{$messagenumber}</span></a></li>
  <li class="dropdown">
    <a href="#" class="dropdown-toggle" data-toggle="dropdown">User <b class="caret"></b></a>
    <ul class="dropdown-menu">
      <li><a href="userinfo.php">User Info</a></li>
      <li><a href="transfer.php">Empire Transfers</a></li>
      <li><a href="blocklist.php">Blocklist</a></li>
      <li><a href="friends.php">Friends/Enemies</a></li>
      <li><a href="logout.php">Logout</a></li>
    </ul>
  </li>
  <li class="dropdown">
  <a href="#" class="dropdown-toggle" data-toggle="dropdown">Alliance <span class="badge">{$alliancemessagenumber}</span><b class="caret"></b></a>
  <ul class="dropdown-menu">
    <li><a href="myalliance.php">My Alliance<span class="badge">{$alliancemessagenumber}</span></a></li>
    <li><a href="alliances.php">Alliances</a></li>
    <li><a href="allianceattacks.php">Alliance Attacks</a></li>
  </ul>
</li>
  <li class="dropdown">
    <a href="#" class="dropdown-toggle" data-toggle="dropdown">Nation <b class="caret"></b></a>
    <ul class="dropdown-menu">
    <li><a href="overview.php">Overview</a></li>
      <li><a href="empireoverview.php">Empire Overview - STOCK</a></li>
      <li><a href="empireoverview2.php">Empire Overview - NET</a></li>
      <li><a href="reports.php">Reports</a></li>
      <li><a href="actions.php">Actions</a></li>
      <li><a href="favoriteactions.php">Favorite Actions</a></li>
      <li><a href="majoractions.php">Major Actions</a></li>
    </ul>
  </li>
  <li class="dropdown">
    <a href="#" class="dropdown-toggle" data-toggle="dropdown">Capitalism <span class="badge">{$dealnumber}</span><b class="caret"></b></a>
    <ul class="dropdown-menu">
      <li><a href="marketplace.php">Resources Marketplace</a></li>
      <li><a href="buyermarketplace.php">Buyer's Resources Marketplace</a></li>
      <li><a href="deals.php">Deals <span class="badge">{$dealnumber}</span></a></li>
      <li><a href="marketplace.php?mode=weapons">Weapons Marketplace</a></li>
      <li><a href="buyermarketplace.php?mode=weapons">Buyer's Weapons Marketplace</a></li>
      <li><a href="marketplace.php?mode=armor">Armor Marketplace</a></li>
      <li><a href="buyermarketplace.php?mode=armor">Buyer's Armor Marketplace</a></li>
      <li><a href="myoffers.php">My Offers</a></li>
      <li><a href="embargoes.php">Embargoes</a></li>
    </ul>
  </li>

  <li class="dropdown">
    <a href="#" class="dropdown-toggle" data-toggle="dropdown">War <span class="badge">{$incomingnumber}</span><b class="caret"></b></a>
    <ul class="dropdown-menu">
      <li><a href="forcesyourway.php">Incoming <span class="badge">{$incomingnumber}</span></a></li>
      <li><a href="makeequipment.php?mode=weapons">Make Weapons</a></li>
      <li><a href="makeequipment.php?mode=armor">Make Armor</a></li>
      <li><a href="createforces.php">Create Forces</a></li>
      <li><a href="equipforces.php">Equip Forces</a></li>
      <li><a href="groupforces.php">Group Forces</a></li>
      <li><a href="sendforces.php">Send Forces</a></li>
    </ul>
  </li>

</ul>
<ul class="nav navbar-nav navbar-right pull-right">
  {$nationline}
  <li><a>Server time: {$currenttime}</a></li>
  <li><a>Next tick: <span class="text-danger" id="countdown">{$countdowntimer}</span></a></li>

  <li class="dropdown"><a href="#" class="dropdown-toggle" data-toggle="dropdown">Search <b class="caret"></b></a>
      <ul class="dropdown-menu">
          <li><a href="search.php">Search User or Nation</a></li>
          <li><a href="rankings.php?mode=allianceless">Allianceless Players</a></li>
          <li><a href="rankings.php?mode=burrozil">Burrozils</a></li>
          <li><a href="rankings.php?mode=zebrica">Zebricas</a></li>
          <li><a href="rankings.php?mode=saddle">Saddle Arabians</a></li>
          <li><a href="rankings.php?mode=przewalskia">Przewalskia</a></li>
      </ul>
      </li>

  <li><a href="statistics.php">Stats</a></li>
  <li class="dropdown"><a href="#" class="dropdown-toggle" data-toggle="dropdown">Rankings <b class="caret"></b></a>
  <ul class="dropdown-menu">
      <li><a href="rankings.php?mode=gdp">GDP Per Turn</a></li>
      <li><a href="rankings.php?mode=statues">Statues</a></li>
      <li><a href="rankings.php?mode=longevity">Longevity</a></li>
      <li><a href="empirerankings.php">Empire Size</a></li>
      <li><a href="ascensions.php">Ascensions</a></li>
      <li><a href="graveyard.php">Graveyard</a></li>
      </ul>
      </li>
      <li class="dropdown">
      <a class="dropdown-toggle" href="#">Feedback <span class="badge">{$pollcount}</span> <b class="caret"></b></a>
      <ul class="dropdown-menu">
        <li><a href="requestfeature.php">Request Feature/Submit Bug</a></li>
        <li><a href="polls.php">Polls <span class="badge">{$pollcount}</span></a></li>
      </ul>
      </li>
      <li><a href="news.php">News</a></li>
      <li class="dropdown"><a href="#" class="dropdown-toggle" data-toggle="dropdown"><font color="orange">Guides </font><b class="caret"></b></a>
      <ul class="dropdown-menu">
        <li><a href="rules.php">Rules</a></li>
        <li><a href="https://docs.google.com/document/u/0/d/1jSinNyYJCHkoDvQgaJkD_z2g9SV5G6v9gsKI-nwmrYM/mobilebasic" target="_new_win">Recommended Guide</a></li>
        <li><a href="images/the_chart.png" target="_new_win">Enviro Loss Chart</a></li>
        <li><a href="guide.php">Another Guide</a></li>
        <li><a href="warguide.php">War Guide</a></li>
        <li><a href="https://files.catbox.moe/iqa8bn.txt">Another War Guide</a></li>
        <li><a href="https://files.catbox.moe/akfmdc.png">Unit Comparison Table</a></li>
      </ul>
</ul>
<div class="navbar-header"></div>
</nav>
<div id="content">
{$errormessages}
{$infomessages}
`

const navLoggedOutHTML = `  <nav class="navbar navbar-default" role="navigation">
    <div class="navbar-header">
    <a class="navbar-brand" href="index.php">&gt;CLOP</a>
    </div>
    <ul class="nav navbar-nav navbar-right pull-right">
      <li><a>Server time: {$currenttime}</a></li>
      <li><a>Next turn: <span class="text-danger" id="countdown"></span></a></li>
      <li><a href="search.php">Search</a></li>
      <li><a href="statistics.php">Stats</a></li>
      <li class="dropdown"><a href="#" class="dropdown-toggle" data-toggle="dropdown">Rankings <b class="caret"></b></a>
      <ul class="dropdown-menu">
          <li><a href="rankings.php?mode=gdp">GDP Per Turn</a></li>
          <li><a href="rankings.php?mode=statues">Statues</a></li>
          <li><a href="rankings.php?mode=longevity">Longevity</a></li>
          <li><a href="empirerankings.php">Empire Size</a></li>
          <li><a href="ascensions.php">Ascensions</a></li>
          <li><a href="graveyard.php">Graveyard</a></li>
      </ul>
      </li>
      <li><a href="news.php">News</a></li>
      <li class="dropdown"><a href="#" class="dropdown-toggle" data-toggle="dropdown"><font color="orange">Guides </font><b class="caret"></b></a>
      <ul class="dropdown-menu">
        <li><a href="rules.php">Rules</a></li>
        <li><a href="https://docs.google.com/document/u/0/d/1jSinNyYJCHkoDvQgaJkD_z2g9SV5G6v9gsKI-nwmrYM/mobilebasic" target="_new_win">Recommended Guide</a></li>
        <li><a href="images/the_chart.png" target="_new_win">Enviro Loss Chart</a></li>
        <li><a href="guide.php">Another Guide</a></li>
        <li><a href="warguide.php">War Guide</a></li>
      </ul>
  </nav>
  <div id="content">
    <p>
      <form name="login_top" method="post" action="login.php" role="form">
        <div class="form-group">
          <label for="username">Username</label>
          <input type="text" class="form-control" id="username" placeholder="Username" name="username" maxlength="25">
        </div>
        <div class="form-group">
          <label for="password">Password</label>
          <input type="password" class="form-control" id="password" placeholder="Password" name="password" maxlength="250">
        </div>
        <span><input type="submit" value="Log in" class="btn btn-success"/> <a href="newuser.php" class="btn btn-info">New User</a></span>
      </form>
    </p>
    {$errormessages}
    {$infomessages}
`
