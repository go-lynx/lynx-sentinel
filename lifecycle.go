package sentinel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/config"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/go-lynx/lynx/log"
	"github.com/go-lynx/lynx/plugins"
)

// sentinelInitOnce guards api.InitWithConfig, which mutates process-global state
// and is not re-entrant. It ensures the global Sentinel core is initialized at
// most once across plugin (re-)initialization attempts.
var (
	sentinelInitOnce sync.Once
	sentinelInitErr  error
)

// InitializeResources implements custom initialization logic for Sentinel plugin
// Scans and loads Sentinel configuration from runtime config
func (s *PlugSentinel) InitializeResources(rt plugins.Runtime) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.BasePlugin.InitializeResources(rt); err != nil {
		return err
	}
	s.rt = rt

	// Initialize configuration structure
	s.conf = &SentinelConfig{}

	// Scan and load Sentinel configuration from runtime config
	err := rt.GetConfig().Value(confPrefix).Scan(s.conf)
	if err != nil {
		return fmt.Errorf("failed to load sentinel configuration: %w", err)
	}

	// Validate and set default configuration
	if err := s.validateAndSetDefaults(); err != nil {
		return fmt.Errorf("sentinel configuration validation failed: %w", err)
	}

	// Initialize Sentinel core
	if err := s.initializeSentinelCore(); err != nil {
		return fmt.Errorf("failed to initialize sentinel core: %w", err)
	}

	// Track whether the rest of initialization completes; on any failure after
	// core init, roll back the partially-initialized plugin state so a retry or
	// re-init does not observe inconsistent state.
	initOK := false
	defer func() {
		if !initOK {
			s.metricsCollector = nil
			s.dashboardServer = nil
			s.sentinelInitialized = false
			s.isInitialized = false
		}
	}()

	// Initialize metrics collector if enabled
	if s.conf.Metrics.Enabled {
		interval, err := time.ParseDuration(s.conf.Metrics.Interval)
		if err != nil {
			interval = 30 * time.Second // default interval
		}
		s.metricsCollector = NewMetricsCollector(interval)
	}

	// Initialize dashboard server if enabled
	if s.conf.Dashboard.Enabled {
		s.dashboardServer = NewDashboardServer(int(s.conf.Dashboard.Port), s.metricsCollector)
	}

	s.isInitialized = true
	initOK = true
	log.Infof("Sentinel plugin initialized successfully")
	return nil
}

// StartupTasks is the legacy (non-context) startup hook. It delegates to the
// context-aware implementation with a background context.
func (s *PlugSentinel) StartupTasks() error {
	return s.startupTasksContext(context.Background())
}

func (s *PlugSentinel) resetStopChannel() {
	if s.stopCh == nil || isStopChannelClosed(s.stopCh) {
		s.stopCh = make(chan struct{})
	}
}

func isStopChannelClosed(stopCh <-chan struct{}) bool {
	if stopCh == nil {
		return true
	}
	select {
	case <-stopCh:
		return true
	default:
		return false
	}
}

// CleanupTasks is the legacy (non-context) cleanup hook. It delegates to the
// context-aware implementation with a background context.
func (s *PlugSentinel) CleanupTasks() error {
	return s.cleanupTasksContext(context.Background())
}

// CheckHealth implements health check for Sentinel plugin
func (s *PlugSentinel) CheckHealth() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.isInitialized {
		return fmt.Errorf("sentinel plugin not initialized")
	}

	// Check if Sentinel is initialized
	if !s.sentinelInitialized {
		return fmt.Errorf("sentinel core not initialized")
	}

	// Perform a simple flow control check to verify functionality
	entry, err := api.Entry("health_check")
	if err != nil {
		return fmt.Errorf("sentinel health check failed: %w", err)
	}
	entry.Exit()

	return nil
}

// Configure allows updating Sentinel configuration at runtime
func (s *PlugSentinel) Configure(c any) error {
	if c == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	newConf, ok := c.(*SentinelConfig)
	if !ok {
		return fmt.Errorf("invalid configuration type for sentinel plugin")
	}

	// Validate new configuration
	if err := s.validateConfiguration(newConf); err != nil {
		return fmt.Errorf("invalid sentinel configuration: %w", err)
	}

	// Update configuration
	s.conf = newConf

	// Reload rules with new configuration
	if s.isInitialized {
		if err := s.reloadRules(); err != nil {
			return fmt.Errorf("failed to reload rules: %w", err)
		}
	}

	log.Infof("Sentinel plugin configuration updated successfully")
	return nil
}

// initializeSentinelCore initializes the Sentinel core components
func (s *PlugSentinel) initializeSentinelCore() error {
	// Configure Sentinel
	sentinelConfig := config.NewDefaultConfig()
	sentinelConfig.Sentinel.App.Name = s.conf.AppName
	sentinelConfig.Sentinel.Log.Dir = s.conf.LogDir
	// Note: Sentinel config LogConfig doesn't have Level field, we'll set it via logging API

	// Initialize Sentinel. api.InitWithConfig mutates process-global state and is
	// not re-entrant, so guard it with sync.Once: subsequent re-inits reuse the
	// existing global core instead of corrupting it.
	sentinelInitOnce.Do(func() {
		sentinelInitErr = api.InitWithConfig(sentinelConfig)
	})
	if sentinelInitErr != nil {
		return fmt.Errorf("failed to initialize sentinel: %w", sentinelInitErr)
	}

	// Set logging level
	if err := s.setLoggingLevel(); err != nil {
		log.Warnf("Failed to set sentinel logging level: %v", err)
	}

	s.sentinelInitialized = true
	return nil
}

// setLoggingLevel sets the Sentinel logging level
func (s *PlugSentinel) setLoggingLevel() error {
	// Note: Sentinel's logging.NewConsoleLogger() doesn't accept level parameter
	// The level is controlled by the global logging configuration
	logging.ResetGlobalLogger(logging.NewConsoleLogger())
	return nil
}

// validateAndSetDefaults validates configuration and sets default values
func (s *PlugSentinel) validateAndSetDefaults() error {
	if s.conf.AppName == "" {
		s.conf.AppName = currentLynxName()
		if s.conf.AppName == "" {
			s.conf.AppName = "lynx-app"
		}
	}

	if s.conf.LogDir == "" {
		s.conf.LogDir = "./logs/sentinel"
	}

	if s.conf.LogLevel == "" {
		s.conf.LogLevel = "info"
	}

	// Set default metrics configuration
	if s.conf.Metrics.Interval == "" {
		s.conf.Metrics.Interval = "30s"
	}

	// Set default dashboard configuration
	if s.conf.Dashboard.Port == 0 {
		s.conf.Dashboard.Port = 8719
	}

	return s.validateConfiguration(s.conf)
}

// validateConfiguration validates the Sentinel configuration
func (s *PlugSentinel) validateConfiguration(conf *SentinelConfig) error {
	if conf.AppName == "" {
		return fmt.Errorf("app_name cannot be empty")
	}

	if conf.Dashboard.Port != 0 && (conf.Dashboard.Port < 1024 || conf.Dashboard.Port > 65535) {
		return fmt.Errorf("dashboard_port must be between 1024 and 65535")
	}

	// Validate flow rules
	for i, rule := range conf.FlowRules {
		if rule.Resource == "" {
			return fmt.Errorf("flow rule %d: resource name cannot be empty", i)
		}
		if rule.Threshold < 0 {
			return fmt.Errorf("flow rule %d: threshold must be non-negative", i)
		}
	}

	// Validate circuit breaker rules
	for i, rule := range conf.CBRules {
		if rule.Resource == "" {
			return fmt.Errorf("circuit breaker rule %d: resource name cannot be empty", i)
		}
		if rule.Threshold < 0 {
			return fmt.Errorf("circuit breaker rule %d: threshold must be non-negative", i)
		}
		if rule.MinRequestAmount < 0 {
			return fmt.Errorf("circuit breaker rule %d: min_request_amount must be non-negative", i)
		}
	}

	return nil
}

// reloadRules reloads all Sentinel rules
func (s *PlugSentinel) reloadRules() error {
	if err := s.loadFlowRules(); err != nil {
		return err
	}
	if err := s.loadCircuitBreakerRules(); err != nil {
		return err
	}
	if err := s.loadSystemRules(); err != nil {
		return err
	}
	return nil
}
