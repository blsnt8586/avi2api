package creativefabrica

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetFlowUsesIDField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["id"] != "existing-flow" || request["flowId"] != nil {
			t.Errorf("GetFlow request must use id: %v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"flow":{"id":"existing-flow","iterations":[{"images":[{"id":"output","imageUrl":"https://example.com/result.png","state":"IMAGE_STATE_GENERATED"}]}]}}`))
	}))
	defer server.Close()
	client := NewWithHTTPClient(server.Client(), "fixture")
	client.FlowURL = server.URL
	result, err := client.GetFlow(context.Background(), "fixture", "existing-flow")
	if err != nil || result.Status != "COMPLETE" || len(result.Outputs) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
