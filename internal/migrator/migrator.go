package migrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"strings"
	"time"

	"monitoring_service/internal/logging"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func Up(databaseURL, migrationsPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	driver, err := postgres.WithConnection(ctx, conn, &postgres.Config{})
	if err != nil {
		conn.Close()
		return err
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "postgres", driver)
	if err != nil {
		driver.Close()
		return err
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		if sourceErr != nil {
			logging.Error(context.Background(), slog.Default(), "failed to close migration source", nil, sourceErr)
		}
		if databaseErr != nil {
			logging.Error(context.Background(), slog.Default(), "failed to close migration database", nil, databaseErr)
		}
	}()
	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return err
	}
	if !dirty && (errors.Is(err, migrate.ErrNilVersion) || version < 2) {
		email := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
		password := os.Getenv("ADMIN_PASSWORD")
		address, parseErr := mail.ParseAddress(email)
		if parseErr != nil || address.Address != email || len(password) < 8 || len(password) > 72 {
			return errors.New("ADMIN_EMAIL and ADMIN_PASSWORD (8-72 bytes) are required for administrator bootstrap")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash bootstrap administrator password: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `SELECT set_config('monitoring_service.admin_email',$1,false),set_config('monitoring_service.admin_password_hash',$2,false)`, email, string(hash)); err != nil {
			return fmt.Errorf("configure administrator migration: %w", err)
		}
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			logging.Info(context.Background(), slog.Default(), "migrations have no changes", nil)
			return nil
		}
		return err
	}

	logging.Info(context.Background(), slog.Default(), "migrations applied successfully", nil)
	return nil
}
