package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/infrai-examples/creator-backlog-pdf/infrai"
)

type Record struct {
	ID                      string    `json:"id"`
	Creator                 string    `json:"creator"`
	Asset                   string    `json:"asset"`
	DeliveredAt             time.Time `json:"delivered_at"`
	DeliveryStatus          string    `json:"delivery_status"`
	SubscriberUpdatePending bool      `json:"subscriber_update_pending"`
	ProcessingNotes         string    `json:"processing_notes"`
}

type Gateway interface {
	SummarizeBatch(context.Context, []infrai.BatchRecord, string) (map[string]string, error)
	RenderPDF(context.Context, string, string) (json.RawMessage, error)
}

type Result struct {
	Included int             `json:"included"`
	PDF      json.RawMessage `json:"pdf"`
}

type Workflow struct {
	Gateway Gateway
}

func (w Workflow) Run(ctx context.Context, records []Record) (Result, error) {
	selected := selectRecords(records)
	if len(selected) == 0 {
		return Result{}, fmt.Errorf("no delivered records with pending subscriber updates")
	}

	batch := make([]infrai.BatchRecord, 0, len(selected))
	for _, record := range selected {
		batch = append(batch, infrai.BatchRecord{ID: record.ID, Prompt: prompt(record)})
	}
	key := stableKey(selected)
	summaries, err := w.Gateway.SummarizeBatch(ctx, batch, "digest-batch-"+key)
	if err != nil {
		return Result{}, err
	}
	markdown, err := reportMarkdown(selected, summaries)
	if err != nil {
		return Result{}, err
	}
	pdf, err := w.Gateway.RenderPDF(ctx, markdown, "digest-pdf-"+key)
	if err != nil {
		return Result{}, err
	}
	return Result{Included: len(selected), PDF: pdf}, nil
}

func selectRecords(records []Record) []Record {
	selected := make([]Record, 0, len(records))
	for _, record := range records {
		if record.DeliveryStatus == "delivered" && record.SubscriberUpdatePending {
			selected = append(selected, record)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].DeliveredAt.Equal(selected[j].DeliveredAt) {
			return selected[i].ID < selected[j].ID
		}
		return selected[i].DeliveredAt.Before(selected[j].DeliveredAt)
	})
	return selected
}

func prompt(record Record) string {
	return fmt.Sprintf("Summarize this completed creator-commerce delivery for an operations digest. State the asset, recipient impact, and processing outcome in two factual sentences. Creator: %s\nAsset: %s\nDelivered: %s\nProcessing notes: %s", record.Creator, record.Asset, record.DeliveredAt.UTC().Format(time.RFC3339), record.ProcessingNotes)
}

func reportMarkdown(records []Record, summaries map[string]string) (string, error) {
	var report strings.Builder
	report.WriteString("# Overnight creator delivery digest\n\n")
	for _, record := range records {
		summary, ok := summaries[record.ID]
		if !ok || strings.TrimSpace(summary) == "" {
			return "", fmt.Errorf("missing summary for record %q", record.ID)
		}
		fmt.Fprintf(&report, "## %s: %s\n\n%s\n\n", record.Creator, record.Asset, summary)
	}
	return report.String(), nil
}

func stableKey(records []Record) string {
	hash := sha256.New()
	for _, record := range records {
		fmt.Fprintf(hash, "%s\x00%s\x00", record.ID, record.DeliveredAt.UTC().Format(time.RFC3339Nano))
	}
	return hex.EncodeToString(hash.Sum(nil))[:24]
}
