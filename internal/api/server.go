package api

import (
	"net/http"
	"strings"

	"paperqual/internal/application"
)

type Server struct {
	service *application.Service
	mux     *http.ServeMux
}

func NewServer(service *application.Service) *Server {
	s := &Server{service: service, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /ready", s.HandleReady)
	s.mux.HandleFunc("POST /api/v1/batches", s.HandleCreateBatch)
	s.mux.HandleFunc("GET /api/v1/batches/{batch_id}", s.HandleGetBatch)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/items", s.HandleRegisterItem)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/items/batch", s.HandleRegisterItems)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/baseline", s.HandleFreezeBaseline)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/treatment-rounds", s.HandleTreatmentRound)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/treatment-rounds/preflight", s.HandleTreatmentRoundPreflight)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/corrections", s.HandleCorrection)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/corrections/batch", s.HandleCorrections)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/review", s.HandleStartReview)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/review/decision", s.HandleSubmitReview)
	s.mux.HandleFunc("POST /api/v1/batches/{batch_id}/decision", s.HandleFinalDecision)
	s.mux.HandleFunc("GET /api/v1/batches/{batch_id}/timeline", s.HandleTimeline)
	s.mux.HandleFunc("GET /api/v1/batches/{batch_id}/certificate", s.HandleCertificate)
	s.mux.HandleFunc("GET /api/v1/batches/{batch_id}/certificate/verify", s.HandleVerifyCertificate)
	s.mux.HandleFunc("/", s.HandleNotFound)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if accept := r.Header.Get("Accept"); accept != "" && !strings.Contains(accept, "application/json") && !strings.Contains(accept, "*/*") {
		writeError(w, http.StatusNotAcceptable, "not_acceptable", "仅支持 application/json", 0)
		return
	}
	s.mux.ServeHTTP(w, r)
}
