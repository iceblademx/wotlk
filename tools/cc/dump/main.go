// Command dump exports server tables used by the custom content importer from a MySQL world
// database into cc_data/server/*.csv. The session is read-only.
//
// Connection details come from .env (SQL_HOSTNAME, SQL_PORT, SQL_USERNAME, SQL_PASSWORD, and
// optionally SQL_DATABASE):
//
//	go run ./tools/cc/dump -list                 # show databases and the relevant tables in each
//	go run ./tools/cc/dump                       # export from the database that has item_template
//	go run ./tools/cc/dump -db acore_world -tables item_template,spell_proc
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

var envFile = flag.String("env", ".env", "File with SQL_HOSTNAME/SQL_PORT/SQL_USERNAME/SQL_PASSWORD[/SQL_DATABASE]")
var outDir = flag.String("out", "cc_data/server", "Output directory for CSV files")
var dbName = flag.String("db", "", "World database name (default: SQL_DATABASE, else the one containing item_template)")
var tablesFlag = flag.String("tables", "", "Comma-separated tables to export (default: all known tables that exist)")
var list = flag.Bool("list", false, "List databases and relevant tables, then exit")

// Tables the importer understands, plus server-side spell overrides worth keeping for reference.
var knownTables = []string{"item_template", "spell_proc", "spell_proc_event", "spell_bonus_data", "spell_dbc",
	"spell_enchant_proc_data", "spell_cooldown_overrides", "item_set_names"}

func readEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			env[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return env, sc.Err()
}

func main() {
	flag.Parse()
	env, err := readEnv(*envFile)
	if err != nil {
		log.Fatalf("reading %s: %v", *envFile, err)
	}
	cfg := mysql.NewConfig()
	cfg.User = env["SQL_USERNAME"]
	cfg.Passwd = env["SQL_PASSWORD"]
	cfg.Net = "tcp"
	port := env["SQL_PORT"]
	if port == "" {
		port = "3306"
	}
	cfg.Addr = env["SQL_HOSTNAME"] + ":" + port
	cfg.Timeout = 15 * time.Second
	cfg.ReadTimeout = 5 * time.Minute
	cfg.AllowNativePasswords = true

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Fatalf("connecting to %s: %v", cfg.Addr, err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY"); err != nil {
		log.Fatalf("setting read-only session: %v", err)
	}

	databases := queryStrings(ctx, conn, "SHOW DATABASES")
	tablesByDB := map[string][]string{}
	for _, d := range databases {
		switch d {
		case "information_schema", "performance_schema", "mysql", "sys":
			continue
		}
		tablesByDB[d] = queryStrings(ctx, conn, "SELECT table_name FROM information_schema.tables WHERE table_schema = ?", d)
	}

	if *list {
		for _, d := range databases {
			tables, ok := tablesByDB[d]
			if !ok {
				continue
			}
			fmt.Printf("%s (%d tables):\n", d, len(tables))
			for _, t := range tables {
				lt := strings.ToLower(t)
				if strings.Contains(lt, "item") || strings.Contains(lt, "spell") || strings.Contains(lt, "enchant") {
					var n int
					if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", d, t)).Scan(&n); err != nil {
						log.Fatal(err)
					}
					if n > 0 {
						fmt.Printf("  %-40s %d rows\n", t, n)
					}
				}
			}
		}
		return
	}

	target := *dbName
	if target == "" {
		target = env["SQL_DATABASE"]
	}
	if target == "" {
		for d, tables := range tablesByDB {
			if contains(tables, "item_template") {
				if target != "" {
					log.Fatalf("both %s and %s contain item_template; choose one with -db", target, d)
				}
				target = d
			}
		}
	}
	if target == "" {
		log.Fatal("no database with an item_template table found; use -list and -db")
	}

	wanted := knownTables
	if *tablesFlag != "" {
		wanted = strings.Split(*tablesFlag, ",")
	}
	if err := os.MkdirAll(*outDir, 0777); err != nil {
		log.Fatal(err)
	}
	for _, t := range wanted {
		t = strings.TrimSpace(t)
		if !contains(tablesByDB[target], t) {
			fmt.Printf("%s.%s: not present, skipped\n", target, t)
			continue
		}
		n, err := exportTable(ctx, conn, target, t, filepath.Join(*outDir, t+".csv"))
		if err != nil {
			log.Fatalf("%s.%s: %v", target, t, err)
		}
		fmt.Printf("%s.%s: %d rows -> %s\n", target, t, n, filepath.ToSlash(filepath.Join(*outDir, t+".csv")))
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func queryStrings(ctx context.Context, conn *sql.Conn, query string, args ...any) []string {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		log.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			log.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func exportTable(ctx context.Context, conn *sql.Conn, database, table, path string) (int, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT * FROM `%s`.`%s`", database, table))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(cols); err != nil {
		return 0, err
	}
	values := make([]sql.RawBytes, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	record := make([]string, len(cols))
	n := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		for i, v := range values {
			if v == nil {
				record[i] = "NULL"
			} else {
				record[i] = string(v)
			}
		}
		if err := w.Write(record); err != nil {
			return n, err
		}
		n++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return n, err
	}
	return n, rows.Err()
}
