package api

import (
	"net/http"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/council"
)

// Read-only Ayni Council observatory (docs/AYNI-COUNCIL-EXPERIENCE.md). Public,
// no auth, no side effects. Every payload is labelled demonstration data until a
// signed roster and live Council providers exist. These endpoints never move
// money, deploy code, or command nodes.

func (s *Server) mountCouncil(mux *http.ServeMux) {
	mux.Handle("GET /v1/council/roster", instrument("v1_council_roster", http.HandlerFunc(s.councilRoster)))
	mux.Handle("GET /v1/council/constitution", instrument("v1_council_constitution", http.HandlerFunc(s.councilConstitution)))
	mux.Handle("GET /v1/council/meetings", instrument("v1_council_meetings", http.HandlerFunc(s.councilMeetings)))
	mux.Handle("GET /v1/council/meetings/{id}", instrument("v1_council_meeting", http.HandlerFunc(s.councilMeeting)))
	mux.Handle("GET /v1/council/decisions", instrument("v1_council_decisions", http.HandlerFunc(s.councilDecisions)))
	mux.Handle("GET /v1/council/decisions/{id}", instrument("v1_council_decision", http.HandlerFunc(s.councilDecision)))
}

func (s *Server) councilRoster(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, council.Roster())
}

func (s *Server) councilConstitution(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, council.Constitution())
}

func (s *Server) councilMeetings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, council.Meetings())
}

func (s *Server) councilMeeting(w http.ResponseWriter, r *http.Request) {
	if m, ok := council.Meeting(r.PathValue("id")); ok {
		writeJSON(w, http.StatusOK, m)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "no such meeting")
}

func (s *Server) councilDecisions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, council.Decisions())
}

func (s *Server) councilDecision(w http.ResponseWriter, r *http.Request) {
	if d, ok := council.Decision(r.PathValue("id")); ok {
		writeJSON(w, http.StatusOK, d)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "no such decision")
}
