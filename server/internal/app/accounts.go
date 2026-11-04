package app

import (
	"database/sql"
	"errors"
	"net/http"
)

const maxAccountPasswordBytes = 128

func validAccountPassword(value string) bool {
	return len(value) >= 12 && len(value) <= maxAccountPasswordBytes
}

// Changing a password revokes all sessions including the current browser and
// Android Bearer sessions. Source tokens are intentionally not rotated here.
func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validAccountPassword(in.NewPassword) {
		failure(w, 400, "invalid_password", "New password must be 12-128 bytes")
		return
	}
	var oldHash string
	if err := a.Store.DB.QueryRowContext(r.Context(),
		"SELECT password_hash FROM users WHERE id=? AND disabled=0", p.ID).Scan(&oldHash); err != nil {
		a.err(w, err)
		return
	}
	if !verifyPassword(oldHash, in.CurrentPassword) {
		failure(w, 403, "wrong_password", "Current password is incorrect")
		return
	}
	if in.CurrentPassword == in.NewPassword {
		failure(w, 400, "unchanged_password", "New password must differ from current password")
		return
	}
	newHash, err := hashPassword(in.NewPassword)
	if err != nil {
		a.err(w, err)
		return
	}
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(),
		"UPDATE users SET password_hash=? WHERE id=? AND password_hash=? AND disabled=0",
		newHash, p.ID, oldHash)
	if err != nil {
		a.err(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		a.err(w, err)
		return
	}
	if affected != 1 {
		failure(w, 409, "credentials_changed", "Password changed concurrently; sign in again")
		return
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id=?", p.ID); err != nil {
		a.err(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	expireSessionCookie(w, a.secure)
	respond(w, 200, map[string]any{"password_changed": true, "sessions_revoked": true})
}

func expireSessionCookie(w http.ResponseWriter, secure bool) {
	cookie := sessionCookie("", secure)
	cookie.MaxAge = -1
	http.SetCookie(w, cookie)
}

// Admin password reset is explicit, requires a new 12-128 byte password, and
// invalidates all of the target user's sessions. It cannot reset oneself.
func (a *App) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		failure(w, 404, "not_found", "User not found")
		return
	}
	if id == p.ID {
		failure(w, 409, "self_reset", "Change your own password using current password")
		return
	}
	var in struct {
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validAccountPassword(in.NewPassword) {
		failure(w, 400, "invalid_password", "New password must be 12-128 bytes")
		return
	}
	newHash, err := hashPassword(in.NewPassword)
	if err != nil {
		a.err(w, err)
		return
	}
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE users SET password_hash=? WHERE id=?", newHash, id)
	if err != nil {
		a.err(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		a.err(w, err)
		return
	}
	if affected != 1 {
		failure(w, 404, "not_found", "User not found")
		return
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id=?", id); err != nil {
		a.err(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"password_reset": true, "sessions_revoked": true})
}

// Account disable immediately prevents both login and publishing with
// existing source tokens (the publish query checks users.disabled).
func (a *App) setUserDisabled(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		failure(w, 404, "not_found", "User not found")
		return
	}
	if id == p.ID {
		failure(w, 409, "self_disable", "You cannot change the enabled state of your own account")
		return
	}
	var in struct {
		Disabled *bool `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Disabled == nil {
		failure(w, 400, "invalid_user", "disabled must be true or false")
		return
	}
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	var role string
	var oldDisabled bool
	if err = tx.QueryRowContext(r.Context(),
		"SELECT role,disabled FROM users WHERE id=?", id).Scan(&role, &oldDisabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, 404, "not_found", "User not found")
			return
		}
		a.err(w, err)
		return
	}
	if *in.Disabled && !oldDisabled && role == "admin" {
		var admins int
		if err = tx.QueryRowContext(r.Context(),
			"SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0").Scan(&admins); err != nil {
			a.err(w, err)
			return
		}
		if admins <= 1 {
			failure(w, 409, "last_admin", "Cannot disable the last enabled administrator")
			return
		}
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE users SET disabled=? WHERE id=?", *in.Disabled, id); err != nil {
		a.err(w, err)
		return
	}
	// Revoke sessions on disable only; re-enabling must require a fresh login.
	if *in.Disabled {
		if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id=?", id); err != nil {
			a.err(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"id": id, "disabled": *in.Disabled})
}
