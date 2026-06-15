package sender

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
)

type EmailSender struct {
	host       string
	port       string
	username   string
	password   string
	from       string
	requireTLS bool
}

func NewEmailSender(host, port, username, password, from string) *EmailSender {
	return &EmailSender{host: host, port: port, username: username, password: password, from: from}
}

// NewVerificationEmailSender requires transport encryption for verification codes.
func NewVerificationEmailSender(host, port, username, password, from string) *EmailSender {
	s := NewEmailSender(host, port, username, password, from)
	s.requireTLS = true
	return s
}

func (s *EmailSender) Send(ctx context.Context, recipient, subject, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(s.host) == "" || strings.TrimSpace(s.port) == "" || strings.TrimSpace(s.from) == "" {
		return fmt.Errorf("SMTP sender is not configured")
	}
	recipient = strings.TrimSpace(recipient)
	parsedRecipient, err := mail.ParseAddress(recipient)
	if err != nil || parsedRecipient.Address != recipient {
		return fmt.Errorf("email recipient is invalid")
	}
	message := strings.Join([]string{
		"From: " + s.from,
		"To: " + recipient,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	address := s.host + ":" + s.port
	dialer := &net.Dialer{}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			_ = connection.Close()
			return err
		}
	}
	client, err := smtp.NewClient(connection, s.host)
	if err != nil {
		_ = connection.Close()
		return err
	}
	defer func() {
		if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.ErrorContext(ctx, "close SMTP connection", "error", err)
		}
	}()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return err
		}
	} else if s.requireTLS {
		return fmt.Errorf("SMTP server does not support STARTTLS")
	}
	var auth smtp.Auth
	if s.username != "" || s.password != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(s.from); err != nil {
		return err
	}
	if err := client.Rcpt(recipient); err != nil {
		return err
	}
	data, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := data.Write([]byte(message)); err != nil {
		_ = data.Close()
		return err
	}
	if err := data.Close(); err != nil {
		return err
	}
	return client.Quit()
}
