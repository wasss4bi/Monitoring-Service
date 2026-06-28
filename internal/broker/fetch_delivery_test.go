package broker

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestMalformedFetchDeliveryIsRecognized(t *testing.T) {
	id := uuid.NewString()
	for _, invalid := range []string{uuid.Nil.String(), "urn:uuid:" + id, strings.ReplaceAll(id, "-", "")} {
		if ValidDeliveryID(invalid) {
			t.Fatal("noncanonical delivery ID accepted", invalid)
		}
	}
	message := uuid.NewString()
	if got, ok := DecodeFetchDelivery([]byte(`{"fetch_id":"`+id+`"}`), message); !ok || got != id {
		t.Fatal(got, ok)
	}
	for _, body := range []string{"garbage", `{}`, `{"fetch_id":"not-a-uuid"}`} {
		if _, ok := DecodeFetchDelivery([]byte(body), message); ok {
			t.Fatal(body)
		}
	}
	if _, ok := DecodeFetchDelivery([]byte(`{"fetch_id":"`+id+`"}`), "invalid"); ok {
		t.Fatal("invalid message ID accepted")
	}
}
