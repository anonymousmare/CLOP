package clop

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Config is everything the server needs from its environment.
type Config struct {
	DSN          string // clopus_clop database
	CompoundsDSN string // optional; ascensions are mirrored into its users table
	Listen       string
	StaticDir    string // images, css, js and the standalone .html pages
	DomainURL    string // "test.4clop..." turns on test-server behaviour
	SecureCookie bool
	AdminUserID  int64
}

// TestServer reports whether this is the test deployment, which hands out
// free resources and runs a war tick every tick.
func (c Config) TestServer() bool {
	return strings.Contains(c.DomainURL, "test.4clop")
}

type App struct {
	cfg       Config
	db        *DB
	compounds *DB
	sessions  *SessionStore
	mux       *http.ServeMux

	// gameMu serialises everything that changes game state: form posts and
	// the tick. The PHP version had no locking at all, which let two
	// simultaneous purchases spend the same money twice.
	gameMu sync.Mutex
}

func NewApp(cfg Config, db, compounds *DB) *App {
	a := &App{cfg: cfg, db: db, compounds: compounds, sessions: NewSessionStore(cfg.SecureCookie), mux: http.NewServeMux()}
	a.routes()
	return a
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}

// Page is the per-request state every handler works with.
type Page struct {
	app    *App
	db     *DB
	w      http.ResponseWriter
	r      *http.Request
	sess   *Session
	form   url.Values // POST body
	query  url.Values // query string
	isPost bool

	errors []string
	infos  []string
	title  string // prefix for <title>, e.g. "Overview - "
	body   strings.Builder
}

type redirectTo string
type dieWith string

func (p *Page) Redirect(to string) { panic(redirectTo(to)) }
func (p *Page) Die(msg string)     { panic(dieWith(msg)) }

func (p *Page) Err(format string, args ...any) {
	p.errors = append(p.errors, fmt.Sprintf(format, args...))
}

func (p *Page) Info(format string, args ...any) {
	p.infos = append(p.infos, fmt.Sprintf(format, args...))
}

func (p *Page) HasErrors() bool { return len(p.errors) > 0 }

// Post is $_POST[k]; Get is $_GET[k]; GP prefers POST over GET like array_merge($_GET, $_POST).
func (p *Page) Post(k string) string { return p.form.Get(k) }
func (p *Page) Get(k string) string  { return p.query.Get(k) }
func (p *Page) GP(k string) string {
	if v, ok := p.form[k]; ok && len(v) > 0 {
		return v[0]
	}
	return p.query.Get(k)
}
func (p *Page) PostB(k string) bool  { return truthy(p.Post(k)) }
func (p *Page) PostI(k string) int64 { return toInt(p.Post(k)) }

func (p *Page) UserID() int64   { return p.sess.Int("user_id") }
func (p *Page) NationID() int64 { return p.sess.Int("nation_id") }

// clientIP is the address the player connected from, as reported by the proxy.
func (p *Page) clientIP() string {
	if ip := strings.TrimSpace(p.r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if xff := p.r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return p.remoteAddr()
}

func (p *Page) remoteAddr() string {
	host, _, err := net.SplitHostPort(p.r.RemoteAddr)
	if err != nil {
		return p.r.RemoteAddr
	}
	return host
}

// csrf validates and rotates the per-page form token "token_<name>".
func (p *Page) csrf(name string) {
	key := "token_" + name
	if p.isPost && (p.Post(key) == "" || p.Post(key) != p.sess.Get(key)) {
		p.Err("Try again.")
	}
	if p.isPost || p.sess.Get(key) == "" {
		p.sess.Set(key, newToken())
	}
}

func (p *Page) token(name string) string { return p.sess.Get("token_" + name) }

// handle registers a page. Pages with layout get the site header and footer
// wrapped around their body.
func (a *App) handle(path string, layout bool, h func(*Page)) {
	a.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		a.serve(w, r, layout, h)
	})
}

func (a *App) serve(w http.ResponseWriter, r *http.Request, layout bool, h func(*Page)) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := &Page{app: a, db: a.db, w: w, r: r, query: r.URL.Query(), form: url.Values{}}
	if r.Method == http.MethodPost {
		p.form = r.PostForm
		p.isPost = len(p.form) > 0
	}
	p.sess = a.sessions.Load(w, r)
	if p.isPost {
		a.gameMu.Lock()
		defer a.gameMu.Unlock()
	}

	defer func() {
		if rec := recover(); rec != nil {
			switch v := rec.(type) {
			case redirectTo:
				http.Redirect(w, r, string(v), http.StatusFound)
			case dieWith:
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, string(v))
			default:
				log.Printf("panic serving %s: %v", r.URL.Path, rec)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}
	}()

	p.switchNation()
	h(p)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if layout {
		fmt.Fprint(w, p.header())
		fmt.Fprint(w, p.body.String())
		fmt.Fprint(w, footerHTML)
	} else {
		fmt.Fprint(w, p.body.String())
	}
}

// switchNation handles the nation picker in the navbar, which can post to
// any page. The post is consumed so the page itself sees a plain GET.
func (p *Page) switchNation() {
	if !p.PostB("switchnation_id") {
		return
	}
	id := p.PostI("switchnation_id")
	if p.db.One("SELECT nation_id FROM nations WHERE user_id = ? AND nation_id = ?", p.UserID(), id) != nil {
		p.sess.Set("nation_id", id)
	} else {
		p.Err("No.")
	}
	p.form = url.Values{}
	p.isPost = false
}

// needsUser sends logged-out players (and players still locked in their
// first 24 hours of stasis) back to the front page.
func (p *Page) needsUser() Row {
	if p.UserID() == 0 {
		p.Redirect("index.php")
	}
	u := p.db.One(`SELECT * FROM users WHERE user_id = ?
		AND (stasismode = 0 OR stasisdate < DATE_SUB(NOW(), INTERVAL 24 HOUR) OR stasisdate IS NULL)`, p.UserID())
	if u == nil {
		p.app.sessions.Destroy(p.sess)
		p.Redirect("index.php")
	}
	return u
}

const nationInfoSQL = `SELECT n.*, u.alliance_id, u.seesecrets, u.stasismode, u.hideicons, u.hideflags
	FROM nations n INNER JOIN users u ON u.user_id = n.user_id WHERE u.user_id = ? AND n.nation_id = ?`

// needsNation returns the current nation joined with its owner's settings,
// or redirects to wherever the player should be instead.
func (p *Page) needsNation() Row {
	if p.NationID() == 0 {
		p.Redirect("nonation.php")
	}
	return p.needsSpecificNation(p.NationID(), true)
}

func (p *Page) needsSpecificNation(nationID int64, updateSession bool) Row {
	n := p.db.One(nationInfoSQL, p.UserID(), nationID)
	if n == nil {
		other := p.db.One("SELECT nation_id FROM nations WHERE user_id = ?", p.UserID())
		if other != nil {
			if updateSession {
				p.sess.Set("nation_id", other.I("nation_id"))
			}
			p.Redirect("messages.php")
		}
		p.sess.Del("nation_id")
		p.Redirect("nonation.php")
	}
	if n.B("stasismode") {
		p.Redirect("userinfo.php")
	}
	return n
}

func (p *Page) isAdmin() bool {
	return p.UserID() != 0 && p.UserID() == p.app.cfg.AdminUserID
}
