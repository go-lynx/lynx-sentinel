package sentinel

import (
	"context"
	"fmt"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/alibaba/sentinel-golang/core/system"
	"github.com/go-lynx/lynx"
	"github.com/go-lynx/lynx/log"
)

// IsContextAware asserts that the plugin's lifecycle genuinely observes context
// cancellation: the core BasePlugin drives StartContext/StopContext and routes
// into the context-aware step hooks below.
func (s *PlugSentinel) IsContextAware() bool {
	return true
}

// StartupTasksContext is the context-aware startup hook. The core BasePlugin
// drives the lifecycle state machine (status transitions, events, health check)
// and passes the caller's context straight through so cancellation is real.
func (s *PlugSentinel) StartupTasksContext(ctx context.Context) error {
	return s.startupTasksContext(ctx)
}

// CleanupTasksContext is the context-aware cleanup hook driven by the core BasePlugin.
func (s *PlugSentinel) CleanupTasksContext(ctx context.Context) error {
	return s.cleanupTasksContext(ctx)
}

// startupTasksContext loads flow-control, circuit-breaker, and system-protection
// rules, then starts the metrics loop and optional dashboard server. ctx is
// checked at every phase boundary; if cancellation is observed after background
// goroutines were launched they are stopped again before returning, so a
// cancelled startup never leaks workers.
func (s *PlugSentinel) startupTasksContext(ctx context.Context) (startErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sentinel startup canceled before execution: %w", err)
	}
	if !s.isInitialized {
		return fmt.Errorf("sentinel plugin not initialized")
	}
	s.resetStopChannel()

	// Load flow control rules
	if err := s.loadFlowRules(); err != nil {
		return fmt.Errorf("failed to load flow rules: %w", err)
	}

	// Load circuit breaker rules
	if err := s.loadCircuitBreakerRules(); err != nil {
		return fmt.Errorf("failed to load circuit breaker rules: %w", err)
	}

	// Load system protection rules
	if err := s.loadSystemRules(); err != nil {
		return fmt.Errorf("failed to load system rules: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sentinel startup canceled before starting background tasks: %w", err)
	}

	// Roll back background workers if startup does not reach the end.
	defer func() {
		if startErr == nil {
			return
		}
		s.signalStop()
		s.wg.Wait()
	}()

	// Start metrics collector
	if s.metricsCollector != nil {
		s.wg.Add(1)
		go s.metricsCollector.Start(&s.wg, s.stopCh)
	}

	// Start dashboard server
	if s.dashboardServer != nil {
		s.wg.Add(1)
		go s.dashboardServer.Start(&s.wg, s.stopCh)
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sentinel startup canceled before attaching to Lynx app: %w", err)
	}
	if app := currentLynxApp(); app != nil {
		if err := app.SetRateLimiter(s); err != nil {
			log.Warnf("failed to attach Sentinel rate limiter capability to Lynx app: %v", err)
		}
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sentinel startup canceled before publishing runtime resources: %w", err)
	}
	if s.rt != nil {
		if err := lynx.RegisterControlPlaneCapabilityResources(s.rt, pluginName, s); err != nil {
			log.Warnf("failed to register sentinel shared resource %s: %v", pluginName, err)
		}
		if err := s.rt.RegisterPrivateResource("metrics_collector", s.metricsCollector); err != nil && s.metricsCollector != nil {
			log.Warnf("failed to register sentinel private metrics resource: %v", err)
		}
		if err := s.rt.RegisterPrivateResource("dashboard_server", s.dashboardServer); err != nil && s.dashboardServer != nil {
			log.Warnf("failed to register sentinel private dashboard resource: %v", err)
		}
	}

	log.Infof("Sentinel plugin started successfully")
	return nil
}

// signalStop closes stopCh exactly once so background workers begin shutting down.
func (s *PlugSentinel) signalStop() {
	if s.stopCh == nil {
		return
	}
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

// cleanupTasksContext stops the metrics loop and dashboard server. Workers are
// always signalled to stop and rules are always cleared; the wait for workers to
// exit is bounded by ctx, and an abandoned wait is reported in the returned error.
func (s *PlugSentinel) cleanupTasksContext(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Infof("Stopping Sentinel plugin...")

	// Signal all background tasks to stop
	s.signalStop()

	// Wait for all background tasks to complete, bounded by ctx.
	var waitErr error
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done:
		default:
			waitErr = fmt.Errorf("sentinel cleanup canceled while waiting for background tasks: %w", ctx.Err())
		}
	}

	// Clear all rules
	flow.ClearRules()
	circuitbreaker.ClearRules()
	system.ClearRules()

	if waitErr != nil {
		log.Warnf("%v", waitErr)
		return waitErr
	}

	log.Infof("Sentinel plugin stopped successfully")
	return nil
}
