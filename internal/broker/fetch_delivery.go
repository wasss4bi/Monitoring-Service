package broker

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
)

func DecodeFetchDelivery(body []byte, messageID string) (string, bool) {
	if !ValidDeliveryID(messageID) {
		return "", false
	}
	var payload struct {
		FetchID string `json:"fetch_id"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", false
	}
	if !ValidDeliveryID(payload.FetchID) {
		return "", false
	}
	return payload.FetchID, true
}

func ValidDeliveryID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && strings.EqualFold(id.String(), value)
}
