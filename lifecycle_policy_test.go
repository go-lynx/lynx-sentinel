package sentinel

import (
	"context"
	"testing"
	"time"

	"github.com/go-lynx/lynx/pkg/security"
	"github.com/go-lynx/lynx/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The production lifecycle policy (lynx/internal/app/lifecycle_policy.go) rejects
// any plugin for which plugins.HasTrueContextLifecycle is false when
// security.IsProduction() is true. These tests pin the plugin to that contract.
func TestPlugSentinel_HasTrueContextLifecycle(t *testing.T) {
	p := NewSentinelPlugin()

	caps := plugins.DescribePluginCapabilities(p)
	assert.True(t, caps.HasLifecycleWithCtx, "plugin must expose StartContext/StopContext/InitializeContext")
	assert.True(t, caps.HasContextSteps, "plugin must implement a context-aware step hook")
	assert.True(t, caps.IsTrulyContextAware)
	assert.True(t, plugins.HasTrueContextLifecycle(p))

	_, ok := plugins.GetTrueContextLifecycle(p)
	assert.True(t, ok)

	var _ plugins.ContextStartupTasker = p
	var _ plugins.ContextCleanupTasker = p
}

func TestPlugSentinel_ProductionLifecyclePolicyAccepts(t *testing.T) {
	t.Setenv("LYNX_ENV", "production")
	require.True(t, security.IsProduction())

	p := NewSentinelPlugin()
	assert.True(t, plugins.HasTrueContextLifecycle(p),
		"plugin %s would be rejected by the production lifecycle policy", p.Name())
}

func TestPlugSentinel_StartupTasksContext_ObservesCancellation(t *testing.T) {
	p := NewSentinelPlugin()
	p.conf = &SentinelConfig{AppName: "cancel-test"}
	p.isInitialized = true
	p.sentinelInitialized = true
	// A collector that would otherwise be started; cancellation must not leak it.
	p.metricsCollector = NewMetricsCollector(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := p.StartupTasksContext(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), time.Second, "cancelled startup must return promptly")
}

func TestPlugSentinel_CleanupTasksContext_BoundedByContext(t *testing.T) {
	p := NewSentinelPlugin()
	p.resetStopChannel()

	// A worker that ignores stopCh for a while: cleanup must not block past ctx.
	release := make(chan struct{})
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		<-release
	}()
	t.Cleanup(func() { close(release); p.wg.Wait() })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.CleanupTasksContext(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second)
	assert.True(t, isStopChannelClosed(p.stopCh), "workers must still be signalled to stop")
}
