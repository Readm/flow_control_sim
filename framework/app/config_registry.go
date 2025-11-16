package app

// ConfigDescriptor describes an externally registered simulation configuration.
type ConfigDescriptor struct {
	Name        string
	Description string
	Config      *Config
}

// ConfigProvider supplies predefined configurations to the simulator/runtime.
type ConfigProvider interface {
	List() []ConfigDescriptor
	Get(name string) *Config
}

type noopConfigProvider struct{}

func (noopConfigProvider) List() []ConfigDescriptor { return nil }
func (noopConfigProvider) Get(string) *Config       { return nil }

var configProvider ConfigProvider = noopConfigProvider{}

// SetConfigProvider installs a global provider used by Web/API components.
func SetConfigProvider(p ConfigProvider) {
	if p == nil {
		configProvider = noopConfigProvider{}
		return
	}
	configProvider = p
}

// GetPredefinedConfigs returns the current provider's descriptors.
func GetPredefinedConfigs() []ConfigDescriptor {
	return configProvider.List()
}

// GetConfigByName returns a deep copy of the configuration by name.
func GetConfigByName(name string) *Config {
	return configProvider.Get(name)
}
