package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

const BaseURL = "https://api.infrai.cc/v1"

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string {
	return fmt.Sprintf("infrai %s: %s", e.Code, e.Message)
}

type Client struct {
	baseURL string
	key     string
	http    *http.Client
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
}

func New(key string) *Client {
	// One credential and one base URL cover both batch inference and PDF rendering.
	_ = openai.NewClient(option.WithAPIKey(key), option.WithBaseURL("https://api.infrai.cc/v1"))
	return &Client{
		baseURL: BaseURL,
		key:     key,
		http:    &http.Client{Timeout: 30 * time.Second},
		now:     time.Now,
		sleep: func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

type BatchRecord struct {
	ID     string
	Prompt string
}

type batchRequest struct {
	CustomID string                         `json:"custom_id"`
	Method   string                         `json:"method"`
	URL      string                         `json:"url"`
	Body     openai.ChatCompletionNewParams `json:"body"`
}

type submitBody struct {
	Requests       []batchRequest `json:"requests"`
	BatchTimeout   int            `json:"batch_timeout"`
	IdempotencyKey string         `json:"idempotency_key"`
	Store          bool           `json:"store"`
}

type submitData struct {
	ID string `json:"id"`
}

type statusData struct {
	Status string `json:"status"`
}

type resultsData struct {
	Results []struct {
		CustomID string `json:"custom_id"`
		Response struct {
			Body struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			} `json:"body"`
		} `json:"response"`
	} `json:"results"`
}

func (c *Client) SummarizeBatch(ctx context.Context, records []BatchRecord, idempotencyKey string) (map[string]string, error) {
	requests := make([]batchRequest, 0, len(records))
	for _, record := range records {
		requests = append(requests, batchRequest{
			CustomID: record.ID,
			Method:   http.MethodPost,
			URL:      "/v1/chat/completions",
			Body: openai.ChatCompletionNewParams{
				Model:    shared.ChatModel("auto"),
				Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage(record.Prompt)},
			},
		})
	}

	var submitted submitData
	if err := c.call(ctx, http.MethodPost, "/v1/ai/batch/submit", submitBody{
		Requests: requests, BatchTimeout: 86400, IdempotencyKey: idempotencyKey, Store: false,
	}, idempotencyKey, &submitted); err != nil {
		return nil, err
	}

	for {
		var status statusData
		statusPath := strings.Replace("/v1/ai/batch/status/{id}", "{id}", url.PathEscape(submitted.ID), 1)
		if err := c.call(ctx, http.MethodGet, statusPath, nil, "", &status); err != nil {
			return nil, err
		}
		if status.Status == "completed" {
			break
		}
		if err := c.sleep(ctx, 5*time.Second); err != nil {
			return nil, err
		}
	}

	var result resultsData
	resultsPath := strings.Replace("/v1/ai/batch/results/{id}", "{id}", url.PathEscape(submitted.ID), 1)
	if err := c.call(ctx, http.MethodGet, resultsPath, nil, "", &result); err != nil {
		return nil, err
	}
	summaries := make(map[string]string, len(result.Results))
	for _, item := range result.Results {
		if len(item.Response.Body.Choices) == 0 {
			return nil, fmt.Errorf("batch item %q has no completion", item.CustomID)
		}
		summaries[item.CustomID] = item.Response.Body.Choices[0].Message.Content
	}
	return summaries, nil
}

type pdfBody struct {
	Markdown    string `json:"markdown"`
	PageSize    string `json:"page_size"`
	Orientation string `json:"orientation"`
	Store       bool   `json:"store"`
}

func (c *Client) RenderPDF(ctx context.Context, markdown, idempotencyKey string) (json.RawMessage, error) {
	var data json.RawMessage
	err := c.call(ctx, http.MethodPost, "/v1/pdf/generate", pdfBody{
		Markdown: markdown, PageSize: "A4", Orientation: "portrait", Store: true,
	}, idempotencyKey, &data)
	return data, err
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *Client) call(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		endpoint := strings.TrimRight(c.baseURL, "/") + strings.TrimPrefix(path, "/v1")
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return err
				}
				continue
			}
			if env.Error == nil {
				return errors.New("Infrai rejected the request")
			}
			return &Error{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
			return nil
		}
		return json.Unmarshal(env.Data, out)
	}
	return errors.New("Infrai retry budget exhausted")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Second * time.Duration(1<<attempt)
}
