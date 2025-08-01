package main

import (
	"fmt"
	"net/url"
)

// Simple test to verify URL parameter generation logic
func appendParams(baseURL string, params map[string][]string) (string, error) {
	if len(params) == 0 {
		return baseURL, nil
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %v", err)
	}

	query := u.Query()
	for key, values := range params {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func main() {
	// Test the Azure params scenario
	baseURL := "http://localhost:8080/probe/metrics/resource"
	params := map[string][]string{
		"subscription":    {"50d91b57-a280-45b5-8d7c-be8005662738"},
		"target":          {"/subscriptions/50d91b57-a280-45b5-8d7c-be8005662738/resourceGroups/WhaTap-Data-KR-MID/providers/Microsoft.Sql/managedInstances/openmetrics-instance-01"},
		"metric":          {"avg_cpu_percent", "virtual_core_count"},
		"interval":        {"PT1M"},
		"aggregation":     {"average"},
		"name":            {"azure_sql_cpu"},
		"metricNamespace": {"microsoft.sql/managedinstances"},
	}

	finalURL, err := appendParams(baseURL, params)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Base URL: %s\n", baseURL)
	fmt.Printf("Final URL: %s\n", finalURL)

	// Verify the URL contains expected parameters
	parsedURL, _ := url.Parse(finalURL)
	query := parsedURL.Query()

	fmt.Printf("\nParsed parameters:\n")
	for key, values := range query {
		fmt.Printf("  %s: %v\n", key, values)
	}

	// Check if all expected params are present
	expectedParams := []string{"subscription", "target", "metric", "interval", "aggregation", "name", "metricNamespace"}
	fmt.Printf("\nValidation:\n")
	for _, param := range expectedParams {
		if values, exists := query[param]; exists {
			fmt.Printf("  ✓ %s: %v\n", param, values)
		} else {
			fmt.Printf("  ✗ %s: missing\n", param)
		}
	}
}
