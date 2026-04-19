package chat

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// resolveWorktree returns a directory to run the CLI in, isolated per
// persona. Without an agent (orchestrator/default) we use projectDir
// as-is. With an agent we maintain a sibling detached worktree under
// `<projectDir>/.worktrees/<agent>` so concurrent /ask runs don't step
// on each other.
//
// Returns the worktree path and a teardown hint. Worktrees are long-lived
// — we don't remove them between runs, so the teardown is just a no-op.
type worktreeResolver struct {
	mu sync.Mutex
}

var defaultWorktreeResolver = &worktreeResolver{}

func resolveWorktree(ctx context.Context, log *slog.Logger, projectDir string, agent AgentSlug) string {
	if agent == "" || projectDir == "" {
		return projectDir
	}
	return defaultWorktreeResolver.ensure(ctx, log, projectDir, string(agent))
}

func (r *worktreeResolver) ensure(ctx context.Context, log *slog.Logger, projectDir, agent string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Not a git repo? Fall back to the plain project dir — feature is best-effort.
	if !isGitRepo(projectDir) {
		return projectDir
	}

	target := filepath.Join(projectDir, ".worktrees", agent)
	if stat, err := os.Stat(target); err == nil && stat.IsDir() {
		return target
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		log.Warn("worktree parent mkdir failed — using project dir",
			"agent", agent, "error", err)
		return projectDir
	}

	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", target, "HEAD")
	cmd.Dir = projectDir
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		log.Warn("git worktree add failed — using project dir",
			"agent", agent,
			"target", target,
			"error", fmt.Errorf("%w: %s", err, errBuf.String()),
		)
		return projectDir
	}

	log.Info("created persona worktree", "agent", agent, "path", target)
	return target
}

func isGitRepo(dir string) bool {
	// Fast path: presence of .git (file or dir). Handles both plain repos
	// and worktrees (where .git is a file pointing at the real dir).
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	return false
}
