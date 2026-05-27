package forwarder

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kpetremann/jackadi/internal/manager/inventory"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
)

func TestTargetedNodesList(t *testing.T) {
	inv := &inventory.Nodes{}
	dispatcher := NewDispatcher[string, string](inv)

	nodes := []node.ID{node.ID("node1"), node.ID("node2"), node.ID("node3")}
	for _, nodeID := range nodes {
		_ = dispatcher.RegisterNode(nodeID)
	}

	result, err := dispatcher.TargetedNodes("node1,node2", proto.TargetMode_LIST)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expected := map[string]bool{
		"node1": true,
		"node2": true,
	}
	if diff := cmp.Diff(result, expected); diff != "" {
		t.Errorf("Mismatch (-got +want):\n%s", diff)
	}
}

func TestTargetedNodesGlob(t *testing.T) {
	inv := &inventory.Nodes{}
	dispatcher := NewDispatcher[string, string](inv)

	nodes := []node.ID{node.ID("web-1"), node.ID("web-2"), node.ID("db-1"), node.ID("cache-1")}
	for _, nodeID := range nodes {
		_ = dispatcher.RegisterNode(nodeID)
	}

	tests := []struct {
		pattern  string
		expected map[string]bool
	}{
		{
			pattern: "web-*",
			expected: map[string]bool{
				"web-1": true,
				"web-2": true,
			},
		},
		{
			pattern: "*-1",
			expected: map[string]bool{
				"web-1":   true,
				"db-1":    true,
				"cache-1": true,
			},
		},
		{
			pattern: "db-*",
			expected: map[string]bool{
				"db-1": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result, err := dispatcher.TargetedNodes(tt.pattern, proto.TargetMode_GLOB)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if diff := cmp.Diff(result, tt.expected); diff != "" {
				t.Errorf("Mismatch for pattern %q (-got +want):\n%s", tt.pattern, diff)
			}
		})
	}
}

func TestTargetedNodesRegex(t *testing.T) {
	inv := &inventory.Nodes{}
	dispatcher := NewDispatcher[string, string](inv)

	nodes := []node.ID{node.ID("web-01"), node.ID("web-02"), node.ID("db-01"), node.ID("cache-01")}
	for _, nodeID := range nodes {
		_ = dispatcher.RegisterNode(nodeID)
	}

	tests := []struct {
		pattern     string
		expected    map[string]bool
		expectError bool
	}{
		{
			pattern: "web-\\d+",
			expected: map[string]bool{
				"web-01": true,
				"web-02": true,
			},
		},
		{
			pattern: ".*-01",
			expected: map[string]bool{
				"web-01":   true,
				"db-01":    true,
				"cache-01": true,
			},
		},
		{
			pattern: "db-.*",
			expected: map[string]bool{
				"db-01": true,
			},
		},
		{
			pattern:     "[invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result, err := dispatcher.TargetedNodes(tt.pattern, proto.TargetMode_REGEX)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error for invalid regex")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if diff := cmp.Diff(result, tt.expected); diff != "" {
				t.Errorf("Mismatch for pattern %q (-got +want):\n%s", tt.pattern, diff)
			}
		})
	}
}

func TestTargetedNodesQuery(t *testing.T) {
	inv := inventory.New()
	inv.DisableRegistryFile()
	dispatcher := NewDispatcher[string, string](&inv)

	nodes := []node.ID{node.ID("web-1"), node.ID("web-2"), node.ID("db-1")}
	for _, nodeID := range nodes {
		_ = dispatcher.RegisterNode(nodeID)
	}

	// Set up node states with specs in the inventory
	for _, nodeID := range nodes {
		inv.MarkNodeStateChange(nodeID, true)
	}

	// Set specs for each node
	specs1 := map[string]any{
		"os":   "linux",
		"role": "webserver",
		"system": map[string]any{
			"cpu":    4,
			"memory": 8,
		},
	}
	specs2 := map[string]any{
		"os":   "linux",
		"role": "webserver",
		"system": map[string]any{
			"cpu":    8,
			"memory": 16,
		},
	}
	specs3 := map[string]any{
		"os":   "linux",
		"role": "database",
		"system": map[string]any{
			"cpu":    8,
			"memory": 32,
		},
	}

	_ = inv.SetSpec(node.ID("web-1"), specs1)
	_ = inv.SetSpec(node.ID("web-2"), specs2)
	_ = inv.SetSpec(node.ID("db-1"), specs3)

	tests := []struct {
		name        string
		query       string
		expected    map[string]bool
		expectError bool
	}{
		{
			name:  "specs exact match",
			query: "specs.os==linux",
			expected: map[string]bool{
				"web-1": true,
				"web-2": true,
				"db-1":  true,
			},
		},
		{
			name:  "specs glob match",
			query: "specs.role=~web*",
			expected: map[string]bool{
				"web-1": true,
				"web-2": true,
			},
		},
		{
			name:  "specs regex match",
			query: "specs.role=~/web.*/",
			expected: map[string]bool{
				"web-1": true,
				"web-2": true,
			},
		},
		{
			name:  "AND condition",
			query: "specs.os==linux and specs.role==webserver",
			expected: map[string]bool{
				"web-1": true,
				"web-2": true,
			},
		},
		{
			name:  "OR condition",
			query: "specs.role==webserver or specs.role==database",
			expected: map[string]bool{
				"web-1": true,
				"web-2": true,
				"db-1":  true,
			},
		},
		{
			name:        "empty query",
			query:       "",
			expectError: true,
		},
		{
			name:        "invalid operator",
			query:       "hostname!=web-1",
			expectError: true,
		},
		{
			name:        "invalid field",
			query:       "invalid.field==value",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dispatcher.TargetedNodes(tt.query, proto.TargetMode_QUERY)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if diff := cmp.Diff(result, tt.expected); diff != "" {
				t.Errorf("Mismatch for query %q (-got +want):\n%s", tt.query, diff)
			}
		})
	}
}

func TestDispatcherLifecycle(t *testing.T) {
	inv := &inventory.Nodes{}
	nodeID := node.ID("node1")

	t.Run("register and send", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		if err := d.RegisterNode(nodeID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		nodes, err := d.TargetedNodes(string(nodeID), proto.TargetMode_EXACT)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !nodes[string(nodeID)] {
			t.Error("node should be dispatchable after registration")
		}
	})

	t.Run("duplicate register", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		_ = d.RegisterNode(nodeID)
		if err := d.RegisterNode(nodeID); err == nil {
			t.Error("expected error on duplicate registration")
		}
	})

	t.Run("send to unknown node", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		if err := d.Send(nodeID, Task[string, string]{}, time.Second); !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("expected ErrNodeNotFound, got %v", err)
		}
	})

	t.Run("send times out when nobody reads", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		_ = d.RegisterNode(nodeID)

		if err := d.Send(nodeID, Task[string, string]{}, 50*time.Millisecond); !errors.Is(err, ErrTimeout) {
			t.Errorf("expected ErrTimeout, got %v", err)
		}
	})

	t.Run("send succeeds when channel is read", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		_ = d.RegisterNode(nodeID)

		ch, err := d.GetTasksChannel(nodeID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		sendErr := make(chan error, 1)
		go func() { sendErr <- d.Send(nodeID, Task[string, string]{Request: "hello"}, time.Second) }()

		task := <-ch
		if task.Request != "hello" {
			t.Errorf("expected task request 'hello', got %q", task.Request)
		}
		if err := <-sendErr; err != nil {
			t.Errorf("unexpected send error: %v", err)
		}
	})

	t.Run("send to closed channel", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		_ = d.RegisterNode(nodeID)
		d.Close(nodeID)

		if err := d.Send(nodeID, Task[string, string]{}, time.Second); !errors.Is(err, ErrClosedTaskChannel) {
			t.Errorf("expected ErrClosedTaskChannel, got %v", err)
		}
	})

	t.Run("unregister removes node from targets", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		_ = d.RegisterNode(nodeID)
		d.UnregisterNode(nodeID)

		if err := d.Send(nodeID, Task[string, string]{}, time.Second); !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("expected ErrNodeNotFound, got %v", err)
		}
	})

	t.Run("forget removes disconnected node from target results", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		_ = d.RegisterNode(nodeID)
		d.Close(nodeID)
		d.UnregisterNode(nodeID)
		if err := d.Forget(nodeID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		nodes, err := d.TargetedNodes(string(nodeID), proto.TargetMode_EXACT)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nodes[string(nodeID)] {
			t.Error("forgotten node should not appear as dispatchable")
		}
	})

	t.Run("forget fails if node is still registered", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		_ = d.RegisterNode(nodeID)
		if err := d.Forget(nodeID); err == nil {
			t.Error("expected error forgetting a still-registered node")
		}
	})

	t.Run("close marks node as not dispatchable", func(t *testing.T) {
		d := NewDispatcher[string, string](inv)
		_ = d.RegisterNode(nodeID)
		d.Close(nodeID)

		nodes, err := d.TargetedNodes(string(nodeID), proto.TargetMode_EXACT)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nodes[string(nodeID)] {
			t.Error("closed node should not be dispatchable")
		}
	})
}

func TestTargetedNodesUnknownMode(t *testing.T) {
	inv := &inventory.Nodes{}
	dispatcher := NewDispatcher[string, string](inv)

	_, err := dispatcher.TargetedNodes("test", proto.TargetMode_UNKNOWN)
	if err == nil {
		t.Error("Expected error for unknown target mode")
	}
}
