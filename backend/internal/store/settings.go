package store

import (
	"database/sql"
	"fmt"
)

// GetSetting reads a key from app_settings. Returns "" (not an error) if
// the key is absent so callers can treat absence and empty the same way.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return v, nil
}

// SetSetting upserts a key/value pair into app_settings. Also bumps
// updated_at so the UI can show "last changed" if it wants.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value,
		                               updated_at = datetime('now')
	`, key, value)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

// GetAllSettings returns every setting as a map. Missing keys are omitted.
// Useful for one-shot frontend dumps on the Settings page.
func (s *Store) GetAllSettings() (map[string]string, error) {
	rows, err := s.DB.Query(`SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Setting keys — centralised so every caller uses the same strings.
const (
	SettingClickUpToken       = "clickup.api_token"
	SettingClickUpWorkspaceID = "clickup.workspace_id"
	SettingClickUpListID      = "clickup.list_id"

	// Axiom Postgres connection settings used by the Customer 360 tab.
	// Stored in app_settings so the user can rotate creds via the UI.
	SettingAxiomHost     = "axiom.host"
	SettingAxiomPort     = "axiom.port"
	SettingAxiomDatabase = "axiom.database"
	SettingAxiomUser     = "axiom.user"
	SettingAxiomPassword = "axiom.password"
	SettingAxiomSSLMode  = "axiom.ssl_mode"

	// AWS Athena settings for the Customer 360 CDR usage panel.
	// Stored in app_settings so the user can flip region / S3 output
	// without a restart and without touching env vars.
	SettingAthenaEnabled         = "athena.enabled"           // "true" | "false"
	SettingAthenaRegion          = "athena.region"            // e.g. eu-west-1
	SettingAthenaDatabase        = "athena.database"          // usage
	SettingAthenaWorkgroup       = "athena.workgroup"         // optional, e.g. primary
	SettingAthenaOutputS3        = "athena.output_s3"         // REQUIRED: s3://bucket/prefix/
	SettingAthenaAccessKeyID     = "athena.aws_access_key_id"     // optional; AWS chain fallback
	SettingAthenaSecretAccessKey = "athena.aws_secret_access_key" // optional
	SettingAthenaSessionToken    = "athena.aws_session_token"    // required when using ASIA temporary creds
)
