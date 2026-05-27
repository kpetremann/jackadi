package inventory

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kpetremann/jackadi/internal/node"
)

// TODO:
// - TestAccept
// - TestGetMatchingXXX
// - TestRegister
// - TestUnregister
// - TestReject
// - TestRemove

func TestAddCandidate(t *testing.T) {
	tests := []struct {
		name          string
		nd            NodeIdentity
		inventory     []NodeIdentity
		want          []NodeIdentity
		expectedError error
	}{
		{
			name:      "empty list",
			nd:        NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			inventory: []NodeIdentity{},
			want: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			},
			expectedError: nil,
		},
		{
			name: "node is not known",
			nd:   NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			inventory: []NodeIdentity{
				{ID: node.ID("node2"), Address: "127.0.0.2", Certificate: "certificate2"},
			},
			want: []NodeIdentity{
				{ID: node.ID("node2"), Address: "127.0.0.2", Certificate: "certificate2"},
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			},
		},
		{
			name: "node is already known",
			nd:   NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			inventory: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
				{ID: node.ID("node2"), Address: "127.0.0.2", Certificate: "certificate1"},
			},
			want: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
				{ID: node.ID("node2"), Address: "127.0.0.2", Certificate: "certificate1"},
			},
			expectedError: ErrNodeAlreadyCandidate,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nodes := New()
			nodes.DisableRegistryFile()
			nodes.registry.candidates = test.inventory
			err := nodes.AddCandidate(test.nd)

			if diff := cmp.Diff(nodes.registry.candidates, test.want); diff != "" {
				t.Errorf("Mismatch for '%s' test:\n%s", test.name, diff)
			}
			if !errors.Is(err, test.expectedError) {
				t.Errorf("Mismatch error: wants '%s', got test:\n%s", test.expectedError, err)
			}
		})
	}
}

func TestRemoveCandidates(t *testing.T) {
	tests := []struct {
		name          string
		nd            NodeIdentity
		inventory     []NodeIdentity
		want          []NodeIdentity
		expectedError error
	}{
		{
			name:          "empty list",
			nd:            NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			inventory:     []NodeIdentity{},
			want:          []NodeIdentity{},
			expectedError: ErrNodeNotFound,
		},
		{
			name: "list with only one node - matching",
			nd:   NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			inventory: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			},
			want:          []NodeIdentity{},
			expectedError: nil,
		},
		{
			name: "list with only one node - not matching",
			nd:   NodeIdentity{ID: node.ID("node2"), Address: "127.0.0.1", Certificate: "certificate1"},
			inventory: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			},
			want: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			},
			expectedError: ErrNodeNotFound,
		},
		{
			name: "list with multiple nodes - matching",
			nd:   NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			inventory: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
				{ID: node.ID("node1"), Address: "127.0.0.2", Certificate: "certificate1"},
				{ID: node.ID("node3"), Address: "127.0.0.3", Certificate: "certificate1"},
				{ID: node.ID("node4"), Address: "127.0.0.4", Certificate: "certificate1"},
			},
			want: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.2", Certificate: "certificate1"},
				{ID: node.ID("node3"), Address: "127.0.0.3", Certificate: "certificate1"},
				{ID: node.ID("node4"), Address: "127.0.0.4", Certificate: "certificate1"},
			},
			expectedError: nil,
		},
		{
			name: "list with multiple nodes - not matching",
			nd:   NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.3", Certificate: "certificate1"},
			inventory: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
				{ID: node.ID("node1"), Address: "127.0.0.2", Certificate: "certificate1"},
				{ID: node.ID("node3"), Address: "127.0.0.3", Certificate: "certificate1"},
				{ID: node.ID("node4"), Address: "127.0.0.4", Certificate: "certificate1"},
			},
			want: []NodeIdentity{
				{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
				{ID: node.ID("node1"), Address: "127.0.0.2", Certificate: "certificate1"},
				{ID: node.ID("node3"), Address: "127.0.0.3", Certificate: "certificate1"},
				{ID: node.ID("node4"), Address: "127.0.0.4", Certificate: "certificate1"},
			},
			expectedError: ErrNodeNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nodes := New()
			nodes.DisableRegistryFile()
			nodes.registry.candidates = test.inventory
			err := nodes.RemoveCandidate(test.nd)

			if diff := cmp.Diff(nodes.registry.candidates, test.want); diff != "" {
				t.Errorf("Mismatch for '%s' test:\n%s", test.name, diff)
			}
			if !errors.Is(err, test.expectedError) {
				t.Errorf("Mismatch error: wants '%s', got test:\n%s", test.expectedError, err)
			}
		})
	}
}

func TestRegister(t *testing.T) {
	nd := NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1"}

	t.Run("from candidate", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)

		if err := nodes.Register(nd, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !nodes.IsRegistered(nd) {
			t.Error("node should be registered")
		}
		_, candidates, _, _ := nodes.List()
		if len(candidates) != 0 {
			t.Errorf("candidate should be removed after registration, got %d", len(candidates))
		}
	})

	t.Run("already registered", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)
		_ = nodes.Register(nd, false)

		if err := nodes.Register(nd, false); !errors.Is(err, ErrNodeAlreadyRegistered) {
			t.Errorf("expected ErrNodeAlreadyRegistered, got %v", err)
		}
	})

	t.Run("not a candidate", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()

		if err := nodes.Register(nd, false); !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("expected ErrNodeNotFound, got %v", err)
		}
	})

	t.Run("rejected node allowRejected=false", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)
		_ = nodes.Reject(nd)

		if err := nodes.Register(nd, false); err == nil {
			t.Error("expected error registering a rejected node")
		}
		if nodes.IsRegistered(nd) {
			t.Error("rejected node should not be registered")
		}
	})

	t.Run("rejected node allowRejected=true", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)
		_ = nodes.Reject(nd)

		if err := nodes.Register(nd, true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !nodes.IsRegistered(nd) {
			t.Error("node should be registered after allowRejected=true")
		}
	})
}

func TestReject(t *testing.T) {
	nd := NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1"}

	t.Run("from candidate", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)

		if err := nodes.Reject(nd); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nodes.IsRegistered(nd) {
			t.Error("rejected node should not be registered")
		}
		_, candidates, rejected, _ := nodes.List()
		if len(candidates) != 0 {
			t.Errorf("candidates should be empty, got %d", len(candidates))
		}
		if len(rejected) != 1 {
			t.Errorf("expected 1 rejected node, got %d", len(rejected))
		}
	})

	t.Run("from accepted", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)
		_ = nodes.Register(nd, false)

		if err := nodes.Reject(nd); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nodes.IsRegistered(nd) {
			t.Error("node should no longer be registered after rejection")
		}
		_, _, rejected, _ := nodes.List()
		if len(rejected) != 1 {
			t.Errorf("expected 1 rejected node, got %d", len(rejected))
		}
	})

	t.Run("candidate cannot re-add after rejection", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)
		_ = nodes.Reject(nd)

		if err := nodes.AddCandidate(nd); !errors.Is(err, ErrNodeRejected) {
			t.Errorf("expected ErrNodeRejected, got %v", err)
		}
	})
}

func TestIsRegistered(t *testing.T) {
	nd := NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1"}

	t.Run("false before registration", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		if nodes.IsRegistered(nd) {
			t.Error("should not be registered")
		}
	})

	t.Run("true after registration", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)
		_ = nodes.Register(nd, false)
		if !nodes.IsRegistered(nd) {
			t.Error("should be registered")
		}
	})

	t.Run("false after rejection", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)
		_ = nodes.Register(nd, false)
		_ = nodes.Reject(nd)
		if nodes.IsRegistered(nd) {
			t.Error("should not be registered after rejection")
		}
	})

	t.Run("different address is not registered", func(t *testing.T) {
		nodes := New()
		nodes.DisableRegistryFile()
		_ = nodes.AddCandidate(nd)
		_ = nodes.Register(nd, false)

		other := NodeIdentity{ID: nd.ID, Address: "10.0.0.1"}
		if nodes.IsRegistered(other) {
			t.Error("node with different address should not be considered registered")
		}
	})
}

func TestMarkNodeStateChange(t *testing.T) {
	nd := NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1"}

	nodes := New()
	nodes.DisableRegistryFile()
	_ = nodes.AddCandidate(nd)
	_ = nodes.Register(nd, false)

	nodes.MarkNodeStateChange(nd.ID, true)
	_, _, _, states := nodes.List()
	if !states[nd.ID].Connected {
		t.Error("node should be connected")
	}

	nodes.MarkNodeStateChange(nd.ID, false)
	_, _, _, states = nodes.List()
	if states[nd.ID].Connected {
		t.Error("node should be disconnected")
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name  string
		node1 NodeIdentity
		node2 NodeIdentity
		want  []diff
	}{
		{
			name:  "same node",
			node1: NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			node2: NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			want:  []diff{},
		},
		{
			name:  "different ID",
			node1: NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			node2: NodeIdentity{ID: node.ID("node2"), Address: "127.0.0.1", Certificate: "certificate1"},
			want: []diff{
				{"ID", node.ID("node1"), node.ID("node2")},
			},
		},
		{
			name:  "different address",
			node1: NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			node2: NodeIdentity{ID: node.ID("node1"), Address: "::1", Certificate: "certificate1"},
			want: []diff{
				{"address", "127.0.0.1", "::1"},
			},
		},
		{
			name:  "different certificate",
			node1: NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			node2: NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate2"},
			want: []diff{
				{"certificate", "hidden", "hidden"},
			},
		},
		{
			name:  "completely different",
			node1: NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1", Certificate: "certificate1"},
			node2: NodeIdentity{ID: node.ID("node2"), Address: "::1", Certificate: "certificate2"},
			want: []diff{
				{"ID", node.ID("node1"), node.ID("node2")},
				{"address", "127.0.0.1", "::1"},
				{"certificate", "hidden", "hidden"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diffs := Compare(test.node1, test.node2)

			if diff := cmp.Diff(diffs, test.want); diff != "" {
				t.Errorf("Mismatch for '%s' test:\n%s", test.name, diff)
			}
		})
	}
}
