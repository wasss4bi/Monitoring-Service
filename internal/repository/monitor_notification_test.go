package repository

import (
	"strings"
	"testing"
	"time"

	"monitoring_service/internal/domain"
)

func notificationFixture() (domain.Monitor, []domain.MonitorRule) {
	completed := time.Date(2026, 10, 30, 17, 0, 0, 0, time.FixedZone("local", 6*3600))
	monitor := domain.Monitor{ID: 7, Name: "Кроссовки", URL: "https://shop.example.com/api", PageURL: "https://shop.example.com/product",
		EndpointType: "json", CreatedAt: completed.Add(-time.Hour), CompletedAt: &completed, RulesCountToFinish: 2}
	rules := []domain.MonitorRule{{ID: 11, MonitorID: 7, Description: "Товар в наличии"}, {ID: 13, MonitorID: 7, Description: "Цена ниже 28990"}}
	return monitor, rules
}

func TestTelegramSuccessShowsOnlyMatchedRulesAndGMT(t *testing.T) {
	monitor, rules := notificationFixture()
	want := "Монитор: Кроссовки\nAPI: https://shop.example.com/api\nURL товара: https://shop.example.com/product\nПравила:\n- Товар в наличии\n- Цена ниже 28990\nДата создания монитора: 2026-10-30 10:00:00 GMT\nДата выполнения монитора: 2026-10-30 11:00:00 GMT"
	if got := formatMonitorSuccess(monitor, rules); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	monitor.EndpointType = "html"
	if strings.Contains(formatMonitorSuccess(monitor, rules), "API:") {
		t.Fatal("HTML notification contains API line")
	}
}

func TestMatchedRuleIdentityAndThreshold(t *testing.T) {
	monitor, rules := notificationFixture()
	if err := validateMatchedRules(monitor, rules); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]domain.MonitorRule{nil, rules[:1], {rules[0], rules[0]}, {{ID: 11, MonitorID: 8, Description: "Other monitor"}, rules[1]}} {
		if validateMatchedRules(monitor, invalid) == nil {
			t.Fatalf("accepted invalid selection: %+v", invalid)
		}
	}
}
