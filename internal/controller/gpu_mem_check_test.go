package controller

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestParseContainerStats(t *testing.T) {
	jsonStr := `{
  "memory_stats": {
    "usage": 206745600,
    "max_usage": 215015424,
    "stats": {
      "active_anon": 0,
      "active_file": 0,
      "cache": 0,
      "dirty": 0,
      "hierarchical_memory_limit": 367001600,
      "hierarchical_memsw_limit": 0,
      "inactive_anon": 203698176,
      "inactive_file": 0,
      "mapped_file": 0,
      "pgfault": 278058,
      "pgmajfault": 0,
      "pgpgin": 276441,
      "pgpgout": 226791,
      "rss": 203698176,
      "rss_huge": 0,
      "total_active_anon": 0,
      "total_active_file": 0,
      "total_cache": 0,
      "total_dirty": 0,
      "total_inactive_anon": 203698176,
      "total_inactive_file": 0,
      "total_mapped_file": 0,
      "total_pgfault": 278058,
      "total_pgmajfault": 0,
      "total_pgpgin": 276441,
      "total_pgpgout": 226791,
      "total_rss": 203698176,
      "total_rss_huge": 0,
      "total_unevictable": 0,
      "total_writeback": 0,
      "unevictable": 0,
      "writeback": 0
    },
    "limit": 16069016000,
    "failcnt": 0
  },
  "name": "whatap-node-agent",
  "id": "fe92515c0da4cd16b434d64392a5fe1454f819477241c870e5441de28cfc5eeb",
  "network_stats": {
    "rxBytes": 12548660,
    "rxDropped": 0,
    "rxErrors": 0,
    "rxPackets": 5266,
    "txBytes": 8825014,
    "txDropped": 0,
    "txErrors": 0,
    "txPackets": 6577
  },
  "restart_count": 0
}`

	var stats ContainerStatsResponse
	if err := json.Unmarshal([]byte(jsonStr), &stats); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if stats.MemoryStats.Usage != 206745600 {
		t.Errorf("Expected usage 206745600, got %d", stats.MemoryStats.Usage)
	}
	if stats.MemoryStats.Stats.InactiveFile != 0 {
		t.Errorf("Expected inactive_file 0, got %d", stats.MemoryStats.Stats.InactiveFile)
	}
	if stats.MemoryStats.Limit != 16069016000 {
		t.Errorf("Expected limit 16069016000, got %d", stats.MemoryStats.Limit)
	}

	// Calculate working set
	workingSet := stats.MemoryStats.Usage - stats.MemoryStats.Stats.InactiveFile
	if workingSet != 206745600 {
		t.Errorf("Expected workingSet 206745600, got %d", workingSet)
	}

	ratio := float64(workingSet) / float64(stats.MemoryStats.Limit)
	expectedRatio := 0.012866
	if ratio < expectedRatio-0.00001 || ratio > expectedRatio+0.00001 {
		t.Errorf("Expected ratio ~%f, got %f", expectedRatio, ratio)
	}
}

func TestParseContainerMeta(t *testing.T) {
	jsonStr := `[
	  {
		"Id": "5122a47810f09dc414ecef60847b965f90fdc5e8de65f2748dcdd805ac12f13f",
		"MemoryLimit": "170Mi"
	  },
	  {
		"Id": "a020057411619c3b807e93035f3c659eb53efc25acbf5496ccc9af920fb1010e",
		"MemoryLimit": "16069016Ki"
	  }
	]`

	var containers []ContainerMeta
	if err := json.Unmarshal([]byte(jsonStr), &containers); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(containers) != 2 {
		t.Fatalf("Expected 2 containers, got %d", len(containers))
	}

	// Test Case 1: 170Mi
	c1 := containers[0]
	if c1.MemoryLimit != "170Mi" {
		t.Errorf("Expected MemoryLimit 170Mi, got %s", c1.MemoryLimit)
	}

	qty, err := resource.ParseQuantity(c1.MemoryLimit)
	if err != nil {
		t.Errorf("Failed to parse quantity: %v", err)
	}
	if qty.Value() != 178257920 {
		t.Errorf("Expected 178257920 bytes, got %d", qty.Value())
	}

	// Test Case 2: 16069016Ki
	c2 := containers[1]
	qty2, err := resource.ParseQuantity(c2.MemoryLimit)
	if err != nil {
		t.Errorf("Failed to parse quantity: %v", err)
	}
	// 16069016 * 1024 = 16454672384
	if qty2.Value() != 16454672384 {
		t.Errorf("Expected 16454672384 bytes, got %d", qty2.Value())
	}
}
