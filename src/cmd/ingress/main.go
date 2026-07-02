// Command ingress is the ultra-thin webhook edge: verify the Meta
// signature, drop duplicates, publish to Redpanda, respond — all within a
// strict latency budget (docs/flows/routing-engine.md).
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/abdeneve/ecos-ai/src/internal/ingress"
	"github.com/abdeneve/ecos-ai/src/internal/platform/cache"
	"github.com/abdeneve/ecos-ai/src/internal/platform/eventbus"
	"github.com/abdeneve/ecos-ai/src/internal/telemetry"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := telemetry.NewLogger("ingress")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	tracer, shutdownTracer, err := telemetry.InitTracer(ctx, "ingress")
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
	brokers := []string{getenv("REDPANDA_BROKERS", "localhost:19092")}
	topic := getenv("CONVERSATION_EVENTS_TOPIC", "conversation-events")
	appSecret := os.Getenv("META_APP_SECRET")
	if appSecret == "" {
		logger.Error("META_APP_SECRET must be set")
		os.Exit(1)
	}

	maxInFlight, err := strconv.Atoi(getenv("PRODUCER_MAX_IN_FLIGHT", "1000"))
	if err != nil {
		logger.Error("invalid PRODUCER_MAX_IN_FLIGHT", "error", err)
		os.Exit(1)
	}

	cacheClient := cache.New(redisAddr)
	defer cacheClient.Close()

	producer, err := eventbus.NewProducer(brokers, topic, maxInFlight, logger)
	if err != nil {
		logger.Error("failed to create producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	handler := ingress.NewHandler(ingress.Config{
		AppSecret:      appSecret,
		IdempotencyTTL: 24 * time.Hour,
		MaxBodyBytes:   1 << 20, // 1 MiB
	}, cacheClient, producer, tracer, logger)

	mux := http.NewServeMux()
	mux.Handle("/webhook", handler)

	addr := ":" + getenv("PORT", "8080")
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("ingress listening", "addr", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
