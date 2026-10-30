package clop

import (
	"fmt"
	"math"
	"strings"
)

// Names used by the war calculator form (huge-ovipositor.html).
var (
	calcForceTypes = map[string]int64{"Cavalry": 1, "Tanks": 2, "Pegasi": 3, "Unicorns": 4, "Naval": 5, "Alicorns": 6}
	calcTypeOrder  = []string{"Cavalry", "Tanks", "Pegasi", "Unicorns", "Naval", "Alicorns"}
	calcWeapons    = map[string]int64{"ScroungedWeapons": 0, "PRC-E6": 1, "PRC-E7": 2, "PRC-E8": 3, "ACFU": 4, "ATFU": 5, "APFU": 6, "AUFU": 7, "K9P": 8,
		"ELBO-GRS": 9, "Chem-LightBattery": 10, "PropWash": 11, "SteamBucket": 12, "CanopyLights": 13, "LongStand": 14, "LongWeight": 15,
		"GridSquares": 16, "Shoreline": 17, "WaterHammer": 18, "WaterlineEraser": 19}
	calcArmor = map[string]int64{"ScroungedArmor": 0, "Barding": 1, "Bigdog": 2, "Nope": 3, "Trundle": 4, "Shepherd": 5, "Ohno": 6, "Titan": 7,
		"Cooler": 8, "Wonder": 9, "Griffin": 10, "Dragon": 11, "Hornshield": 12, "Librarian": 13, "Shining": 14, "D2A": 15, "C-PON3": 16,
		"Esohes": 17, "Shubidu": 18}
)

// pageWarCalc simulates one round of combat from the calculator form,
// using the same battle engine as the real war ticks.
func pageWarCalc(p *Page) {
	if !p.isPost {
		p.Redirect("huge-ovipositor.html")
	}
	db := p.db
	weapons := map[int64]Row{}
	for _, w := range db.Query("SELECT * FROM weapondefs") {
		weapons[w.I("weapon_id")] = w
	}
	armors := map[int64]Row{}
	for _, a := range db.Query("SELECT * FROM armordefs") {
		armors[a.I("armor_id")] = a
	}

	// Attacker rows come first in the shared weapon/armor/size/training arrays.
	attackTypes, defendTypes := p.form["attack_type[]"], p.form["defend_type[]"]
	field := func(name string, i int) string {
		vals := p.form[name+"[]"]
		if i < len(vals) {
			return vals[i]
		}
		return ""
	}
	b := &Battle{}
	if p.Post("defender_bonus") == "Yes" {
		b.Shielded = func(*Unit) bool { return true }
	}
	var nextID int64
	typeOf := map[*Unit]string{}
	add := func(typeName string, i int, attacking bool) {
		typ, ok := calcForceTypes[typeName]
		if !ok {
			return
		}
		weapon, armor := field("weapon", i), field("armor", i)
		size, training := toInt(field("size", i)), toInt(field("training", i))
		if size < 1 {
			return
		}
		if training > 20 {
			training = 20
		} else if training < 0 {
			training = 0
		}
		prefix := "D_"
		if attacking {
			prefix = "A_"
		}
		r := Row{"weapon_id": str(calcWeapons[weapon]), "armor_id": str(calcArmor[armor]), "type": str(typ),
			"size": str(size), "training": str(training), "nation_id": "1"}
		if !attacking {
			r["nation_id"] = "2"
		}
		for k, v := range weapons[calcWeapons[weapon]] {
			if strings.HasPrefix(k, "dmg_") {
				r[k] = v
			}
		}
		for k, v := range armors[calcArmor[armor]] {
			if strings.HasPrefix(k, "arm_") {
				r[k] = v
			}
		}
		nextID++
		r["force_id"] = str(nextID)
		r["name"] = esc(fmt.Sprintf("%s%s_%s_%s_size_%d_train_%d", prefix, typeName, weapon, armor, size, training))
		u := newUnit(r, attacking)
		typeOf[u] = typeName
		b.Units = append(b.Units, u)
	}
	for i, t := range attackTypes {
		add(t, i, true)
	}
	for j, t := range defendTypes {
		add(t, len(attackTypes)+j, false)
	}

	initial := map[bool]map[string]int64{true: {}, false: {}}
	remaining := map[bool]map[string]int64{true: {}, false: {}}
	damage := map[bool]map[int]float64{true: {}, false: {}}
	for _, u := range b.Units {
		initial[u.Attacking][typeOf[u]] += u.Size
		for t := 1; t <= 5; t++ {
			damage[u.Attacking][t] += u.Dmg[t] * float64(u.Size)
		}
	}
	b.Run()

	var out strings.Builder
	fmt.Fprintf(&out, "Defender Bonus: %s<h2>Battle Result</h2>", esc(p.Post("defender_bonus")))
	for _, h := range b.Hits() {
		fmt.Fprintf(&out, "%s (size %d) hit %s (size %d) for %s damage (%d hits)<br>",
			h.Attacker.Name, h.Attacker.Size, h.Defender.Name, h.Defender.Size, phpFloat(round(h.Damage, 6)), h.Hits)
	}
	for _, u := range b.Units {
		loss := u.Losses()
		left := u.Size
		switch {
		case loss <= 0:
		case loss < u.Size:
			fmt.Fprintf(&out, "%s lost %d size!<br>", u.Name, loss)
			left -= loss
		default:
			fmt.Fprintf(&out, "%s IS KILL<br>", u.Name)
			left = 0
		}
		remaining[u.Attacking][typeOf[u]] += left
	}
	summary := func(attacking bool, heading, label string) {
		fmt.Fprintf(&out, "<h2>%s Summary:</h2>", heading)
		out.WriteString(`<div><table border="1" style="float: left"><tr><td>Attack vs</td><td>Total Damage</td></tr>`)
		for t := 1; t <= 5; t++ {
			fmt.Fprintf(&out, "<tr><td>%s</td><td>%s</td></tr>", calcTypeOrder[t-1], phpFloat(damage[attacking][t]))
		}
		out.WriteString("</table></div>")
		fmt.Fprintf(&out, `<div><table border="1" style="float: left"><tr><td>%s</td><td>Initial</td><td>Died</td><td>Remaining</td></tr>`, label)
		for _, name := range calcTypeOrder {
			if attacking && name == "Alicorns" {
				continue
			}
			init, rem := initial[attacking][name], remaining[attacking][name]
			fmt.Fprintf(&out, "<tr><td>%s</td><td>%d </td><td>%d</td><td>%d</td></tr>", name, init, init-rem, rem)
		}
		out.WriteString("</table></div>")
	}
	summary(true, "Attackers", "Attackers")
	summary(false, "Defenders", "Defenders")
	p.Raw(out.String())
}

// pageAscendCalc estimates how many alicorns an ascending player needs to
// hold off the empires' growing airstrikes.
func pageAscendCalc(p *Page) {
	p.title = "Ascend Calc - "
	var b strings.Builder
	if p.PostB("ascend-calc") {
		ticks := toInt(p.Post("ticks"))
		rep := abs(toInt(p.Post("reputation")))
		pegasi := toInt(p.Post("pegasi"))
		barracks := toFloat(p.Post("barracks"))
		fmt.Fprintf(&b, "ticks %d<br>rep %d<br>pegasi %d<br>barracks/training %s<br>", ticks, rep, pegasi, phpFloat(barracks))
		if ticks > 10000 {
			ticks = 10000
		}
		for x := int64(0); x <= ticks; x++ {
			rep++
			var fresh int64
			if rep >= 25 {
				fresh = ceil(float64(rep)/4) * 2
			}
			pegasi += fresh
			fmt.Fprintf(&b, "<br>tick %d<br>rep: %d<br>new_pegasi: %d<br>total pegasi: %d<br>", x, rep, fresh, pegasi)
		}
		// Pegasi armor vs naval is 0.45; alicorns defend at home (.75x).
		pegasiDmg := round(1.4*.1*math.Pow(1.5, (20-barracks)/20), 3) * .75
		alicornDmg := round(10*.45*math.Pow(1.5, (barracks-20)/20), 3)
		defend := ceil(float64(pegasi)*pegasiDmg) + 1
		kill := ceil(float64(pegasi) / alicornDmg)
		killed := floor(alicornDmg * float64(defend))
		fmt.Fprintf(&b, `<br><br>SUMMARY<br>=======<br>Damage Per Pegasi: %s<br>Total Pegasi: %d<br>Pegasi Killed by Alicorns: %d<br>Pegasi Remaining: %d<br>
<br>Damage to Pegasi per Alicorn: %s<br>
<br>Alicorns Required to Oneshot Pegasi: %d<br>ALICORNS REQUIRED TO DEFEND: %d<br>
<br>Gems Upkeep for Alicorns: %d<br>Bits to Create Alicorns: %s (mil)`,
			phpFloat(pegasiDmg), pegasi, killed, pegasi-killed, phpFloat(alicornDmg), kill, defend, defend*10, phpFloat(float64(defend*2000000)/1000000))
	}
	b.WriteString(`<center><form action="ascend-calc.php" method="post">
<input name="reputation" class="form-control" placeholder="Reputation" style="width:200px;"/>
<input name="pegasi" class="form-control" placeholder="Superpower Pegasi" style="width:200px;"/>
<input name="ticks" class="form-control" placeholder="Ticks To War" style="width:200px;"/>
<input name="barracks" class="form-control" placeholder="Barracks/Training" style="width:200px;"/>
<input type="submit" name="ascend-calc" value="Calculate" class="btn btn-info"/>
</form>`)
	p.Raw(b.String())
}
