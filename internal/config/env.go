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
	DebugMode              string
}

var (
	envConfig *EnvConfig
	once      sync.Once
)

// GetEnvConfig returns the singleton instance of environment configuration
func GetEnvConfig() *EnvConfig {
	once.Do(func() {
		debugVal := os.Getenv("DEBUG")
		if debugVal == "" {
			debugVal = os.Getenv("debug")
		}
		envConfig = &EnvConfig{
			WhatapLicense:          os.Getenv("WHATAP_LICENSE"),
			WhatapHost:             os.Getenv("WHATAP_HOST"),
			WhatapPort:             os.Getenv("WHATAP_PORT"),
			WhatapDefaultNamespace: os.Getenv("WHATAP_DEFAULT_NAMESPACE"),
			EnableWebhooks:         os.Getenv("ENABLE_WEBHOOKS"),
			DebugMode:              debugVal,
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

// GetDebugMode returns the cached DEBUG value
func GetDebugMode() string {
	return GetEnvConfig().DebugMode
}
