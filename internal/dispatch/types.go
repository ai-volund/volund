// Package dispatch defines the NATS task dispatch layer between the control
// plane and agent pods.
//
// Transport format uses plain JSON structs (not proto) so that encoding/json
// works cleanly for both sides without protojson complications around oneofs.
// The agent converts these to proto types before calling the LLM router.
package dispatch

// Task is the JSON payload published to volund.pool.{profileName} when the
// control plane dispatches work to an available agent pod.
type Task struct {
	TaskID         string    `json:"task_id"`
	ConversationID string    `json:"conversation_id"`
	TenantID       string    `json:"tenant_id"`
	AgentID        string    `json:"agent_id"`
	ProfileName    string    `json:"profile_name"` // used to pick pool queue group
	ProfileType    string    `json:"profile_type"` // "orchestrator" | "specialist"
	InstanceID     string    `json:"instance_id,omitempty"` // set when dispatching to a claimed instance
	DBTaskID       string    `json:"db_task_id,omitempty"`  // persisted task row ID for status tracking
	ParentTaskID   string    `json:"parent_task_id,omitempty"`  // task that spawned this delegation
	DelegationDepth int     `json:"delegation_depth,omitempty"` // 0 = top-level, incremented per hop

	// LLM config — zero values mean agent uses its own defaults.
	SystemPrompt  string  `json:"system_prompt,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	Model         string  `json:"model,omitempty"`
	MaxTokens     int32   `json:"max_tokens,omitempty"`
	Temperature   float64 `json:"temperature,omitempty"`
	MaxToolRounds int     `json:"max_tool_rounds,omitempty"`

	// TraceContext carries W3C trace context headers for distributed tracing.
	TraceContext map[string]string `json:"trace_context,omitempty"`

	// Full conversation history. Agent appends as it runs.
	Messages []Message `json:"messages"`

	// EnabledTools restricts which built-ins are active. Empty = all.
	EnabledTools []string `json:"enabled_tools,omitempty"`

	// Skills are resolved skill specs to load at task start.
	// Populated by the gateway from the agent's profile skills list.
	Skills []SkillSpec `json:"skills,omitempty"`

	// Credentials are scoped, short-lived tokens issued by the credential broker.
	// Attached at dispatch time for tools that need external API access.
	Credentials []TaskCredential `json:"credentials,omitempty"`
}

// TaskCredential is a short-lived credential token attached to a task.
type TaskCredential struct {
	Provider  string   `json:"provider"`   // e.g. "github", "jira", "slack"
	Token     string   `json:"token"`      // the decrypted credential value
	Scopes    []string `json:"scopes"`     // granted scopes
	ExpiresIn int      `json:"expires_in"` // TTL in seconds
}

// SkillSpec mirrors the agent-side skill.Spec for transport over NATS.
type SkillSpec struct {
	Name        string           `json:"name"`
	Type        string           `json:"type"` // prompt, mcp, cli
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Prompt      string           `json:"prompt,omitempty"`
	Runtime     *SkillRuntimeSpec `json:"runtime,omitempty"`
	CLI         *SkillCLISpec    `json:"cli,omitempty"`
	Parameters  []SkillParameter `json:"parameters,omitempty"`
}

// SkillRuntimeSpec is the transport-side MCP runtime config.
type SkillRuntimeSpec struct {
	Image     string `json:"image,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Transport string `json:"transport,omitempty"`
}

// SkillCLISpec is the transport-side CLI wrapper config.
type SkillCLISpec struct {
	Binary          string   `json:"binary"`
	AllowedCommands []string `json:"allowedCommands"`
}

// SkillParameter is a transport-side tool parameter definition.
type SkillParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

// Message is a transport-friendly LLM message.
type Message struct {
	Role    string  `json:"role"` // "system" | "user" | "assistant" | "tool"
	Content []Block `json:"content"`
}

// Block is a single content block within a Message.
type Block struct {
	Type string `json:"type"` // "text" | "tool_use" | "tool_result"

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ToolUseID string `json:"tool_use_id,omitempty"`
	Name      string `json:"name,omitempty"`
	InputJSON string `json:"input_json,omitempty"`

	// tool_result
	Content string `json:"content,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

// ProfileConfig holds the model and runtime settings for an agent profile.
// The gateway resolves this from the profile store and injects it into the task.
type ProfileConfig struct {
	Provider      string  `json:"provider,omitempty"`
	Model         string  `json:"model,omitempty"`
	MaxTokens     int32   `json:"max_tokens,omitempty"`
	Temperature   float64 `json:"temperature,omitempty"`
	MaxToolRounds int     `json:"max_tool_rounds,omitempty"`
	SystemPrompt  string  `json:"system_prompt,omitempty"`
}

// SteerMessage is the payload published to volund.agent.{instanceId}.steer
// when a user sends a mid-run correction.
type SteerMessage struct {
	Content string `json:"content"`
}

// PoolSubject returns the NATS subject for a given agent profile name.
// Agent pods subscribe to this with a queue group so NATS delivers each
// task to exactly one pod.
func PoolSubject(profileName string) string {
	return "volund.pool." + profileName
}

// ConvStreamSubject returns the NATS subject where an agent publishes
// its real-time token events for a conversation.
func ConvStreamSubject(convID string) string {
	return "volund.conv." + convID + ".stream"
}

// InstanceSubject returns the NATS subject for dispatching a task to a
// specific agent instance (bypassing the pool queue group).
func InstanceSubject(instanceID string) string {
	return "volund.agent." + instanceID + ".task"
}

// SteerSubject returns the NATS subject for mid-run steering messages.
func SteerSubject(instanceID string) string {
	return "volund.agent." + instanceID + ".steer"
}
