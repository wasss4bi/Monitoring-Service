// Run from the repository root: go run ./tools/loadgen/export-queries.go
// SQL bodies are copied from Go string literals, without rewriting filters or joins.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type spec struct{ file, method, needle, name, types, args, setup string }

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	specs := []spec{
		{"monitoring_execution_repository.go", "ScheduleDue", "WITH outstanding AS", "01-scheduler", "integer,boolean,text[],boolean", "100,false,ARRAY[]::text[],false", ""},
		{"monitoring_execution_repository.go", "ScheduleDue", "WITH outstanding AS", "01b-scheduler-browser", "integer,boolean,text[],boolean", "100,false,ARRAY[]::text[],true", ""},
		{"monitoring_execution_repository.go", "ClaimFetch", "SELECT m.id FROM monitors", "02-fetch-owner-lock", "uuid", ":'fetch_id'", fetchSetup},
		{"monitoring_execution_repository.go", "ClaimFetch", "WITH claimed AS", "03-fetch-claim", "uuid,uuid,bigint", ":'fetch_id','00000000-0000-5000-8000-000000000001',60000000", fetchSetup},
		{"monitoring_execution_repository.go", "ActiveMonitors", "SELECT m.id,m.user_id", "04-evaluation-monitor-rules", "bigint", ":monitor_id", `SELECT id AS monitor_id FROM monitors WHERE status='monitoring' AND deleted_at IS NULL ORDER BY id LIMIT 1 \gset`},
		{"monitor_repository.go", "ListRules", "SELECT id, monitor_id, json_path", "05-rules-batch", "bigint[]", ":'monitor_ids'", `SELECT array_agg(id)::text AS monitor_ids FROM (SELECT id FROM monitors ORDER BY id LIMIT 100) m \gset`},
		{"outbox_repository.go", "Claim", "WITH candidate AS", "06-outbox-claim", "uuid,bigint", "'00000000-0000-5000-8000-000000000001',30000000", ""},
		{"monitoring_execution_repository.go", "ClaimNotification", "UPDATE notifications n SET status='sending'", "07-notification-claim", "bigint,bigint,uuid", ":notification_id,30000000,'00000000-0000-5000-8000-000000000001'", `SELECT id AS notification_id FROM notifications WHERE status='pending' ORDER BY id LIMIT 1 \gset`},
		{"monitoring_execution_repository.go", "RecoverExpiredNotifications", "SELECT id,attempts_count,created_at", "08-notification-recovery", "integer", "1000", ""},
		{"endpoint_fetch_repository.go", "Get", "SELECT fetch_id::text, endpoint_id", "09-fetch-history-get", "uuid", ":'fetch_id'", `SELECT fetch_id::text AS fetch_id FROM endpoint_fetches ORDER BY created_at DESC LIMIT 1 \gset`},
		{"processed_message_repository.go", "IsProcessed", "SELECT EXISTS", "10-inbox-exists", "varchar,uuid", ":'consumer_name',:'message_id'", `SELECT consumer_name,message_id::text FROM processed_messages ORDER BY processed_at DESC LIMIT 1 \gset`},
		{"processed_message_repository.go", "RecordOnce", "INSERT INTO processed_messages", "11-inbox-record-once", "varchar,uuid", ":'consumer_name',:'message_id'", `SELECT consumer_name,message_id::text FROM processed_messages ORDER BY processed_at DESC LIMIT 1 \gset`},
		{"idempotency_repository.go", "Get", "SELECT key, status, request_hash", "12-api-idempotency", "varchar", ":'key'", `SELECT key FROM idempotency_keys ORDER BY (expires_at>NOW()) DESC,created_at DESC LIMIT 1 \gset`},
		{"monitoring_execution_repository.go", "RecoverExpiredFetches", "SELECT f.fetch_id::text", "13-fetch-recovery", "integer", "1000", ""},
		{"monitoring_execution_repository.go", "SwitchEndpointType", "SELECT id,user_id,name FROM monitors", "14-endpoint-active-monitors", "bigint", ":endpoint_id", `SELECT endpoint_id FROM monitors WHERE status='monitoring' AND deleted_at IS NULL ORDER BY id LIMIT 1 \gset`},
		{"monitoring_maintenance_repository.go", "DeleteExpired", "DELETE FROM endpoint_fetches", "15-fetch-retention", "timestamptz,integer", "NOW()-INTERVAL '7 days',1000", ""},
		{"monitor_repository.go", "ListByUser", "WHERE m.user_id = $1 AND m.deleted_at IS NULL", "16-api-monitor-list", "bigint,timestamptz,bigint,integer", ":user_id,NULL,NULL,100", `SELECT user_id FROM monitors ORDER BY id LIMIT 1 \gset`},
		{"notification_repository.go", "ListByUser", "SELECT n.id,n.monitor_id", "17-api-notification-list", "bigint,timestamptz,bigint,integer", ":user_id,NULL,NULL,100", `SELECT m.user_id FROM notifications n JOIN monitors m ON m.id=n.monitor_id ORDER BY n.id LIMIT 1 \gset`},
	}
	for _, s := range specs {
		path := filepath.Join(*root, "internal", "repository", s.file)
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		must(err)
		sqlText := ""
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != s.method {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				v, e := strconv.Unquote(lit.Value)
				if e == nil && strings.Contains(v, s.needle) {
					if sqlText != "" {
						panic("ambiguous SQL source")
					}
					sqlText = v
				}
				return true
			})
		}
		if sqlText == "" {
			panic(fmt.Sprintf("source SQL missing: %s.%s", s.file, s.method))
		}
		if s.file == "monitor_repository.go" && s.method == "ListByUser" {
			prefix := ""
			for _, decl := range f.Decls {
				g, ok := decl.(*ast.GenDecl)
				if !ok || g.Tok != token.CONST {
					continue
				}
				for _, value := range g.Specs {
					v, ok := value.(*ast.ValueSpec)
					if !ok || len(v.Names) != 1 || v.Names[0].Name != "monitorSelect" {
						continue
					}
					lit, ok := v.Values[0].(*ast.BasicLit)
					if !ok {
						panic("monitorSelect is no longer a literal")
					}
					prefix, err = strconv.Unquote(lit.Value)
					must(err)
				}
			}
			if prefix == "" {
				panic("monitorSelect missing")
			}
			sqlText = prefix + sqlText
		}
		header := fmt.Sprintf("-- Exact SQL source: internal/repository/%s, %s\n-- EXPLAIN ANALYZE executes writes; every file rolls them back.\n\\set ON_ERROR_STOP on\n\\timing on\n", s.file, s.method)
		if s.method == "SwitchEndpointType" || (s.file == "endpoint_fetch_repository.go" && s.method == "Get") {
			header = "-- Optional repository-only baseline: no current runtime callers found.\n" + header
		}
		body := header + s.setup + "\nBEGIN;\nSET LOCAL statement_timeout='10min';\nPREPARE loadgen_query(" + s.types + ") AS\n" + strings.TrimSpace(sqlText) + ";\nEXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE loadgen_query(" + s.args + ");\nDEALLOCATE loadgen_query;\nROLLBACK;\n"
		dest := filepath.Join(*root, "docs", "loadgen", "sql", s.name+".sql")
		must(os.MkdirAll(filepath.Dir(dest), 0755))  // #nosec G301 -- generated SQL documentation directories must be readable
		must(os.WriteFile(dest, []byte(body), 0644)) // #nosec G306 -- generated SQL documentation is intentionally readable
		fmt.Println(dest)
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}

const fetchSetup = `SELECT f.fetch_id::text AS fetch_id FROM endpoint_fetches f JOIN monitors m ON m.id=f.monitor_id JOIN monitored_endpoints e ON e.id=f.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id WHERE f.status='pending' AND m.check_status='scheduled' AND m.status='monitoring' AND m.deleted_at IS NULL AND f.generation=m.generation AND t.is_active ORDER BY f.scheduled_at LIMIT 1 \gset`
