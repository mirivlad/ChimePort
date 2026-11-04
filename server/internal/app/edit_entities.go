package app

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// Only the human-readable label changes: slugs and token permissions stay stable.
func (a *App) editChannel(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		failure(w, 404, "not_found", "Channel not found")
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || len(in.Description) > 1000 {
		failure(w, 400, "invalid_channel", "Invalid channel name or description")
		return
	}
	result, err := a.Store.DB.ExecContext(r.Context(),
		"UPDATE channels SET name=?, description=? WHERE id=? AND owner_id=?",
		in.Name, in.Description, id, p.ID)
	if err != nil {
		a.err(w, err)
		return
	}
	rows, err := result.RowsAffected()
	if err != nil {
		a.err(w, err)
		return
	}
	if rows != 1 {
		failure(w, 404, "not_found", "Channel not found")
		return
	}
	respond(w, 200, map[string]any{"id": id, "name": in.Name, "description": in.Description})
}

func (a *App) editSource(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		failure(w, 404, "not_found", "Source not found")
		return
	}
	var in struct {
		Name string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || len(in.Description) > 1000 {
		failure(w, 400, "invalid_source", "Invalid source name or description")
		return
	}
	result, err := a.Store.DB.ExecContext(r.Context(),
		"UPDATE sources SET name=?, description=? WHERE id=? AND owner_id=?",
		in.Name, in.Description, id, p.ID)
	if err != nil {
		a.err(w, err)
		return
	}
	rows, err := result.RowsAffected()
	if err != nil {
		a.err(w, err)
		return
	}
	if rows != 1 {
		failure(w, 404, "not_found", "Source not found")
		return
	}
	respond(w, 200, map[string]any{"id": id, "name": in.Name, "description": in.Description})
}
