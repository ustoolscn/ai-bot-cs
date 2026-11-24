package openai

import (
	"context"
	"encoding/json"
	"fmt"

	"ai-bot/backend/internal/domain"
)

// TypeSafeDecision evaluates all yes/no questions in one System One request.
func (c *Client) TypeSafeDecision(ctx context.Context, state any, instructions map[string]string) (domain.ChatResult, error) {
	type question struct {
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
	}
	questions := make(map[string]question, len(instructions))
	for name, prompt := range instructions {
		questions[name] = question{"noul", prompt}
	}
	var response struct {
		Answers map[string]struct {
			Type        string   `json:"type"`
			Probability *float64 `json:"noul"`
		} `json:"answers"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	var result domain.ChatResult
	err := c.do(ctx, "/systemone", map[string]any{"model": c.Model, "state": state, "questions": questions}, &response)
	if err != nil {
		return result, err
	}
	result.InputTokens, result.OutputTokens = response.Usage.Input, response.Usage.Output
	flags := make(map[string]bool, len(questions))
	for name := range questions {
		answer := response.Answers[name]
		if answer.Type != "noul" || answer.Probability == nil || *answer.Probability < 0 || *answer.Probability > 1 {
			return result, fmt.Errorf("TypeSafe 判断结果 %q 缺少有效概率", name)
		}
		flags[name] = *answer.Probability >= 0.5
	}
	data, _ := json.Marshal(flags)
	result.Content = string(data)
	return result, nil
}
