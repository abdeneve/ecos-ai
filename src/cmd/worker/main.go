// Command worker is the agent worker: it consumes conversation events
// partitioned by tenant_id, applies state machine transitions, and
// persists session state to Redis and history to ScyllaDB. LLM and MCP
// integration are out of scope for this vertical slice (design.md
// Non-Goals) — this binary proves the ingestion-to-persistence path end to
// end without them.
package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/abdeneve/ecos-ai/src/internal/platform/cache"
	"github.com/abdeneve/ecos-ai/src/internal/platform/eventbus"
	"github.com/abdeneve/ecos-ai/src/internal/platform/storage"
	"github.com/abdeneve/ecos-ai/src/internal/telemetry"
	"github.com/abdeneve/ecos-ai/src/internal/worker"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := telemetry.NewLogger("worker")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	_, shutdownTracer, err := telemetry.InitTracer(ctx, "worker")
	if err != nil {
		logger.Error("failed to init tracer", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil {
			logger.Error("failed to shutdown tracer", "error", err)
		}
	}()

	redisAddr := getenv("REDIS_ADDR", "localhost:6379")
	scyllaHosts := []string{getenv("SCYLLA_HOST", "localhost")}
	keyspace := getenv("SCYLLA_KEYSPACE", "ecos_ai")
	brokers := []string{getenv("REDPANDA_BROKERS", "localhost:19092")}
	topic := getenv("CONVERSATION_EVENTS_TOPIC", "conversation-events")
	group := getenv("CONSUMER_GROUP", "agent-worker")

	maxConcurrent, err := strconv.Atoi(getenv("MAX_CONCURRENT_DOWNSTREAM", "32"))
	if err != nil {
		logger.Error("invalid MAX_CONCURRENT_DOWNSTREAM", "error", err)
		os.Exit(1)
	}
	lockTTLSeconds, err := strconv.Atoi(getenv("TENANT_LOCK_TTL_SECONDS", "5"))
	if err != nil {
		logger.Error("invalid TENANT_LOCK_TTL_SECONDS", "error", err)
		os.Exit(1)
	}

	sessions := cache.New(redisAddr)
	defer sessions.Close()

	history, err := storage.New(scyllaHosts, keyspace)
	if err != nil {
		logger.Error("failed to connect to scylla", "error", err)
		os.Exit(1)
	}
	defer history.Close()

	processor := worker.NewProcessor(sessions, history, maxConcurrent, time.Duration(lockTTLSeconds)*time.Second, logger)

	consumer, err := eventbus.NewConsumer(brokers, topic, group, processor.HandleMessage, logger)
	if err != nil {
		logger.Error("failed to create consumer", "error", err)
		os.Exit(1)
	}
	defer consumer.Close()

	logger.Info("worker consuming", "topic", topic, "group", group)
	if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("consumer exited with error", "error", err)
		os.Exit(1)
	}
	logger.Info("worker shutting down gracefully")
}
