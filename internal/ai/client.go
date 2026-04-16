package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Chunk struct {
	Text string
	Done bool
	Err  error
}

type Client struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(apiURL, apiKey, model string) *Client {
	return &Client{
		apiURL: apiURL,
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) HasKey() bool {
	return c.apiKey != ""
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type deltaContent struct {
	Content string `json:"content"`
}

type choice struct {
	Delta deltaContent `json:"delta"`
}

type streamResponse struct {
	Choices []choice `json:"choices"`
}

func (c *Client) ChatStream(ctx context.Context, messages []Message) <-chan Chunk {
	ch := make(chan Chunk, 64)

	go func() {
		defer close(ch)

		body, err := json.Marshal(chatRequest{
			Model:    c.model,
			Messages: messages,
			Stream:   true,
		})
		if err != nil {
			ch <- Chunk{Err: fmt.Errorf("marshal request: %w", err)}
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/api/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			ch <- Chunk{Err: fmt.Errorf("create request: %w", err)}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			ch <- Chunk{Err: fmt.Errorf("request failed: %w", err)}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			ch <- Chunk{Err: fmt.Errorf("upstream returned %d", resp.StatusCode)}
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				ch <- Chunk{Done: true}
				return
			}

			var sr streamResponse
			if err := json.Unmarshal([]byte(data), &sr); err != nil {
				continue // skip malformed chunks
			}

			if len(sr.Choices) > 0 && sr.Choices[0].Delta.Content != "" {
				ch <- Chunk{Text: sr.Choices[0].Delta.Content}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- Chunk{Err: fmt.Errorf("read stream: %w", err)}
			return
		}

		// If we reach here without [DONE], still signal completion
		ch <- Chunk{Done: true}
	}()

	return ch
}
