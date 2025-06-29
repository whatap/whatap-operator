package config

import (
	"os"
	"sync"
)

// EnvConfig holds cached environment variables
type EnvConfig struct {
	WhatapLicense          string
	WhatapHost             string
	WhatapPort             string
	WhatapDefaultNamespace string
	EnableWebhooks         string
}

var (
	envConfig *EnvConfig
	once      sync.Once
)

// GetEnvConfig returns the singleton instance of environment configuration
func GetEnvConfig() *EnvConfig {
	once.Do(func() {
		envConfig = &EnvConfig{
			WhatapLicense:          os.Getenv("WHATAP_LICENSE"),
			WhatapHost:             os.Getenv("WHATAP_HOST"),
			WhatapPort:             os.Getenv("WHATAP_PORT"),
			WhatapDefaultNamespace: os.Getenv("WHATAP_DEFAULT_NAMESPACE"),
			EnableWebhooks:         os.Getenv("ENABLE_WEBHOOKS"),
		}
	})
	return envConfig
}

// GetWhatapLicense returns the cached WHATAP_LICENSE value
func GetWhatapLicense() string {
	return GetEnvConfig().WhatapLicense
}

// GetWhatapHost returns the cached WHATAP_HOST value
func GetWhatapHost() string {
	return GetEnvConfig().WhatapHost
}

// GetWhatapPort returns the cached WHATAP_PORT value
func GetWhatapPort() string {
	return GetEnvConfig().WhatapPort
}

// GetWhatapDefaultNamespace returns the cached WHATAP_DEFAULT_NAMESPACE value
func GetWhatapDefaultNamespace() string {
	return GetEnvConfig().WhatapDefaultNamespace
}

// GetEnableWebhooks returns the cached ENABLE_WEBHOOKS value
func GetEnableWebhooks() string {
	return GetEnvConfig().EnableWebhooks
}