// Package sentinel provides a Sentinel flow-control and circuit-breaker plugin for
// the go-lynx framework. It wraps alibaba/sentinel-golang and exposes flow control,
// circuit breaking, and system protection rules as first-class lynx plugin capabilities,
// including Prometheus metrics, a built-in web dashboard, and Kratos middleware hooks.
package sentinel

import (
	"context"
	"fmt"
	"time"

	"github.com/alibaba/sentinel-golang/api"
	"github.com/go-lynx/lynx"
	"github.com/go-lynx/lynx/pkg/factory"
	"github.com/go-lynx/lynx/plugins"
)

// ControlPlaneCapabilities declares Sentinel's explicit governance contract.
func (s *PlugSentinel) ControlPlaneCapabilities() []lynx.ControlPlaneCapability {
	return []lynx.ControlPlaneCapability{
		lynx.ControlPlaneCapabilityRateLimit,
		lynx.ControlPlaneCapabilityTrafficProtection,
	}
}

// EntryOption Type aliases for convenience
type EntryOption = api.EntryOption

// SentinelEntry is a type alias for a Sentinel resource entry.
type SentinelEntry = any

// init registers the Sentinel plugin with the global plugin factory
func init() {
	factory.GlobalTypedFactory().RegisterPlugin(PluginName, ConfPrefix, func() plugins.Plugin {
		return NewSentinelPlugin()
	})
}

// GetSentinel returns the global Sentinel plugin instance
func GetSentinel() (*PlugSentinel, error) {
	// Try to get from application plugin manager first
	if lynx.Lynx() != nil && lynx.Lynx().GetPluginManager() != nil {
		plugin := lynx.Lynx().GetPluginManager().GetPlugin(PluginName)
		if plugin != nil {
			if sentinelPlugin, ok := plugin.(*PlugSentinel); ok {
				return sentinelPlugin, nil
			}
			return nil, fmt.Errorf("plugin '%s' is not a Sentinel plugin", PluginName)
		}
	}

	// Fallback to factory
	plugin, err := factory.GlobalTypedFactory().CreatePlugin(PluginName)
	if err != nil {
		return nil, fmt.Errorf("Sentinel plugin not found: %w", err)
	}

	sentinelPlugin, ok := plugin.(*PlugSentinel)
	if !ok {
		return nil, fmt.Errorf("plugin is not a Sentinel plugin instance")
	}

	return sentinelPlugin, nil
}

// Entry is a convenience function for resource entry check
func Entry(resource string, opts ...EntryOption) (any, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return nil, err
	}

	return plugin.Entry(resource, opts...)
}

// EntryWithContext is a convenience function for resource entry check with context
func EntryWithContext(ctx context.Context, resource string, opts ...EntryOption) (any, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return nil, err
	}

	return plugin.EntryWithContext(ctx, resource, opts...)
}

// Execute is a convenience function for executing a function with Sentinel protection
func Execute(resource string, fn func() error, opts ...EntryOption) error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}

	return plugin.Execute(resource, fn, opts...)
}

// ExecuteWithContext is a convenience function for executing a function with Sentinel protection and context
func ExecuteWithContext(ctx context.Context, resource string, fn func(context.Context) error, opts ...EntryOption) error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}

	return plugin.ExecuteWithContext(ctx, resource, fn, opts...)
}

// CheckFlow is a convenience function for checking flow control
func CheckFlow(resource string) *FlowControlResult {
	plugin, err := GetSentinel()
	if err != nil {
		return &FlowControlResult{
			Allowed:  false,
			Resource: resource,
			Reason:   fmt.Sprintf("plugin error: %v", err),
		}
	}

	return plugin.CheckFlow(resource)
}

// GetCircuitBreakerState is a convenience function for getting circuit breaker state
func GetCircuitBreakerState(resource string) (*CircuitBreakerState, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return nil, err
	}

	return plugin.GetCircuitBreakerState(resource), nil
}

// OnConfigUpdate handles configuration updates
func (s *PlugSentinel) OnConfigUpdate(config any) error {
	// Handle configuration updates
	if sentinelConfig, ok := config.(*SentinelConfig); ok {
		s.conf = sentinelConfig
		// Reload rules if needed
		return s.reloadRules()
	}
	return fmt.Errorf("invalid config type for Sentinel plugin")
}

// IsHealthy checks if the plugin is healthy
func (s *PlugSentinel) IsHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isInitialized && s.sentinelInitialized
}

// AddFlowRule is a convenience function for adding a flow rule
func AddFlowRule(resource string, qpsLimit float64, controlBehavior string) error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}

	return plugin.AddFlowRule(resource, qpsLimit, controlBehavior)
}

// RemoveFlowRule is a convenience function for removing a flow rule
func RemoveFlowRule(resource string) error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}

	return plugin.RemoveFlowRule(resource)
}

// AddCircuitBreakerRule is a convenience function for adding a circuit breaker rule
func AddCircuitBreakerRule(resource string, strategy int32, threshold float64, minRequestAmount uint64) error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}
	return plugin.AddCircuitBreakerRule(resource, strategy, threshold, minRequestAmount)
}

// RemoveCircuitBreakerRule is a convenience function for removing a circuit breaker rule
func RemoveCircuitBreakerRule(resource string) error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}

	return plugin.RemoveCircuitBreakerRule(resource)
}

// GetMetrics is a convenience function for getting metrics summary
func GetMetrics() (map[string]any, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return nil, err
	}

	if plugin.metricsCollector == nil {
		return nil, fmt.Errorf("metrics collector not available")
	}

	return plugin.metricsCollector.GetMetricsSummary(), nil
}

// GetResourceStats is a convenience function for getting resource statistics
func GetResourceStats(resource string) (*ResourceStats, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return nil, err
	}

	if plugin.metricsCollector == nil {
		return nil, fmt.Errorf("metrics collector not available")
	}

	return plugin.metricsCollector.GetResourceStats(resource), nil
}

// GetAllResourceStats is a convenience function for getting all resource statistics
func GetAllResourceStats() (map[string]*ResourceStats, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return nil, err
	}

	if plugin.metricsCollector == nil {
		return nil, fmt.Errorf("metrics collector not available")
	}

	return plugin.metricsCollector.GetAllResourceStats(), nil
}

// GetDashboardURL is a convenience function for getting dashboard URL
func GetDashboardURL() (string, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return "", err
	}

	if plugin.dashboardServer == nil {
		return "", fmt.Errorf("dashboard server not available")
	}

	return plugin.dashboardServer.GetURL(), nil
}

// CreateHTTPMiddleware creates HTTP middleware for Sentinel protection
func CreateHTTPMiddleware(resourceExtractor func(any) string) (any, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return nil, err
	}

	return plugin.CreateHTTPMiddleware(resourceExtractor), nil
}

// CreateGRPCInterceptor creates gRPC interceptor for Sentinel protection
func CreateGRPCInterceptor() (any, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return nil, err
	}

	return plugin.CreateGRPCInterceptor(), nil
}

// ReloadRules reloads all rules from configuration
func ReloadRules() error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}

	// Trigger configuration reload
	return plugin.reloadRules()
}

// ResetMetrics resets all metrics
func ResetMetrics() error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}

	if plugin.metricsCollector == nil {
		return fmt.Errorf("metrics collector not available")
	}

	plugin.metricsCollector.Reset()
	return nil
}

// IsHealthy checks if the Sentinel plugin is healthy
func IsHealthy() (bool, error) {
	plugin, err := GetSentinel()
	if err != nil {
		return false, err
	}

	return plugin.IsHealthy(), nil
}

// GetPluginInfo returns plugin information
func GetPluginInfo() map[string]any {
	return map[string]any{
		"name":        PluginName,
		"version":     PluginVersion,
		"description": PluginDescription,
		"weight":      PluginWeight,
	}
}

// WaitForReady waits for the plugin to be ready. It polls at 100 ms intervals
// until the plugin reports healthy, the context is canceled, or the plugin stops.
func WaitForReady(ctx context.Context) error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if plugin.IsHealthy() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-plugin.stopCh:
			return fmt.Errorf("plugin stopped")
		case <-ticker.C:
		}
	}
}

// Shutdown gracefully shuts down the Sentinel plugin
func Shutdown() error {
	plugin, err := GetSentinel()
	if err != nil {
		return err
	}
	return plugin.CleanupTasks()
}
