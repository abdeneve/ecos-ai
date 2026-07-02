// Package ingress implements the ultra-thin webhook edge: verify the
// caller, drop anything already seen, publish, and respond — all without
// waiting on the LLM or any other downstream processing (design.md,
// decisions 1-4).
package ingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	v1 "github.com/abdeneve/ecos-ai/src/contracts/events/v1"
	"github.com/abdeneve/ecos-ai/src/internal/platform/eventbus"
	"go.opentelemetry.io/otel/trace"
)

// IdempotencyChecker is the subset of cache.Client the handler depends on,
// so tests can substitute a fake without a real Redis.
type IdempotencyChecker interface {
	SeenBefore(ctx context.Context, messageID string, ttl time.Duration) (bool, error)
}

// Publisher is the subset of eventbus.Producer the handler depends on.
type Publisher interface {
	Produce(ctx context.Context, key, value []byte) error
}

// Config holds the handler's tunables.
type Config struct {
	AppSecret      string
	IdempotencyTTL time.Duration
	MaxBodyBytes   int64
}

// Handler is the net/http handler for the Meta WhatsApp webhook.
type Handler struct {
	cfg       Config
	checker   IdempotencyChecker
	publisher Publisher
	tracer    trace.Tracer
	logger    *slog.Logger
}

// NewHandler builds the webhook handler.
func NewHandler(cfg Config, checker IdempotencyChecker, publisher Publisher, tracer trace.Tracer, logger *slog.Logger) *Handler {
	return &Handler{cfg: cfg, checker: checker, publisher: publisher, tracer: tracer, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := h.tracer.Start(r.Context(), "ingress.webhook")
	defer span.End()
	traceID := span.SpanContext().TraceID().String()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, h.cfg.MaxBodyBytes))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !h.validSignature(r.Header.Get("X-Hub-Signature-256"), body) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	tenantID, messageID, payload, ok := extractMessage(body)
	if !ok {
		// Not a message event (e.g. a delivery-status callback) - ack
		// without publishing, there is nothing to route.
		w.WriteHeader(http.StatusOK)
		return
	}

	duplicate, err := h.checker.SeenBefore(ctx, messageID, h.cfg.IdempotencyTTL)
	if err != nil {
		h.logger.Error("ingress: idempotency check failed", "error", err, "trace_id", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if duplicate {
		w.WriteHeader(http.StatusOK)
		return
	}

	event := v1.ConversationEvent{
		TenantID:   tenantID,
		MessageID:  messageID,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
		TraceID:    traceID,
	}
	encoded, err := event.Encode()
	if err != nil {
		h.logger.Error("ingress: constructed an invalid event", "error", err, "trace_id", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := h.publisher.Produce(ctx, []byte(tenantID), encoded); err != nil {
		if errors.Is(err, eventbus.ErrProducerSaturated) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		h.logger.Error("ingress: publish failed", "error", err, "trace_id", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) validSignature(header string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.cfg.AppSecret))
	mac.Write(body)
	return hmac.Equal(sig, mac.Sum(nil))
}

// whatsAppPayload is the minimal subset of Meta's WhatsApp webhook shape
// needed to route a message: which tenant it belongs to (the receiving
// phone number) and the message itself.
type whatsAppPayload struct {
	Entry []struct {
		Changes []struct {
			Value struct {
				Metadata struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Messages []json.RawMessage `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

type whatsAppMessage struct {
	ID string `json:"id"`
}

// extractMessage pulls the tenant (phone_number_id), message_id, and the
// raw message body out of a Meta webhook payload. ok is false when the
// payload doesn't contain an inbound message (e.g. a status update),
// signaling the caller to ack without publishing.
func extractMessage(body []byte) (tenantID, messageID string, payload []byte, ok bool) {
	var p whatsAppPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return "", "", nil, false
	}
	for _, entry := range p.Entry {
		for _, change := range entry.Changes {
			if len(change.Value.Messages) == 0 {
				continue
			}
			raw := change.Value.Messages[0]
			var msg whatsAppMessage
			if err := json.Unmarshal(raw, &msg); err != nil || msg.ID == "" {
				continue
			}
			return change.Value.Metadata.PhoneNumberID, msg.ID, raw, true
		}
	}
	return "", "", nil, false
}
