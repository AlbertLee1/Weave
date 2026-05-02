// Package aip implements the persistent conversation-thread service for
// the AI Platform (US-279). A Thread is an ordered sequence of Messages
// the caller exchanges with an LLM Provider; Threads are scoped to one
// provider (see Provider interface) and optionally seeded with a system
// prompt that primes every assistant response.
//
// This package is deliberately persistence-agnostic — the Store interface
// accepts in-memory, PostgreSQL, or any other backing implementation.
// HTTP wiring lives in handlers.go and the PG-backed Store lives in
// cmd/server/aip_store.go (matches the dep-direction trick used by
// pgFeatureFlagsStore + pgTenantQuotaStore).
package aip

import (
	"fmt"
	"regexp"
	"time"
)

// Roles permitted on Message.Role. Mirrors the OpenAI / Anthropic chat
// role taxonomy: a stored thread may have one optional "system" anchor
// at the top followed by alternating user / assistant turns. RoleTool
// (US-284) carries the result of a function call requested by an
// assistant message earlier in the thread; it always references the
// originating tool_call by ToolCallID.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Provider names recognised by IsKnownProvider. Any other string is
// rejected at the API boundary so misconfigured threads cannot be
// persisted with a provider Weave does not know how to dispatch.
const (
	ProviderMock      = "mock"
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
)

// Thread is one persisted AIP conversation row. Messages live in a
// separate aip_messages table and are loaded on demand via Store.ListMessages.
type Thread struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model,omitempty"`
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	CreatedBy    string    `json:"createdBy,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ThreadUpdate is the partial-update payload for Store.UpdateThread.
// Pointer fields carry the three-state "omit=preserve" semantic, same
// shape as featureflags.FlagUpdate.
type ThreadUpdate struct {
	Title        *string `json:"title,omitempty"`
	Model        *string `json:"model,omitempty"`
	SystemPrompt *string `json:"systemPrompt,omitempty"`
}

// Message is one row in aip_messages. ID is a monotonic sequence
// allocated by the store on Append, so callers compare IDs to order
// messages within a thread.
//
// ToolCalls (US-284) is non-empty on assistant rows that requested one
// or more function invocations — Content is typically empty in that
// case and the model is awaiting tool results before continuing.
// ToolCallID + ToolName are set on RoleTool rows and reference the
// assistant tool_call being answered.
//
// ParentMessageID + BranchID (US-374) form the branch-tree backbone:
// every message has at most one parent on its branch (nil = branch
// root); BranchID groups sibling chains so a thread can carry multiple
// alternative continuations from any pivot point.
type Message struct {
	ID              int64      `json:"id"`
	ThreadID        string     `json:"threadId"`
	Role            string     `json:"role"`
	Content         string     `json:"content"`
	TokenCount      int        `json:"tokenCount,omitempty"`
	ToolCalls       []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID      string     `json:"toolCallId,omitempty"`
	ToolName        string     `json:"toolName,omitempty"`
	ParentMessageID *int64     `json:"parentMessageId,omitempty"`
	BranchID        string     `json:"branchId,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// DefaultBranchID is the branch identifier used for the linear (non-forked)
// history of every thread. Messages whose store impl predates US-374 read
// back as branch_id='main' via the migration default.
const DefaultBranchID = "main"

// ValidateBranchID returns an error when id is not a permitted branch
// identifier. Mirrors the SQL CHECK on aip_messages.branch_id.
func ValidateBranchID(id string) error {
	if id == "" {
		return fmt.Errorf("branch id must not be empty")
	}
	if !threadIDRE.MatchString(id) {
		return fmt.Errorf("branch id %q is invalid: allowed characters are [A-Za-z0-9._-] and length must be 1..128", id)
	}
	return nil
}

// threadIDRE matches the canonical thread identifier shape: the same
// allowlist used by feature_flags / tenant_quotas (1..128 of
// alphanumeric, dot, underscore, hyphen). Threads are typically created
// with a UUID-style ID minted by the handler.
var threadIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// providerRE matches the lowercase provider identifier admitted by both
// IsKnownProvider and the aip_threads_provider_format CHECK constraint.
var providerRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ValidateThreadID returns an error when id is not an acceptable thread
// identifier. Matches the SQL CHECK constraint on aip_threads.id.
func ValidateThreadID(id string) error {
	if id == "" {
		return fmt.Errorf("thread id must not be empty")
	}
	if !threadIDRE.MatchString(id) {
		return fmt.Errorf("thread id %q is invalid: allowed characters are [A-Za-z0-9._-] and length must be 1..128", id)
	}
	return nil
}

// ValidateProvider returns an error when provider does not match the
// canonical lowercase identifier shape. Empty providers are rejected;
// a thread MUST be bound to a provider at create time.
func ValidateProvider(provider string) error {
	if provider == "" {
		return fmt.Errorf("provider must not be empty")
	}
	if !providerRE.MatchString(provider) {
		return fmt.Errorf("provider %q is invalid: must match %s", provider, providerRE.String())
	}
	return nil
}

// IsKnownProvider reports whether name is a built-in provider Weave
// can dispatch (mock / openai / anthropic). Custom providers may still
// pass ValidateProvider but will fail at SendMessage time when the
// Registry has no matching factory.
func IsKnownProvider(name string) bool {
	switch name {
	case ProviderMock, ProviderOpenAI, ProviderAnthropic:
		return true
	}
	return false
}

// KnownProviders returns the list of built-in provider identifiers in a
// stable order (mock first so tests / dev callers default to it).
func KnownProviders() []string {
	return []string{ProviderMock, ProviderOpenAI, ProviderAnthropic}
}

// ValidateRole returns an error when role is not one of the four
// permitted message roles. Matches the SQL CHECK on aip_messages.role.
func ValidateRole(role string) error {
	switch role {
	case RoleSystem, RoleUser, RoleAssistant, RoleTool:
		return nil
	}
	return fmt.Errorf("role %q is invalid: must be one of system/user/assistant/tool", role)
}
