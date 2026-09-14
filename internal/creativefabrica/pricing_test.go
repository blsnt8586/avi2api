package creativefabrica

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFlowCostUsesWholeOrderEndpoint(t *testing.T) {
	for _, test := range []struct {
		body string
		want int64
		fail bool
	}{
		{body: `{"totalCoinAmount":"12000"}`, want: 12000},
		{body: `{"totalCoinAmount":"0"}`, want: 0},
		{body: `{"coinAmount":"6000"}`, fail: true},
		{body: `{"totalCoinAmount":"-1"}`, fail: true},
	} {
		t.Run(test.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/FlowService/CalculateGenerationCost") &&
					r.URL.Path != "/creativefabrica.flow.v2.FlowService/CalculateGenerationCost" {
					t.Errorf("not the whole Flow estimator: %s", r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["settings"] == nil {
					t.Error("settings not forwarded")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := NewWithHTTPClient(server.Client(), "test")
			client.FlowURL = server.URL
			total, err := client.CalculateFlowGenerationCost(context.Background(), "fixture", map[string]any{"model": "FLOW_MODEL_OPENAI_GPT_IMAGE_2"})
			if (err != nil) != test.fail || (!test.fail && total != test.want) {
				t.Fatalf("total=%d err=%v", total, err)
			}
		})
	}
}
