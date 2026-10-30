package clop

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"html"
	"math"
	"sort"
	"strconv"
	"strings"
)

var regionNames = map[int64]string{
	0: "The Heavily Fortified Island of Admin",
	1: "Saddle Arabia",
	2: "Zebrica",
	3: "Burrozil",
	4: "Przewalskia",
}

var subregionNames = map[int64]string{0: "", 1: "North ", 2: "Central ", 3: "South "}

// regionIcons is the resource icon shown next to each region's name.
var regionIcons = map[int64]string{0: "Drugs", 1: "Oil", 2: "Copper", 3: "Apples", 4: "Machinery Parts"}

var forceTypeNames = map[int64]string{1: "Cavalry", 2: "Tanks", 3: "Pegasi", 4: "Unicorns", 5: "Naval", 6: "Alicorns"}

// truthy mirrors PHP's notion of a true-ish string.
func truthy(s string) bool {
	return s != "" && s != "0"
}

// toInt mirrors PHP's (int) cast: leading whitespace, optional sign, digits.
func toInt(s string) int64 {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	end := 0
	if end < len(s) && (s[end] == '-' || s[end] == '+') {
		end++
	}
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, err := strconv.ParseInt(s[:end], 10, 64)
	if err != nil {
		if f, ferr := strconv.ParseFloat(s[:end], 64); ferr == nil {
			return int64(f)
		}
		return 0
	}
	return n
}

// toFloat parses the longest numeric prefix of s, like PHP numeric strings.
func toFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	for end := len(s); end > 0; end-- {
		if f, err := strconv.ParseFloat(s[:end], 64); err == nil {
			return f
		}
	}
	return 0
}

// isDigits is PHP's ctype_digit.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil && strings.TrimSpace(s) != ""
}

// str renders a value the way PHP string interpolation would.
func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return phpFloat(x)
	case bool:
		if x {
			return "1"
		}
		return ""
	default:
		return fmt.Sprint(x)
	}
}

// phpFloat prints a float with PHP's default precision of 14 digits.
func phpFloat(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'G', 14, 64)
}

// commas groups the integer part of a number with commas: 1234567.5 -> 1,234,567.5
func commas(v any) string {
	s := str(v)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String() + frac
	if neg {
		out = "-" + out
	}
	return out
}

// numberFormat is PHP's number_format with no decimals.
func numberFormat(f float64) string {
	return commas(int64(math.Round(f)))
}

func esc(s string) string {
	return html.EscapeString(s)
}

func nl2br(s string) string {
	r := strings.NewReplacer("\r\n", "<br />\r\n", "\n\r", "<br />\n\r", "\n", "<br />\n", "\r", "<br />\r")
	return r.Replace(s)
}

// alnumSpace reports whether s only uses the characters allowed in names.
func alnumSpace(s string, allowAllWhitespace bool) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == ' ':
		case allowAllWhitespace && (c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'):
		default:
			return false
		}
	}
	return true
}

func newToken() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// mysqlEscape reproduces mysqli::real_escape_string. Passwords were always
// escaped before hashing, so the hash has to be computed the same way to
// keep existing accounts working.
func mysqlEscape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "\x00", "\\0", "\n", "\\n", "\r", "\\r", "'", "\\'", "\"", "\\\"", "\x1a", "\\Z")
	return r.Replace(s)
}

func passwordHash(password string) string {
	sum := sha1.Sum([]byte(mysqlEscape(password) + "saltlick"))
	return hex.EncodeToString(sum[:])
}

func ceil(f float64) int64  { return int64(math.Ceil(f)) }
func floor(f float64) int64 { return int64(math.Floor(f)) }

// round mirrors PHP round(): half away from zero at the given precision.
func round(f float64, precision int) float64 {
	p := math.Pow(10, float64(precision))
	return math.Round(f*p) / p
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// sortedKeys returns map keys in ascending order (PHP ksort on string keys).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// numberClass picks the bootstrap colour used for signed amounts.
func numberClass(n float64) string {
	switch {
	case n > 0:
		return "text-success"
	case n == 0:
		return "text-warning"
	default:
		return "text-danger"
	}
}
