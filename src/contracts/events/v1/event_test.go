package v1

import (
	"testing"
	"time"
)

func validEvent() ConversationEvent {
	return ConversationEvent{
		TenantID:   "tenant-123",
		MessageID:  "msg-abc",
		OccurredAt: time.Now().UTC(),
		Payload:    []byte(`{"body":"hello"}`),
		TraceID:    "trace-xyz",
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	original := validEvent()

	raw, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode() unexpected error: %v", err)
	}

	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() unexpected error: %v", err)
	}

	if decoded.TenantID != original.TenantID || decoded.MessageID != original.MessageID || decoded.TraceID != original.TraceID {
		t.Fatalf("decoded event does not match original: got %+v, want %+v", decoded, original)
	}
}

func TestEncodeMissingRequiredField(t *testing.T) {
	e := validEvent()
	e.TenantID = ""

	if _, err := e.Encode(); err == nil {
		t.Fatal("expected error for missing tenant_id, got nil")
	}
}

func TestDecodeRejectsMalformedJSON(t *testing.T) {
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestDecodeRejectsMissingTraceID(t *testing.T) {
	raw := []byte(`{
		"tenant_id": "tenant-123",
		"message_id": "msg-abc",
		"occurred_at": "2026-07-02T10:00:00Z",
		"payload": {"body":"hello"}
	}`)

	if _, err := Decode(raw); err == nil {
		t.Fatal("expected error for missing trace_id, got nil")
	}
}
