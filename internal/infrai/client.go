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
)

type Client struct {
	baseURL string
	key     string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type envelope[T any] struct {
	OK       bool            `json:"ok"`
	Data     T               `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Domain struct {
	ZoneID string `json:"zone_id"`
}

type Presign struct {
	URL string `json:"url"`
}

func New(baseURL, key string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    httpClient,
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (c *Client) AddDomain(ctx context.Context, domain, requestID string) (Domain, error) {
	body := map[string]any{"domain": domain, "metadata": map[string]string{"request_id": requestID}}
	return do[Domain](ctx, c, http.MethodPost, "/v1/dns/domain/add", body)
}

func (c *Client) UpsertCNAME(ctx context.Context, zoneID, name, content string) error {
	body := map[string]any{
		"zone_id": zoneID, "record_type": "CNAME", "name": name,
		"content": content, "ttl": 300,
	}
	_, err := do[json.RawMessage](ctx, c, http.MethodPut, "/v1/dns/record/upsert", body)
	return err
}

func (c *Client) CreateBucket(ctx context.Context, name string) error {
	_, err := do[json.RawMessage](ctx, c, http.MethodPost, "/v1/storage/bucket/create", map[string]string{"name": name})
	return err
}

func (c *Client) PresignPhotoPut(ctx context.Context, bucket, key, contentType, requestID string, maxBytes int64) (Presign, error) {
	path := "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + url.PathEscape(key)
	body := map[string]any{
		"op": "put", "expires_seconds": 900, "content_type": contentType,
		"max_bytes": maxBytes, "idempotency_key": requestID,
	}
	return do[Presign](ctx, c, http.MethodPost, path, body)
}

func do[T any](ctx context.Context, c *Client, method, path string, body any) (T, error) {
	var zero T
	payload, err := json.Marshal(body)
	if err != nil {
		return zero, fmt.Errorf("encode request: %w", err)
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return zero, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.http.Do(req)
		if err != nil {
			return zero, fmt.Errorf("send request: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return zero, fmt.Errorf("read response: %w", readErr)
		}

		var env envelope[T]
		if err := json.Unmarshal(raw, &env); err != nil {
			return zero, fmt.Errorf("decode response envelope: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return zero, err
			}
			continue
		}
		if !env.OK {
			var detail errorBody
			if len(env.Error) > 0 {
				_ = json.Unmarshal(env.Error, &detail)
			}
			if detail.Message == "" {
				detail.Message = "Infrai request rejected"
			}
			return zero, &APIError{Code: detail.Code, Message: detail.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return zero, fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		return env.Data, nil
	}
	return zero, errors.New("retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}
