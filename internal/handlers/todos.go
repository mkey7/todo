package handlers

import (
	"net/http"
	"strconv"

	"todo/internal/models"
	"todo/internal/store"
)

func (h *Handler) ListTodos(w http.ResponseWriter, r *http.Request) {
	groupID := ptrInt64Param(r, "group_id")
	status := strParam(r, "status")

	ts, err := store.ListTodos(h.DB, groupID, status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ts, err = store.WithChildren(h.DB, ts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ts == nil {
		ts = []models.Todo{}
	}
	writeJSON(w, http.StatusOK, ts)
}

func (h *Handler) CreateTodo(w http.ResponseWriter, r *http.Request) {
	var t models.Todo
	if err := decodeJSON(r, &t); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	created, err := store.CreateTodo(h.DB, t)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) UpdateTodo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var t models.Todo
	if err := decodeJSON(r, &t); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	updated, err := store.UpdateTodo(h.DB, id, t)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) PatchTodoStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	updated, err := store.SetTodoStatus(h.DB, id, body.Status)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteTodo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := store.DeleteTodo(h.DB, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
