package clop

import (
	"fmt"
	"math"
	"strings"
)

func pageOverview(p *Page) {
	p.title = "Overview - "
	nation := p.needsNation()
	nid := p.NationID()
	db := p.db
	p.csrf("overview")

	if !p.HasErrors() {
		if p.Post("action") == "Update Information" {
			db.Exec("UPDATE nations SET description = ? WHERE nation_id = ?", p.Post("description"), nid)
			nation["description"] = p.Post("description")
			p.Info("Description updated.")
		}
		resourceID := p.PostI("resource_id")
		switch {
		case p.PostB("recycle"):
			overviewRecycle(p, nation, resourceID, p.PostI("recycleamount"))
		case p.PostB("disable"):
			amount := p.PostI("disableamount")
			r := db.One(`SELECT r.*, rd.name FROM resources r INNER JOIN resourcedefs rd ON r.resource_id = rd.resource_id
				WHERE r.nation_id = ? AND r.resource_id = ?`, nid, resourceID)
			if amount < 1 {
				p.Err("No amount entered.")
			}
			if amount > r.I("amount")-r.I("disabled") {
				p.Err("You don't have that many to disable.")
			}
			if resourceID == resBarracks {
				p.Err("There's no disabling the Barracks.")
			}
			if resourceID == resAlicornFacility {
				p.Err("Hell no.")
			}
			if !p.HasErrors() {
				db.Exec("UPDATE resources SET disabled = disabled + ? WHERE nation_id = ? AND resource_id = ?", amount, nid, resourceID)
				p.Info("%d %s disabled.", amount, r.S("name"))
			}
		case p.PostB("reenable"):
			amount := p.PostI("reenableamount")
			r := db.One(`SELECT r.*, rd.name FROM resources r INNER JOIN resourcedefs rd ON r.resource_id = rd.resource_id
				WHERE r.nation_id = ? AND r.resource_id = ?`, nid, resourceID)
			if amount > r.I("disabled") {
				p.Err("You did not have that many %s disabled.", r.S("name"))
			}
			if amount < 1 {
				p.Err("No amount entered.")
			}
			if !p.HasErrors() {
				db.Exec("UPDATE resources SET disabled = ? WHERE nation_id = ? AND resource_id = ?", r.I("disabled")-amount, nid, resourceID)
				p.Info("%d %s reenabled.", amount, r.S("name"))
			}
		}
	}

	gov := nation.S("government")
	gdp := applyGovernmentGDP(db.baseGDP(nid), gov, nation.I("satisfaction"))

	type stock struct {
		name   string
		amount int64
	}
	var buildings []Row
	resources := map[string]int64{}
	taxes := map[string]int64{}
	for _, r := range db.Query(`SELECT r.amount, rd.name, r.resource_id, rd.is_building, r.disabled, rd.satisfaction_on_destroy
		FROM resources r INNER JOIN resourcedefs rd ON r.resource_id = rd.resource_id WHERE r.nation_id = ? ORDER BY rd.name`, nid) {
		if r.B("is_building") {
			buildings = append(buildings, r)
			continue
		}
		resources[r.S("name")] = r.I("amount")
		taxes[r.S("name")] = stockpileTax(r.I("amount"), 50000)
	}
	equipment := func(table, defs, idcol string) []stock {
		var out []stock
		for _, r := range db.Query(`SELECT r.amount, rd.name FROM `+table+` r INNER JOIN `+defs+` rd ON r.`+idcol+` = rd.`+idcol+`
			WHERE r.nation_id = ? ORDER BY rd.name`, nid) {
			out = append(out, stock{r.S("name"), r.I("amount")})
			taxes[r.S("name")] = stockpileTax(r.I("amount"), 1000)
		}
		return out
	}
	weapons := equipment("weapons", "weapondefs", "weapon_id")
	armor := equipment("armor", "armordefs", "armor_id")

	flows := db.resourceFlows("n.nation_id = ?", nid)
	var milApples, milGems, milGas, milCoffee int64
	for _, r := range db.Query("SELECT SUM(size) AS totalsize, type FROM forces WHERE nation_id = ? GROUP BY type", nid) {
		size := r.I("totalsize")
		switch r.I("type") {
		case 1:
			milApples += size * 5
		case 2, 5:
			milGas += size * 5
		case 3:
			milCoffee += size * 5
		case 4:
			milGems += size * 5
		case 6:
			milGems += size * 10
		}
	}
	proj := db.project(nation)
	attackers, defenders := forcesAt(db, nid)

	if len(attackers) > 0 {
		p.Raw(`<center><span class="text-danger">You have attackers in your nation!</span></center>`)
	}
	sedisplay := fmt.Sprintf(`(<span class="%s">%s</span> per tick)`, relationTurnClass(proj.SE), proj.SE)
	nlrdisplay := fmt.Sprintf(`(<span class="%s">%s</span> per tick)`, relationTurnClass(proj.NLR), proj.NLR)
	if gov == "Alicorn Elite" || gov == "Transponyism" {
		sedisplay, nlrdisplay = "(Ascending)", "(Ascending)"
	}
	inactive := ""
	if !nation.B("active_economy") {
		inactive = `        <tr><td style="text-align: right;"><span class="text-danger">Warning:</span></td><td><span class="text-danger">Your economic type is not active!</span></td></tr>`
	}
	satClass := "text-danger"
	if nation.I("satisfaction") > 0 {
		satClass = "text-success"
	}
	p.Echo(overviewTopHTML, V{
		"nationinfo":   nation,
		"region":       subregionNames[nation.I("subregion")] + regionNames[nation.I("region")],
		"inactive":     inactive,
		"secolor":      numberClass(nation.F("se_relation")),
		"nlrcolor":     numberClass(nation.F("nlr_relation")),
		"sedisplay":    sedisplay,
		"nlrdisplay":   nlrdisplay,
		"satColor":     satClass,
		"satColor2":    numberClass(float64(proj.Sat)),
		"satperturn":   proj.Sat,
		"displaygdp":   commas(math.Round(gdp*100) / 100),
		"displayfunds": commas(nation.S("funds")),
		"description":  esc(nation.S("description")),
		"token":        p.token("overview"),
	})

	hideIcons := nation.B("hideicons")
	var b strings.Builder
	b.WriteString(`<div class="row">
  <div class="col-md-6">
   <div class="panel panel-default">
     <div class="panel-heading">Resources</div>
     <table class="table">
      <thead>
        <tr>
`)
	if !hideIcons {
		b.WriteString("          <td></td>\n")
	}
	b.WriteString(`          <td style="text-align: right;">Resource</td>
          <td>Qty</td>
          <td>Generated</td>
          <td>Used</td>
		      <td>Loss</td>
          <td>Net</td>
          <td>Ticks-Worth</td>
        </tr>
      </thead>
      <tbody>
`)
	for name := range flows.Generated {
		if _, ok := resources[name]; !ok {
			resources[name] = 0
		}
	}
	for name := range flows.Used {
		if _, ok := resources[name]; !ok {
			resources[name] = 0
		}
	}
	for _, name := range sortedKeys(resources) {
		amount, gen, used, tax := resources[name], flows.Generated[name], flows.Used[name], taxes[name]
		net := gen - used - tax
		amountClass := "text-danger"
		if amount >= used {
			amountClass = "text-success"
		} else if amount >= gen-used {
			amountClass = "text-warning"
		}
		taxClass := "text-success"
		if tax != 0 {
			taxClass = "text-danger"
		}
		reservesClass, reserves := "text-warning", ""
		switch {
		case amount < used:
			reservesClass, reserves = "text-danger", "NONE"
		case net >= 0:
			reservesClass, reserves = "text-success", "N/A"
		default:
			reserves = str(floor(float64(amount-used) / float64(abs(net))))
		}
		b.WriteString("    <tr>\n")
		if !hideIcons {
			fmt.Fprintf(&b, "    <td style=\"width: 16px;\"><img src=\"images/icons/%s.png\"/></td>\n", name)
		}
		fmt.Fprintf(&b, `    <td style="text-align: right;">%s</td>
    <td><span class="%s">%s</span></td>
    <td style="text-align: center;"><span class="text-success">%s</span></td>
    <td style="text-align: center;"><span class="text-danger">%s</span></td>
	  <td style="text-align: center;"><span class="%s">%s</span></td>
    <td style="text-align: center;"><span class="%s">%s</span></td>
    <td style="text-align: center;"><span class="%s">%s</span></td>
    </tr>
`, name, amountClass, commas(amount), commas(gen), commas(used), taxClass, commas(tax), numberClass(float64(net)), commas(net), reservesClass, reserves)
	}
	fmt.Fprintf(&b, `       </tbody>
     </table>
     <center>Your military also uses <b>%s apples, %s gems, %s coffee, and %s gasoline</b> every 12 hours.</center>
   </div>
  </div>
  <div class="col-md-6">
   <div class="panel panel-default">
     <div class="panel-heading">Buildings</div>
     <table class="table">
       <tbody>
`, commas(milApples), commas(milGems), commas(milCoffee), commas(milGas))
	token := p.token("overview")
	for _, bd := range buildings {
		satwarning, disabledinfo, reenable, disable := "", "", "", ""
		if s := bd.I("satisfaction_on_destroy"); s != 0 {
			gainlose := "lose"
			if s > 0 {
				gainlose = "gain"
			}
			satwarning = fmt.Sprintf(" You will %s %d satisfaction for each building you destroy!", gainlose, abs(s))
		}
		if bd.I("disabled") > 0 {
			disabledinfo = fmt.Sprintf("(%d disabled)", bd.I("disabled"))
			reenable = fmt.Sprintf(`       <div class="input-group">
        <span class="input-group-btn">
		<input type="submit" name="reenable" value="Reenable %s" class="btn btn-success">
		</span>
		<input name="reenableamount" placeholder="Amount" value="" class="form-control">
		</div>`, bd.S("name"))
		}
		if bd.I("disabled") < bd.I("amount") {
			disable = fmt.Sprintf(`		<div class="input-group">
		<span class="input-group-btn">
		<input type="submit" name="disable" value="Disable %s" class="btn btn-warning">
		</span>
		<input name="disableamount" placeholder="Amount" value="" class="form-control">
		</div>`, bd.S("name"))
		}
		b.WriteString(tpl(` <tr>
   <td style="text-align: right;">{$name}</td>
   <td><span class="text-success">{$amount} {$disabledinfo}</span></td>
   <td>
	 <form name="recycle" method="post" action="overview.php">
	   <input type="hidden" name="token_overview" value="{$token}"/>
	   <input type="hidden" name="resource_id" value="{$resource_id}"/>
	   {$reenable}
	   {$disable}
	   <div class="input-group">
	   <span class="input-group-btn">
	   <input type="submit" onclick="return confirm('Really destroy {$name}?{$satwarning}')" name="recycle" value="Destroy {$name}" class="btn btn-danger"/>
	   </span>
	   <input name="recycleamount" type="text" placeholder="Amount" value="" class="form-control">
	   </div>
	 </form>
   </td>
 </tr>
`, V{"name": bd.S("name"), "amount": bd.S("amount"), "disabledinfo": disabledinfo, "token": token,
			"resource_id": bd.S("resource_id"), "reenable": reenable, "disable": disable, "satwarning": satwarning}))
	}
	b.WriteString(`       </tbody>
     </table>
   </div>
  </div>
</div>
<div class="row">
`)
	for _, eq := range []struct {
		title, col string
		items      []stock
	}{{"Weapons", "Weapon", weapons}, {"Armor", "Armor", armor}} {
		fmt.Fprintf(&b, `  <div class="col-md-6">
   <div class="panel panel-default">
     <div class="panel-heading">%s</div>
     <table class="table">
      <thead>
        <tr>
          <td style="text-align: right;">%s</td>
          <td>Qty</td>
		  <td>Loss per Tick</td>
        </tr>
      </thead>
      <tbody>
`, eq.title, eq.col)
		for _, it := range eq.items {
			taxClass := "text-success"
			if taxes[it.name] != 0 {
				taxClass = "text-danger"
			}
			fmt.Fprintf(&b, `    <tr>
    <td style="text-align: right;">%s</td>
    <td>%s</td>
	<td><span class="%s">%s</span></td>
    </tr>
`, it.name, commas(it.amount), taxClass, commas(taxes[it.name]))
		}
		b.WriteString("       </tbody>\n     </table>\n   </div>\n  </div>\n")
	}
	b.WriteString("</div>\n")
	p.Raw(b.String())

	p.Raw(forceCards(attackers, "Attackers", "container1", "No attackers are in this nation."))
	p.Raw(forceCards(defenders, "Defenders", "container2", "No defenders are in this nation."))
	p.Raw(favoritesList(p))
}

// stockpileTax is how much of a stockpile above the limit is siphoned off each tick.
func stockpileTax(amount, limit int64) int64 {
	if amount <= limit {
		return 0
	}
	return ceil(float64(amount-limit) / 500)
}

// relationTurnClass colours a per-tick relation change.
func relationTurnClass(v string) string {
	if !isNumeric(v) || toInt(v) == 0 {
		return "text-success"
	}
	if toInt(v) > 0 {
		return "text-warning"
	}
	return "text-danger"
}

func overviewRecycle(p *Page, nation Row, resourceID, amount int64) {
	db, nid := p.db, p.NationID()
	r := db.One(`SELECT r.*, rd.name, rd.satisfaction_on_destroy FROM resources r
		INNER JOIN resourcedefs rd ON r.resource_id = rd.resource_id WHERE r.nation_id = ? AND r.resource_id = ?`, nid, resourceID)
	if r == nil {
		p.Err("You already recycled your last one!")
	}
	if amount > r.I("amount") {
		p.Err("You don't have that many to destroy.")
	}
	if resourceID == resAlicornFacility {
		p.Err("That's a good way to be violently deposed.")
	}
	if amount < 1 {
		p.Err("No amount entered.")
	}
	if p.HasErrors() {
		return
	}
	if r.I("amount") == amount {
		db.Exec("DELETE FROM resources WHERE nation_id = ? AND resource_id = ?", nid, resourceID)
	} else {
		db.Exec("UPDATE resources SET amount = amount - ?, disabled = GREATEST(CAST(disabled AS SIGNED) - ?, 0) WHERE nation_id = ? AND resource_id = ?",
			amount, amount, nid, resourceID)
	}
	onDestroy := r.I("satisfaction_on_destroy")
	switch {
	case onDestroy > 0:
		got := db.affectSatisfactionSilent(nid, nation.I("satisfaction"), onDestroy*amount, nation.S("government"))
		p.Info("Your little ponies are happy to be rid of the %s! (%d sat)", r.S("name"), got)
		nation.Set("satisfaction", nation.I("satisfaction")+got)
	case onDestroy < 0:
		got := db.affectSatisfactionSilent(nid, nation.I("satisfaction"), onDestroy*amount, nation.S("government"))
		p.Info("Your little ponies are sad to see the %s go! (%d sat)", r.S("name"), got)
		nation.Set("satisfaction", nation.I("satisfaction")+got)
	default:
		p.Info("%d %s destroyed.", amount, r.S("name"))
	}
}

const overviewTopHTML = `<center><h3>{$nationinfo['name']}</h3></center>
<center><h4>{$region}</h4></center>
<center><h4>Age: {$nationinfo['age']}</h4></center>
<div class="row">
  <div class="col-md-8">
    <div class="panel panel-default">
      <div class="panel-heading">Nation</div>
      <table class="table">
        <tbody>
        <tr><td style="text-align: right;">Government Type</td><td>{$nationinfo['government']}</td></tr>
        <tr><td style="text-align: right;">Economic Type</td><td>{$nationinfo['economy']}</td></tr>
{$inactive}
          <tr><td style="text-align: right;">Relationship with Solar Empire</td><td><span class="{$secolor}">{$nationinfo['se_relation']}</span>
          {$sedisplay}</td></tr>
          <tr><td style="text-align: right;">Relationship with New Lunar Republic</td><td><span class="{$nlrcolor}">{$nationinfo['nlr_relation']}</span>
          {$nlrdisplay}</td></tr>
          <tr><td style="text-align: right;">Satisfaction</td><td><span class="{$satColor}">{$nationinfo['satisfaction']}</span> (<span class="{$satColor2}">{$satperturn}</span> per tick)</td></tr>
          <tr><td style="text-align: right;">GDP</td><td><span class="text-success">{$displaygdp}</span> bits per tick</td></tr>
          <tr><td style="text-align: right;">Funds</td><td><span class="text-success">{$displayfunds}</span> bits</td></tr>
        </tbody>
      </table>
    </div>
  </div>
  <div class="col-md-4">
    <div class="panel panel-default">
      <div class="panel-heading">Description</div>
      <div class="panel-body">
        <form name="changeinfo" method="post" action="overview.php" class="form" role="form">
          <textarea class="form-control" name="description">{$description}</textarea>
          <input type="hidden" name="token_overview" value="{$token}"/>
          </br>
          <input name="action" type="submit" value="Update Information" class="btn btn-success"/>
        </form><br/>
        <a href="viewnation.php?nation_id={$nationinfo['nation_id']}">View Nation</a>
      </div>
    </div>
  </div>
</div>
`

// forcesAt returns the attacking and defending units sitting in a nation,
// each row annotated with display names.
func forcesAt(db *DB, nationID int64) (attackers, defenders []Row) {
	for _, r := range db.Query(`SELECT fg.name AS groupname, fg.forcegroup_id, fg.attack_mission, fg.departuredate,
		fg.nation_id AS ownernation_id, n.name AS ownername, n.region AS ownerregion,
		f.*, fg.location_id, rd1.name AS weaponname, rd2.name AS armorname FROM forces f
		INNER JOIN forcegroups fg ON f.forcegroup_id = fg.forcegroup_id
		LEFT JOIN weapondefs rd1 ON f.weapon_id = rd1.weapon_id
		LEFT JOIN armordefs rd2 ON f.armor_id = rd2.armor_id
		LEFT JOIN nations n ON fg.nation_id = n.nation_id
		WHERE fg.departuredate IS NULL AND fg.location_id = ?
		ORDER BY fg.forcegroup_id, f.name`, nationID) {
		switch r.I("ownernation_id") {
		case -1:
			r["ownername"] = "Solar Empire"
		case -2:
			r["ownername"] = "New Lunar Republic"
		case -3:
			r["ownername"] = "Occupy Equestria"
		default:
			r["ownerregionname"] = regionNames[r.I("ownerregion")]
		}
		decorateForce(r)
		if r.B("attack_mission") {
			attackers = append(attackers, r)
		} else {
			defenders = append(defenders, r)
		}
	}
	return
}

// decorateForce fills in the display fields every force listing uses.
func decorateForce(r Row) {
	r["lowertype"] = strings.ToLower(forceTypeNames[r.I("type")])
	r["typename"] = forceTypeNames[r.I("type")]
	if r.S("weaponname") == "" {
		r["weapon_id"] = "0"
		r["weaponname"] = "Scrounged Weapons"
	}
	if r.S("armorname") == "" {
		r["armor_id"] = "0"
		r["armorname"] = "Scrounged Armor"
	}
}

// forceUnitCell renders one unit inside a force group card.
func forceUnitCell(f Row, sized bool) string {
	img := `<img src="images/{$f['lowertype']}/w-{$f['armor_id']}-{$f['weapon_id']}.png"/>`
	if sized {
		img = `<img src="images/{$f['lowertype']}/w-{$f['armor_id']}-{$f['weapon_id']}.png" width="80" height="50"/>`
	}
	equip := ""
	if f.I("type") != 6 {
		equip = "{$f['weaponname']}<br/>\n{$f['armorname']}<br/>\n"
	}
	return tpl("<tr><td>\n<center>"+img+"</center>\n{$f['name']}<br/>\n{$f['typename']}<br/>\n"+equip+
		"Size: {$f['size']}<br/>\nTraining: {$f['training']}</td></tr>\n", V{"f": f})
}

// forceCards renders force groups as masonry cards, as on the overview.
func forceCards(units []Row, heading, containerID, empty string) string {
	if len(units) == 0 {
		return fmt.Sprintf(`<div class="row"><center><h5>%s</h5></center></div>`, empty)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<center><h4>%s</h4></center>
<div id="%s" class="js-masonry" data-masonry-options='{ "itemSelector": ".masonryitem", "isFitWidth": true }'>
`, heading, containerID)
	current := ""
	for _, f := range units {
		if f.S("forcegroup_id") != current {
			if current != "" {
				b.WriteString("</table></div></div>")
			}
			owner := f.S("ownername")
			if f.I("ownernation_id") > 0 {
				owner = fmt.Sprintf(`<a href="viewnation.php?nation_id=%s">%s</a> (%s)`, f.S("ownernation_id"), f.S("ownername"), f.S("ownerregionname"))
			}
			fmt.Fprintf(&b, `<div class="masonryitem" style="padding: 10px; width: 300px">
	<div class="panel panel-default">
	    <div class="panel-heading">%s<br/>
Owned by %s</div>
<table class="table table-striped table-bordered">
`, f.S("groupname"), owner)
			current = f.S("forcegroup_id")
		}
		b.WriteString(forceUnitCell(f, true))
	}
	b.WriteString("</table></div></div></div>")
	return b.String()
}
