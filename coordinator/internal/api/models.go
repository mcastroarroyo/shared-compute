package api

import (
	"net/http"
	"time"
)

type modelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type modelList struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

// handleModels reports the union of models advertised by connected providers. From M3 this
// is replaced by the signed registry manifest.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	list := modelList{Object: "list"}
	for _, id := range s.reg.Models() {
		list.Data = append(list.Data, modelObject{
			ID: id, Object: "model", Created: now, OwnedBy: "shared-compute",
		})
	}
	writeJSON(w, http.StatusOK, list)
}
