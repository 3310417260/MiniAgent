package llm

import "context"

type Client interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
	GenerateStream(ctx context.Context, req GenerateRequest, onDelta func(string)) (GenerateResponse, error)
}
