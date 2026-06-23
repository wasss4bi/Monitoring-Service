package service

import (
	v1 "github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1"
	"testing"
	"time"
)

func TestHTMLMonitoringUsesScalarContractAndRuleThreshold(t *testing.T) {
	request, monitors := remoteFixture()
	monitors[0].PathType = "selector"
	monitors[0].RulesCountToFinish = 2
	for i := range monitors[0].Rules {
		monitors[0].Rules[i].ExpectedValue = 2000
	}
	response := &v1.FetchResponse{RequestId: request.RequestId}
	for _, id := range []string{"1", "2"} {
		response.Results = append(response.Results, &v1.SelectorResult{Id: id, Outcome: &v1.SelectorResult_Value{Value: &v1.ScalarValue{Value: &v1.ScalarValue_Text{Text: "1 234,50 ₽"}}}})
	}
	observation, err := evaluateRemoteResponse(request, response, monitors, 0, time.Minute)
	if err != nil || !observation.Succeeded || len(observation.Matched) != 2 {
		t.Fatalf("threshold evaluation: %+v %v", observation, err)
	}
	response.Results[1].Outcome = &v1.SelectorResult_Error{Error: &v1.ExtractionError{Code: v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_NOT_FOUND}}
	observation, err = evaluateRemoteResponse(request, response, monitors, 0, time.Minute)
	if err != nil || observation.Succeeded || observation.Terminal || len(observation.Matched) != 1 {
		t.Fatalf("partial match must stay active: %+v %v", observation, err)
	}
}
