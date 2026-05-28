package inventory

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/serializer"
)

var ErrNodeAlreadyRegistered = errors.New("node already registered")
var ErrNodeAlreadyCandidate = errors.New("node already known but not yet accepted")
var ErrNodeNotFound = errors.New("unknown node")
var ErrNodeRejected = errors.New("rejected node")

type RogueNodeError struct {
	diffs []diff
}

func (e *RogueNodeError) Error() string {
	return fmt.Sprintf("potential rogue node detected: diff between rogue and existing node: %+v", e.diffs)
}

type NodeState struct {
	Connected bool
	Since     time.Time
	LastMsg   time.Time
	specs     map[string]any
}

func NewNodeState() NodeState {
	return NodeState{
		specs: make(map[string]any),
	}
}

type NodeIdentity struct {
	ID          node.ID
	Address     string
	Certificate string
}

type registry struct {
	Accepted map[node.ID]NodeIdentity
	States   map[node.ID]NodeState

	// Nodes which have been rejected manually
	Rejected []NodeIdentity

	// candidates are nodes which have not been registered yet.
	candidates []NodeIdentity
}

type Nodes struct {
	mutex                *sync.Mutex
	registry             registry
	registryPath         string
	registryFileDisabled bool
}

func New() Nodes {
	return Nodes{
		mutex:                &sync.Mutex{},
		registryPath:         config.RegistryFileName,
		registryFileDisabled: false,
		registry: registry{
			Accepted: make(map[node.ID]NodeIdentity),
			States:   make(map[node.ID]NodeState),
		},
	}
}

// LoadRegistry loads the registry file from disk if it exists.
func (n *Nodes) LoadRegistry() error {
	if n.registryFileDisabled {
		return nil
	}
	return n.loadRegistryFile()
}

// DisableRegistryFile disables registry file persistence for testing purposes.
func (n *Nodes) DisableRegistryFile() {
	n.registryFileDisabled = true
}

func (n *Nodes) loadRegistryFile() error {
	data, err := os.ReadFile(n.registryPath)
	if err != nil {
		return err
	}
	return serializer.JSON.Unmarshal(data, &n.registry)
}

func (n *Nodes) saveRegistryFile() error {
	if n.registryFileDisabled {
		return nil
	}

	data, err := serializer.JSON.Marshal(n.registry)
	if err != nil {
		return err
	}

	return os.WriteFile(n.registryPath, data, 0600)
}

func (n *Nodes) List() ([]NodeIdentity, []NodeIdentity, []NodeIdentity, map[node.ID]NodeState) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	accepted := make([]NodeIdentity, 0, len(n.registry.Accepted))
	for _, nd := range n.registry.Accepted {
		accepted = append(accepted, nd)
	}
	candidates := slices.Clone(n.registry.candidates)
	rejected := slices.Clone(n.registry.Rejected)
	states := maps.Clone(n.registry.States)

	return accepted, candidates, rejected, states
}

func (n *Nodes) AddCandidate(nd NodeIdentity) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if slices.Contains(n.registry.candidates, nd) {
		return ErrNodeAlreadyCandidate
	}

	if _, rejected := n.isRejected(nd); rejected {
		return ErrNodeRejected
	}
	n.registry.candidates = append(n.registry.candidates, nd)
	return nil
}

func (n *Nodes) removeCandidate(nd NodeIdentity) error {
	for i, c := range n.registry.candidates {
		if c == nd {
			n.registry.candidates = slices.Delete(n.registry.candidates, i, i+1)
			return nil
		}
	}
	return ErrNodeNotFound
}

// TODO: is it still needed?
func (n *Nodes) RemoveCandidate(nd NodeIdentity) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	return n.removeCandidate(nd)
}

func (n *Nodes) Register(nd NodeIdentity, allowRejected bool) error {
	slog.Debug("registering node", "node", nd.ID)
	n.mutex.Lock()
	defer n.mutex.Unlock()

	existing, ok := n.registry.Accepted[nd.ID]
	if ok {
		diffs := Compare(existing, nd)
		if len(diffs) == 0 {
			return ErrNodeAlreadyRegistered
		}
		return &RogueNodeError{diffs}
	}

	candidateIndex, isCandidate := n.isCandidate(nd)
	rejectedIndex, isRejected := n.isRejected(nd)

	if !isCandidate && !isRejected {
		return ErrNodeNotFound
	}

	if isCandidate && isRejected {
		// in theory it should not happen
		return errors.New("node is both candidate and rejected")
	}

	if isCandidate {
		n.registry.candidates = slices.Delete(n.registry.candidates, candidateIndex, candidateIndex+1)
	}

	if isRejected {
		if !allowRejected {
			return errors.New("cannot register: node is rejected")
		}
		n.registry.Rejected = slices.Delete(n.registry.Rejected, rejectedIndex, rejectedIndex+1)
	}

	n.registry.Accepted[nd.ID] = nd
	if err := n.saveRegistryFile(); err != nil {
		slog.Error("unable to permanently register node", "error", err)
	}

	return nil
}

func (n *Nodes) unregister(nd NodeIdentity) error {
	// cleaning the states (ignore if not found, as in some cases in can be absent)
	changed := false
	for id := range n.registry.States {
		if id == nd.ID {
			delete(n.registry.States, id)
			changed = true
			break
		}
	}

	// mandatory registry cleaning
	found := false
	for name, registered := range n.registry.Accepted {
		if registered == nd {
			delete(n.registry.Accepted, name)
			changed = true
			found = true
			break
		}
	}

	if changed {
		if err := n.saveRegistryFile(); err != nil {
			return fmt.Errorf("unable to permanently remove node states: %w", err)
		}
	}

	if !found {
		return ErrNodeNotFound
	}

	return nil
}

func (n *Nodes) Unregister(nd NodeIdentity) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	return n.unregister(nd)
}

func (n *Nodes) Reject(nd NodeIdentity) error {
	slog.Debug("reject request received", "node", nd.ID)
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if ok := n.isRegistered(nd); ok {
		if err := n.unregister(nd); err != nil {
			return fmt.Errorf("reject did not unregister: %w", err)
		}
	}

	if indexToDelete, ok := n.isCandidate(nd); ok {
		n.registry.candidates = slices.Delete(n.registry.candidates, indexToDelete, indexToDelete+1)
	}

	n.registry.Rejected = append(n.registry.Rejected, nd)

	slog.Debug("node rejected", "node", nd.ID)
	if err := n.saveRegistryFile(); err != nil {
		return fmt.Errorf("unable to permanently reject node: %w", err)
	}
	return nil
}

func (n *Nodes) Remove(nd NodeIdentity) error {
	slog.Debug("remove request received", "node", nd.ID)
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if registered := n.isRegistered(nd); registered {
		if err := n.unregister(nd); err != nil {
			return fmt.Errorf("remove did not unregister: %w", err)
		}
	}

	if candidateIndex, isCandidate := n.isCandidate(nd); isCandidate {
		n.registry.candidates = slices.Delete(n.registry.candidates, candidateIndex, candidateIndex+1)
	}

	if rejectedIndex, isRejected := n.isRejected(nd); isRejected {
		n.registry.Rejected = slices.Delete(n.registry.Rejected, rejectedIndex, rejectedIndex+1)
	}

	slog.Debug("node removed", "node", nd.ID)
	if err := n.saveRegistryFile(); err != nil {
		return fmt.Errorf("unable to permanently remove node: %w", err)
	}
	return nil
}

func (n *Nodes) MarkNodeActive(id node.ID) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	state, ok := n.registry.States[id]
	if !ok {
		n.registry.States[id] = NewNodeState()
		state = n.registry.States[id]
	}

	state.LastMsg = time.Now()
	n.registry.States[id] = state
}

func (n *Nodes) MarkNodeStateChange(id node.ID, connected bool) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	state, ok := n.registry.States[id]
	if !ok {
		n.registry.States[id] = NewNodeState()
		state = n.registry.States[id]
	}

	state.Connected = connected
	state.Since = time.Now()
	n.registry.States[id] = state
}

func (n *Nodes) GetSpec(id node.ID) map[string]any {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	state, ok := n.registry.States[id]
	if !ok {
		return nil
	}

	return state.specs
}

func (n *Nodes) GetAllSpecs() map[node.ID]map[string]any {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	out := make(map[node.ID]map[string]any, len(n.registry.States))
	for k, v := range n.registry.States {
		out[k] = maps.Clone(v.specs)
	}

	return out
}

func (n *Nodes) SetSpec(id node.ID, specs map[string]any) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if specs == nil {
		return nil
	}

	state, ok := n.registry.States[id]
	if !ok {
		return fmt.Errorf("node not connected: %s %p", string(id), n)
	}

	state.specs = specs
	n.registry.States[id] = state

	return nil
}
