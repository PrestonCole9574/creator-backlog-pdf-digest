package infrai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSummarizeBatchDoesNotStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ai/batch/submit" {
			var body struct {
				Store bool `json:"store"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode batch submission: %v", err)
			}
			if body.Store {
				t.Error("batch submission must not store results")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"test-batch"}}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"STOP","message":"submitted"}}`))
	}))
	defer server.Close()

	client := New("test-key")
	client.baseURL = server.URL + "/v1"
	_, err := client.SummarizeBatch(context.Background(), []BatchRecord{{ID: "one", Prompt: "summary"}}, "test-key")
	if err == nil {
		t.Fatal("expected polling to stop after submission")
	}
}
