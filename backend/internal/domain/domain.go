package domain

import (
	"context"
	"encoding/json"
	"time"
)

type ContentPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	URL         string `json:"url,omitempty"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	WaveURL     string `json:"waveUrl,omitempty"`
	Transcript  string `json:"transcript,omitempty"`
	StorageKey  string `json:"storageKey,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Data        []byte `json:"-"`
}

type InboundMessage struct {
	MentionsBot                                               bool
	EventID, Channel, BotID, ConversationType, ConversationID string
	ConversationName                                          string
	SenderID, SenderName, PlatformMessageID, EventType, Text  string
	Parts                                                     []ContentPart
	EventTime                                                 time.Time
	ReplyDeadline                                             *time.Time
	Raw                                                       []byte
}

type OutboundMessage struct {
	ID, Channel, BotID, ConversationType, ConversationID string
	ReplyToMessageID, Text                               string
	ReferenceMessageID, MentionUserID, MentionUserName   string
	Format                                               string
	Parts                                                []ContentPart
	ReplyDeadline                                        *time.Time
	Sequence                                             int
}

type ChatContentPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	URL         string `json:"url,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Detail      string `json:"detail,omitempty"`
	DataURL     string `json:"-"`
}

// CompletionState is replayed only within the current provider tool loop.
type CompletionState struct {
	Reasoning       json.RawMessage
	ServerToolCalls json.RawMessage
}
type ChatMessage struct {
	CompletionState `json:"-"`
	Name            string            `json:"name,omitempty"`
	Role            string            `json:"role"`
	Content         string            `json:"content"`
	Parts           []ChatContentPart `json:"parts,omitempty"`
	ToolCalls       []ToolCall        `json:"toolCalls,omitempty"`
	ToolCallID      string            `json:"toolCallId,omitempty"`
}
type SearchSource struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"siteName"`
}

type ChatResult struct {
	CompletionState                     `json:"-"`
	Searched                            bool
	SearchQueries                       []string
	Sources                             []SearchSource
	Content                             string
	InputTokens, OutputTokens           int
	CachedInputTokens, CacheWriteTokens int
	ToolCalls                           []ToolCall
}

// ToolCall is a function call requested by the model.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec describes a function the model may call.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatOptions are per-request options.
type ChatOptions struct {
	Tools            []ToolSpec
	MaxOutputTokens  int
	DisableWebSearch bool
	RequireWebSearch bool
}

type ChannelAdapter interface {
	ParseInbound(context.Context, string, []byte, map[string]string) (*InboundMessage, []byte, error)
	Send(context.Context, OutboundMessage) (string, error)
}
type ChatModel interface {
	Chat(context.Context, []ChatMessage) (ChatResult, error)
}
type EmbeddingModel interface {
	Embed(context.Context, []string) ([][]float32, error)
}
type KnowledgeRetriever interface {
	Retrieve(context.Context, []string, []float32, int) ([]KnowledgeHit, error)
}
type KnowledgeHit struct {
	ChunkID, DocumentID, Content string
	Score                        float64
}
type FileStorage interface {
	Save(context.Context, string, []byte) (string, error)
	Read(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
type Tool interface {
	Name() string
	Call(context.Context, map[string]any) (any, error)
}
