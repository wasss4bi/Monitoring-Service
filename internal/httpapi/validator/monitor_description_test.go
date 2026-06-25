package validator

import (
	"strings"
	"testing"
)

func TestMonitorProductPageRequired(t *testing.T) {
	for _, raw := range []string{"", " \t", "http://shop.example.com/item", "https://other.example.com/item", "https://shop.example.com/item#fragment", "https://shop.example.com/" + strings.Repeat("я", 2000)} {
		if _, fields := NormalizeMonitorPageURL(raw, "https://shop.example.com/api"); len(fields["page_url"]) == 0 {
			t.Fatalf("accepted invalid product URL %q", raw)
		}
	}
	page, fields := NormalizeMonitorPageURL("https://SHOP.example.com/item/", "https://shop.example.com/api")
	if len(fields) != 0 || page != "https://shop.example.com/item" {
		t.Fatalf("page=%q fields=%v", page, fields)
	}
}

func TestRuleDescriptionRequiredAndTrimmed(t *testing.T) {
	for _, description := range []string{"", " \t\n", strings.Repeat("я", 256), "invalid\x00description"} {
		_, fields := NormalizeGenericMonitorRuleInput(RawMonitorRuleInput{Path: "price", Operator: "<=", Value: 100, Description: description})
		if len(fields["description"]) == 0 {
			t.Fatalf("accepted description %q", description)
		}
	}
	input, fields := NormalizeGenericMonitorRuleInput(RawMonitorRuleInput{Path: "price", Operator: "<=", Value: 100, Description: "  Цена ниже 100  "})
	if len(fields) != 0 || input.Description != "Цена ниже 100" {
		t.Fatalf("input=%+v fields=%v", input, fields)
	}
	_, fields = NormalizeGenericMonitorRuleInput(RawMonitorRuleInput{Path: "price", Operator: "<=", Value: 100, Description: strings.Repeat("я", 255)})
	if len(fields) != 0 {
		t.Fatalf("255 Unicode characters rejected: %v", fields)
	}
	_, _, fields = NormalizeHTMLMonitorCreateInput("https://shop.example.com/item", []RawMonitorRuleInput{{Path: ".price", Operator: "<=", Value: 100}})
	if len(fields["rules[0].description"]) == 0 {
		t.Fatalf("missing indexed error: %v", fields)
	}
}
