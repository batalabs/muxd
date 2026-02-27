package agent

import (
	"sync"
	"time"

	"github.com/batalabs/muxd/internal/checkpoint"
	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/mcp"
	"github.com/batalabs/muxd/internal/provider"
	"github.com/batalabs/muxd/internal/store"
	"github.com/batalabs/muxd/internal/tools"
)

// ---------------------------------------------------------------------------
// Agent event types -- used by adapters to observe agent loop progress
// ---------------------------------------------------------------------------

// EventKind classifies agent events.
type EventKind int

const (
	EventDelta      EventKind = iota // streaming text chunk
	EventStreamDone                  // one API call finished
	EventToolStart                   // about to execute a tool
	EventToolDone                    // tool execution completed
	EventTurnDone                    // full turn complete (end_turn)
	EventError                       // unrecoverable error
	EventCompacted                   // context was compacted
	EventAskUser                     // ask_user tool: pause for user input
	EventTitled                      // session title + tags generated
	EventRetrying                    // rate limit retry in progress
)

// Event carries data for a single agent event.
type Event struct {
	Kind                     EventKind
	DeltaText                string                // EventDelta
	Blocks                   []domain.ContentBlock // EventStreamDone
	StopReason               string                // EventStreamDone / EventTurnDone
	InputTokens              int                   // EventStreamDone
	OutputTokens             int                   // EventStreamDone
	CacheCreationInputTokens int                   // EventStreamDone
	CacheReadInputTokens     int                   // EventStreamDone
	ToolUseID                string                // EventToolStart / EventToolDone
	ToolName                 string                // EventToolStart / EventToolDone
	ToolResult               string                // EventToolDone
	ToolIsError              bool                  // EventToolDone
	Err                      error                 // EventError
	AskPrompt                string                // EventAskUser: question text
	AskResponse              chan<- string         // EventAskUser: adapter sends answer here
	NewTitle                 string                // EventTitled
	NewTags                  string                // EventTitled
	ModelUsed                string                // EventTitled / EventCompacted
	RetryAttempt             int                   // EventRetrying
	RetryAfter               time.Duration         // EventRetrying
	RetryMessage             string                // EventRetrying
}

// EventFunc is the callback signature for agent event delivery.
// Called synchronously from Submit's goroutine. The adapter handles
// thread safety on its side.
type EventFunc func(Event)

// LoopLimit is the maximum number of agent loop iterations per turn.
// Higher than 25 to accommodate long, tool-heavy audits/refactors.
const LoopLimit = 60

// ---------------------------------------------------------------------------
// Store interface -- decouples agent from concrete store implementation
// ---------------------------------------------------------------------------

// Store is the interface the agent uses for persistence. Concrete
// implementations live in the store package (or main for now).
type Store interface {
	AppendMessage(sessionID, role, content string, tokens int) error
	AppendMessageBlocks(sessionID, role string, blocks []domain.ContentBlock, tokens int) error
	UpdateSessionTokens(sessionID string, inputTokens, outputTokens int) error
	UpdateSessionTitle(sessionID, title string) error
	UpdateSessionModel(sessionID, model string) error
	UpdateSessionTags(sessionID, tags string) error
	GetMessages(sessionID string) ([]domain.TranscriptMessage, error)
	CreateSession(projectPath, model string) (*domain.Session, error)
	SaveCompaction(sessionID, summaryText string, cutoffSequence int) error
	LatestCompaction(sessionID string) (summaryText string, cutoffSequence int, err error)
	GetMessagesAfterSequence(sessionID string, afterSequence int) ([]domain.TranscriptMessage, error)
	MessageMaxSequence(sessionID string) (int, error)
	BranchSession(fromSessionID string, atSequence int) (*domain.Session, error)
}

// ScheduledToolJobStore is an optional extension used by scheduling tools.
type ScheduledToolJobStore interface {
	CreateScheduledToolJob(toolName string, toolInput map[string]any, scheduledFor time.Time, recurrence string) (string, error)
	ListScheduledToolJobs(limit int) ([]store.ScheduledToolJob, error)
	CancelScheduledToolJob(id string) error
	UpdateScheduledToolJob(id string, toolInput map[string]any, scheduledFor *time.Time, recurrence *string) error
}

// ---------------------------------------------------------------------------
// Service -- standalone agent loop, drivable by any adapter
// ---------------------------------------------------------------------------

// Service encapsulates the agent conversation loop independent of any
// UI framework. Adapters (TUI, Telegram, future HTTP) call Submit() to drive
// the loop and receive progress via the callback.
type Service struct {
	mu sync.Mutex

	apiKey     string
	modelID    string
	modelLabel string
	prov       provider.Provider

	store   Store
	session *domain.Session

	messages        []domain.TranscriptMessage
	inputTokens     int
	outputTokens    int
	lastInputTokens int
	agentLoopCount  int

	running     bool
	cancelled   bool
	titled      bool
	userRenamed bool // true when user manually renamed the session

	// Cwd is the working directory used for system prompts.
	Cwd string

	// Todos is the per-session in-memory todo list.
	todos tools.TodoList

	// planMode is true when the agent is in plan mode (write tools disabled).
	planMode bool

	// isSubAgent is true when this Service is a sub-agent spawned by the task tool.
	isSubAgent bool

	// Git state
	gitAvailable bool
	gitRepoRoot  string
	checkpoints  []checkpoint.Checkpoint
	redoStack    []checkpoint.Checkpoint

	// braveAPIKey is the Brave Search API key from preferences.
	braveAPIKey    string
	textbeltAPIKey string

	// Per-task utility model overrides (resolved model IDs).
	modelCompact string // for compaction summaries
	modelTitle   string // for auto-title generation
	modelTags    string // for auto-tag generation

	xClientID     string
	xClientSecret string
	xAccessToken  string
	xRefreshToken string
	xTokenExpiry  string
	xTokenSaver   func(accessToken, refreshToken, tokenExpiry string) error

	// disabledTools are excluded from model tool specs and execution.
	disabledTools map[string]bool

	// mcpManager manages MCP server connections and tool routing.
	mcpManager *mcp.Manager

	// memory is the per-project persistent fact store.
	memory *tools.ProjectMemory
}

// NewService creates a new Service for the given session.
func NewService(apiKey, modelID, modelLabel string, store Store, session *domain.Session, prov provider.Provider) *Service {
	var inTok, outTok int
	if session != nil {
		inTok = session.InputTokens
		outTok = session.OutputTokens
	}
	return &Service{
		apiKey:        apiKey,
		modelID:       modelID,
		modelLabel:    modelLabel,
		prov:          prov,
		store:         store,
		session:       session,
		inputTokens:   inTok,
		outputTokens:  outTok,
		disabledTools: map[string]bool{},
	}
}
