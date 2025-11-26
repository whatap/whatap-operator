package controller

import (
	"encoding/json"
	"testing"
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
