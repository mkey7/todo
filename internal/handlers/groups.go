package handlers

import (
	"net/http"
	"strconv"

	"todo/internal/models"
	"todo/internal/store"
)

func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	gs, err := store.ListGroups(h.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if gs == nil {
		gs = []models.Group{}
	}
	writeJSON(w, http.StatusOK, gs)
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var g models.Group
	if err := decodeJSON(r, &g); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	created, err := store.CreateGroup(h.DB, g)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var g models.Group
	if err := decodeJSON(r, &g); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	updated, err := store.UpdateGroup(h.DB, id, g)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := store.DeleteGroup(h.DB, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
