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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// GpuMemChecker monitors dcgm-exporter memory usage and restarts the pod if it exceeds the threshold
type GpuMemChecker struct {
	ClientSet kubernetes.Interface
	Interval  time.Duration
	Log       logr.Logger
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

type ContainerMeta struct {
	Id          string `json:"Id"`
	MemoryLimit string `json:"MemoryLimit"`
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
			r.Log.Info("Ticker fired, initiating check")
			r.checkGpuPods(ctx)
		}
	}
}

func (r *GpuMemChecker) checkGpuPods(ctx context.Context) {
	r.Log.Info("Listing GPU pods")

	// Find pods with label whatap-gpu: true
	listOpts := metav1.ListOptions{
		LabelSelector: "whatap-gpu=true",
	}

	podList, err := r.ClientSet.CoreV1().Pods("").List(ctx, listOpts)
	if err != nil {
		r.Log.Error(err, "Failed to list GPU pods")
		return
	}

	r.Log.Info("Listed GPU pods", "count", len(podList.Items))

	for _, pod := range podList.Items {
		r.checkPod(ctx, &pod)
	}
}

func (r *GpuMemChecker) checkPod(ctx context.Context, pod *corev1.Pod) {
	r.Log.Info("Checking pod details", "pod", pod.Name, "phase", pod.Status.Phase)
	// Skip if pod is being deleted
	if pod.DeletionTimestamp != nil {
		r.Log.Info("Pod is terminating, skipping", "pod", pod.Name)
		return
	}
	// Skip if pod is not running
	if pod.Status.Phase != corev1.PodRunning {
		r.Log.Info("Pod not running, skipping", "pod", pod.Name)
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
		r.Log.Info("dcgm-exporter container not found", "pod", pod.Name)
		// dcgm-exporter might not be ready or present?
		return
	}

	// Strip prefix (docker://, containerd://, etc)
	if idx := strings.Index(containerID, "://"); idx != -1 {
		containerID = containerID[idx+3:]
	}
	r.Log.Info("Found container ID", "pod", pod.Name, "containerID", containerID)

	// Check memory usage
	r.Log.Info("Requesting memory stats", "pod", pod.Name, "ip", pod.Status.PodIP)
	workingSet, _, err := r.getMemoryStats(ctx, pod.Status.PodIP, containerID)
	if err != nil {
		r.Log.Info("Failed to get memory stats", "pod", pod.Name, "error", err)
		return
	}

	limit, err := r.getContainerLimit(ctx, pod.Status.PodIP, containerID)
	if err != nil {
		r.Log.Info("Failed to get container limit", "pod", pod.Name, "error", err)
		return
	}

	r.Log.Info("Memory stats retrieved", "pod", pod.Name, "workingSet", workingSet, "limit", limit)

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

		if err := r.ClientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
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

func (r *GpuMemChecker) getContainerLimit(ctx context.Context, podIP, containerID string) (uint64, error) {
	url := fmt.Sprintf("http://%s:6801/container", podIP)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var containers []ContainerMeta
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return 0, err
	}

	for _, c := range containers {
		if c.Id == containerID {
			if c.MemoryLimit == "" || c.MemoryLimit == "0" {
				return 0, nil
			}
			qty, err := resource.ParseQuantity(c.MemoryLimit)
			if err != nil {
				return 0, fmt.Errorf("failed to parse memory limit '%s': %v", c.MemoryLimit, err)
			}
			return uint64(qty.Value()), nil
		}
	}

	return 0, fmt.Errorf("container not found in metadata")
}
