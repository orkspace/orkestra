// pkg/note/kube_pods_test.go
package note

import (
	"testing"
)

// enrichedDeployment builds a minimal enriched resource map as children.go
// would produce it — with _pods embedded.
func enrichedDeployment(pods []map[string]interface{}) map[string]interface{} {
	raw := make([]interface{}, len(pods))
	for i, p := range pods {
		raw[i] = p
	}
	return map[string]interface{}{
		"metadata": map[string]interface{}{"name": "web"},
		"_pods":    raw,
	}
}

func pod(name, ip, phase string, ready bool, restartCount int64) map[string]interface{} {
	return map[string]interface{}{
		"name":         name,
		"ip":           ip,
		"phase":        phase,
		"ready":        ready,
		"node":         "node-1",
		"restartCount": restartCount,
	}
}

func TestNotePodNames(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want string
	}{
		{
			name: "two pods",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-abc", "10.0.0.1", "Running", true, 0),
				pod("web-def", "10.0.0.2", "Running", true, 0),
			}),
			want: "web-abc, web-def",
		},
		{
			name: "single pod",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-abc", "10.0.0.1", "Running", true, 0),
			}),
			want: "web-abc",
		},
		{
			name: "no pods (enrichment Empty()",
			obj:  enrichedDeployment(nil),
			want: "",
		},
		{
			name: "_pods key absent",
			obj:  map[string]interface{}{"metadata": map[string]interface{}{"name": "web"}},
			want: "",
		},
		{
			name: "nil obj",
			obj:  nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notePodNames(tt.obj)
			if got != tt.want {
				t.Errorf("notePodNames() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNotePodIPs(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want string
	}{
		{
			name: "two pods with IPs",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-abc", "10.0.0.1", "Running", true, 0),
				pod("web-def", "10.0.0.2", "Running", true, 0),
			}),
			want: "10.0.0.1, 10.0.0.2",
		},
		{
			name: "pod with empty IP (not yet assigned)",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-abc", "", "Pending", false, 0),
			}),
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notePodIPs(tt.obj)
			if got != tt.want {
				t.Errorf("notePodIPs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNotePodCount(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want int
	}{
		{
			name: "three pods",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-0", "10.0.0.1", "Running", true, 0),
				pod("web-1", "10.0.0.2", "Running", true, 0),
				pod("web-2", "10.0.0.3", "Running", false, 0),
			}),
			want: 3,
		},
		{
			name: "no pods",
			obj:  enrichedDeployment(nil),
			want: 0,
		},
		{
			name: "nil obj",
			obj:  nil,
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notePodCount(tt.obj)
			if got != tt.want {
				t.Errorf("notePodCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNoteReadyPodCount(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want int
	}{
		{
			name: "two of three ready",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-0", "10.0.0.1", "Running", true, 0),
				pod("web-1", "10.0.0.2", "Running", true, 0),
				pod("web-2", "10.0.0.3", "Pending", false, 0),
			}),
			want: 2,
		},
		{
			name: "none ready",
			obj:  enrichedDeployment(nil),
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := noteReadyPodCount(tt.obj)
			if got != tt.want {
				t.Errorf("noteReadyPodCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNoteHasCrashingPod(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want bool
	}{
		{
			name: "pod with 3 restarts → crashing",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-abc", "10.0.0.1", "Running", false, 3),
			}),
			want: true,
		},
		{
			name: "pod with 2 restarts → not crashing (threshold is > 2)",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-abc", "10.0.0.1", "Running", true, 2),
			}),
			want: false,
		},
		{
			name: "all pods healthy",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-abc", "10.0.0.1", "Running", true, 0),
				pod("web-def", "10.0.0.2", "Running", true, 1),
			}),
			want: false,
		},
		{
			name: "no pods",
			obj:  enrichedDeployment(nil),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := noteHasCrashingPod(tt.obj)
			if got != tt.want {
				t.Errorf("noteHasCrashingPod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotePodPhases(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want string
	}{
		{
			name: "running and pending",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-0", "10.0.0.1", "Running", true, 0),
				pod("web-1", "10.0.0.2", "Pending", false, 0),
			}),
			want: "Running, Pending",
		},
		{
			name: "no pods",
			obj:  enrichedDeployment(nil),
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notePodPhases(tt.obj)
			if got != tt.want {
				t.Errorf("notePodPhases() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNotePodNodes(t *testing.T) {
	obj := enrichedDeployment([]map[string]interface{}{
		pod("web-0", "10.0.0.1", "Running", true, 0),
		pod("web-1", "10.0.0.2", "Running", true, 0),
	})
	got := notePodNodes(obj)
	if got != "node-1, node-1" {
		t.Errorf("notePodNodes() = %q, want %q", got, "node-1, node-1")
	}
}

func TestNotePodMaxRestarts(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want int64
	}{
		{
			name: "highest of three",
			obj: enrichedDeployment([]map[string]interface{}{
				pod("web-0", "10.0.0.1", "Running", true, 1),
				pod("web-1", "10.0.0.2", "Running", true, 5),
				pod("web-2", "10.0.0.3", "Running", false, 3),
			}),
			want: 5,
		},
		{
			name: "no pods",
			obj:  enrichedDeployment(nil),
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notePodMaxRestarts(tt.obj)
			if got != tt.want {
				t.Errorf("notePodMaxRestarts() = %d, want %d", got, tt.want)
			}
		})
	}
}

func podWithOrdinal(name, ip string, ordinal int64) map[string]interface{} {
	return map[string]interface{}{
		"name":         name,
		"ip":           ip,
		"phase":        "Running",
		"ready":        true,
		"node":         "node-1",
		"restartCount": int64(0),
		"ordinal":      ordinal,
	}
}

func TestNotePodByOrdinal(t *testing.T) {
	obj := map[string]interface{}{
		"_pods": []interface{}{
			podWithOrdinal("redis-0", "10.0.0.1", 0),
			podWithOrdinal("redis-1", "10.0.0.2", 1),
			podWithOrdinal("redis-2", "10.0.0.3", 2),
		},
	}
	t.Run("ordinal 0", func(t *testing.T) {
		got := notePodByOrdinal(obj, 0)
		m, ok := got.(map[string]interface{})
		if !ok || m["name"] != "redis-0" {
			t.Errorf("podByOrdinal(0) = %v, want name=redis-0", got)
		}
	})
	t.Run("ordinal 2", func(t *testing.T) {
		got := notePodByOrdinal(obj, 2)
		m, ok := got.(map[string]interface{})
		if !ok || m["ip"] != "10.0.0.3" {
			t.Errorf("podByOrdinal(2) = %v, want ip=10.0.0.3", got)
		}
	})
	t.Run("ordinal not found", func(t *testing.T) {
		got := notePodByOrdinal(obj, 9)
		if got != nil {
			t.Errorf("podByOrdinal(9) = %v, want nil", got)
		}
	})
}

func makePodWithContainers(name, phase string, containers []interface{}) map[string]interface{} {
	return map[string]interface{}{
		"name":         name,
		"phase":        phase,
		"ready":        false,
		"ip":           "10.0.0.1",
		"node":         "node-1",
		"restartCount": int64(0),
		"ordinal":      int64(0),
		"exitCode":     int64(-1),
		"containers":   containers,
	}
}

func makeContainer(name, state, reason string) map[string]interface{} {
	return map[string]interface{}{
		"name":         name,
		"image":        "nginx:latest",
		"state":        state,
		"reason":       reason,
		"ready":        false,
		"restartCount": int64(3),
	}
}

func TestNotePodCrashLoopDetected(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want bool
	}{
		{"nil", nil, false},
		{"no _pods", map[string]interface{}{}, false},
		{"pod with no containers", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Running", nil)},
		}, false},
		{"container Running", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
			},
		}, false},
		{"container in CrashLoopBackOff", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Waiting", "CrashLoopBackOff"),
				}),
			},
		}, true},
		{"one healthy one crashing", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
				makePodWithContainers("p-1", "Running", []interface{}{
					makeContainer("app", "Waiting", "CrashLoopBackOff"),
				}),
			},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notePodCrashLoopDetected(tt.obj); got != tt.want {
				t.Errorf("notePodCrashLoopDetected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotePodImagePullBackOffDetected(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want bool
	}{
		{"nil", nil, false},
		{"no _pods", map[string]interface{}{}, false},
		{"pod with no containers", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Pending", nil)},
		}, false},
		{"container Running", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
			},
		}, false},
		{"container ImagePullBackOff", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Pending", []interface{}{
					makeContainer("app", "Waiting", "ImagePullBackOff"),
				}),
			},
		}, true},
		{"one healthy one ImagePullBackOff", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
				makePodWithContainers("p-1", "Pending", []interface{}{
					makeContainer("app", "Waiting", "ImagePullBackOff"),
				}),
			},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notePodImagePullBackOffDetected(tt.obj); got != tt.want {
				t.Errorf("notePodImagePullBackOffDetected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotePodErrImagePullDetected(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want bool
	}{
		{"nil", nil, false},
		{"no _pods", map[string]interface{}{}, false},
		{"pod with no containers", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Pending", nil)},
		}, false},
		{"container Running", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
			},
		}, false},
		{"container ErrImagePull", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Pending", []interface{}{
					makeContainer("app", "Waiting", "ErrImagePull"),
				}),
			},
		}, true},
		{"one healthy one ErrImagePull", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
				makePodWithContainers("p-1", "Pending", []interface{}{
					makeContainer("app", "Waiting", "ErrImagePull"),
				}),
			},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notePodErrImagePullDetected(tt.obj); got != tt.want {
				t.Errorf("notePodErrImagePullDetected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotePodErrorDetected(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want bool
	}{
		{"nil", nil, false},
		{"no _pods", map[string]interface{}{}, false},
		{"pod with no containers", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Running", nil)},
		}, false},
		{"container Running", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
			},
		}, false},
		{"container Error (terminated)", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Failed", []interface{}{
					makeContainer("app", "Terminated", "Error"),
				}),
			},
		}, true},
		{"one healthy one Error", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
				makePodWithContainers("p-1", "Failed", []interface{}{
					makeContainer("app", "Terminated", "Error"),
				}),
			},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notePodErrorDetected(tt.obj); got != tt.want {
				t.Errorf("notePodErrorDetected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotePodOOMKilledDetected(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want bool
	}{
		{"nil", nil, false},
		{"no _pods", map[string]interface{}{}, false},
		{"pod with no containers", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Running", nil)},
		}, false},
		{"container Running", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
			},
		}, false},
		{"container OOMKilled", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Failed", []interface{}{
					makeContainer("app", "Terminated", "OOMKilled"),
				}),
			},
		}, true},
		{"one healthy one OOMKilled", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
				makePodWithContainers("p-1", "Failed", []interface{}{
					makeContainer("app", "Terminated", "OOMKilled"),
				}),
			},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notePodOOMKilledDetected(tt.obj); got != tt.want {
				t.Errorf("notePodOOMKilledDetected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotePodContainerReasons(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want string
	}{
		{"nil", nil, ""},
		{"no _pods", map[string]interface{}{}, ""},
		{"containers all running (no reason)", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Running", ""),
				}),
			},
		}, ""},
		{"one CrashLoopBackOff", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Waiting", "CrashLoopBackOff"),
				}),
			},
		}, "CrashLoopBackOff"},
		{"deduplicates same reason", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Waiting", "CrashLoopBackOff"),
				}),
				makePodWithContainers("p-1", "Running", []interface{}{
					makeContainer("app", "Waiting", "CrashLoopBackOff"),
				}),
			},
		}, "CrashLoopBackOff"},
		{"two distinct reasons", map[string]interface{}{
			"_pods": []interface{}{
				makePodWithContainers("p-0", "Running", []interface{}{
					makeContainer("app", "Waiting", "CrashLoopBackOff"),
					makeContainer("sidecar", "Waiting", "ImagePullBackOff"),
				}),
			},
		}, "CrashLoopBackOff, ImagePullBackOff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notePodContainerReasons(tt.obj); got != tt.want {
				t.Errorf("notePodContainerReasons() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotePodContainerState(t *testing.T) {
	tests := []struct {
		name          string
		obj           interface{}
		ordinal       int64
		containerName string
		want          string
	}{
		{"nil", nil, 0, "app", ""},
		{"no _pods", map[string]interface{}{}, 0, "app", ""},
		{"ordinal not found", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Running", []interface{}{
				makeContainer("app", "Running", ""),
			})},
		}, 1, "app", ""},
		{"container not found", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Running", []interface{}{
				makeContainer("app", "Running", ""),
			})},
		}, 0, "other", ""},
		{"Running container", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Running", []interface{}{
				makeContainer("app", "Running", ""),
			})},
		}, 0, "app", "Running"},
		{"Waiting container", map[string]interface{}{
			"_pods": []interface{}{makePodWithContainers("p-0", "Running", []interface{}{
				makeContainer("app", "Waiting", "CrashLoopBackOff"),
			})},
		}, 0, "app", "Waiting"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notePodContainerState(tt.obj, tt.ordinal, tt.containerName); got != tt.want {
				t.Errorf("notePodContainerState() = %v, want %v", got, tt.want)
			}
		})
	}
}
