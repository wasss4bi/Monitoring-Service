package sender

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

type telegramRoundTrip func(*http.Request) (*http.Response, error)

func (f telegramRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTelegramTextPartsPreserveTextAndUnicode(t *testing.T) {
	for _, body := range []string{
		"Монитор: Товар\nПравила:\n- Есть в наличии",
		strings.Repeat("я", 4096),
		strings.Repeat("a", 4095) + "😀",
		"Монитор: Товар\nAPI: https://example.com/" + strings.Repeat("a", 9000) + "\nДата выполнения: GMT",
		strings.Repeat("Цена 😀\n", 2000),
	} {
		parts := telegramTextParts(body)
		if strings.Join(parts, "") != body {
			t.Fatal("text was lost or reordered")
		}
		for _, part := range parts {
			if part == "" || !utf8.ValidString(part) || len(utf16.Encode([]rune(part))) > telegramMessageLimit {
				t.Fatal("invalid Telegram part")
			}
		}
	}
	if parts := telegramTextParts("short message"); len(parts) != 1 || parts[0] != "short message" {
		t.Fatal("short message changed")
	}
}

func TestTelegramMultipartDeliveryAndFailure(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		var sent []string
		sender := NewTelegramSender("fixture-token")
		sender.client = &http.Client{Transport: telegramRoundTrip(func(r *http.Request) (*http.Response, error) {
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["chat_id"] != "fixture-chat" {
				t.Fatal("wrong chat")
			}
			sent = append(sent, payload["text"])
			status := http.StatusOK
			if failSecond && len(sent) == 2 {
				status = http.StatusTooManyRequests
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
		})}
		body := strings.Repeat("x", telegramMessageLimit*2+10)
		err := sender.Send(t.Context(), "fixture-chat", "", body)
		if failSecond {
			var status *HTTPStatusError
			if !errors.As(err, &status) || status.StatusCode != 429 || len(sent) != 2 {
				t.Fatalf("err=%v parts=%d", err, len(sent))
			}
		} else if err != nil || strings.Join(sent, "") != body || len(sent) != 3 {
			t.Fatalf("delivery err=%v parts=%d", err, len(sent))
		}
	}
}

func TestTelegramCancellationAndTokenRedaction(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	sender := NewTelegramSender("fixture-token")
	sender.client = &http.Client{Transport: telegramRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})}
	if err := sender.Send(ctx, "fixture-chat", "", strings.Repeat("a", 5000)); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	sender.client = &http.Client{Transport: telegramRoundTrip(func(r *http.Request) (*http.Response, error) { return nil, errors.New(r.URL.String()) })}
	err := sender.Send(t.Context(), "fixture-chat", "", "test")
	if err == nil || strings.Contains(err.Error(), "fixture-token") {
		t.Fatal("transport error exposed token")
	}
}

func TestTelegramRejectsInvalidTextBeforeSending(t *testing.T) {
	sender := NewTelegramSender("fixture-token")
	sender.client = &http.Client{Transport: telegramRoundTrip(func(r *http.Request) (*http.Response, error) { t.Fatal("invalid text sent"); return nil, nil })}
	for _, body := range []string{"", " \n\t", string([]byte{0xff})} {
		if sender.Send(t.Context(), "fixture-chat", "", body) == nil {
			t.Fatal("invalid text accepted")
		}
	}
}
