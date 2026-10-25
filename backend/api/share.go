package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"ft_lgtm/backend/ipfs"
	"ft_lgtm/backend/telemetry"
)

var shareIDPattern = regexp.MustCompile(`^[a-z2-7]{8}([a-z2-7]{8})?$`)

type SharedCodeReader interface {
	SharedCode(ctx context.Context, id string) (string, string, error)
}

type ShareResponse struct {
	Code     string `json:"code"`
	IPFSLink string `json:"ipfs_link"`
}

type shareHandler struct {
	reader     SharedCodeReader
	gatewayURL string
}

func NewShareHandler(reader SharedCodeReader, gatewayURL string) http.Handler {
	if reader == nil {
		panic("api: NewShareHandler called with nil reader")
	}
	h := &shareHandler{reader: reader, gatewayURL: strings.TrimRight(gatewayURL, "/")}
	return otelhttp.NewHandler(h, "/api/share/{id}", publicEndpoint,
		otelhttp.WithSpanNameFormatter(func(operation string, _ *http.Request) string { return operation }))
}

func (h *shareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !shareIDPattern.MatchString(id) {
		http.Error(w, "invalid share id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("share.id", id))
	logger := telemetry.Logger(instrumentationName)

	lookupCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()

	cid, code, err := h.reader.SharedCode(lookupCtx, id)
	if errors.Is(err, ipfs.ErrShareNotFound) {
		logger.InfoContext(ctx, "share link not found", "share.id", id)
		http.Error(w, "share not found", http.StatusNotFound)
		return
	}
	if err != nil {
		span.SetStatus(codes.Error, "loading shared code failed")
		logger.ErrorContext(ctx, "loading shared code failed", "share.id", id, "error", err)
		http.Error(w, "could not load shared code", http.StatusInternalServerError)
		return
	}

	span.SetAttributes(attribute.String("code.cid", cid))
	logger.InfoContext(ctx, "share link opened", "share.id", id, "code.cid", cid)
	writeJSON(w, http.StatusOK, ShareResponse{Code: code, IPFSLink: h.gatewayURL + "/ipfs/" + cid})
}
