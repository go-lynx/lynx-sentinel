package sentinel

import (
	"github.com/go-lynx/lynx/plugins"
)

const (
	pluginName        = "sentinel.flow_control"
	PluginName        = pluginName
	pluginVersion     = "v1.6.3"
	PluginVersion     = pluginVersion
	pluginDescription = "Sentinel flow control and circuit breaker plugin for lynx framework"
	PluginDescription = pluginDescription
	confPrefix        = "lynx.sentinel"
	ConfPrefix        = "lynx.sentinel"
	// pluginWeight is intentionally high so Sentinel initializes before the plugins it protects.
	pluginWeight = 200
	PluginWeight = pluginWeight
)

// NewSentinelPlugin creates a new Sentinel plugin instance.
func NewSentinelPlugin() *PlugSentinel {
	return &PlugSentinel{
		BasePlugin: plugins.NewBasePlugin(
			plugins.GeneratePluginID("", pluginName, pluginVersion),
			pluginName,
			pluginDescription,
			pluginVersion,
			confPrefix,
			pluginWeight,
		),
		stopCh: make(chan struct{}),
	}
}
