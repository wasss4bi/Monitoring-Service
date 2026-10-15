package repository

import (
	"fmt"
	"strings"

	"monitoring_service/internal/domain"
)

func validateMatchedRules(monitor domain.Monitor, rules []domain.MonitorRule) error {
	seen := make(map[int64]struct{}, len(rules))
	for _, rule := range rules {
		if rule.ID < 1 || rule.MonitorID != monitor.ID || strings.TrimSpace(rule.Description) == "" {
			return fmt.Errorf("invalid matched rule for monitor %d", monitor.ID)
		}
		if _, duplicate := seen[rule.ID]; duplicate {
			return fmt.Errorf("duplicate matched rule for monitor %d", monitor.ID)
		}
		seen[rule.ID] = struct{}{}
	}
	if len(seen) < monitor.RulesCountToFinish {
		return fmt.Errorf("matched rule count is below monitor %d threshold", monitor.ID)
	}
	return nil
}

// The final text is persisted in the completion transaction. A delivery retry
// cannot pick up a later monitor name or a different set of rule descriptions.
func formatMonitorSuccess(monitor domain.Monitor, rules []domain.MonitorRule) string {
	var body strings.Builder
	fmt.Fprintf(&body, "Монитор: %s\n", notificationLine(monitor.Name))
	if monitor.EndpointType == domain.EndpointTypeJSON {
		fmt.Fprintf(&body, "API: %s\n", monitor.URL)
	}
	fmt.Fprintf(&body, "URL товара: %s\nПравила:\n", monitor.PageURL)
	for _, rule := range rules {
		fmt.Fprintf(&body, "- %s\n", notificationLine(rule.Description))
	}
	fmt.Fprintf(&body, "Дата создания монитора: %s GMT\n", monitor.CreatedAt.UTC().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&body, "Дата выполнения монитора: %s GMT", monitor.CompletedAt.UTC().Format("2006-01-02 15:04:05"))
	return body.String()
}

func notificationLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
