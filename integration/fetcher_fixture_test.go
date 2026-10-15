//go:build integration

package integration

import (
	"context"
	"encoding/json"
	v1 "github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1"
	"monitoring_service/internal/httpapi"
	"monitoring_service/internal/httpapi/handler"
	"monitoring_service/internal/repository"
	"net/http"
	"testing"
)

type fixtureFetcher struct {
	fetch   func(context.Context, *v1.FetchRequest) (*v1.FetchResponse, error)
	prepare func(context.Context, *v1.PrepareSessionRequest) (*v1.PrepareSessionResponse, error)
}

func (f fixtureFetcher) Fetch(ctx context.Context, r *v1.FetchRequest) (*v1.FetchResponse, error) {
	return f.fetch(ctx, r)
}
func (f fixtureFetcher) Prepare(ctx context.Context, r *v1.PrepareSessionRequest) (*v1.PrepareSessionResponse, error) {
	if f.prepare != nil {
		return f.prepare(ctx, r)
	}
	return &v1.PrepareSessionResponse{RequestId: r.RequestId, SessionState: fixtureSession}, nil
}

var fixtureSession = []byte(`{"version":1,"userAgent":"fixture","browserProfile":"private-profile-id","cookieJar":{"cookies":[]}}`)

func fixtureValues(request *v1.FetchRequest, values map[string]string) *v1.FetchResponse {
	response := &v1.FetchResponse{RequestId: request.RequestId, Fetch: &v1.FetchSummary{NetworkPerformed: true}}
	for _, selector := range request.Selectors {
		item := &v1.SelectorResult{Id: selector.Id}
		if value, ok := values[selector.Expression]; ok {
			item.Outcome = &v1.SelectorResult_Value{Value: &v1.ScalarValue{Value: &v1.ScalarValue_Text{Text: value}}}
		} else {
			item.Outcome = &v1.SelectorResult_Error{Error: &v1.ExtractionError{Code: v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_NOT_FOUND}}
		}
		response.Results = append(response.Results, item)
	}
	return response
}
func fixtureFailure(request *v1.FetchRequest, code v1.FetchErrorCode, httpStatus uint32) *v1.FetchResponse {
	response := &v1.FetchResponse{RequestId: request.RequestId, Failure: &v1.FetchError{Code: code}, Fetch: &v1.FetchSummary{NetworkPerformed: true}}
	if code == v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REQUIRED || code == v1.FetchErrorCode_FETCH_ERROR_CODE_PROFILE_UNAVAILABLE {
		response.Fetch.NetworkPerformed = false
	}
	if httpStatus != 0 {
		response.Failure.HttpStatus = &httpStatus
	}
	for _, selector := range request.Selectors {
		response.Results = append(response.Results, &v1.SelectorResult{Id: selector.Id, Outcome: &v1.SelectorResult_Error{Error: &v1.ExtractionError{Code: v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_NOT_EVALUATED}}})
	}
	return response
}
func fixturePrice(value string) fixtureFetcher {
	return fixtureFetcher{fetch: func(_ context.Context, r *v1.FetchRequest) (*v1.FetchResponse, error) {
		return fixtureValues(r, map[string]string{"woocommerce_product_object.regular_price": value}), nil
	}}
}
func fixturePriceBody(t *testing.T, body string) fixtureFetcher {
	t.Helper()
	// Decode test data into a scalar fixture. Production JSON selection is tested
	// in the separate Fetcher repository; Monitoring receives typed RPC results.
	var doc struct {
		Product struct {
			Price json.RawMessage `json:"regular_price"`
		} `json:"woocommerce_product_object"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	if len(doc.Product.Price) > 0 {
		var text string
		if json.Unmarshal(doc.Product.Price, &text) == nil {
			values["woocommerce_product_object.regular_price"] = text
		} else {
			values["woocommerce_product_object.regular_price"] = string(doc.Product.Price)
		}
	}
	return fixtureFetcher{fetch: func(_ context.Context, r *v1.FetchRequest) (*v1.FetchResponse, error) {
		return fixtureValues(r, values), nil
	}}
}
func fixtureDocumentFailure(name string) fixtureFetcher {
	return fixtureFetcher{fetch: func(_ context.Context, r *v1.FetchRequest) (*v1.FetchResponse, error) {
		code := v1.FetchErrorCode_FETCH_ERROR_CODE_INVALID_DOCUMENT
		if name == "oversized JSON" {
			code = v1.FetchErrorCode_FETCH_ERROR_CODE_RESPONSE_TOO_LARGE
		}
		return fixtureFailure(r, code, 0), nil
	}}
}
func monitoringRouter(t *testing.T, cfg httpapi.RouterConfig) http.Handler {
	t.Helper()
	access, err := handler.NewAccessControl(repository.NewUserRepository(cfg.DB))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AccessControl = access
	return httpapi.NewRouter(cfg)
}
