//go:build integration

package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/httpapi/handler"
	"monitoring_service/internal/migrator"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"
)

func rbacDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	if os.Getenv("MONITORING_SERVICE_CURRENT_DB_TESTS") != "1" {
		t.Skip("MONITORING_SERVICE_CURRENT_DB_TESTS=1 and isolated DATABASE_URL required")
	}
	endpoint, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil || endpoint.Host == "" {
		t.Fatal("valid DATABASE_URL required")
	}
	admin, err := sql.Open("postgres", endpoint.String())
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_rbac_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.ExecContext(t.Context(), "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		if err := admin.Close(); err != nil {
			t.Errorf("close admin: %v", err)
		}
		t.Fatal(err)
	}
	query := endpoint.Query()
	query.Set("search_path", schema)
	endpoint.RawQuery = query.Encode()
	db, err := sql.Open("postgres", endpoint.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE"); err != nil {
			t.Error(err)
		}
		if err := admin.Close(); err != nil {
			t.Errorf("close admin: %v", err)
		}
	})
	return db, endpoint.String()
}

func TestAdminMigrationAndHTTPAuthorization(t *testing.T) {
	db, dsn := rbacDB(t)
	const email = "bootstrap-admin@example.com"
	const password = "fixture-admin-password-strong"
	t.Setenv("ADMIN_EMAIL", email)
	t.Setenv("ADMIN_PASSWORD", password)
	if err := migrator.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	var role, hash string
	if err := db.QueryRowContext(t.Context(), `SELECT role,password FROM users WHERE email=$1`, email).Scan(&role, &hash); err != nil {
		t.Fatal(err)
	}
	if role != "admin" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		t.Fatal("administrator was not created with bcrypt")
	}
	users := repository.NewUserRepository(db)
	access, err := handler.NewAccessControl(users)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(httpapi.NewRouter(httpapi.RouterConfig{DB: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), JWTSecret: "fixture-jwt-secret", AccessControl: access, // #nosec G101 -- test-only fixture; not a real credential
		UserService: service.NewUserService(users), ContactService: service.NewContactService(repository.NewContactRepository(db)),
		TargetHostnameService: service.NewTargetHostnameService(repository.NewTargetHostnameRepository(db))}))
	defer app.Close()
	call := func(method, path string, body any, token string, want int) map[string]any {
		t.Helper()
		var encoded []byte
		if body != nil {
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequestWithContext(t.Context(), method, app.URL+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := app.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Errorf("close response body: %v", err)
			}
		}()
		if response.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d", method, path, response.StatusCode, want)
		}
		var result any
		if strings.Contains(response.Header.Get("Content-Type"), "application/json") {
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal("invalid JSON response")
			}
		}
		object, _ := result.(map[string]any)
		return object
	}
	login := func(address, secret string) string {
		t.Helper()
		response := call("POST", "/auth/login", map[string]string{"email": address, "password": secret}, "", 200)
		token, ok := response["access_token"].(string)
		if !ok || token == "" {
			t.Fatal("missing access token")
		}
		return token
	}
	oversized, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.URL+"/users", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	if err != nil {
		t.Fatal(err)
	}
	oversized.ContentLength = -1
	tooLarge, err := app.Client().Do(oversized)
	if err != nil {
		t.Fatal(err)
	}
	_ = tooLarge.Body.Close()
	if tooLarge.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request status=%d", tooLarge.StatusCode)
	}
	adminToken := login(email, password)
	user := call("POST", "/users", map[string]string{"name": "FixtureUser", "email": "ordinary@example.com", "password": "Fixture-user-Password7!"}, "", 201) // #nosec G101 -- test-only fixture; not a real credential
	id, ok := user["id"].(float64)
	if !ok {
		t.Fatal("missing registered user ID")
	}
	userToken := login("ordinary@example.com", "Fixture-user-Password7!")
	ownerPath := "/users/" + strconv.FormatInt(int64(id), 10)
	call("GET", ownerPath, nil, userToken, 200)
	call("GET", "/users", nil, userToken, 403)
	call("GET", "/users", nil, adminToken, 200)
	call("GET", "/users/1", nil, userToken, 403)
	call("DELETE", ownerPath, nil, userToken, 403)
	call("POST", ownerPath+"/check-password", map[string]string{"password": "Fixture-user-Password7!"}, userToken, 200) // #nosec G101 -- test-only fixture; not a real credential
	call("POST", "/users/1/check-password", map[string]string{"password": "Fixture-user-Password7!"}, userToken, 403)   // #nosec G101 -- test-only fixture; not a real credential
	call("GET", "/contacts", nil, "", 401)
	call("GET", "/contacts", nil, userToken, 200)
	call("POST", "/contacts", map[string]string{"name": "untrusted-channel"}, userToken, 403)
	call("GET", "/target-hostnames", nil, "", 401)
	call("GET", "/target-hostnames", nil, userToken, 403)
	target := map[string]any{"hostname": "fixture.example.com", "limit_per_minute": 60, "bootstrap_rules": []map[string]any{{"origin": "https://fixture.example.com", "path_prefix": "/verify", "methods": []string{"POST"}}}}
	call("POST", "/target-hostnames", target, userToken, 403)
	created := call("POST", "/target-hostnames", target, adminToken, 201)
	targetID, ok := created["id"].(float64)
	if !ok {
		t.Fatal("missing hostname ID")
	}
	targetPath := "/target-hostnames/" + strconv.FormatInt(int64(targetID), 10)
	call("PATCH", targetPath, map[string]any{"bootstrap_rules": []any{}}, adminToken, 200)
	var rules string
	if err := db.QueryRowContext(t.Context(), `SELECT bootstrap_rules::text FROM target_hostnames WHERE id=$1`, int64(targetID)).Scan(&rules); err != nil || rules != "[]" {
		t.Fatal("bootstrap rules were not persisted and cleared")
	}
	// Changing only profile data with the same password keeps all sessions valid.
	secondToken := login("ordinary@example.com", "Fixture-user-Password7!")
	call("PUT", ownerPath, map[string]string{"name": "FixtureUser", "email": "ordinary@example.com", "password": "Fixture-user-Password7!"}, userToken, 200) // #nosec G101 -- test-only fixture; not a real credential
	call("GET", ownerPath, nil, secondToken, 200)
	call("PUT", ownerPath, map[string]string{"name": "FixtureUser", "email": "ordinary@example.com", "password": "Changed-user-Password8!"}, userToken, 200)
	call("GET", ownerPath, nil, userToken, 401)
	call("GET", ownerPath, nil, secondToken, 401)
	newToken := login("ordinary@example.com", "Changed-user-Password8!")
	call("GET", ownerPath, nil, newToken, 200)
	call("GET", "/users", nil, adminToken, 200)

	var channelID, contactID int64
	if err := db.QueryRowContext(t.Context(), `INSERT INTO contacts(name) VALUES('test-channel') RETURNING id`).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `INSERT INTO user_contacts(user_id,contact_id,contact,is_verified,is_active) VALUES($1,$2,'old',true,true) RETURNING id`, int64(id), channelID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	contacts := repository.NewUserContactRepository(db)
	updated, err := contacts.Update(t.Context(), int64(id), contactID, domain.UpdateUserContactInput{Contact: "new", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.IsActive || updated.IsVerified {
		t.Fatal("changed contact must not stay active or verified")
	}
	updated, err = contacts.Update(t.Context(), int64(id), contactID, domain.UpdateUserContactInput{Contact: "new", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.IsActive || updated.IsVerified {
		t.Fatal("unverified contact must not be activated")
	}
	call("GET", "/metrics", nil, "", 404)
	if _, err := db.ExecContext(t.Context(), `UPDATE users SET role='user' WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	call("GET", "/target-hostnames", nil, adminToken, 403)
	if _, err := db.ExecContext(t.Context(), `UPDATE users SET role='admin' WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	// Existing credentials stay authoritative; ENV does not reset the password.
	t.Setenv("ADMIN_EMAIL", "")
	t.Setenv("ADMIN_PASSWORD", "")
	if err := migrator.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	var again string
	if err := db.QueryRowContext(t.Context(), `SELECT password FROM users WHERE email=$1`, email).Scan(&again); err != nil || again != hash {
		t.Fatal("repeat migration changed administrator password")
	}
}

func TestAdminMigrationRejectsExistingEmail(t *testing.T) {
	db, dsn := rbacDB(t)
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE users(id BIGSERIAL PRIMARY KEY,name TEXT NOT NULL,email TEXT NOT NULL UNIQUE,password TEXT NOT NULL);INSERT INTO users(name,email,password)VALUES('Existing user','collision@example.com','existing-hash')`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADMIN_EMAIL", "collision@example.com")
	t.Setenv("ADMIN_PASSWORD", "fixture-admin-password-strong")
	if err := migrator.Up(dsn, "../../migrations"); err == nil {
		t.Fatal("existing ordinary user was silently promoted")
	}
	var hash string
	if err := db.QueryRowContext(t.Context(), `SELECT password FROM users WHERE email='collision@example.com'`).Scan(&hash); err != nil || hash != "existing-hash" {
		t.Fatal("conflicting account was modified")
	}
}

func TestMigrationsApplyNewVersionOnce(t *testing.T) {
	db, dsn := rbacDB(t)
	t.Setenv("ADMIN_EMAIL", "migration-test@example.com")
	t.Setenv("ADMIN_PASSWORD", "fixture-admin-password-strong")
	if err := migrator.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	paths, err := filepath.Glob("../../migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		body, err := os.ReadFile(path) // #nosec G304 -- path is supplied by local configuration or a test fixture
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(path)), body, 0600); err != nil { // #nosec G703 -- destination uses a temporary directory and filepath.Base
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "000003_test.up.sql"), []byte("CREATE TABLE migration_once_test (id integer); INSERT INTO migration_once_test VALUES (1);"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADMIN_EMAIL", "")
	t.Setenv("ADMIN_PASSWORD", "")
	for range 2 {
		if err := migrator.Up(dsn, dir); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM migration_once_test").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration should run once: count=%d, err=%v", count, err)
	}
}
