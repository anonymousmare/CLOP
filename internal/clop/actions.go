package clop

import (
	"fmt"
	"strings"
)

// Recipe ids with special rules.
const (
	recipeBarracks     = 29
	recipeDrugFarm     = 36
	recipeSellDrugsSE  = 37
	recipeSellDrugsNLR = 38
	recipeSolarEnviro  = 40
	recipeLunarEnviro  = 41
	recipeForbiddenLab = 57
	recipeSunWorship   = 58
	recipeMoonWorship  = 59
	recipeFactory      = 60
)

func patronGov(gov string) bool { return gov == "Lunar Client" || gov == "Solar Vassal" }

// capTimes limits how many of a capped building can be built.
func (p *Page) capTimes(times *int64, resourceID, max int64, full string) {
	have := p.db.resourceAmount(p.NationID(), resourceID)
	if have >= max {
		p.Err(full)
	} else if *times > max-have {
		*times = max - have
	}
}

// performRecipe runs an action from actions.php or favoriteactions.php
// `times` times, applying all of its costs and effects.
func (p *Page) performRecipe(nation Row, recipeID, times int64) {
	db, nid := p.db, p.NationID()
	gov := nation.S("government")
	se, nlr := nation.I("se_relation"), nation.I("nlr_relation")
	if times < 1 {
		p.Err("Whole numbers greater than 0.")
	}
	switch recipeID {
	case recipeForbiddenLab:
		if nation.B("seesecrets") {
			p.Err("You don't need to build another one of those again.")
		}
		if patronGov(gov) {
			p.Err("Your patron will not allow you to begin forbidden research.")
		}
	case recipeBarracks:
		p.capTimes(&times, resBarracks, 20, "You already have 20 barracks; there's no point in making more.")
	case recipeDrugFarm:
		if patronGov(gov) {
			p.Err("Your patron forbids the manufacture of drugs.")
		}
	case recipeSellDrugsSE, recipeSellDrugsNLR:
		if patronGov(gov) {
			p.Err("Your patron forbids the selling of drugs.")
		}
		if nation.I("age") < 7 {
			p.Err("You cannot sell drugs if your nation is younger than a week.")
		}
		rel, empire := se, "The Solar Empire"
		if recipeID == recipeSellDrugsNLR {
			rel, empire = nlr, "The New Lunar Republic"
		}
		if rel <= -500 {
			p.Err("%s has blocked all traffic from your nation.", empire)
		} else if rel-times < -500 {
			times = 500 + rel
		}
	case recipeSolarEnviro:
		if se < 900 {
			p.Err("You don't have enough relation with the Solar Empire to build this.")
		} else {
			p.capTimes(&times, resSolarEnviro, 5, "You already have 5 of those.")
		}
	case recipeLunarEnviro:
		if nlr < 900 {
			p.Err("You don't have enough relation with the New Lunar Republic to build this.")
		} else {
			p.capTimes(&times, resLunarEnviro, 5, "You already have 5 of those.")
		}
	case recipeSunWorship:
		if se < 0 {
			p.Err("You must have at least neutral relations with the Solar Empire to build this.")
		} else {
			p.capTimes(&times, resSunWorship, 2, "You already have 2 of those.")
		}
	case recipeMoonWorship:
		if nlr < 0 {
			p.Err("You must have at least neutral relations with the New Lunar Republic to build this.")
		} else {
			p.capTimes(&times, resMoonWorship, 2, "You already have 2 of those.")
		}
	case recipeFactory:
		if nlr < 0 || se < 0 {
			p.Err("You must have at least neutral relations with both the New Lunar Republic and the Solar Empire to build this.")
		} else {
			p.capTimes(&times, resFactory, 1, "You already have a factory.")
		}
	}

	recipe := db.One(`SELECT r.*, rd.name AS resourcename FROM recipes r
		LEFT JOIN resourcedefs rd ON r.resource_id = rd.resource_id WHERE r.recipe_id = ?`, recipeID)
	if recipe == nil ||
		(recipe.I("region") != 0 && recipe.I("region") != nation.I("region")) ||
		(recipe.I("subregion") != 0 && recipe.I("subregion") != nation.I("subregion")) {
		p.Err("Nice try!")
	}
	cost := recipe.I("cost")
	if nation.I("region") == 4 {
		cost = recipe.I("cost_przewalskia")
	}
	cost *= times
	if nation.I("funds") < cost {
		p.Err("You don't have enough money to build that.")
	}
	if p.HasErrors() {
		return
	}

	type use struct {
		resourceID, amount int64
	}
	var uses []use
	var spent []string
	for _, item := range db.Query(`SELECT ri.resource_id, ri.is_used_up, ri.amount, rd.name FROM recipeitems ri
		LEFT JOIN resourcedefs rd ON rd.resource_id = ri.resource_id WHERE ri.recipe_id = ?`, recipeID) {
		need := item.I("amount")
		if item.B("is_used_up") {
			need *= times
			uses = append(uses, use{item.I("resource_id"), need})
			spent = append(spent, fmt.Sprintf("You spent %d %s.", need, item.S("name")))
		}
		have := db.One("SELECT amount FROM resources WHERE nation_id = ? AND resource_id = ?", nid, item.I("resource_id"))
		if have == nil {
			p.Err("You don't have any %s.", item.S("name"))
		} else if need > have.I("amount") {
			p.Err("You don't have enough %s to do that.", item.S("name"))
		}
	}
	if p.HasErrors() {
		return
	}

	p.infos = spent
	for _, u := range uses {
		db.Exec("UPDATE resources SET amount = amount - ? WHERE nation_id = ? AND resource_id = ?", u.amount, nid, u.resourceID)
	}
	label := fmt.Sprintf("%d %s", times, recipe.S("name"))
	if recipe.I("resource_id") != 0 {
		gained := recipe.I("amount") * times
		db.Exec(`INSERT INTO resources (nation_id, resource_id, amount, disabled) VALUES (?, ?, ?, 0)
			ON DUPLICATE KEY UPDATE amount = amount + ?`, nid, recipe.I("resource_id"), gained, gained)
		p.Info("You gained %d %s.", gained, recipe.S("resourcename"))
	}
	if eff := recipe.I("se_relation") * times; eff != 0 {
		p.Info("%s", db.affectEmpireRelations(nid, se, eff, label, "Solar Empire"))
	}
	if eff := recipe.I("nlr_relation") * times; eff != 0 {
		p.Info("%s", db.affectEmpireRelations(nid, nlr, eff, label, "New Lunar Republic"))
	}
	if eff := recipe.I("satisfaction") * times; eff != 0 {
		p.Info("%s", db.affectSatisfaction(nid, nation.I("satisfaction"), eff, label, gov))
	}
	if cost != 0 {
		db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", cost, nid)
		if cost > 0 {
			p.Info("You paid %s bits.", commas(cost))
		} else {
			p.Info("You gained %s bits.", commas(-cost))
		}
		nation.Set("funds", nation.I("funds")-cost)
	}
	p.Info("%s completed successfully.", recipe.S("name"))
	db.Exec("UPDATE resources SET disabled = amount WHERE disabled > amount AND nation_id = ?", nid)
	db.Exec("DELETE FROM resources WHERE amount = 0 AND nation_id = ?", nid)
	db.report(nid, joinBR(p.infos))
}

func pageActions(p *Page) {
	p.title = "Actions - "
	nation := p.needsNation()
	p.csrf("actions")
	if !p.HasErrors() {
		recipeID, times := p.PostI("recipe_id"), p.PostI("times")
		if p.PostB("favorite") {
			p.db.Exec("INSERT INTO recipefavorites (nation_id, recipe_id, times) VALUES (?, ?, ?)", p.NationID(), recipeID, times)
			p.Info("Favorite added.")
		} else if recipeID != 0 {
			p.performRecipe(nation, recipeID, times)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<center><div class="well">Funds: <span class="text-success">%s Bits</span></div></center>
<center><div id="container" class="js-masonry" data-masonry-options='{ "itemSelector": ".masonryitem", "isFitWidth": true }'>
`, commas(nation.S("funds")))
	group, first := "", true
	for _, r := range p.db.Query(`SELECT r.name, r.region, r.subregion, r.recipe_id, r.description, r.cost, r.cost_przewalskia, rg.name AS groupname
		FROM recipes r LEFT JOIN recipegroups rg ON rg.recipegroup_id = r.recipegroup_id ORDER BY rg.recipegroup_id, r.name`) {
		region, sub := r.I("region"), r.I("subregion")
		if region != 0 && (region != nation.I("region") || (sub != 0 && sub != nation.I("subregion"))) {
			continue
		}
		cost := r.I("cost")
		if nation.I("region") == 4 {
			cost = r.I("cost_przewalskia")
		}
		if r.S("groupname") != group || first {
			if !first {
				b.WriteString("</table></div></div>")
			}
			fmt.Fprintf(&b, `    <div class="masonryitem" style="padding: 10px; width: 400px">
	<div class="panel panel-default">
	<div class="panel-heading h4">%s</div>
<table class="table table-striped table-bordered">
`, r.S("groupname"))
			first, group = false, r.S("groupname")
		}
		costClass := "text-success"
		if cost > 0 {
			costClass = "text-danger"
		} else if cost == 0 {
			costClass = "text-warning"
		}
		b.WriteString(tpl(`<tr><td>{$description}<br/>
    <span class="{$costclass}">{$displaycost}</span> bits<br/>
<form action="actions.php" method="post" class="form-horizontal"><input type="hidden" name="token_actions" value="{$token}"/>
<input type="hidden" name="recipe_id" value="{$recipe_id}"/>
<div class="form-inline"><input type="submit" value="{$name}" class="btn btn-success"/>
<span class="pull-right"><input name="times" value="1" class="form-control" type="text" placeholder="Times" style="width:75px"/></span>
</div>
<input type="submit" name="favorite" value="Add to Favorites" class="btn btn-info"/>
</form>
</td></tr>
`, V{"description": r.S("description"), "costclass": costClass, "displaycost": commas(cost), "token": p.token("actions"),
			"recipe_id": r.S("recipe_id"), "name": r.S("name")}))
	}
	b.WriteString("</table></div></div></div></center>")
	p.Raw(b.String())
}

func pageFavoriteActions(p *Page) {
	p.title = "Favorite Actions - "
	nation := p.needsNation()
	p.csrf("favoriteactions")
	if !p.HasErrors() {
		recipeID, times := p.PostI("recipe_id"), p.PostI("times")
		if p.PostB("remove") {
			p.db.Exec("DELETE FROM recipefavorites WHERE nation_id = ? AND recipe_id = ? AND times = ?", p.NationID(), recipeID, times)
			p.Info("Favorite removed.")
		} else if recipeID != 0 {
			p.performRecipe(nation, recipeID, times)
		}
	}
	if p.db.One("SELECT 1 AS x FROM recipefavorites WHERE nation_id = ? LIMIT 1", p.NationID()) != nil {
		p.Raw(fmt.Sprintf(`<center><div class="well">Funds: <span class="text-success">%s Bits</span></div></center>
`, commas(nation.S("funds"))))
	}
	p.Raw(favoritesList(p))
}

// favoritesList renders the favorite-action cards (also shown on the overview).
func favoritesList(p *Page) string {
	if p.token("favoriteactions") == "" {
		p.sess.Set("token_favoriteactions", newToken())
	}
	favs := p.db.Query(`SELECT r.name, rf.times, r.recipe_id FROM recipes r
		INNER JOIN recipefavorites rf ON rf.recipe_id = r.recipe_id WHERE rf.nation_id = ? ORDER BY r.name, rf.times`, p.NationID())
	if len(favs) == 0 {
		return `<center>You have no actions listed as your favorites.</center>`
	}
	token := p.token("favoriteactions")
	customRow := func(recipeID string) string {
		return tpl(`<tr><td><center>
<form action="favoriteactions.php" method="post" class="form-horizontal">
<input type="hidden" name="token_favoriteactions" value="{$token}"/>
<input type="hidden" name="recipe_id" value="{$recipe_id}"/>
<div class="form-inline"><input type="submit" name="perform" value="This many:" class="btn btn-success"/>
<span><input name="times" value="1" class="form-control" type="text" placeholder="Times" style="width:75px"/></span>
</div></form></td></tr>
`, V{"token": token, "recipe_id": recipeID})
	}
	var b strings.Builder
	b.WriteString(`<center><div id="container" class="js-masonry" data-masonry-options='{ "itemSelector": ".masonryitem", "isFitWidth": true }'>
`)
	name, last := "", ""
	for i, f := range favs {
		if i == 0 || f.S("name") != name {
			if i > 0 {
				b.WriteString(customRow(last))
				b.WriteString("</table></div></div>\n")
			}
			fmt.Fprintf(&b, `    <div class="masonryitem" style="padding: 10px; width: 300px">
	<div class="panel panel-default">
	<div class="panel-heading h4">%s</div>
<table class="table table-striped table-bordered">
`, f.S("name"))
			name = f.S("name")
		}
		last = f.S("recipe_id")
		b.WriteString(tpl(`<tr><td><center>
<form action="favoriteactions.php" method="post">
<input type="hidden" name="token_favoriteactions" value="{$token}"/>
<input type="hidden" name="recipe_id" value="{$recipe_id}"/>
<input type="hidden" name="times" value="{$times}"/>
<input type="submit" name="perform" value="{$times} times" class="btn btn-success"/>
<input type="submit" name="remove" value="Remove" class="btn btn-danger"/>
</form></center>
</td></tr>
`, V{"token": token, "recipe_id": f.S("recipe_id"), "times": f.S("times")}))
	}
	b.WriteString(customRow(last))
	b.WriteString("</table></div></div></div></center>")
	return b.String()
}
