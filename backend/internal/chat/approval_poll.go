package chat

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"
)

// ApprovalPollTimeout is the upper bound the agent loop blocks on
// a human reviewing an approval the model just created. Past that,
// the loop reports "pending" so the user gets a response and can
// retry once the approval is acted on. 60s feels right empirically:
// long enough that a human watching the /approvals tab can act in
// time, short enough that an idle approval doesn't hang the chat.
const ApprovalPollTimeout = 60 * time.Second

// ApprovalPollInterval is how often we recheck the approval row.
// 2s is fast enough that "approve and watch the chat unblock" feels
// instant without hitting SQLite hard.
const ApprovalPollInterval = 2 * time.Second

// buildApprovalPoll returns a poller bound to the given DB. The
// returned function reads approvals.status until it leaves
// 'pending' or the timeout fires, then reports the resolution.
//
// Status values seen in practice: 'pending', 'approved',
// 'rejected', 'snoozed' (treated as pending). 'expired' may also
// appear from outside the agent loop; treated as not-resolved.
func buildApprovalPoll(db *sql.DB, log *slog.Logger) approvalPoll {
	return func(ctx context.Context, approvalID string) (string, bool) {
		if db == nil || approvalID == "" {
			return "", false
		}
		deadline := time.Now().Add(ApprovalPollTimeout)
		ticker := time.NewTicker(ApprovalPollInterval)
		defer ticker.Stop()

		// Check immediately before the first sleep — useful in tests
		// and on edge cases where the human approves before the
		// model even gets the create response.
		if status, ok := readApprovalStatus(db, approvalID); ok {
			if isResolved(status) {
				return status, true
			}
		}

		for {
			select {
			case <-ctx.Done():
				return "", false
			case <-ticker.C:
				if time.Now().After(deadline) {
					if log != nil {
						log.Info("approval poll timeout",
							"approval_id", approvalID,
							"timeout", ApprovalPollTimeout)
					}
					return "", false
				}
				status, ok := readApprovalStatus(db, approvalID)
				if !ok {
					continue
				}
				if isResolved(status) {
					if log != nil {
						log.Info("approval resolved",
							"approval_id", approvalID, "status", status)
					}
					return status, true
				}
			}
		}
	}
}

func readApprovalStatus(db *sql.DB, id string) (string, bool) {
	var status string
	err := db.QueryRow(`SELECT status FROM approvals WHERE id = ?`, id).Scan(&status)
	if err != nil {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(status)), true
}

// isResolved decides whether to break out of the poll loop. Only
// 'approved' and 'rejected' count — pending/snoozed mean a human
// hasn't decided yet, so keep waiting.
func isResolved(status string) bool {
	switch status {
	case "approved", "rejected":
		return true
	}
	return false
}
