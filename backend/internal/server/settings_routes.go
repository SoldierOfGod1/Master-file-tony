package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/SoldierOfGod1/command-centre/internal/clickup"
	"github.com/SoldierOfGod1/command-centre/internal/store"
)

// looksMasked detects the masked placeholder form "••••XXXX" so we can avoid
// overwriting a real stored secret with the mask when the user re-saves
// the form without editing it.
func looksMasked(v string) bool {
	return strings.HasPrefix(v, "••") || strings.HasPrefix(v, "\u2022\u2022")
}

// Whitelist of settings keys the UI is allowed to read/write. Anything
// outside this set is rejected so we don't accidentally leak or accept
// random keys the rest of the system also stores in the same table.
var allowedSettings = map[string]bool{
	store.SettingClickUpToken:       true,
	store.SettingClickUpWorkspaceID: true,
	store.SettingClickUpListID:      true,

	// Customer 360 — Axiom Postgres connection
	store.SettingAxiomHost:     true,
	store.SettingAxiomPort:     true,
	store.SettingAxiomDatabase: true,
	store.SettingAxiomUser:     true,
	store.SettingAxiomPassword: true,
	store.SettingAxiomSSLMode:  true,
}

// handleGetSettings returns every whitelisted setting. Secret-like values
// (api_token, password) are masked so screenshares can't leak them — the
// full value stays in the DB.
func (a *API) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := a.Store.GetAllSettings()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	out := make(map[string]any, len(allowedSettings))
	for k := range allowedSettings {
		v := all[k]
		if isSecretKey(k) && v != "" {
			if len(v) > 4 {
				v = "••••" + v[len(v)-4:]
			} else {
				v = "••••"
			}
		}
		out[k] = v
	}
	// Derived flags so the UI can render an "OK" pill without knowing
	// which keys are the required ones.
	out["clickup.configured"] = all[store.SettingClickUpToken] != "" && all[store.SettingClickUpListID] != ""
	out["axiom.configured"] = all[store.SettingAxiomHost] != "" &&
		all[store.SettingAxiomUser] != "" &&
		all[store.SettingAxiomPassword] != "" &&
		all[store.SettingAxiomDatabase] != ""
	jsonOK(w, out)
}

// isSecretKey returns true for setting keys whose values must be masked on
// read (tokens, passwords). Keep this in sync with any new secret keys
// added to allowedSettings.
func isSecretKey(key string) bool {
	return key == store.SettingClickUpToken || key == store.SettingAxiomPassword
}

// handleUpdateSettings accepts a partial JSON object of setting updates.
// Keys outside the allow-list are silently dropped. Empty values overwrite
// the stored value (so the user can clear a token).
//
// A special rule: when clickup.list_id changes we kick EnsureListStatuses
// so the new list also gets our 10-status pipeline. Runs in a goroutine so
// the HTTP response isn't blocked on a ClickUp round-trip.
func (a *API) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid json")
		return
	}

	changedListID := false
	axiomChanged := false
	for k, v := range body {
		if !allowedSettings[k] {
			continue
		}
		// Skip writes for masked secret values — re-saving the form with
		// "••••ABCD" displayed must not overwrite the real stored secret.
		if isSecretKey(k) && looksMasked(v) {
			continue
		}
		if err := a.Store.SetSetting(k, v); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if k == store.SettingClickUpListID {
			changedListID = true
		}
		if strings.HasPrefix(k, "axiom.") {
			axiomChanged = true
		}
	}

	// Post-write side effects. Run async so the client can return quickly.
	if changedListID {
		go a.ensureStatusesForCurrentList()
	}
	if axiomChanged && a.CustomerMgr != nil {
		// Drop every cached pool since we don't know which connection the
		// legacy axiom.* settings are backing. The new multi-connection
		// write path (connections_routes.go) invalidates per-id which is
		// more precise — this is the fallback for the legacy flow.
		a.CustomerMgr.InvalidateAll()
	}

	// Echo the masked view back to the client.
	a.handleGetSettings(w, r)
}

// ensureStatusesForCurrentList runs EnsureListStatuses with the freshest
// credentials from the DB. Called when the user changes the ClickUp list
// via the settings page.
func (a *API) ensureStatusesForCurrentList() {
	all, err := a.Store.GetAllSettings()
	if err != nil {
		a.Log.Warn("post-update statuses: read settings", "error", err)
		return
	}
	token := all[store.SettingClickUpToken]
	listID := all[store.SettingClickUpListID]
	if token == "" || listID == "" {
		return
	}
	client := clickup.New(token)
	if err := client.EnsureListStatuses(listID, clickup.ProjectStatuses); err != nil {
		a.Log.Warn("post-update ensure statuses", "error", err)
		return
	}
	a.Log.Info("clickup statuses pushed to new list", "list_id", listID)
}
