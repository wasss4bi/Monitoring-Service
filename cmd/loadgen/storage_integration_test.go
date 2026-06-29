//go:build integration

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/lib/pq"
)

// Read-only comparison of bounded samples after COPY, including type conversion and precision.
func TestStoredDatasetMatchesGenerator(t *testing.T) {
	raw := os.Getenv("LOADGEN_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("LOADGEN_TEST_DATABASE_URL is not set")
	}
	name, dsn, err := safeDSN(raw)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	}()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	}()
	if err = guard(t.Context(), conn, name); err != nil {
		t.Fatal(err)
	}
	var body []byte
	if err = conn.QueryRowContext(t.Context(), `SELECT report FROM loadgen_meta.run WHERE state='complete'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	var saved report
	if err = json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	c := saved.Config
	if err = c.validate(); err != nil {
		t.Fatal(err)
	}
	for _, job := range c.jobs() {
		t.Run(job.name, func(t *testing.T) {
			types := map[string]string{}
			rows, err := conn.QueryContext(t.Context(), `SELECT attname,format_type(atttypid,atttypmod) FROM pg_attribute WHERE attrelid=$1::regclass AND attnum>0 AND NOT attisdropped`, job.name)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var col, typ string
				if err = rows.Scan(&col, &typ); err != nil {
					if err := rows.Close(); err != nil {
						t.Errorf("close rows: %v", err)
					}
					t.Fatal(err)
				}
				types[col] = typ
			}
			err = rows.Err()
			if err := rows.Close(); err != nil {
				t.Errorf("close rows: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			selected := map[int64]bool{}
			for _, i := range []int64{1, 2, 3, job.count / 4, job.count / 2, job.count * 3 / 4, job.count - 2, job.count - 1, job.count} {
				if i > 0 && i <= job.count {
					selected[i] = true
				}
			}
			var values []string
			var args []any
			appendRow := func(row []any) {
				var params []string
				for i, col := range job.columns {
					args = append(args, row[i])
					params = append(params, fmt.Sprintf("$%d::%s", len(args), types[col]))
				}
				values = append(values, "("+strings.Join(params, ",")+")")
			}
			if job.next != nil {
				for i := int64(1); i <= job.count; i++ {
					row, ok := job.next()
					if !ok {
						t.Fatal("stream ended early")
					}
					if selected[i] {
						appendRow(row)
					}
				}
			} else {
				for i := int64(1); i <= job.count; i++ {
					if selected[i] {
						appendRow(job.row(i))
					}
				}
			}
			if len(values) == 0 {
				return
			}
			var names, predicates []string
			for _, col := range job.columns {
				q := pq.QuoteIdentifier(col)
				names = append(names, q)
				predicates = append(predicates, "a."+q+" IS NOT DISTINCT FROM e."+q)
			}
			pk := pq.QuoteIdentifier(job.columns[0])
			join := "a." + pk + "=e." + pk
			if job.name == "processed_messages" || job.name == "monitor_fetch_evaluations" {
				pk = pq.QuoteIdentifier(job.columns[1])
				join += " AND a." + pk + "=e." + pk
			}
			query := `SELECT count(*) FROM (VALUES ` + strings.Join(values, ",") + `) e(` + strings.Join(names, ",") + `) JOIN ` + pq.QuoteIdentifier(job.name) + ` a ON ` + join + ` WHERE ` + strings.Join(predicates, " AND ")
			var matched int
			if err = conn.QueryRowContext(t.Context(), query, args...).Scan(&matched); err != nil {
				t.Fatal(err)
			}
			if matched != len(values) {
				t.Fatalf("%d/%d stored samples match the final generator", matched, len(values))
			}
		})
	}
}
