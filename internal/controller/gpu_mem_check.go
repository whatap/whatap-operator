package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// GpuMemChecker monitors dcgm-exporter memory usage and restarts the pod if it exceeds the threshold
type GpuMemChecker struct {
	Client   client.Client
	Interval time.Duration
	Log      logr.Logger
}

type ContainerStatsResponse struct {
	MemoryStats MemoryStats `json:"memory_stats"`
}

type MemoryStats struct {
	Usage uint64             `json:"usage"`
	Limit uint64             `json:"limit"`
	Stats MemoryStatsDetails `json:"stats"`
}

type MemoryStatsDetails struct {
	InactiveFile uint64 `json:"inactive_file"`
	Rss          uint64 `json:"rss"`
}

// Start implements manager.Runnable
func (r *GpuMemChecker) Start(ctx context.Context) error {
	r.Log = logf.Log.WithName("gpu-mem-checker")
	r.Log.Info("Starting GPU memory checker")

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.checkGpuPods(ctx)
		}
	}
}

func (r *GpuMemChecker) checkGpuPods(ctx context.Context) {
	podList := &corev1.PodList{}
	// Find pods with label whatap-gpu: true
	opts := []client.ListOption{
		client.MatchingLabels{"whatap-gpu": "true"},
	}

	if err := r.Client.List(ctx, podList, opts...); err != nil {
		r.Log.Error(err, "Failed to list GPU pods")
		return
	}

	for _, pod := range podList.Items {
		r.checkPod(ctx, &pod)
	}
}

func (r *GpuMemChecker) checkPod(ctx context.Context, pod *corev1.Pod) {
	// Skip if pod is not running
	if pod.Status.Phase != corev1.PodRunning {
		return
	}

	// Find dcgm-exporter container ID
	var containerID string
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "dcgm-exporter" {
			containerID = status.ContainerID
			break
		}
	}

	if containerID == "" {
		// dcgm-exporter might not be ready or present?
		return
	}

	// Strip prefix (docker://, containerd://, etc)
	if idx := strings.Index(containerID, "://"); idx != -1 {
		containerID = containerID[idx+3:]
	}

	// Check memory usage
	workingSet, limit, err := r.getMemoryStats(ctx, pod.Status.PodIP, containerID)
	if err != nil {
		r.Log.V(1).Info("Failed to get memory stats", "pod", pod.Name, "error", err)
		return
	}

	if limit == 0 {
		return
	}

	// Check if usage > 70% of limit
	// Use float to avoid overflow/underflow issues in simple multiplication
	ratio := float64(workingSet) / float64(limit)
	if ratio > 0.7 {
		r.Log.Info("dcgm-exporter memory usage high, restarting pod",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"workingSet", workingSet,
			"limit", limit,
			"ratio", ratio)

		if err := r.Client.Delete(ctx, pod); err != nil {
			r.Log.Error(err, "Failed to delete pod", "pod", pod.Name)
		}
	}
}

func (r *GpuMemChecker) getMemoryStats(ctx context.Context, podIP, containerID string) (uint64, uint64, error) {
	url := fmt.Sprintf("http://%s:6801/container/%s/stats", podIP, containerID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, 0, err
	}

	// Use short timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var stats ContainerStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return 0, 0, err
	}

	// memory_working_set = usage - inactive_file
	var workingSet uint64
	if stats.MemoryStats.Usage > stats.MemoryStats.Stats.InactiveFile {
		workingSet = stats.MemoryStats.Usage - stats.MemoryStats.Stats.InactiveFile
	} else {
		workingSet = 0
	}

	return workingSet, stats.MemoryStats.Limit, nil
}
