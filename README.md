# >CLOP

A browser game of war, politics, markets, deals, and ponies.

The game server is written in Go. It keeps the original page URLs
(`overview.php`, `marketplace.php`, ...), the original MariaDB schema and the
original game rules, so an existing database works as-is.

## Layout

| Path | What |
| --- | --- |
| `cmd/clop` | Server entrypoint |
| `internal/clop` | Pages, game rules, the tick and the battle engine |
| `internal/clop/content` | Long static page text (rules, guides, major action descriptions) |
| `clop/` | Static files served as-is: images, css, js, the war calculator and spreadsheet import tools |
| `clop/tables with data.sql`, `clop/tables without data.sql` | Database schema and game data |

## Running

With Docker:

```bash
MYSQL_PASS=... DOMAIN_URL=localhost docker compose up --build
```

Without Docker (needs Go 1.22+ and a MariaDB loaded with the two `.sql` files):

```bash
MYSQL_HOST=127.0.0.1:3306 MYSQL_PASS=... go run ./cmd/clop
```

Configuration is by environment variable:

| Variable | Default | Meaning |
| --- | --- | --- |
| `MYSQL_HOST` | `mariadb:3306` | Database address |
| `MYSQL_USER` | `root` | Database user |
| `MYSQL_PASS` | | Database password |
| `MYSQL_DATABASE` | `clopus_clop` | Game database (ascensions are also mirrored into `compounds.users`) |
| `LISTEN_ADDR` | `:8000` | HTTP listen address |
| `STATIC_DIR` | `clop` | Directory with images/css/js |
| `DOMAIN_URL` | | Containing `test.4clop` turns on test-server mode: new players get huge stockpiles, every tick is a war tick and forces arrive instantly |
| `SECURE_COOKIES` | | `1` marks the session cookie Secure (use behind HTTPS) |

## Ticks

The server runs the game tick itself at every even hour (UTC); the midnight and
noon ticks are also war ticks. Pass `-scheduler=false` to turn that off and run
ticks from elsewhere with:

```bash
clop tick
```

`clop tick -at 2026-01-02T00:00:00Z` runs a tick as if it were that time, for
example to redo a missed midnight war tick.

## Tests

```bash
go test ./...
```

## Differences from the PHP version

The rewrite keeps the game's behaviour, with these deliberate fixes:

- All SQL is parameterised, and every form post and the tick are serialised, so
  two simultaneous purchases can no longer spend the same money twice.
- User-supplied text (messages, descriptions, polls, search terms) is escaped
  everywhere it is shown.
- `makepolls.php` now requires the admin account (it had no access check).
- Solar Vassal / Lunar Client conversions actually change the government even
  when satisfaction is above 1,250, and check the right buildings in every nation.
- Favorite actions apply the same limits as actions (barracks cap, drug age
  limit, worship centres, factories).
- The tick counts the satisfaction lost to a failing economy or Authoritarian /
  Oppressive upkeep in that tick's GDP and caps, and Solar Empire hatred decays
  through all three tiers, like New Lunar Republic hatred always did.
- The failed-login limit (20 per IP per 2 hours) works; the Tor exit-node check,
  which relied on a retired DNS service, is gone.
- Numbers with decimals are grouped correctly (`152,345.5`), and rankings by
  region or allianceless status now paginate.
- Session cookies are named `CLOPSESSID`; existing PHP sessions end, so
  players log in again once. Passwords are unchanged.
