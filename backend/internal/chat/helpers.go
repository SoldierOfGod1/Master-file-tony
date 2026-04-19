package chat

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// SaveMessage inserts a message into the messages table.
func SaveMessage(db *sql.DB, conversationID, role, content, source string, metadata map[string]any) error {
	metaJSON := "{}"
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metaJSON = string(data)
	}

	_, err := db.Exec(
		`INSERT INTO messages (conversation_id, role, content, source, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		conversationID, role, content, source, metaJSON, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}
	return nil
}

// CreateConversation inserts a new conversation row.
func CreateConversation(db *sql.DB, id, title, projectDir, source string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO conversations (id, title, project_dir, source, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'active', ?, ?)`,
		id, title, projectDir, source, now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to create conversation: %w", err)
	}
	return nil
}

// GetConversation returns a conversation with its messages.
func GetConversation(db *sql.DB, id string) (map[string]any, error) {
	row := db.QueryRow(
		`SELECT id, title, project_dir, source, status, created_at, updated_at
		 FROM conversations WHERE id = ?`, id,
	)

	var convID, title, projectDir, source, status, createdAt, updatedAt string
	if err := row.Scan(&convID, &title, &projectDir, &source, &status, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}

	rows, err := db.Query(
		`SELECT id, role, content, source, metadata, created_at
		 FROM messages WHERE conversation_id = ? ORDER BY id ASC`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []map[string]any
	for rows.Next() {
		var msgID int
		var role, content, msgSource, metaStr, msgCreatedAt string
		if err := rows.Scan(&msgID, &role, &content, &msgSource, &metaStr, &msgCreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, map[string]any{
			"id":        msgID,
			"role":      role,
			"content":   content,
			"source":    msgSource,
			"metadata":  metaStr,
			"createdAt": msgCreatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate messages: %w", err)
	}

	if messages == nil {
		messages = []map[string]any{}
	}

	return map[string]any{
		"id":         convID,
		"title":      title,
		"projectDir": projectDir,
		"source":     source,
		"status":     status,
		"createdAt":  createdAt,
		"updatedAt":  updatedAt,
		"messages":   messages,
	}, nil
}

// ListConversations returns conversations filtered by status (empty = all).
func ListConversations(db *sql.DB, status string) ([]map[string]any, error) {
	query := `SELECT id, title, project_dir, source, status, created_at, updated_at FROM conversations`
	var args []any
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY updated_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		var id, title, projectDir, source, st, createdAt, updatedAt string
		if err := rows.Scan(&id, &title, &projectDir, &source, &st, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}
		result = append(result, map[string]any{
			"id":         id,
			"title":      title,
			"projectDir": projectDir,
			"source":     source,
			"status":     st,
			"createdAt":  createdAt,
			"updatedAt":  updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate conversations: %w", err)
	}

	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

// HashPIN returns the SHA-256 hex digest of a PIN.
func HashPIN(pin string) string {
	h := sha256.Sum256([]byte(pin))
	return hex.EncodeToString(h[:])
}

// VerifyPIN checks whether pin matches the given SHA-256 hash.
func VerifyPIN(pin, hash string) bool {
	return HashPIN(pin) == hash
}
