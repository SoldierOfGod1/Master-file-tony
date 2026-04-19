package chat

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
)

// QueueItem wraps a request and a channel to receive the result.
type QueueItem struct {
	Request  ExecuteRequest
	Response chan QueueResult
}

// QueueResult carries the response text or an error back to the caller.
type QueueResult struct {
	Content string
	Error   error
}

// QueueManager maintains a per-project buffered channel so that at most one
// Claude CLI process runs per project directory at a time. It also exposes
// the Loop Operator surface (ListActive / Kill / Pause) so the dashboard
// can intervene mid-run.
type QueueManager struct {
	executor *Executor
	queues   map[string]chan *QueueItem
	mu       sync.Mutex
	log      *slog.Logger
	db       *sql.DB
	tracker  *loopTracker
}

// NewQueueManager creates a QueueManager backed by the given executor, DB, and
// logger.
func NewQueueManager(executor *Executor, db *sql.DB, log *slog.Logger) *QueueManager {
	return &QueueManager{
		executor: executor,
		queues:   make(map[string]chan *QueueItem),
		log:      log,
		db:       db,
		tracker:  newLoopTracker(),
	}
}

// ListActive returns a snapshot of every project queue's current state —
// which conversation is running (if any), whether it's paused, and how
// many items are pending.
func (qm *QueueManager) ListActive() []LoopState {
	qm.mu.Lock()
	for dir, ch := range qm.queues {
		qm.tracker.setPending(dir, len(ch))
	}
	qm.mu.Unlock()
	return qm.tracker.snapshot()
}

// Kill cancels the currently-running CLI in that project queue. No effect
// if nothing is running there.
func (qm *QueueManager) Kill(projectDir string) bool {
	return qm.tracker.kill(projectDir)
}

// SetPaused toggles pause on a project queue. Pending items stay queued;
// the worker just waits on the pause channel before pulling the next one.
func (qm *QueueManager) SetPaused(projectDir string, paused bool) {
	qm.tracker.setPaused(projectDir, paused)
}

// Submit enqueues an ExecuteRequest for its project directory and blocks until
// the result is available. Returns an error if the project queue is full.
func (qm *QueueManager) Submit(req ExecuteRequest) (string, error) {
	ch := qm.getOrCreateQueue(req.ProjectDir)

	item := &QueueItem{
		Request:  req,
		Response: make(chan QueueResult, 1),
	}

	select {
	case ch <- item:
		// enqueued successfully
	default:
		return "", fmt.Errorf("queue full for project: %s", req.ProjectDir)
	}

	result := <-item.Response
	return result.Content, result.Error
}

// getOrCreateQueue returns the channel for the given project directory,
// creating one (and its worker goroutine) if it does not yet exist.
func (qm *QueueManager) getOrCreateQueue(projectDir string) chan *QueueItem {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	ch, exists := qm.queues[projectDir]
	if exists {
		return ch
	}

	ch = make(chan *QueueItem, 5)
	qm.queues[projectDir] = ch

	go qm.worker(projectDir, ch)

	qm.log.Info("queue worker started", "projectDir", projectDir)
	return ch
}

// worker processes items sequentially for a single project directory.
func (qm *QueueManager) worker(projectDir string, ch chan *QueueItem) {
	for item := range ch {
		// If the operator paused this queue, block until they unpause. New
		// items keep arriving on ch in the meantime — they just wait.
		qm.tracker.waitIfPaused(projectDir)
		qm.tracker.setPending(projectDir, len(ch))

		// Save the user message before executing.
		if err := SaveMessage(qm.db, item.Request.ConversationID, "user", item.Request.Prompt, "api", nil); err != nil {
			qm.log.Error("failed to save user message", "error", err, "conversationId", item.Request.ConversationID)
		}

		// Hand the tracker a cancel hook so the Loop Operator can kill this
		// specific CLI run. Context is cancelled either when Execute
		// returns normally (we call cancel() via defer) or when Kill fires.
		ctx, cancel := context.WithCancel(context.Background())
		qm.tracker.start(projectDir, item.Request.ConversationID, cancel)

		content, err := qm.executor.ExecuteContext(ctx, item.Request)

		qm.tracker.finish(projectDir)
		cancel()

		// Save the assistant response (even on error, partial content may be useful).
		if content != "" {
			var meta map[string]any
			if item.Request.Agent != "" {
				meta = map[string]any{"agent": string(item.Request.Agent)}
			}
			if saveErr := SaveMessage(qm.db, item.Request.ConversationID, "assistant", content, "claude-cli", meta); saveErr != nil {
				qm.log.Error("failed to save assistant message", "error", saveErr, "conversationId", item.Request.ConversationID)
			}
		}

		// #2 — pattern extraction: only on clean success, and only when the
		// request targeted a specific persona. This gives each agent an
		// append-only running log of "what I was asked + what I answered"
		// without needing a second LLM pass.
		if err == nil && content != "" {
			appendAgentMemory(qm.log, item.Request, content)
		}

		item.Response <- QueueResult{Content: content, Error: err}
	}
}
