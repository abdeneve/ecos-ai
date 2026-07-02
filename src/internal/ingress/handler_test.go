package ingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abdeneve/ecos-ai/src/internal/platform/eventbus"
	"go.opentelemetry.io/otel/trace"
)

const testSecret = "test-app-secret"

type fakeChecker struct {
	seen map[string]bool
}

func newFakeChecker() *fakeChecker { return &fakeChecker{seen: map[string]bool{}} }

func (f *fakeChecker) SeenBefore(_ context.Context, messageID string, _ time.Duration) (bool, error) {
	if f.seen[messageID] {
		return true, nil
	}
	f.seen[messageID] = true
	return false, nil
}

type fakePublisher struct {
	published  int
	saturated  bool
	err        error
}

func (f *fakePublisher) Produce(_ context.Context, _, _ []byte) error {
	if f.saturated {
		return eventbus.ErrProducerSaturated
	}
	if f.err != nil {
		return f.err
	}
	f.published++
	return nil
}

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newTestHandler(checker IdempotencyChecker, publisher Publisher) *Handler {
	cfg := Config{
		AppSecret:      testSecret,
		IdempotencyTTL: 24 * time.Hour,
		MaxBodyBytes:   1 << 20,
	}
	tracer := trace.NewNoopTracerProvider().Tracer("test")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHandler(cfg, checker, publisher, tracer, logger)
}

func messageBody(phoneNumberID, messageID string) []byte {
	return []byte(fmt.Sprintf(`{
		"entry": [{
			"changes": [{
				"value": {
					"metadata": {"phone_number_id": %q},
					"messages": [{"id": %q, "type": "text", "text": {"body": "hello"}}]
				}
			}]
		}]
	}`, phoneNumberID, messageID))
}

func doRequest(t *testing.T, h *Handler, body []byte, signature string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestServeHTTP_ValidMessagePublishes(t *testing.T) {
	body := messageBody("tenant-1", "wamid.1")
	pub := &fakePublisher{}
	h := newTestHandler(newFakeChecker(), pub)

	rec := doRequest(t, h, body, sign(body, testSecret))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if pub.published != 1 {
		t.Fatalf("published = %d, want 1", pub.published)
	}
}

func TestServeHTTP_InvalidSignatureRejected(t *testing.T) {
	body := messageBody("tenant-1", "wamid.1")
	pub := &fakePublisher{}
	h := newTestHandler(newFakeChecker(), pub)

	rec := doRequest(t, h, body, "sha256="+strings.Repeat("00", 32))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if pub.published != 0 {
		t.Fatalf("published = %d, want 0 for rejected signature", pub.published)
	}
}

func TestServeHTTP_MissingSignatureRejected(t *testing.T) {
	body := messageBody("tenant-1", "wamid.1")
	h := newTestHandler(newFakeChecker(), &fakePublisher{})

	rec := doRequest(t, h, body, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestServeHTTP_DuplicateMessageNotRepublished(t *testing.T) {
	body := messageBody("tenant-1", "wamid.dup")
	pub := &fakePublisher{}
	h := newTestHandler(newFakeChecker(), pub)
	sig := sign(body, testSecret)

	first := doRequest(t, h, body, sig)
	if first.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", first.Code)
	}

	second := doRequest(t, h, body, sig)
	if second.Code != http.StatusOK {
		t.Fatalf("duplicate request status = %d, want 200 (still acked)", second.Code)
	}

	if pub.published != 1 {
		t.Fatalf("published = %d, want exactly 1 (duplicate must not republish)", pub.published)
	}
}

func TestServeHTTP_NonMessageEventAckedWithoutPublish(t *testing.T) {
	body := []byte(`{"entry": [{"changes": [{"value": {"metadata": {"phone_number_id": "tenant-1"}, "messages": []}}]}]}`)
	pub := &fakePublisher{}
	h := newTestHandler(newFakeChecker(), pub)

	rec := doRequest(t, h, body, sign(body, testSecret))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if pub.published != 0 {
		t.Fatalf("published = %d, want 0 for a non-message event", pub.published)
	}
}

func TestServeHTTP_SaturatedProducerReturnsServiceUnavailable(t *testing.T) {
	body := messageBody("tenant-1", "wamid.2")
	pub := &fakePublisher{saturated: true}
	h := newTestHandler(newFakeChecker(), pub)

	rec := doRequest(t, h, body, sign(body, testSecret))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestServeHTTP_PublishErrorReturnsInternalServerError(t *testing.T) {
	body := messageBody("tenant-1", "wamid.3")
	pub := &fakePublisher{err: errors.New("boom")}
	h := newTestHandler(newFakeChecker(), pub)

	rec := doRequest(t, h, body, sign(body, testSecret))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestServeHTTP_RejectsNonPOST(t *testing.T) {
	h := newTestHandler(newFakeChecker(), &fakePublisher{})
	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestServeHTTP_LatencyBudget is the latency test from tasks.md 4.7: under
// normal conditions (fast in-memory dependencies), handling a webhook call
// should be comfortably within budget. It is not a substitute for load
// testing against real Redis/Redpanda, but it guards against accidental
// synchronous work (e.g. waiting on broker acks) creeping into the handler.
func TestServeHTTP_LatencyBudget(t *testing.T) {
	body := messageBody("tenant-1", "wamid.latency")
	h := newTestHandler(newFakeChecker(), &fakePublisher{})
	sig := sign(body, testSecret)

	const budget = 5 * time.Millisecond
	start := time.Now()
	rec := doRequest(t, h, body, sig)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if elapsed > budget {
		t.Fatalf("handler took %s, want <= %s against in-memory dependencies", elapsed, budget)
	}
}
