package domain

import (
	"context"
	"time"
)

type ContentPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	URL         string `json:"url,omitempty"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	StorageKey  string `json:"storageKey,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Data        []byte `json:"-"`
}

type InboundMessage struct {
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

type ChatMessage struct {
	Role    string            `json:"role"`
	Content string            `json:"content"`
	Parts   []ChatContentPart `json:"parts,omitempty"`
}
type ChatResult struct {
	Content                   string
	InputTokens, OutputTokens int
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
