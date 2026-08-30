package api

import (
	"net/http"
	"time"
)

type modelObject struct {
	ID            string `json:"id"`
	Object        string `json:"object"`
	Created       int64  `json:"created"`
	OwnedBy       string `json:"owned_by"`
	HardwareClass string `json:"hardware_class,omitempty"`
	ContextLength int    `json:"context_length,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
}

type modelList struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

// handleModels serves the signed registry catalog when configured; otherwise the union of
// models advertised by connected providers.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	list := modelList{Object: "list"}

	if s.cat != nil && s.cat.Enabled() {
		for _, m := range s.cat.Models() {
			list.Data = append(list.Data, modelObject{
				ID: m.ModelID, Object: "model", Created: now, OwnedBy: "shared-compute",
				HardwareClass: m.HardwareClass, ContextLength: m.ContextLength,
				Quantization: m.Quantization,
			})
		}
	} else {
		for _, id := range s.reg.Models() {
			list.Data = append(list.Data, modelObject{
				ID: id, Object: "model", Created: now, OwnedBy: "shared-compute",
			})
		}
	}
	writeJSON(w, http.StatusOK, list)
}
