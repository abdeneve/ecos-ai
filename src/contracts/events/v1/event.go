// Package v1 is the versioned conversation event contract shared between
// the ingress webhook (producer) and the agent worker (consumer). See
// VERSIONING.md for the rules governing how this contract may evolve.
package v1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed schema.json
var schemaJSON []byte

var compiledSchema = mustCompileSchema()

func mustCompileSchema() *jsonschema.Schema {
	compiler := jsonschema.NewCompiler()
	const resourceName = "conversation-event.schema.json"
	if err := compiler.AddResource(resourceName, bytes.NewReader(schemaJSON)); err != nil {
		panic(fmt.Sprintf("v1: invalid embedded schema: %v", err))
	}
	schema, err := compiler.Compile(resourceName)
	if err != nil {
		panic(fmt.Sprintf("v1: failed to compile embedded schema: %v", err))
	}
	return schema
}

// ConversationEvent is the v1 shape of an event flowing from the ingress
// webhook to the agent worker over Redpanda.
type ConversationEvent struct {
	TenantID   string          `json:"tenant_id"`
	MessageID  string          `json:"message_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
	TraceID    string          `json:"trace_id"`
}

// Validate validates raw JSON bytes against the v1 schema. Use this to
// check events received from an untrusted or external source before
// decoding them into a ConversationEvent.
func Validate(raw []byte) error {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("v1: invalid JSON: %w", err)
	}
	if err := compiledSchema.Validate(doc); err != nil {
		return fmt.Errorf("v1: schema validation failed: %w", err)
	}
	return nil
}

// Encode marshals and validates the event against the v1 schema, returning
// the wire bytes ready to publish.
func (e ConversationEvent) Encode() ([]byte, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("v1: marshal event: %w", err)
	}
	if err := Validate(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Decode validates raw wire bytes against the v1 schema and unmarshals them
// into a ConversationEvent.
func Decode(raw []byte) (ConversationEvent, error) {
	if err := Validate(raw); err != nil {
		return ConversationEvent{}, err
	}
	var e ConversationEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return ConversationEvent{}, fmt.Errorf("v1: unmarshal event: %w", err)
	}
	return e, nil
}
