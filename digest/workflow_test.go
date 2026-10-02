package digest

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/infrai-examples/creator-backlog-pdf/infrai"
)

type recordingGateway struct {
	batch    []infrai.BatchRecord
	markdown string
}

func (g *recordingGateway) SummarizeBatch(_ context.Context, batch []infrai.BatchRecord, _ string) (map[string]string, error) {
	g.batch = batch
	return map[string]string{"r1": "Course files reached subscribers. Transcoding completed.", "r3": "Preset reached subscribers. Validation completed."}, nil
}

func (g *recordingGateway) RenderPDF(_ context.Context, markdown, _ string) (json.RawMessage, error) {
	g.markdown = markdown
	return json.RawMessage(`{"id":"pdf_123"}`), nil
}

func TestWorkflowSelectsDeliveredPendingRecordsAndHandsSummariesToPDF(t *testing.T) {
	t0 := time.Date(2026, 9, 16, 22, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		records []Record
		wantIDs []string
	}{
		{
			name: "filters backlog and orders oldest delivery first",
			records: []Record{
				{ID: "r3", Creator: "Nia", Asset: "Color preset", DeliveredAt: t0.Add(time.Hour), DeliveryStatus: "delivered", SubscriberUpdatePending: true, ProcessingNotes: "validated"},
				{ID: "r2", Creator: "Omar", Asset: "Audio pack", DeliveredAt: t0, DeliveryStatus: "processing", SubscriberUpdatePending: true},
				{ID: "r1", Creator: "Mina", Asset: "Course files", DeliveredAt: t0, DeliveryStatus: "delivered", SubscriberUpdatePending: true, ProcessingNotes: "transcoded"},
				{ID: "r4", Creator: "Pia", Asset: "Template", DeliveredAt: t0, DeliveryStatus: "delivered", SubscriberUpdatePending: false},
			},
			wantIDs: []string{"r1", "r3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &recordingGateway{}
			result, err := (Workflow{Gateway: gateway}).Run(context.Background(), tt.records)
			if err != nil {
				t.Fatal(err)
			}
			gotIDs := make([]string, len(gateway.batch))
			for i, item := range gateway.batch {
				gotIDs[i] = item.ID
			}
			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Fatalf("batch IDs = %v, want %v", gotIDs, tt.wantIDs)
			}
			if result.Included != 2 || !strings.Contains(gateway.markdown, "Course files reached subscribers") || !strings.Contains(gateway.markdown, "Preset reached subscribers") {
				t.Fatalf("handoff result = %#v, markdown = %q", result, gateway.markdown)
			}
		})
	}
}
