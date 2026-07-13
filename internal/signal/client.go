// Package signal sends notifications through a signal-cli-rest-api instance.
package signal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
)

// Client sends messages via a signal-cli-rest-api instance's /v2/send endpoint.
type Client struct {
	baseURL    string
	username   string
	password   string
	sender     string
	recipients []string
	httpClient *http.Client
}

// New builds a Client from the given Signal configuration and per-request timeout.
func New(cfg config.SignalConfig, timeout time.Duration) *Client {
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		username:   cfg.Username,
		password:   cfg.Password,
		sender:     cfg.SenderNumber,
		recipients: cfg.Recipients,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type sendMessageRequest struct {
	Message    string   `json:"message"`
	Number     string   `json:"number"`
	Recipients []string `json:"recipients"`
}

// Send posts message to the configured signal-cli-rest-api instance, using
// HTTP Basic Auth on every request.
func (c *Client) Send(ctx context.Context, message string) error {
	body, err := json.Marshal(sendMessageRequest{
		Message:    message,
		Number:     c.sender,
		Recipients: c.recipients,
	})
	if err != nil {
		return fmt.Errorf("signal: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v2/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("signal: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("signal: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("signal: send failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
