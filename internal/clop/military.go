package clop

import (
	"fmt"
	"strings"
	"time"
)

var forceCosts = map[int64]int64{1: 200000, 2: 300000, 3: 300000, 4: 400000, 5: 250000, 6: 2000000}

func ascendingGov(gov string) bool { return gov == "Alicorn Elite" || gov == "Transponyism" }

func pageCreateForces(p *Page) {
	p.title = "Create Forces - "
	nation := p.needsNation()
	db, nid := p.db, p.NationID()
	gov := nation.S("government")
	p.csrf("createforces")
	if !p.HasErrors() && p.PostB("createforce") {
		size, typ, name, merc := p.PostI("size"), p.PostI("forcetype"), p.Post("name"), p.Post("mercenaries")
		if size < 1 {
			p.Err("No size entered.")
		}
		cost, ok := forceCosts[typ]
		if !ok {
			p.Err("No type entered.")
		}
		if typ == 6 && !ascendingGov(gov) {
			p.Err("Nope.")
		}
		mult := 1.0
		switch merc {
		case "se":
			mult = 3 - nation.F("se_relation")/1000
		case "nlr":
			mult = 3 - nation.F("nlr_relation")/1000
		}
		total := float64(cost) * mult * float64(size)
		if nation.F("funds") < total {
			p.Err("You don't have the money!")
		}
		if name == "" {
			p.Err("No name entered.")
		}
		if !alnumSpace(name, true) {
			p.Err("Only English letters and numbers for the force name.")
		}
		training := int64(10)
		if merc == "" {
			training = db.resourceAmount(nid, resBarracks)
			if training > 20 {
				training = 20
			}
		}
		if merc != "" && typ == 6 {
			p.Err("Alicorn mercenaries? Where are you going to find those?")
		}
		if db.One("SELECT COUNT(*) AS totalcount FROM forces WHERE nation_id = ?", nid).I("totalcount") >= 100 {
			p.Err("You have too many different forces.")
		}
		if !p.HasErrors() {
			db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", int64(total), nid)
			group := db.Exec("INSERT INTO forcegroups (nation_id, location_id, destination_id, name) VALUES (?, ?, 0, ?)", nid, nid, name)
			db.Exec(`INSERT INTO forces (nation_id, size, type, training, name, forcegroup_id, weapon_id, armor_id)
				VALUES (?, ?, ?, ?, ?, ?, 0, 0)`, nid, size, typ, training, name, group)
			db.report(nid, "You have created the military force "+esc(name)+".")
			p.Redirect("equipforces.php")
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<center><div class="well">Funds: <span class="text-success">%s Bits</span></div></center>
<center><h4>Create New Force</h4></center>
<center><table>
<tr><th>Force</th><th>Price per Unit</th><th>Upkeep</th></tr>
<tr><td>Cavalry</td><td>200,000</td><td>5 apples/12 hours</td></tr>
<tr><td>Tanks</td><td>300,000</td><td>5 gasoline/12 hours</td></tr>
<tr><td>Pegasi</td><td>300,000</td><td>5 coffee/12 hours</td></tr>
<tr><td>Unicorns</td><td>400,000</td><td>5 gems/12 hours</td></tr>
<tr><td>Naval</td><td>250,000</td><td>5 gasoline/12 hours</td></tr>
`, commas(nation.S("funds")))
	maxType := int64(5)
	if ascendingGov(gov) {
		maxType = 6
		b.WriteString("<tr><td>Alicorns</td><td>2,000,000</td><td>10 gems/12 hours</td></tr>\n")
	}
	b.WriteString("</table></center>\n<center>Mercenaries start with training 10 but cost double to quadruple, depending on your relationship.</center>\n")
	if ascendingGov(gov) {
		b.WriteString("<center>Alicorns need no weapons or armor; they always do 10 damage and have an armor value of .10.</center>\n")
	}
	fmt.Fprintf(&b, `<center><form action="createforces.php" method="post">
<input type="hidden" name="token_createforces" value="%s"/>
<select name="forcetype" class="form-control" style="width:210px;">
<option value=""></option>
`, p.token("createforces"))
	for t := int64(1); t <= maxType; t++ {
		fmt.Fprintf(&b, "        <option value=\"%d\"%s>%s</option>\n", t, selected(p.Post("forcetype") == str(t)), forceTypeNames[t])
	}
	b.WriteString("</select><br/>\n<select name=\"mercenaries\" class=\"form-control\" style=\"width:210px;\">\n")
	for _, m := range [][2]string{{"", "National Recruits"}, {"se", "SE Mercenaries"}, {"nlr", "NLR Mercenaries"}} {
		fmt.Fprintf(&b, "        <option value=\"%s\"%s>%s</option>\n", m[0], selected(p.Post("mercenaries") == m[0]), m[1])
	}
	b.WriteString(`</select><br/>
<input name="size" class="form-control" placeholder="Size" style="width:200px;"/>
<input name="name" class="form-control" placeholder="Name" style="width:200px;"/><br/>
<input type="submit" name="createforce" value="Create Force" class="btn btn-info"/>
</form>
`)
	p.Raw(b.String())
}

func selected(b bool) string {
	if b {
		return " selected "
	}
	return ""
}

// equipmentKind is weapons or armor, as fitted to forces.
type equipmentKind struct {
	stock, defs, idcol, scrounged, verb string
}

var (
	weaponEquip = equipmentKind{"weapons", "weapondefs", "weapon_id", "scrounged weapons", "armed with"}
	armorEquip  = equipmentKind{"armor", "armordefs", "armor_id", "scrounged armor", "equipped with"}
)

func pageEquipForces(p *Page) {
	p.title = "Equip Forces - "
	p.needsNation()
	db, nid := p.db, p.NationID()
	p.csrf("equipforces")
	if !p.HasErrors() && p.isPost {
		force := db.One(`SELECT f.* FROM forces f INNER JOIN forcegroups fg ON f.forcegroup_id = fg.forcegroup_id
			WHERE fg.location_id = ? AND f.nation_id = ? AND f.force_id = ?`, nid, nid, p.PostI("force_id"))
		if force == nil {
			p.Err("Force not found.")
		} else if p.PostB("changeweapon") {
			equipForce(p, force, weaponEquip, p.PostI("weapon_id"))
		} else if p.PostB("changearmor") {
			equipForce(p, force, armorEquip, p.PostI("armor_id"))
		}
	}

	options := func(k equipmentKind) map[int64][]Row {
		out := map[int64][]Row{}
		for _, r := range db.Query("SELECT d.name, s.amount, d.type, d."+k.idcol+" AS id FROM "+k.defs+" d LEFT JOIN "+k.stock+
			" s ON (s."+k.idcol+" = d."+k.idcol+" AND s.nation_id = ?)", nid) {
			r["displayname"] = r.S("name")
			if r.B("amount") {
				r["displayname"] = fmt.Sprintf("%s (Have %s extra)", r.S("name"), r.S("amount"))
			}
			out[r.I("type")] = append(out[r.I("type")], r)
		}
		return out
	}
	weapons, armors := options(weaponEquip), options(armorEquip)
	forces := db.Query(`SELECT fg.name AS groupname, f.* FROM forces f INNER JOIN forcegroups fg ON f.forcegroup_id = fg.forcegroup_id
		WHERE fg.location_id = ? AND f.nation_id = ? AND fg.departuredate IS NULL ORDER BY fg.forcegroup_id`, nid, nid)
	if len(forces) == 0 {
		p.Raw("<center>You have no forces at home to equip!</center>")
		return
	}
	token := p.token("equipforces")
	var b strings.Builder
	group := ""
	for _, f := range forces {
		if f.S("forcegroup_id") != group {
			if group != "" {
				b.WriteString("</table></div>")
			}
			fmt.Fprintf(&b, `<div class="panel panel-default">
    <div class="panel-heading">
        <h3 class="panel-title">%s</h3>
    </div>
    <table class="table table-bordered">
`, f.S("groupname"))
			group = f.S("forcegroup_id")
		}
		fmt.Fprintf(&b, `        <tr>
        <td>
            %s</td><td>%s<br/>
            Size: %s<br/>
            Training: %s
        </td>
        <td>
`, f.S("name"), forceTypeNames[f.I("type")], f.S("size"), f.S("training"))
		if f.I("type") != 6 {
			for _, eq := range []struct {
				field, button string
				opts          []Row
				current       int64
			}{{"weapon_id", "Change Weapon", weapons[f.I("type")], f.I("weapon_id")}, {"armor_id", "Change Armor", armors[f.I("type")], f.I("armor_id")}} {
				fmt.Fprintf(&b, `            <form action="equipforces.php" method="post">
                <input type="hidden" name="token_equipforces" value="%s"/>
                <input type="hidden" name="force_id" value="%s"/>
                <div class="input-group">
                    <select name="%s" class="form-control" style="width:200px;">
                        <option value="">Scrounged</option>
`, token, f.S("force_id"), eq.field)
				for _, o := range eq.opts {
					fmt.Fprintf(&b, "                            <option value=\"%s\"%s>%s</option>\n", o.S("id"), selected(o.I("id") == eq.current), o.S("displayname"))
				}
				name := "changeweapon"
				if eq.field == "armor_id" {
					name = "changearmor"
				}
				fmt.Fprintf(&b, `                    </select>
                    <input type="submit" name="%s" value="%s" class="btn btn-info"/>
                </div>
            </form><br/>
`, name, eq.button)
			}
		}
		b.WriteString("        </td>\n    </tr>\n")
	}
	b.WriteString("</table></div>")
	p.Raw(b.String())
}

// equipForce swaps a force's weapons or armor, returning the old kit to stock.
func equipForce(p *Page, force Row, k equipmentKind, newID int64) {
	db, nid := p.db, p.NationID()
	size := force.I("size")
	name := k.scrounged
	var have int64
	if newID != 0 {
		item := db.One("SELECT d.name, s.amount FROM "+k.defs+" d LEFT JOIN "+k.stock+" s ON (d."+k.idcol+" = s."+k.idcol+
			" AND s.nation_id = ?) WHERE d."+k.idcol+" = ? AND d.type = ?", nid, newID, force.I("type"))
		if item == nil {
			p.Err("That doesn't fit %s.", force.S("name"))
			return
		}
		name, have = item.S("name"), item.I("amount")
		if have < size {
			p.Err("You don't have enough %s to equip %s!", name, force.S("name"))
		}
	}
	if newID == force.I(k.idcol) {
		p.Err("That's already that force's %s.", strings.TrimSuffix(k.stock, "s"))
	}
	if p.HasErrors() {
		return
	}
	if old := force.I(k.idcol); old != 0 {
		db.Exec("INSERT INTO "+k.stock+" (nation_id, "+k.idcol+", amount) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?", nid, old, size, size)
	}
	if newID != 0 {
		if have == size {
			db.Exec("DELETE FROM "+k.stock+" WHERE "+k.idcol+" = ? AND nation_id = ?", newID, nid)
		} else {
			db.Exec("UPDATE "+k.stock+" SET amount = amount - ? WHERE "+k.idcol+" = ? AND nation_id = ?", size, newID, nid)
		}
	}
	db.Exec("UPDATE forces SET "+k.idcol+" = ? WHERE force_id = ?", newID, force.I("force_id"))
	p.Info("%s %s %s.", force.S("name"), k.verb, name)
}

func pageGroupForces(p *Page) {
	p.title = "Group Forces - "
	p.needsNation()
	db, nid := p.db, p.NationID()
	p.csrf("groupforces")
	if !p.HasErrors() && p.isPost {
		groupForcesAction(p)
	}

	forces := db.Query(`SELECT fg.name AS groupname, f.*, fg.location_id, rd1.name AS weaponname, rd2.name AS armorname FROM forces f
		INNER JOIN forcegroups fg ON f.forcegroup_id = fg.forcegroup_id
		LEFT JOIN weapondefs rd1 ON f.weapon_id = rd1.weapon_id
		LEFT JOIN armordefs rd2 ON f.armor_id = rd2.armor_id
		WHERE f.nation_id = ? ORDER BY fg.forcegroup_id, f.name`, nid)
	if len(forces) == 0 {
		p.Raw("<center>You have no forces to group!</center>")
		return
	}
	byGroup := map[string][]Row{}
	groupsAt := map[string][]Row{}
	seenGroup := map[string]bool{}
	for _, f := range forces {
		decorateForce(f)
		byGroup[f.S("forcegroup_id")] = append(byGroup[f.S("forcegroup_id")], f)
		if !seenGroup[f.S("forcegroup_id")] {
			seenGroup[f.S("forcegroup_id")] = true
			groupsAt[f.S("location_id")] = append(groupsAt[f.S("location_id")], f)
		}
	}
	token := p.token("groupforces")
	var b strings.Builder
	group := ""
	for _, f := range forces {
		v := V{"token": token, "f": f}
		if f.S("forcegroup_id") != group {
			if group != "" {
				b.WriteString("</table></div>")
			}
			var opts strings.Builder
			for _, g := range groupsAt[f.S("location_id")] {
				if g.S("forcegroup_id") != f.S("forcegroup_id") {
					fmt.Fprintf(&opts, "                            <option value=\"%s\">%s</option>\n", g.S("forcegroup_id"), g.S("groupname"))
				}
			}
			v["groupoptions"] = opts.String()
			b.WriteString(tpl(groupHeaderHTML, v))
			group = f.S("forcegroup_id")
		}
		var opts strings.Builder
		for _, o := range byGroup[f.S("forcegroup_id")] {
			if o.S("force_id") != f.S("force_id") {
				fmt.Fprintf(&opts, "                            <option value=\"%s\">%s</option>\n", o.S("force_id"), o.S("name"))
			}
		}
		v["forceoptions"] = opts.String()
		if f.I("type") != 6 {
			v["equipment"] = f.S("weaponname") + "<br/>\n                " + f.S("armorname") + "<br/>\n"
		}
		if f.I("location_id") != nid {
			v["extramessage"] = `You will lose that force\'s weapons and armor!`
		}
		b.WriteString(tpl(groupForceHTML, v))
	}
	b.WriteString("</table></div>")
	p.Raw(b.String())
}

const groupHeaderHTML = `<div class="panel panel-default">
    <div class="panel-heading">
        <div class="row">
            <div class="col-md-6">
                <form action="groupforces.php" method="post">
                    <input type="hidden" name="token_groupforces" value="{$token}"/>
                    <input type="hidden" name="forcegroup_id" value="{$f['forcegroup_id']}"/>
                    <div class="input-group">
                        <input name="name" class="form-control" placeholder="Name" value="{$f['groupname']}"/>
                        <span class="input-group-btn">
                            <input type="submit" name="renamegroup" value="Rename Group" class="btn btn-info"/>
                        </span>
                    </div>
                </form>
            </div>
            <div class="col-md-6">
                <form action="groupforces.php" method="post">
                    <input type="hidden" name="token_groupforces" value="{$token}"/>
                    <input type="hidden" name="forcegroup_id" value="{$f['forcegroup_id']}"/>
                    <div class="input-group">
                        <input type="submit" name="mergegroups" value="Merge With" class="btn btn-info"/>
                        <select name="targetgroup_id" class="form-control" style="width:200px;">
{$groupoptions}                        </select>
                    </div>
                </form>
            </div>
        </div>
    </div>
    <table class="table table-bordered">

`

const groupForceHTML = `        <tr>
            <td>
                <form action="groupforces.php" method="post">
                    <input type="hidden" name="token_groupforces" value="{$token}"/>
                    <input type="hidden" name="force_id" value="{$f['force_id']}"/>
                    <div class="input-group">
                        <input name="name" class="form-control" placeholder="Name" value="{$f['name']}"/>
                        <span class="input-group-btn">
                            <input type="submit" name="renameforce" value="Rename Force" class="btn btn-info"/>
                        </span>
                    </div>
                </form>
                <br/>
                {$f['typename']}<br/>
                {$equipment}                Size: {$f['size']}<br/>
                Training: {$f['training']}</td><td>
                <form action="groupforces.php" method="post">
                    <input type="hidden" name="token_groupforces" value="{$token}"/>
                    <input type="hidden" name="force_id" value="{$f['force_id']}"/>
                    <div class="input-group">
                        <input type="submit" name="combineforces" value="Combine With" class="btn btn-info"/>
                        <select name="targetforce_id" class="form-control" style="width:200px;">
{$forceoptions}                        </select>
                    </div>
                </form><br/>
                <form action="groupforces.php" method="post">
                    <input type="hidden" name="token_groupforces" value="{$token}"/>
                    <input type="hidden" name="force_id" value="{$f['force_id']}"/>
                    <div class="input-group">
                        <span class="input-group-btn">
                            <input type="submit" name="splitforce" value="Split Off:" class="btn btn-info"/>
                        </span>
                        <input name="size" class="form-control" placeholder="Size" style="width:60px;"/>
                    </div>
                </form><br/>
                <form action="groupforces.php" method="post">
                    <input type="hidden" name="token_groupforces" value="{$token}"/>
                    <input type="hidden" name="force_id" value="{$f['force_id']}"/>
                    <input type="submit" name="splitgroup" value="Make Own Group" class="btn btn-info"/>
                </form><br/>
                <form action="groupforces.php" method="post">
                    <input type="hidden" name="token_groupforces" value="{$token}"/>
                    <input type="hidden" name="force_id" value="{$f['force_id']}"/>
                    <input type="submit" onclick="return confirm('Really destroy {$f['name']}? {$extramessage}')" name="destroy" value="Destroy" class="btn btn-danger"/>
                </form>
            </td>
        </tr>
`

func groupForcesAction(p *Page) {
	db, nid := p.db, p.NationID()
	forceID := p.PostI("force_id")
	validName := func(name, what string) bool {
		if name == "" {
			p.Err("No name entered.")
		}
		if !alnumSpace(name, true) {
			p.Err("Only English letters and numbers for the %s name.", what)
		}
		return !p.HasErrors()
	}
	switch {
	case p.PostB("renameforce"):
		if validName(p.Post("name"), "force") {
			db.Exec("UPDATE forces SET name = ? WHERE force_id = ? AND nation_id = ?", p.Post("name"), forceID, nid)
		}
	case p.PostB("renamegroup"):
		if validName(p.Post("name"), "group") {
			db.Exec("UPDATE forcegroups SET name = ? WHERE forcegroup_id = ? AND nation_id = ?", p.Post("name"), p.PostI("forcegroup_id"), nid)
		}
	case p.PostB("mergegroups"):
		from, to := p.PostI("forcegroup_id"), p.PostI("targetgroup_id")
		this := db.One("SELECT location_id, departuredate FROM forcegroups WHERE forcegroup_id = ? AND nation_id = ?", from, nid)
		that := db.One("SELECT location_id, departuredate FROM forcegroups WHERE forcegroup_id = ? AND nation_id = ?", to, nid)
		switch {
		case this == nil:
			p.Err("Group not found!")
		case that == nil:
			p.Err("Target group not found!")
		case from == to:
			p.Err("That's the same group.")
		case this.B("departuredate") || that.B("departuredate"):
			p.Err("Groups in transit cannot merge!")
		case this.S("location_id") != that.S("location_id"):
			p.Err("Groups must be in the same place to merge!")
		default:
			db.Exec("UPDATE forces SET forcegroup_id = ? WHERE forcegroup_id = ?", to, from)
			db.Exec("DELETE FROM forcegroups WHERE forcegroup_id = ?", from)
		}
	case p.PostB("splitgroup"):
		g := db.One(`SELECT fg.departuredate, fg.forcegroup_id, fg.location_id, fg.attack_mission, fg.oldmission, f.name
			FROM forcegroups fg INNER JOIN forces f ON f.forcegroup_id = fg.forcegroup_id WHERE f.force_id = ? AND f.nation_id = ?`, forceID, nid)
		switch {
		case g == nil:
			p.Err("Group not found!")
		case g.B("departuredate"):
			p.Err("Cannot split a force group in transit!")
		case db.One("SELECT COUNT(*) AS count FROM forces WHERE forcegroup_id = ?", g.I("forcegroup_id")).I("count") == 1:
			p.Err("That force is already in its own group!")
		default:
			id := db.Exec(`INSERT INTO forcegroups (name, location_id, destination_id, nation_id, attack_mission, oldmission) VALUES (?, ?, 0, ?, ?, ?)`,
				g.S("name"), g.I("location_id"), nid, g.I("attack_mission"), g.I("oldmission"))
			db.Exec("UPDATE forces SET forcegroup_id = ? WHERE force_id = ?", id, forceID)
		}
	case p.PostB("combineforces"):
		target := p.PostI("targetforce_id")
		this := db.One("SELECT forcegroup_id, weapon_id, size, training, armor_id, nation_id, type FROM forces WHERE force_id = ?", forceID)
		that := db.One("SELECT forcegroup_id, weapon_id, training, armor_id, nation_id, type FROM forces WHERE force_id = ?", target)
		if this.I("nation_id") != nid || that.I("nation_id") != nid || forceID == target {
			p.Err("Force not found.")
		}
		for _, check := range []struct{ col, msg string }{
			{"forcegroup_id", "Forces must be in the same group to be combined."},
			{"weapon_id", "Forces must have the same weapons to be combined."},
			{"armor_id", "Forces must have the same armor to be combined."},
			{"training", "Forces must have the same amount of training to be combined."},
			{"type", "Forces must be of the same type to be combined."},
		} {
			if this.S(check.col) != that.S(check.col) {
				p.Err(check.msg)
			}
		}
		if !p.HasErrors() {
			db.Exec("UPDATE forces SET size = size + ? WHERE force_id = ?", this.I("size"), target)
			db.Exec("DELETE FROM forces WHERE force_id = ?", forceID)
		}
	case p.PostB("splitforce"):
		size := p.PostI("size")
		f := db.One("SELECT forcegroup_id, weapon_id, size, training, armor_id, type, name FROM forces WHERE force_id = ? AND nation_id = ?", forceID, nid)
		if f == nil {
			p.Err("Force not found.")
		} else if size >= f.I("size") {
			p.Err("Can't split off all of it.")
		} else if size < 1 {
			p.Err("No size entered.")
		}
		if db.One("SELECT COUNT(*) AS totalcount FROM forces WHERE nation_id = ?", nid).I("totalcount") >= 100 {
			p.Err("You have too many different forces.")
		}
		if !p.HasErrors() {
			name := f.S("name") + " Splitoff"
			if len(name) > 50 {
				name = name[:50]
			}
			db.Exec("UPDATE forces SET size = size - ? WHERE force_id = ?", size, forceID)
			db.Exec(`INSERT INTO forces (forcegroup_id, weapon_id, size, training, armor_id, type, name, nation_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				f.I("forcegroup_id"), f.I("weapon_id"), size, f.I("training"), f.I("armor_id"), f.I("type"), name, nid)
		}
	case p.PostB("destroy"):
		f := db.One(`SELECT fg.location_id, f.forcegroup_id, f.weapon_id, f.armor_id, f.size FROM forces f
			INNER JOIN forcegroups fg ON fg.forcegroup_id = f.forcegroup_id WHERE f.force_id = ? AND f.nation_id = ?`, forceID, nid)
		if f == nil {
			p.Err("Force not found.")
			return
		}
		if f.I("location_id") == nid {
			if w := f.I("weapon_id"); w != 0 {
				db.Exec("INSERT INTO weapons (weapon_id, nation_id, amount) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?", w, nid, f.I("size"), f.I("size"))
			}
			if a := f.I("armor_id"); a != 0 {
				db.Exec("INSERT INTO armor (armor_id, nation_id, amount) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?", a, nid, f.I("size"), f.I("size"))
			}
		}
		db.Exec("DELETE FROM forces WHERE force_id = ?", forceID)
		db.Exec("DELETE FROM forcegroups WHERE forcegroup_id NOT IN (SELECT forcegroup_id FROM forces)")
	}
}

// travelHours is how long a trip takes, counted from the first war tick
// after departure.
func travelHours(attack, sameRegion bool) int {
	switch {
	case !attack && sameRegion:
		return 12
	case attack && sameRegion:
		return 24
	case !attack:
		return 36
	}
	return 48
}

// arrivalDate predicts when a force group will arrive.
func arrivalDate(departure string, attack, sameRegion bool) string {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", departure, time.UTC)
	if err != nil {
		return ""
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	tickAfter := day.Add(12 * time.Hour)
	if t.Hour() >= 12 {
		tickAfter = day.Add(24 * time.Hour)
	}
	return tickAfter.Add(time.Duration(travelHours(attack, sameRegion)) * time.Hour).Format("2006-01-02 15:04:05")
}

func pageSendForces(p *Page) {
	p.title = "Send Forces - "
	nation := p.needsNation()
	db, nid := p.db, p.NationID()
	p.csrf("sendforces")
	if !p.HasErrors() && p.isPost {
		sendForcesAction(p, nation)
	}

	forces := db.Query(`SELECT fg.name AS groupname, fg.attack_mission, fg.departuredate, n.name AS destinationname, n.region AS destinationregion,
		fg.destination_id, n2.name AS locationname, n2.region AS locationregion, n2.user_id AS locationowner, fg.oldmission,
		f.*, fg.location_id, rd1.name AS weaponname, rd2.name AS armorname FROM forces f
		INNER JOIN forcegroups fg ON f.forcegroup_id = fg.forcegroup_id
		LEFT JOIN weapondefs rd1 ON f.weapon_id = rd1.weapon_id
		LEFT JOIN armordefs rd2 ON f.armor_id = rd2.armor_id
		LEFT JOIN nations n ON fg.destination_id = n.nation_id
		LEFT JOIN nations n2 ON fg.location_id = n2.nation_id
		WHERE f.nation_id = ? ORDER BY fg.forcegroup_id, f.name`, nid)
	var b strings.Builder
	b.WriteString("<center>\n")
	if len(forces) == 0 {
		b.WriteString("You have no forces to send!\n</center>")
		p.Raw(b.String())
		return
	}
	token := p.token("sendforces")
	b.WriteString(`<div id="container" class="js-masonry" data-masonry-options='{ "itemSelector": ".masonryitem", "isFitWidth": true }'>
`)
	group := ""
	for _, f := range forces {
		decorateForce(f)
		f["locationregionname"] = regionNames[f.I("locationregion")]
		f["destinationregionname"] = regionNames[f.I("destinationregion")]
		if f.S("forcegroup_id") != group {
			if group != "" {
				b.WriteString("</table></div></div>")
			}
			v := V{"f": f, "token": token}
			if !f.B("departuredate") {
				v["mission"] = "Defending"
				if f.B("attack_mission") {
					v["mission"] = "Attacking"
				}
				if f.I("locationowner") == p.UserID() && f.I("location_id") != nid {
					v["transfer"] = tpl(`				<input type="submit" name="transfer" onclick="return confirm('Really transfer ownership of this group to {$f['locationname']}?')" value="Transfer Group" class="btn btn-warning"/>
`, v)
				}
				b.WriteString(tpl(sendGroupHomeHTML, v))
			} else {
				f["missiondescription"] = "defend"
				if f.B("attack_mission") {
					f["missiondescription"] = "attack"
				}
				f["arrivaldate"] = arrivalDate(f.S("departuredate"), f.B("attack_mission"), f.S("destinationregion") == f.S("locationregion"))
				switch {
				case f.B("oldmission") && f.I("destination_id") != nid:
					v["recall"] = `<input type="submit" onclick="return confirm('Really send your forces home?')" name="recall" value="Go Home" class="btn btn-info"/>`
				case !f.B("oldmission"):
					v["recall"] = `<input type="submit" onclick="return confirm('Really recall this force?')" name="recall" value="Recall Force" class="btn btn-info"/>`
				}
				b.WriteString(tpl(sendGroupMovingHTML, v))
			}
			group = f.S("forcegroup_id")
		}
		b.WriteString(forceUnitCell(f, false))
	}
	b.WriteString("</table></div></div></div>\n</center>")
	p.Raw(b.String())
}

const sendGroupHomeHTML = `<div class="masonryitem" style="padding: 10px; width: 300px">
	<div class="panel panel-default">
	    <div class="panel-heading">
	    	{$f['groupname']}<br/>
			{$mission} at <a href="viewnation.php?nation_id={$f['location_id']}">{$f['locationname']}</a> ({$f['locationregionname']})<br/>
			<form action="sendforces.php" method="post">
				<input type="hidden" name="token_sendforces" value="{$token}"/>
				<input type="hidden" name="forcegroup_id" value="{$f['forcegroup_id']}"/>
				<input name="name" class="form-control" placeholder="Target Nation" value="" style="width:233px;"/>
				<input type="submit" onclick="return confirm('Really attack this nation?')" name="attack" value="Attack Nation" class="btn btn-danger"/>
				<input type="submit" onclick="return confirm('Really defend this nation?')" name="defend" value="Defend Nation" class="btn btn-success"/>
{$transfer}			</form>
		</div>
		<table class="table table-striped table-bordered">
`

const sendGroupMovingHTML = `<div class="masonryitem" style="padding: 10px; width: 300px">
	<div class="panel panel-default">
	    <div class="panel-heading">
{$f['groupname']}<br/>
Coming from <a href="viewnation.php?nation_id={$f['location_id']}">{$f['locationname']}</a> ({$f['locationregionname']})<br/>
Going to <a href="viewnation.php?nation_id={$f['destination_id']}">{$f['destinationname']}</a> ({$f['destinationregionname']}) to {$f['missiondescription']}<br/>
Left on {$f['departuredate']}<br/>
Arrives at {$f['arrivaldate']}<br/>
<form action="sendforces.php" method="post">
<input type="hidden" name="token_sendforces" value="{$token}"/>
<input type="hidden" name="forcegroup_id" value="{$f['forcegroup_id']}"/>
{$recall}
</form></div>
<table class="table table-striped table-bordered">
`

func sendForcesAction(p *Page, nation Row) {
	db, nid, uid := p.db, p.NationID(), p.UserID()
	groupID := p.PostI("forcegroup_id")
	switch {
	case p.PostB("attack"), p.PostB("defend"):
		attack := p.PostB("attack")
		mission, opposite := 0, 1
		if attack {
			mission, opposite = 1, 0
		}
		group := db.One(`SELECT fg.forcegroup_id, fg.departuredate, fg.location_id, n.region FROM forcegroups fg
			LEFT JOIN nations n ON fg.location_id = n.nation_id WHERE fg.forcegroup_id = ? AND fg.nation_id = ?`, groupID, nid)
		target := db.One("SELECT n.*, u.stasismode, u.alliance_id FROM nations n INNER JOIN users u ON n.user_id = u.user_id WHERE n.name = ?", p.Post("name"))
		sent := db.One("SELECT COUNT(*) AS count FROM forcegroups WHERE nation_id = ? AND destination_id = ?", nid, target.I("nation_id")).I("count")
		switch {
		case group == nil:
			p.Err("Shenanigans.")
		case target == nil:
			p.Err("Nation not found.")
		case target.I("user_id") == p.app.cfg.AdminUserID:
			p.Err("You're just a fountain of bad ideas today, aren't you?")
		case group.I("location_id") == target.I("nation_id"):
			p.Err("They're already there.")
		case target.I("user_id") == uid && attack:
			p.Err("You already got that one.")
		case group.B("departuredate"):
			p.Err("That force is already on the move.")
		case target.I("age") < 21 && attack:
			p.Err("That nation hasn't been active for three weeks.")
		case nation.I("age") < 21:
			p.Err("Your nation hasn't been active for three weeks.")
		case target.B("stasismode"):
			p.Err("That nation's owner is in stasis.")
		case nation.I("alliance_id") != 0 && nation.I("alliance_id") == target.I("alliance_id") && attack:
			p.Err("You cannot attack someone in your alliance.")
		case sent >= 10:
			p.Err("You cannot send more than 10 attacks to the same target nation")
		}
		if p.HasErrors() {
			return
		}
		tid := target.I("nation_id")
		if target.I("region") != group.I("region") {
			carriers := db.One("SELECT SUM(ad.carrying * f.size) AS totalcarrier FROM forces f INNER JOIN armordefs ad ON f.armor_id = ad.armor_id WHERE f.forcegroup_id = ?", groupID).I("totalcarrier")
			carried := db.One("SELECT SUM(size) AS totalcarried FROM forces WHERE forcegroup_id = ? AND (type = 1 OR type = 2 OR type = 4)", groupID).I("totalcarried")
			if carriers < carried {
				p.Err("You don't have the carrying capacity in your group to go to a different region!")
			}
		}
		if attack && db.One("SELECT SUM(size) AS totalalicorns FROM forces WHERE forcegroup_id = ? AND type = 6", groupID).B("totalalicorns") {
			p.Err("Your alicorns refuse to attack another nation.")
		}
		if db.One(`SELECT forcegroup_id FROM forcegroups
			WHERE ((attack_mission = ? AND destination_id = ? AND departuredate IS NOT NULL)
			OR (location_id = ? AND departuredate IS NOT NULL AND oldmission = ?)
			OR (location_id = ? AND departuredate IS NULL AND attack_mission = ?))
			AND nation_id = ?`, opposite, tid, tid, opposite, tid, opposite, nid) != nil {
			p.Err("You already have a force doing the opposite!")
		}
		if p.HasErrors() {
			return
		}
		if attack {
			econ := nation.S("economy")
			if econ == target.S("economy") && (econ == "Free Market" || econ == "State Controlled") {
				p.Info("You lose 100 satisfaction for attacking another %s economy!", econ)
				db.Exec("UPDATE nations SET satisfaction = satisfaction - 100 WHERE nation_id = ?", nid)
			}
			free := func(g string) bool { return g == "Democracy" || g == "Independence" || g == "Decentralization" }
			if free(target.S("government")) && free(nation.S("government")) {
				p.Info("You lose 200 satisfaction for attacking another free country!")
				db.Exec("UPDATE nations SET satisfaction = satisfaction - 200 WHERE nation_id = ?", nid)
			}
		}
		db.Exec(`UPDATE forcegroups SET destination_id = ?, oldmission = attack_mission, attack_mission = ?, departuredate = NOW()
			WHERE forcegroup_id = ?`, tid, mission, groupID)
		p.Info("Force en route.")

	case p.PostB("transfer"):
		g := db.One(`SELECT fg.forcegroup_id, fg.departuredate, fg.location_id FROM forcegroups fg
			LEFT JOIN nations n ON fg.location_id = n.nation_id
			WHERE fg.forcegroup_id = ? AND fg.location_id != ? AND fg.nation_id = ? AND n.user_id = ?`, groupID, nid, nid, uid)
		if g == nil {
			p.Err("Shenanigans.")
		} else if g.B("departuredate") {
			p.Err("Ixnay.")
		} else {
			db.Exec("UPDATE forcegroups SET nation_id = ? WHERE forcegroup_id = ?", g.I("location_id"), groupID)
			db.Exec("UPDATE forces SET nation_id = ? WHERE forcegroup_id = ?", g.I("location_id"), groupID)
			p.Info("Group transferred.")
		}

	case p.PostB("recall"):
		g := db.One("SELECT forcegroup_id, departuredate, oldmission FROM forcegroups WHERE forcegroup_id = ? AND nation_id = ?", groupID, nid)
		switch {
		case g == nil:
			p.Err("Shenanigans.")
		case !g.B("departuredate"):
			p.Err("They're not on the move.")
		case g.B("oldmission"):
			db.Exec("UPDATE forcegroups SET departuredate = NOW(), destination_id = ?, attack_mission = 0 WHERE forcegroup_id = ?", nid, groupID)
			p.Info("Force sent home.")
		default:
			db.Exec("UPDATE forcegroups SET departuredate = NULL, destination_id = 0, attack_mission = oldmission WHERE forcegroup_id = ?", groupID)
			p.Info("Force recalled.")
		}
	}
}

func pageForcesYourWay(p *Page) {
	p.title = "Incoming Forces - "
	nation := p.needsNation()
	var attackers, defenders []Row
	for _, f := range p.db.Query(`SELECT fg.name AS groupname, fg.forcegroup_id, fg.attack_mission, fg.departuredate,
		n.nation_id AS ownernation_id, n.name AS ownername, n.region AS ownerregion,
		n2.name AS locationname, n2.region AS locationregion,
		f.*, fg.location_id, rd1.name AS weaponname, rd2.name AS armorname FROM forces f
		INNER JOIN forcegroups fg ON f.forcegroup_id = fg.forcegroup_id
		LEFT JOIN weapondefs rd1 ON f.weapon_id = rd1.weapon_id
		LEFT JOIN armordefs rd2 ON f.armor_id = rd2.armor_id
		LEFT JOIN nations n ON fg.nation_id = n.nation_id
		LEFT JOIN nations n2 ON fg.location_id = n2.nation_id
		WHERE fg.departuredate IS NOT NULL AND fg.destination_id = ?
		ORDER BY fg.forcegroup_id, f.name`, p.NationID()) {
		decorateForce(f)
		f["arrivaldate"] = arrivalDate(f.S("departuredate"), f.B("attack_mission"), nation.S("region") == f.S("locationregion"))
		f["ownerregionname"] = regionNames[f.I("ownerregion")]
		f["locationregionname"] = regionNames[f.I("locationregion")]
		if f.B("attack_mission") {
			attackers = append(attackers, f)
		} else {
			defenders = append(defenders, f)
		}
	}
	p.Raw(incomingCards(attackers, "Attackers", "container1", "No one is coming to attack you."))
	p.Raw(incomingCards(defenders, "Defenders", "container2", "No one is coming to protect you."))
}

func incomingCards(units []Row, heading, containerID, empty string) string {
	if len(units) == 0 {
		return fmt.Sprintf(`<div class="row"><center><h5>%s</h5></center></div>`, empty)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<center><h4>%s</h4></center>
<div id="%s" class="js-masonry" data-masonry-options='{ "itemSelector": ".masonryitem", "isFitWidth": true }'>
`, heading, containerID)
	group := ""
	for _, f := range units {
		if f.S("forcegroup_id") != group {
			if group != "" {
				b.WriteString("</table></div></div>")
			}
			b.WriteString(tpl(`<div class="masonryitem" style="padding: 10px; width: 300px">
	<div class="panel panel-default">
	    <div class="panel-heading">{$f['groupname']}<br/>
Owned by <a href="viewnation.php?nation_id={$f['ownernation_id']}">{$f['ownername']}</a> ({$f['ownerregionname']})<br/>
Coming from <a href="viewnation.php?nation_id={$f['location_id']}">{$f['locationname']}</a> ({$f['locationregionname']})<br/>
Left on {$f['departuredate']}<br/>
Arriving at {$f['arrivaldate']}</div>
<table class="table table-striped table-bordered">
`, V{"f": f}))
			group = f.S("forcegroup_id")
		}
		b.WriteString(forceUnitCell(f, true))
	}
	b.WriteString("</table></div></div></div>")
	return b.String()
}

func pageMakeEquipment(p *Page) {
	p.needsNation()
	db, nid := p.db, p.NationID()
	mode := p.GP("mode")
	if mode != "weapons" && mode != "armor" {
		p.Redirect("overview.php")
	}
	singular, plural := "weapon", "weapons"
	p.title = "Make Weapons - "
	if mode == "armor" {
		singular, plural = "armor", "armor"
		p.title = "Make Armor - "
	}
	nation := db.One("SELECT * FROM nations WHERE nation_id = ?", nid)
	p.csrf("make" + plural)
	if !p.HasErrors() {
		if recipeID := p.PostI(singular + "recipe_id"); recipeID != 0 {
			makeEquipment(p, nation, singular, plural, recipeID, p.PostI("times"))
			nation = db.One("SELECT * FROM nations WHERE nation_id = ?", nid)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<center><div class="well">Funds: <span class="text-success">%s Bits</span></div></center>
`, commas(nation.S("funds")))
	typ := int64(0)
	token := p.token("make" + plural)
	recipes := db.Query("SELECT wr.name, wr." + singular + "recipe_id AS recipe_id, wr.description, wr.cost, wd.type FROM " + singular + "recipes wr LEFT JOIN " +
		singular + "defs wd ON wd." + singular + "_id = wr." + singular + "_id ORDER BY wd.type, wr.cost, wr.name")
	for _, r := range recipes {
		if r.I("type") != typ {
			if typ != 0 {
				b.WriteString("</table></center>")
			}
			fmt.Fprintf(&b, `    <center><h3>%s</h3></center>
    <center><table class="table table-striped table-bordered">
`, forceTypeNames[r.I("type")])
			typ = r.I("type")
		}
		desc := r.S("description")
		for _, it := range db.Query("SELECT rd.name, wri.is_used_up, wri.amount FROM "+singular+"recipeitems wri INNER JOIN resourcedefs rd ON rd.resource_id = wri.resource_id WHERE wri."+
			singular+"recipe_id = ? ORDER BY is_used_up DESC", r.I("recipe_id")) {
			if it.B("is_used_up") {
				desc += fmt.Sprintf("%s %s<br/>", it.S("amount"), it.S("name"))
			} else {
				desc += fmt.Sprintf("(Requires %s %s)<br/>", it.S("amount"), it.S("name"))
			}
		}
		b.WriteString(tpl(`<tr><td><div class="row">
	<div class="col-md-6"><p><b>{$name}</b><br/>{$desc}</p></div>
	<div class="col-md-1"><p class="text-danger">{$cost}</p></div>
	<div class="col-md-5">
		<form action="makeequipment.php" method="post">
            <input type="hidden" name="token_make{$plural}" value="{$token}"/>
            <input type="hidden" name="mode" value="{$mode}"/>
			<input type="hidden" name="{$singular}recipe_id" value="{$recipe_id}"/>
			<div class="row">
				<div class="col-xs-7">
					<input type="submit" value="{$name}" class="btn btn-success btn-block"/>
				</div>
				<div class="col-xs-5">
					<div class="input-group">
					    <input name="times" value="1" class="form-control" type="text"/>
					    <span class="input-group-addon">times</span>
					</div>
				</div>
			</div>
		</form>
	</div>
</div></td></tr>
`, V{"name": r.S("name"), "desc": desc, "cost": commas(r.S("cost")), "plural": plural, "token": token, "mode": mode,
			"singular": singular, "recipe_id": r.S("recipe_id")}))
	}
	b.WriteString("</table></center>")
	p.Raw(b.String())
}

func makeEquipment(p *Page, nation Row, singular, plural string, recipeID, times int64) {
	db, nid := p.db, p.NationID()
	if times < 1 {
		p.Err("Whole numbers greater than 0.")
	}
	recipe := db.One("SELECT w.*, wd.name AS itemname FROM "+singular+"recipes w LEFT JOIN "+singular+"defs wd ON w."+singular+"_id = wd."+singular+
		"_id WHERE w."+singular+"recipe_id = ?", recipeID)
	if recipe == nil {
		p.Err("Nice try!")
		return
	}
	cost := recipe.I("cost") * times
	if nation.I("funds") < cost {
		p.Err("You don't have enough money to build that.")
	}
	if p.HasErrors() {
		return
	}
	var uses [][2]int64
	var spent []string
	for _, it := range db.Query("SELECT ri.resource_id, ri.is_used_up, ri.amount, rd.name FROM "+singular+"recipeitems ri LEFT JOIN resourcedefs rd ON rd.resource_id = ri.resource_id WHERE ri."+
		singular+"recipe_id = ?", recipeID) {
		need := it.I("amount")
		if it.B("is_used_up") {
			need *= times
			uses = append(uses, [2]int64{it.I("resource_id"), need})
			spent = append(spent, fmt.Sprintf("You spent %d %s.", need, it.S("name")))
		}
		have := db.One("SELECT amount FROM resources WHERE nation_id = ? AND resource_id = ?", nid, it.I("resource_id"))
		if have == nil {
			p.Err("You don't have any %s.", it.S("name"))
		} else if need > have.I("amount") {
			p.Err("You don't have enough %s to do that.", it.S("name"))
		}
	}
	if p.HasErrors() {
		return
	}
	p.infos = spent
	for _, u := range uses {
		db.Exec("UPDATE resources SET amount = amount - ? WHERE nation_id = ? AND resource_id = ?", u[1], nid, u[0])
	}
	if itemID := recipe.I(singular + "_id"); itemID != 0 {
		gained := recipe.I("amount") * times
		db.Exec("INSERT INTO "+plural+" (nation_id, "+singular+"_id, amount) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?", nid, itemID, gained, gained)
		p.Info("You gained %d %s.", gained, recipe.S("itemname"))
	}
	if cost != 0 {
		db.Exec("UPDATE nations SET funds = funds - ? WHERE nation_id = ?", cost, nid)
		p.Info("You paid %s bits.", commas(cost))
	}
	p.Info("%s completed successfully.", recipe.S("name"))
	db.Exec("UPDATE resources SET disabled = amount WHERE disabled > amount AND nation_id = ?", nid)
	db.Exec("DELETE FROM resources WHERE amount = 0 AND nation_id = ?", nid)
	db.report(nid, joinBR(p.infos))
}
