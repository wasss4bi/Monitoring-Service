//go:build integration

package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"monitoring_service/internal/authjwt"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/httpapi/handler"
	"monitoring_service/internal/migrator"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type verificationCapture struct {
	calls           int
	recipient, body string
	fail            bool
}

func (s *verificationCapture) Send(_ context.Context, recipient, subject, body string) error {
	s.calls++
	if s.fail {
		return fmt.Errorf("delivery failed")
	}
	s.recipient = recipient
	s.body = body
	return nil
}
func TestContactVerificationFlow(t *testing.T) {
	db, dsn := rbacDB(t)
	t.Setenv("ADMIN_EMAIL", "verification-admin@example.com")
	t.Setenv("ADMIN_PASSWORD", "admin-password-strong")
	if err := migrator.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	var uid int64
	if err := db.QueryRowContext(t.Context(), `INSERT INTO users(name,email,password) VALUES('Test','verification-user@example.com','unused') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	access, err := handler.NewAccessControl(users)
	if err != nil {
		t.Fatal(err)
	}
	email, telegram := &verificationCapture{}, &verificationCapture{}
	vr := repository.NewContactVerificationRepository(db)
	vs := service.NewContactVerificationService(vr, "test-verification-secret", email, telegram)
	router := httpapi.NewRouter(httpapi.RouterConfig{DB: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), JWTSecret: "token-secret", AccessControl: access, ContactVerificationService: vs, UserContactService: service.NewUserContactService(repository.NewUserContactRepository(db))})
	server := httptest.NewServer(router)
	defer server.Close()
	token, err := authjwt.Generate("token-secret", authjwt.Claims{UserID: uid, AuthVersion: 1, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, body any, want int) {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, err := http.NewRequestWithContext(t.Context(), "POST", server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			b, _ := io.ReadAll(res.Body)
			t.Fatalf("%s status=%d want=%d body=%s", path, res.StatusCode, want, b)
		}
	}
	for _, channel := range []string{"email", "telegram"} {
		t.Run(channel, func(t *testing.T) {
			var channelID, contactID int64
			value := "test@example.com"
			if channel == "telegram" {
				value = "123456789"
			}
			if err := db.QueryRowContext(t.Context(), `INSERT INTO contacts(name) VALUES($1) RETURNING id`, channel).Scan(&channelID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(t.Context(), `INSERT INTO user_contacts(user_id,contact_id,contact) VALUES($1,$2,$3) RETURNING id`, uid, channelID, value).Scan(&contactID); err != nil {
				t.Fatal(err)
			}
			path := fmt.Sprintf("/me/contacts/%d/verification", contactID)
			call(path, nil, 202)
			call(path, nil, 429)
			var challenge, payload string
			var hash []byte
			if err := db.QueryRowContext(t.Context(), `SELECT challenge_id::text,code_hash FROM contact_verifications WHERE contact_id=$1`, contactID).Scan(&challenge, &hash); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(t.Context(), `SELECT payload::text FROM outbox_events WHERE aggregate_type='user_contact' AND aggregate_id=$1`, contactID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(payload, challenge) {
				t.Fatal("outbox missing challenge")
			}
			cap := email
			if channel == "telegram" {
				cap = telegram
			}
			cap.fail = true
			if err := vs.Deliver(t.Context(), challenge); err == nil {
				t.Fatal("failed delivery should be retried")
			}
			cap.fail = false
			if err := vs.Deliver(t.Context(), challenge); err != nil {
				t.Fatal(err)
			}
			cap.calls = 1
			cap = email
			if channel == "telegram" {
				cap = telegram
			}
			code := strings.TrimPrefix(cap.body, "Verification code: ")
			if len(code) != 6 || cap.recipient != value {
				t.Fatal("message not delivered to contact")
			}
			var outbox map[string]string
			if err := json.Unmarshal([]byte(payload), &outbox); err != nil || len(outbox) != 1 || outbox["contact_verification_id"] != challenge || len(hash) != 32 {
				t.Fatal("unexpected verification storage")
			}
			if err := vs.Deliver(t.Context(), challenge); err != nil || cap.calls != 1 {
				t.Fatal("duplicate delivered")
			}
			if _, err := db.ExecContext(t.Context(), `UPDATE contact_verifications SET expires_at=NOW()-INTERVAL '1 second' WHERE contact_id=$1`, contactID); err != nil {
				t.Fatal(err)
			}
			call(path+"/confirm", map[string]string{"code": code}, 400)
			if _, err := db.ExecContext(t.Context(), `UPDATE contact_verifications SET expires_at=NOW()+INTERVAL '10 minutes' WHERE contact_id=$1`, contactID); err != nil {
				t.Fatal(err)
			}
			wrong := "000000"
			if wrong == code {
				wrong = "000001"
			}
			for i := 0; i < 5; i++ {
				call(path+"/confirm", map[string]string{"code": wrong}, 400)
			}
			call(path+"/confirm", map[string]string{"code": code}, 400)
			if _, err := db.ExecContext(t.Context(), `UPDATE contact_verifications SET requested_at=NOW()-INTERVAL '61 seconds' WHERE contact_id=$1`, contactID); err != nil {
				t.Fatal(err)
			}
			call(path, nil, 202)
			if err := db.QueryRowContext(t.Context(), `SELECT challenge_id::text FROM contact_verifications WHERE contact_id=$1`, contactID).Scan(&challenge); err != nil {
				t.Fatal(err)
			}
			if err := vs.Deliver(t.Context(), challenge); err != nil {
				t.Fatal(err)
			}
			code = strings.TrimPrefix(cap.body, "Verification code: ")
			contactRepo := repository.NewUserContactRepository(db)
			if _, err := contactRepo.Update(t.Context(), uid, contactID, domain.UpdateUserContactInput{Contact: value + "x"}); err != nil {
				t.Fatal(err)
			}
			if _, err := contactRepo.Update(t.Context(), uid, contactID, domain.UpdateUserContactInput{Contact: value}); err != nil {
				t.Fatal(err)
			}
			call(path+"/confirm", map[string]string{"code": code}, 400)
			if _, err := db.ExecContext(t.Context(), `UPDATE contact_verifications SET requested_at=NOW()-INTERVAL '61 seconds' WHERE contact_id=$1`, contactID); err != nil {
				t.Fatal(err)
			}
			call(path, nil, 202)
			if err := db.QueryRowContext(t.Context(), `SELECT challenge_id::text FROM contact_verifications WHERE contact_id=$1`, contactID).Scan(&challenge); err != nil {
				t.Fatal(err)
			}
			if err := vs.Deliver(t.Context(), challenge); err != nil {
				t.Fatal(err)
			}
			code = strings.TrimPrefix(cap.body, "Verification code: ")
			call(path+"/confirm", map[string]string{"code": code}, 204)
			call(path+"/confirm", map[string]string{"code": code}, 400)
			contact, err := contactRepo.Get(t.Context(), uid, contactID)
			if err != nil || !contact.IsVerified || contact.IsActive {
				t.Fatalf("contact=%+v err=%v", contact, err)
			}
		})
	}
}
