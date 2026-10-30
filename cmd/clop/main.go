// Command clop runs the >CLOP game server.
//
//	clop            serve the game and run a tick every two hours
//	clop tick       run a single tick and exit
//	clop tick -at 2026-01-02T00:00:00Z
//	                run a tick as if it were that time (e.g. to redo a
//	                missed midnight or noon war tick)
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/anonymousmare/clop/internal/clop"
)

func main() {
	cfg := clop.Config{
		Listen:       envOr("LISTEN_ADDR", ":8000"),
		StaticDir:    envOr("STATIC_DIR", "clop"),
		DomainURL:    os.Getenv("DOMAIN_URL"),
		SecureCookie: os.Getenv("SECURE_COOKIES") == "1",
		AdminUserID:  1,
	}
	host := envOr("MYSQL_HOST", "mariadb:3306")
	user := envOr("MYSQL_USER", "root")
	pass := os.Getenv("MYSQL_PASS")
	dsn := func(db string) string {
		// The game was written for MariaDB's old non-strict default
		// sql_mode, where out-of-range values are clamped rather than rejected.
		return fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8&interpolateParams=true&loc=UTC&sql_mode=%%27NO_ENGINE_SUBSTITUTION%%27",
			user, pass, host, db)
	}
	cfg.DSN = dsn(envOr("MYSQL_DATABASE", "clopus_clop"))
	cfg.CompoundsDSN = dsn("compounds")

	scheduler := flag.Bool("scheduler", true, "run the game tick every two hours")
	flag.StringVar(&cfg.Listen, "listen", cfg.Listen, "address to serve HTTP on")
	flag.StringVar(&cfg.StaticDir, "static", cfg.StaticDir, "directory holding images, css, js and html")
	flag.Parse()

	db, err := clop.OpenDB(cfg.DSN)
	if err != nil {
		log.Fatal(err)
	}
	compounds, err := clop.OpenDB(cfg.CompoundsDSN)
	if err != nil {
		log.Printf("compounds database unavailable: %v", err)
		compounds = nil
	}
	app := clop.NewApp(cfg, db, compounds)

	if flag.Arg(0) == "tick" {
		at := time.Now()
		tickFlags := flag.NewFlagSet("tick", flag.ExitOnError)
		atFlag := tickFlags.String("at", "", "RFC 3339 time to run the tick as")
		_ = tickFlags.Parse(flag.Args()[1:])
		if *atFlag != "" {
			if at, err = time.Parse(time.RFC3339, *atFlag); err != nil {
				log.Fatalf("bad -at time: %v", err)
			}
		}
		app.RunTick(at)
		return
	}
	if *scheduler {
		go app.RunScheduler()
	}
	log.Printf("serving >CLOP on %s", cfg.Listen)
	srv := &http.Server{Addr: cfg.Listen, Handler: app, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
