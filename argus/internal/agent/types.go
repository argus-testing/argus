package agent

import (
	"context"
	"encoding/json"
)

// Role identifies the author of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type TextPart struct {
	Text string
}

type ImagePart struct {
	Data      []byte
	MediaType string
}

type AudioPart struct {
	Data      []byte
	MediaType string
}

type ToolCallPart struct {
	CallID       string
	Name         string
	Arguments    map[string]any
	ProviderData map[string]any
}

type ToolResultPart struct {
	CallID string
	Name   string
	Result any
}

type MessagePart struct {
	Text       *TextPart
	Image      *ImagePart
	Audio      *AudioPart
	ToolCall   *ToolCallPart
	ToolResult *ToolResultPart
}

// ResponsePart is a message part emitted by a model.
type ResponsePart = MessagePart

type Message struct {
	Role  Role
	Parts []MessagePart
}

type ModelRef struct {
	Provider string
	Model    string
}

type GenerationOptions struct {
	Temperature     *float64
	MaxOutputTokens *int
	JSONMode        bool
}

type AgentSpec struct {
	Name        string
	Model       ModelRef
	Instruction string
	Tools       []Tool
	Generation  GenerationOptions
}

type ToolContext struct {
	State map[string]any
}

type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Invoke      func(context.Context, map[string]any, ToolContext) (any, error)
}

// ToolOutput separates the serializable function response from optional
// multimodal observations that should be supplied as a normal user message.
type ToolOutput struct {
	Result   any
	Followup []MessagePart
}

type ModelRequest struct {
	Model             ModelRef
	SystemInstruction string
	Messages          []Message
	Tools             []Tool
	Generation        GenerationOptions
}

type TextDelta struct {
	Text string
}

type ModelResponse struct {
	Parts []ResponsePart
}

type ModelEvent interface{}

type Provider interface {
	Stream(context.Context, ModelRequest, func(ModelEvent) error) error
}

type Session struct {
	ID       string
	State    map[string]any
	Messages []Message
}

type SessionStore interface {
	GetOrCreate(context.Context, string, map[string]any) (*Session, error)
	Append(context.Context, string, Message) error
}

type ToolCallEvent struct {
	Call ToolCallPart
}

type ToolResultEvent struct {
	Result ToolResultPart
}

type CompletedEvent struct {
	Message Message
}

type RuntimeEvent interface{}
