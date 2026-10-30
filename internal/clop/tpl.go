package clop

import (
	"regexp"
	"strings"
)

// V holds the variables a template can refer to.
type V map[string]any

// tplVar matches the PHP heredoc forms the page markup was written with:
// {$name}, {$name['key']}, {$name["key"]} and bare $name.
var tplVar = regexp.MustCompile(`\{\$(\w+)(?:\[['"]([\w ]+)['"]\])?\}|\$([A-Za-z_]\w*)`)

// tpl fills a PHP-heredoc-style template. The page markup was carried over
// from the original heredocs mostly verbatim, so they keep their variable
// syntax. Braced references to unknown variables render as "" (like PHP);
// bare $name is only replaced when name is defined, so jQuery's $ and
// similar never get swallowed.
func tpl(s string, v V) string {
	return tplVar.ReplaceAllStringFunc(s, func(m string) string {
		sub := tplVar.FindStringSubmatch(m)
		if sub[3] != "" {
			val, ok := v[sub[3]]
			if !ok {
				return m
			}
			return str(val)
		}
		val := v[sub[1]]
		if sub[2] == "" {
			return str(val)
		}
		return lookup(val, sub[2])
	})
}

func lookup(container any, key string) string {
	switch c := container.(type) {
	case Row:
		return c[key]
	case map[string]string:
		return c[key]
	case V:
		return str(c[key])
	case map[string]any:
		return str(c[key])
	}
	return ""
}

// page-level buffer helpers

func (p *Page) Echo(s string, v V) {
	if v == nil {
		v = V{}
	}
	if _, ok := v["_SESSION"]; !ok {
		v["_SESSION"] = p.sess.snapshot()
	}
	p.body.WriteString(tpl(s, v))
}

func (p *Page) Raw(parts ...string) {
	for _, s := range parts {
		p.body.WriteString(s)
	}
}

// joinBR joins report lines the way the game always has.
func joinBR(lines []string) string {
	return strings.Join(lines, "<br/>")
}
