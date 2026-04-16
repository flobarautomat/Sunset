package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	provider   string // "sunset" or "anthropic"
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(provider, apiURL, apiKey, model string) *Client {
	return &Client{
		provider: provider,
		apiURL:   apiURL,
		apiKey:   apiKey,
		model:    model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) HasKey() bool {
	return c.apiKey != ""
}

func (c *Client) ChatStream(ctx context.Context, messages []Message) <-chan Chunk {
	if c.provider == "anthropic" {
		return c.chatStreamAnthropic(ctx, messages)
	}
	return c.chatStreamSunset(ctx, messages)
}

// --- Sunset / OpenAI-compatible provider ---

type openAIChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type deltaContent struct {
	Content string `json:"content"`
}

type openAIChoice struct {
	Delta deltaContent `json:"delta"`
}

type openAIStreamResponse struct {
	Choices []openAIChoice `json:"choices"`
}

func (c *Client) chatStreamSunset(ctx context.Context, messages []Message) <-chan Chunk {
	ch := make(chan Chunk, 64)

	go func() {
		defer close(ch)

		// Sunset proxy doesn't support role:"system" in streaming mode —
		// fold system content into the first user message.
		var systemPrefix string
		var filtered []Message
		for _, m := range messages {
			if m.Role == "system" {
				systemPrefix += m.Content + "\n\n"
			} else {
				filtered = append(filtered, m)
			}
		}
		if systemPrefix != "" && len(filtered) > 0 && filtered[0].Role == "user" {
			filtered[0] = Message{Role: "user", Content: systemPrefix + filtered[0].Content}
		}

		body, err := json.Marshal(openAIChatRequest{
			Model:    c.model,
			Messages: filtered,
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

		c.readSSE(ch, req, func(data string) *Chunk {
			var sr openAIStreamResponse
			if err := json.Unmarshal([]byte(data), &sr); err != nil {
				return nil
			}
			if len(sr.Choices) > 0 && sr.Choices[0].Delta.Content != "" {
				return &Chunk{Text: sr.Choices[0].Delta.Content}
			}
			return nil
		})
	}()

	return ch
}

// --- Anthropic Messages API ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream"`
	Messages  []anthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicStreamEvent struct {
	Type  string          `json:"type"`
	Delta json.RawMessage `json:"delta,omitempty"`
}

type anthropicTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (c *Client) chatStreamAnthropic(ctx context.Context, messages []Message) <-chan Chunk {
	ch := make(chan Chunk, 64)

	go func() {
		defer close(ch)

		// Separate system message from conversation messages
		var system string
		var convMessages []anthropicMessage
		for _, m := range messages {
			if m.Role == "system" {
				system = m.Content
			} else {
				convMessages = append(convMessages, anthropicMessage{
					Role:    m.Role,
					Content: m.Content,
				})
			}
		}

		// Strip provider prefix if present (e.g. "anthropic/claude-haiku-4-5-20251001" -> "claude-haiku-4-5-20251001")
		model := c.model
		if parts := strings.SplitN(model, "/", 2); len(parts) == 2 {
			model = parts[1]
		}

		body, err := json.Marshal(anthropicRequest{
			Model:     model,
			MaxTokens: 1024,
			Stream:    true,
			Messages:  convMessages,
			System:    system,
		})
		if err != nil {
			ch <- Chunk{Err: fmt.Errorf("marshal request: %w", err)}
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
		if err != nil {
			ch <- Chunk{Err: fmt.Errorf("create request: %w", err)}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		c.readSSE(ch, req, func(data string) *Chunk {
			var event anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return nil
			}
			switch event.Type {
			case "content_block_delta":
				var delta anthropicTextDelta
				if err := json.Unmarshal(event.Delta, &delta); err != nil {
					return nil
				}
				if delta.Text != "" {
					return &Chunk{Text: delta.Text}
				}
			case "message_stop":
				return &Chunk{Done: true}
			case "error":
				return &Chunk{Err: fmt.Errorf("anthropic error: %s", string(event.Delta))}
			}
			return nil
		})
	}()

	return ch
}

// readSSE performs the HTTP request and reads SSE lines, calling parseData for each data payload.
func (c *Client) readSSE(ch chan<- Chunk, req *http.Request, parseData func(string) *Chunk) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		ch <- Chunk{Err: fmt.Errorf("request failed: %w", err)}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		ch <- Chunk{Err: fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(body))}
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

		if chunk := parseData(data); chunk != nil {
			ch <- *chunk
			if chunk.Done || chunk.Err != nil {
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- Chunk{Err: fmt.Errorf("read stream: %w", err)}
		return
	}

	ch <- Chunk{Done: true}
}
