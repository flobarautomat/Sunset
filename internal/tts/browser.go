package tts

import "context"

type browserProvider struct{}

func (b *browserProvider) Speak(_ context.Context, text, _ string) (*Result, error) {
	return &Result{
		Type: "text",
		Text: text,
	}, nil
}
