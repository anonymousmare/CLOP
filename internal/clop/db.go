package clop

import (
	"database/sql"
	"log"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

// Row is one result row with every column rendered as text, the same way
// mysqli handed rows to the original PHP code. NULL becomes "".
type Row map[string]string

func (r Row) S(k string) string   { return r[k] }
func (r Row) I(k string) int64    { return toInt(r[k]) }
func (r Row) F(k string) float64  { return toFloat(r[k]) }
func (r Row) B(k string) bool     { return truthy(r[k]) }
func (r Row) Set(k string, v any) { r[k] = str(v) }

// DB wraps database/sql with the small set of helpers every page uses.
// Errors are logged rather than returned: the game has always treated a
// failed query as "no rows" and carried on.
type DB struct {
	*sql.DB
}

func OpenDB(dsn string) (*DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	return &DB{db}, nil
}

// Query returns all rows of q.
func (db *DB) Query(q string, args ...any) []Row {
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		logQueryErr(err, q)
		return nil
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		logQueryErr(err, q)
		return nil
	}
	var out []Row
	raw := make([]sql.RawBytes, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			logQueryErr(err, q)
			return out
		}
		row := make(Row, len(cols))
		for i, c := range cols {
			row[c] = string(raw[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		logQueryErr(err, q)
	}
	return out
}

// One returns the first row of q, or nil when there is none.
func (db *DB) One(q string, args ...any) Row {
	rows := db.Query(q, args...)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// Exec runs a statement and returns the last insert id (0 if none).
func (db *DB) Exec(q string, args ...any) int64 {
	res, err := db.DB.Exec(q, args...)
	if err != nil {
		logQueryErr(err, q)
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

// ExecOK is Exec for callers that need to know whether the statement worked.
func (db *DB) ExecOK(q string, args ...any) bool {
	if _, err := db.DB.Exec(q, args...); err != nil {
		logQueryErr(err, q)
		return false
	}
	return true
}

func logQueryErr(err error, q string) {
	log.Printf("sql error: %v in %s", err, strings.Join(strings.Fields(q), " "))
}

// deleteNation removes a nation and everything that hangs off it. Forces
// that were travelling to or sitting in it are sent home, and the nation's
// open deals are cancelled.
func (db *DB) deleteNation(nationID int64) {
	db.Exec("DELETE FROM resources WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM marketplace WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM weaponsmarketplace WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM armormarketplace WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM buyermarketplace WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM weaponsbuyermarketplace WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM armorbuyermarketplace WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM nations WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM weapons WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM armor WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM recipefavorites WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM forcegroups WHERE nation_id = ?", nationID)
	db.Exec("DELETE FROM forces WHERE nation_id = ?", nationID)
	db.Exec(`UPDATE forcegroups SET location_id = nation_id, departuredate = NULL, destination_id = 0, attack_mission = 0
		WHERE destination_id = ? OR location_id = ?`, nationID, nationID)
	for _, d := range db.Query("SELECT deal_id FROM deals WHERE fromnation = ? OR tonation = ?", nationID, nationID) {
		db.deleteDealItems(d.I("deal_id"))
	}
	db.Exec("DELETE FROM deals WHERE fromnation = ? OR tonation = ?", nationID, nationID)
}

// deleteDealItems removes every offered and requested line of a deal.
func (db *DB) deleteDealItems(dealID int64) {
	for _, t := range []string{"dealitems_offered", "dealitems_requested", "dealarmor_offered",
		"dealarmor_requested", "dealweapons_offered", "dealweapons_requested"} {
		db.Exec("DELETE FROM "+t+" WHERE deal_id = ?", dealID)
	}
}

// systemMessage drops an automated message into a user's inbox.
func (db *DB) systemMessage(userID int64, msg string) {
	db.Exec("INSERT INTO messages (fromuser, touser, message, fromdeleted, sent) VALUES (0, ?, ?, 1, NOW())", userID, msg)
}

func (db *DB) news(msg string) {
	db.Exec("INSERT INTO news (message, posted) VALUES (?, NOW())", msg)
}

func (db *DB) report(nationID int64, report string) {
	db.Exec("INSERT INTO reports (nation_id, report, time) VALUES (?, ?, NOW())", nationID, report)
}

// reportAllNations files the same report in every nation a user owns.
func (db *DB) reportAllNations(userID int64, report string) {
	db.Exec("INSERT INTO reports (report, nation_id, time) SELECT ?, nation_id, NOW() FROM nations WHERE user_id = ?", report, userID)
}

// resourceAmount is how many of a resource a nation holds (0 if none).
func (db *DB) resourceAmount(nationID, resourceID int64) int64 {
	r := db.One("SELECT amount FROM resources WHERE nation_id = ? AND resource_id = ?", nationID, resourceID)
	return r.I("amount")
}
