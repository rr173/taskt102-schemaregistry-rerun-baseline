package httpapi

import (
	"net/http"
)

func (h *Handler) Overview(w http.ResponseWriter, req *http.Request) {
	overview, err := h.r.Overview()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error(), "", 0)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}
