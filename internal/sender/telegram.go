package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const telegramMessageLimit = 4096
const telegramPartInterval = time.Second

type TelegramSender struct {
	token  string
	client *http.Client
}

func NewTelegramSender(token string) *TelegramSender {
	return &TelegramSender{token: strings.TrimSpace(token), client: &http.Client{}}
}

type HTTPStatusError struct{ StatusCode int }

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("remote service returned HTTP %d", e.StatusCode)
}
func (s *TelegramSender) Send(ctx context.Context, chatID, _ string, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.token == "" {
		return fmt.Errorf("Telegram bot token is not configured")
	}
	if strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("Telegram chat id is empty")
	}
	if strings.TrimSpace(body) == "" || !utf8.ValidString(body) {
		return fmt.Errorf("Telegram message must contain valid UTF-8 text")
	}
	for index, part := range telegramTextParts(body) {
		if index > 0 {
			// Separate parts target the same chat; do not introduce a burst.
			timer := time.NewTimer(telegramPartInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.sendPart(ctx, chatID, part); err != nil {
			return fmt.Errorf("send Telegram part %d: %w", index+1, err)
		}
	}
	return nil
}

func (s *TelegramSender) sendPart(ctx context.Context, chatID, body string) error {
	payload, err := json.Marshal(map[string]string{"chat_id": chatID, "text": body})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+s.token+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create Telegram request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// net/http errors can contain the complete URL, including the bot token.
		return fmt.Errorf("Telegram request failed")
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &HTTPStatusError{StatusCode: response.StatusCode}
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return fmt.Errorf("decode Telegram API response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("Telegram API rejected message")
	}
	return nil
}

func telegramTextParts(body string) []string {
	var parts []string
	for body != "" {
		units, cut, lastLine := 0, 0, 0
		for index, char := range body {
			width := 1
			if char > 0xffff {
				width = 2
			}
			if units+width > telegramMessageLimit {
				break
			}
			units += width
			cut = index + utf8.RuneLen(char)
			if char == '\n' {
				lastLine = cut
			}
		}
		if cut < len(body) && lastLine > 0 && strings.TrimSpace(body[:lastLine]) != "" {
			cut = lastLine
		}
		parts = append(parts, body[:cut])
		body = body[cut:]
	}
	return parts
}
