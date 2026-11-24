package app

import (
	"context"

	"ai-bot/backend/internal/openai"
)

type queryEmbeddingKey struct {
	profile, endpoint, model, text string
	dimensions                     int
}

// queryEmbeddings belongs to one turn. Failed requests can be retried by
// another consumer, and a rewritten query gets its own vector.
type queryEmbeddings map[queryEmbeddingKey][]float32

func (cache *queryEmbeddings) get(a *App, ctx context.Context, cl *openai.Client, profile, text string) ([]float32, error) {
	key := queryEmbeddingKey{profile, cl.BaseURL, cl.Model, text, cl.Dimensions}
	if vector, ok := (*cache)[key]; ok {
		return vector, nil
	}
	vectors, err := a.embed(ctx, cl, profile, "embedding", []string{text})
	if err != nil {
		return nil, err
	}
	if *cache == nil {
		*cache = make(queryEmbeddings)
	}
	(*cache)[key] = vectors[0]
	return vectors[0], nil
}
