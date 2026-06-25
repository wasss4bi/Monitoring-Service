package validator

import "testing"

func TestNormalizeBrowserPageURL(t *testing.T) {
	for _, tc := range []struct {
		name, page, endpointType, want, field string
		headers                               map[string]string
	}{
		{name: "legacy", endpointType: "json"},
		{name: "native", page: "https://API.example.com/item/1/", endpointType: "json", want: "https://api.example.com/item/1"},
		{name: "cross host", page: "https://other.example.com/item", endpointType: "json", field: "browser_page_url"},
		{name: "HTTP", page: "http://api.example.com/item", endpointType: "json", field: "browser_page_url"},
		{name: "userinfo", page: "https://user:secret@api.example.com/item", endpointType: "json", field: "browser_page_url"}, // #nosec G101 -- test-only fixture; not a real credential
		{name: "port", page: "https://api.example.com:444/item", endpointType: "json", field: "browser_page_url"},
		{name: "fragment", page: "https://api.example.com/item#state", endpointType: "json", field: "browser_page_url"},
		{name: "HTML", page: "https://api.example.com/item", endpointType: "html", field: "browser_page_url"},
		{name: "custom headers", page: "https://api.example.com/item", endpointType: "json", headers: map[string]string{"Cookie": "private"}, field: "headers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, fields := NormalizeBrowserPageURL(tc.page, "https://api.example.com/api/item?id=1", tc.endpointType, tc.headers)
			if tc.field == "" {
				if len(fields) != 0 || got != tc.want {
					t.Fatalf("got %q, fields %v; want %q", got, fields, tc.want)
				}
			} else if len(fields[tc.field]) == 0 {
				t.Fatalf("expected field error %s; got %v", tc.field, fields)
			}
		})
	}
}
