package sentinel

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/config"
	kratosmiddleware "github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/go-lynx/lynx"
	"github.com/go-lynx/lynx/plugins"
)

var (
	sentinelTestInitOnce sync.Once
	sentinelTestInitErr  error
)

type sentinelTestConfigSource struct {
	kv *config.KeyValue
}

type sentinelTestConfigWatcher struct {
	stop chan struct{}
}

type sentinelTestHeader map[string][]string

type sentinelTestTransport struct {
	kind        transport.Kind
	endpoint    string
	operation   string
	reqHeader   sentinelTestHeader
	replyHeader sentinelTestHeader
}

func (s *sentinelTestConfigSource) Load() ([]*config.KeyValue, error) {
	return []*config.KeyValue{s.kv}, nil
}

func (s *sentinelTestConfigSource) Watch() (config.Watcher, error) {
	return &sentinelTestConfigWatcher{stop: make(chan struct{})}, nil
}

func (w *sentinelTestConfigWatcher) Next() ([]*config.KeyValue, error) {
	<-w.stop
	return nil, context.Canceled
}

func (w *sentinelTestConfigWatcher) Stop() error {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	return nil
}

func (h sentinelTestHeader) Get(key string) string {
	values := h.Values(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (h sentinelTestHeader) Set(key string, value string) {
	for existingKey := range h {
		if strings.EqualFold(existingKey, key) {
			delete(h, existingKey)
		}
	}
	h[key] = []string{value}
}

func (h sentinelTestHeader) Add(key string, value string) {
	for existingKey := range h {
		if strings.EqualFold(existingKey, key) {
			h[existingKey] = append(h[existingKey], value)
			return
		}
	}
	h[key] = []string{value}
}

func (h sentinelTestHeader) Keys() []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	return keys
}

func (h sentinelTestHeader) Values(key string) []string {
	for existingKey, values := range h {
		if strings.EqualFold(existingKey, key) {
			return append([]string(nil), values...)
		}
	}
	return nil
}

func (t *sentinelTestTransport) Kind() transport.Kind {
	return t.kind
}

func (t *sentinelTestTransport) Endpoint() string {
	return t.endpoint
}

func (t *sentinelTestTransport) Operation() string {
	return t.operation
}

func (t *sentinelTestTransport) RequestHeader() transport.Header {
	return t.reqHeader
}

func (t *sentinelTestTransport) ReplyHeader() transport.Header {
	return t.replyHeader
}

func ensureSentinelCoreForTests(t *testing.T) {
	t.Helper()

	sentinelTestInitOnce.Do(func() {
		logDir, err := os.MkdirTemp("", "lynx-sentinel-test-*")
		if err != nil {
			sentinelTestInitErr = err
			return
		}

		plugin := NewSentinelPlugin()
		plugin.conf = &SentinelConfig{
			AppName: "sentinel-contract-test",
			LogDir:  logDir,
		}
		if err := plugin.validateAndSetDefaults(); err != nil {
			sentinelTestInitErr = err
			return
		}
		sentinelTestInitErr = plugin.initializeSentinelCore()
	})

	if sentinelTestInitErr != nil {
		t.Fatalf("initialize sentinel core: %v", sentinelTestInitErr)
	}
}

func newSentinelTestConfig(t *testing.T, appName string) config.Config {
	t.Helper()

	cfg := config.New(config.WithSource(&sentinelTestConfigSource{
		kv: &config.KeyValue{
			Key:    appName + ".yaml",
			Format: "yaml",
			Value: []byte("lynx:\n" +
				"  application:\n" +
				"    name: " + appName + "\n" +
				"    version: v0.0.1\n"),
		},
	}))
	if err := cfg.Load(); err != nil {
		t.Fatalf("load sentinel test config: %v", err)
	}
	t.Cleanup(func() {
		_ = cfg.Close()
	})
	return cfg
}

func newSentinelMiddlewareHarness(t *testing.T) *PlugSentinel {
	t.Helper()
	ensureSentinelCoreForTests(t)

	plugin := NewSentinelPlugin()
	plugin.conf = &SentinelConfig{AppName: "sentinel-middleware-test"}
	if err := plugin.validateAndSetDefaults(); err != nil {
		t.Fatalf("validate sentinel config: %v", err)
	}
	plugin.isInitialized = true
	plugin.sentinelInitialized = true
	plugin.metricsCollector = NewMetricsCollector(time.Second)
	return plugin
}

func executeMiddleware(t *testing.T, mw kratosmiddleware.Middleware, ctx context.Context) {
	t.Helper()

	handlerCalled := false
	next := mw(func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "ok", nil
	})

	reply, err := next(ctx, nil)
	if err != nil {
		t.Fatalf("middleware execution failed: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("middleware reply = %#v, want ok", reply)
	}
	if !handlerCalled {
		t.Fatal("expected middleware to call downstream handler")
	}
}

func TestStartupTasks_AttachesRateLimiterAndRegistersCapabilityResources(t *testing.T) {
	ensureSentinelCoreForTests(t)
	lynx.ClearDefaultApp()
	t.Cleanup(lynx.ClearDefaultApp)

	cfg := newSentinelTestConfig(t, "sentinel-startup-test")
	app, err := lynx.NewStandaloneApp(cfg)
	if err != nil {
		t.Fatalf("create standalone app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Close()
	})
	lynx.SetDefaultApp(app)

	rt := plugins.NewSimpleRuntime()
	rt.SetConfig(cfg)

	plugin := NewSentinelPlugin()
	plugin.conf = &SentinelConfig{AppName: "sentinel-startup-test"}
	if err := plugin.validateAndSetDefaults(); err != nil {
		t.Fatalf("validate sentinel config: %v", err)
	}
	plugin.rt = rt
	plugin.isInitialized = true
	plugin.sentinelInitialized = true
	t.Cleanup(func() {
		_ = plugin.CleanupTasks()
	})

	if err := plugin.StartupTasks(); err != nil {
		t.Fatalf("startup sentinel plugin: %v", err)
	}

	controlPlane := app.GetControlPlane()
	if controlPlane == nil {
		t.Fatal("expected default app control plane to be available after sentinel startup")
	}
	if controlPlane.HTTPRateLimit() == nil {
		t.Fatal("expected HTTP rate limiter capability to be attached to app control plane")
	}
	if controlPlane.GRPCRateLimit() == nil {
		t.Fatal("expected gRPC rate limiter capability to be attached to app control plane")
	}

	rateLimitAlias, err := rt.GetSharedResource(lynx.ControlPlaneCapabilityResourceName(PluginName, lynx.ControlPlaneCapabilityRateLimit))
	if err != nil {
		t.Fatalf("resolve rate_limit alias: %v", err)
	}
	if _, ok := rateLimitAlias.(lynx.RateLimiter); !ok {
		t.Fatalf("rate_limit alias type = %T, want lynx.RateLimiter", rateLimitAlias)
	}

	trafficProtectionAlias, err := rt.GetSharedResource(lynx.ControlPlaneCapabilityResourceName(PluginName, lynx.ControlPlaneCapabilityTrafficProtection))
	if err != nil {
		t.Fatalf("resolve traffic_protection alias: %v", err)
	}
	if _, ok := trafficProtectionAlias.(*PlugSentinel); !ok {
		t.Fatalf("traffic_protection alias type = %T, want *PlugSentinel", trafficProtectionAlias)
	}
}

func TestHTTPRateLimit_UsesTransportOperation(t *testing.T) {
	plugin := newSentinelMiddlewareHarness(t)
	mw := plugin.HTTPRateLimit()
	ctx := transport.NewServerContext(context.Background(), &sentinelTestTransport{
		kind:        transport.KindHTTP,
		endpoint:    "http://127.0.0.1:8080",
		operation:   "/demo.http.v1.Service/Get",
		reqHeader:   sentinelTestHeader{},
		replyHeader: sentinelTestHeader{},
	})

	executeMiddleware(t, mw, ctx)

	if got := plugin.metricsCollector.passedCounter["/demo.http.v1.Service/Get"]; got != 1 {
		t.Fatalf("HTTP operation passed count = %d, want 1", got)
	}
}

func TestGRPCRateLimit_UsesTransportOperation(t *testing.T) {
	plugin := newSentinelMiddlewareHarness(t)
	mw := plugin.GRPCRateLimit()
	ctx := transport.NewServerContext(context.Background(), &sentinelTestTransport{
		kind:        transport.KindGRPC,
		endpoint:    "grpc://127.0.0.1:9090",
		operation:   "/demo.grpc.v1.Service/Get",
		reqHeader:   sentinelTestHeader{},
		replyHeader: sentinelTestHeader{},
	})

	executeMiddleware(t, mw, ctx)

	if got := plugin.metricsCollector.passedCounter["/demo.grpc.v1.Service/Get"]; got != 1 {
		t.Fatalf("gRPC operation passed count = %d, want 1", got)
	}
}
