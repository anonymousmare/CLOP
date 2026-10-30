package clop

import "testing"

func TestCommas(t *testing.T) {
	cases := map[any]string{
		int64(0): "0", int64(999): "999", int64(1000): "1,000", int64(-1234567): "-1,234,567",
		152345.5: "152,345.5", "100000000000000": "100,000,000,000,000", int64(-123): "-123",
	}
	for in, want := range cases {
		if got := commas(in); got != want {
			t.Errorf("commas(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestToInt(t *testing.T) {
	cases := map[string]int64{"": 0, "12": 12, " 7x": 7, "-5": -5, "abc": 0, "3.9": 3}
	for in, want := range cases {
		if got := toInt(in); got != want {
			t.Errorf("toInt(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPHPFloat(t *testing.T) {
	tenish, pointThree := 1.1, 0.1
	tenish, pointThree = (tenish-1)*100, pointThree+0.2 // float noise: 10.000000000000009, 0.30000000000000004
	cases := map[float64]string{10: "10", 1.5: "1.5", tenish: "10", pointThree: "0.3", 1.3125: "1.3125"}
	for in, want := range cases {
		if got := phpFloat(in); got != want {
			t.Errorf("phpFloat(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestTpl(t *testing.T) {
	v := V{"name": "Alicia", "n": Row{"age": "5"}, "count": int64(3)}
	got := tpl(`<b>{$name}</b> {$n['age']} {$n["age"]} $count {$missing} $(jquery) $unset`, v)
	want := `<b>Alicia</b> 5 5 3  $(jquery) $unset`
	if got != want {
		t.Errorf("tpl = %q, want %q", got, want)
	}
	// Substituted values are never re-expanded.
	if got := tpl("{$a}", V{"a": "{$b}", "b": "x"}); got != "{$b}" {
		t.Errorf("tpl re-expanded a value: %q", got)
	}
}

func TestPasswordHashMatchesPHP(t *testing.T) {
	// php -r 'echo sha1(addslashes("pw'\''\\x")."saltlick");'
	if got := passwordHash(`pw'\x`); got != "656d68bf61a4232d0f2c882243c1a6567e3cc41c" {
		t.Errorf("passwordHash = %s", got)
	}
}

func TestArrivalDate(t *testing.T) {
	cases := []struct {
		dep        string
		attack     bool
		sameRegion bool
		want       string
	}{
		{"2026-01-01 03:00:00", false, true, "2026-01-02 00:00:00"},
		{"2026-01-01 13:00:00", true, true, "2026-01-03 00:00:00"},
		{"2026-01-01 11:59:59", false, false, "2026-01-03 00:00:00"},
		{"2026-01-01 12:00:00", true, false, "2026-01-04 00:00:00"},
	}
	for _, c := range cases {
		if got := arrivalDate(c.dep, c.attack, c.sameRegion); got != c.want {
			t.Errorf("arrivalDate(%s, %v, %v) = %s, want %s", c.dep, c.attack, c.sameRegion, got, c.want)
		}
	}
}

func TestRelationDecay(t *testing.T) {
	cases := map[int64]int64{0: 0, 300: -1, 450: -5, 1000: -31, -460: 0, -1000: 19}
	for rel, want := range cases {
		if got := relationDecay(rel); got != want {
			t.Errorf("relationDecay(%d) = %d, want %d", rel, got, want)
		}
	}
}

func TestOversatisfactionLoss(t *testing.T) {
	if got := oversatisfactionLoss(1000, "Loose Despotism"); got != 15+10+5 {
		t.Errorf("loss at 1000 = %d", got)
	}
	if got := oversatisfactionLoss(1500, "Democracy"); got != 30 {
		t.Errorf("democracy loss at 1500 = %d", got)
	}
	if got := oversatisfactionLoss(250, "Loose Despotism"); got != 0 {
		t.Errorf("loss at 250 = %d", got)
	}
}
