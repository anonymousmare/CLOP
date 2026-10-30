package clop

import (
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

// routes keeps the original .php URLs so existing links, bookmarks and the
// standalone HTML tools keep working.
func (a *App) routes() {
	pages := []struct {
		path   string
		layout bool
		h      func(*Page)
	}{
		{"/index.php", true, pageIndex},
		{"/login.php", true, pageLogin},
		{"/logout.php", false, pageLogout},
		{"/newuser.php", true, pageNewUser},
		{"/nonation.php", true, pageNoNation},

		{"/overview.php", true, pageOverview},
		{"/empireoverview.php", true, func(p *Page) { pageEmpireOverview(p, false) }},
		{"/empireoverview2.php", true, func(p *Page) { pageEmpireOverview(p, true) }},
		{"/reports.php", true, pageReports},
		{"/actions.php", true, pageActions},
		{"/favoriteactions.php", true, pageFavoriteActions},
		{"/majoractions.php", true, pageMajorActions},

		{"/marketplace.php", true, pageMarketplace},
		{"/buyermarketplace.php", true, pageBuyerMarketplace},
		{"/myoffers.php", true, pageMyOffers},
		{"/deals.php", true, pageDeals},
		{"/makedeal.php", true, pageMakeDeal},
		{"/embargoes.php", true, pageEmbargoes},
		{"/transfer.php", true, pageTransfer},

		{"/createforces.php", true, pageCreateForces},
		{"/equipforces.php", true, pageEquipForces},
		{"/groupforces.php", true, pageGroupForces},
		{"/sendforces.php", true, pageSendForces},
		{"/forcesyourway.php", true, pageForcesYourWay},
		{"/makeequipment.php", true, pageMakeEquipment},

		{"/alliances.php", true, pageAlliances},
		{"/myalliance.php", true, pageMyAlliance},
		{"/viewalliance.php", true, pageViewAlliance},
		{"/allianceattacks.php", true, pageAllianceAttacks},

		{"/messages.php", true, pageMessages},
		{"/userinfo.php", true, pageUserInfo},
		{"/blocklist.php", true, pageBlocklist},
		{"/friends.php", true, pageFriends},
		{"/viewuser.php", true, pageViewUser},
		{"/viewnation.php", true, pageViewNation},
		{"/search.php", true, pageSearch},

		{"/rankings.php", true, pageRankings},
		{"/empirerankings.php", true, pageEmpireRankings},
		{"/statistics.php", true, pageStatistics},
		{"/news.php", true, pageNews},
		{"/topmessage.php", true, pageTopMessage},
		{"/graveyard.php", true, pageGraveyard},
		{"/viewgraveyard.php", true, pageViewGraveyard},
		{"/ascensions.php", true, pageAscensions},
		{"/viewascension.php", true, pageViewAscension},

		{"/rules.php", true, pageStatic("Rules - ", contentHTML("rules"))},
		{"/guide.php", true, pageStatic("Guide - ", contentHTML("guide"))},
		{"/warguide.php", true, pageWarGuide},
		{"/purchase.php", true, pagePurchase},
		{"/polls.php", true, pagePolls},
		{"/requestfeature.php", true, pageRequestFeature},
		{"/chat.php", true, pageChat},

		{"/getmessages.php", false, pageGetMessages},
		{"/addmessage.php", false, pageAddMessage},
		{"/makepolls.php", false, pageMakePolls},
		{"/adminkilleveryone.php", false, pageAdminKill},
		{"/ascend-calc.php", false, pageAscendCalc},
		{"/huge-ovipositor-backend.php", false, pageWarCalc},
	}
	for _, pg := range pages {
		a.handle(pg.path, pg.layout, pg.h)
	}
	a.mux.HandleFunc("/", a.static)
}

// staticExt lists what may be served from the static directory. Anything
// else in it (SQL dumps, licences) stays private.
var staticExt = map[string]bool{
	".png": true, ".gif": true, ".jpg": true, ".jpeg": true, ".ico": true,
	".css": true, ".js": true, ".html": true, ".txt": true,
	".woff": true, ".woff2": true, ".ttf": true, ".svg": true, ".eot": true,
}

func (a *App) static(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		a.serve(w, r, true, pageIndex)
		return
	}
	clean := path.Clean("/" + r.URL.Path)
	if !staticExt[strings.ToLower(path.Ext(clean))] {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(a.cfg.StaticDir, filepath.FromSlash(clean)))
}
