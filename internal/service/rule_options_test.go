package service

import (
	"testing"
)

func TestConfiguredMoneyRule(t *testing.T) {
	if value, ok := ruleNumericValue("68\u2009150\u2009₽", "money"); !ok || value != 68150 {
		t.Fatalf("money conversion: %v %v", value, ok)
	}
	for _, bad := range []string{"1.234", "1 23", "1,2,3", "NaN", "Infinity", "price 68150"} {
		if _, ok := ruleNumericValue(bad, "money"); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, ok := ruleNumericValue("68 150", "number"); ok {
		t.Fatal("default format changed")
	}
}
