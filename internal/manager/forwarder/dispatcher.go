package forwarder

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/claytonsingh/golib/dotaccess"
	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/manager/inventory"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
)

var ErrNodeNotFound = errors.New("node not found")
var ErrClosedTaskChannel = errors.New("closed task channel")
var ErrTimeout = errors.New("timeout")

type Task[R, A any] struct {
	Request    R
	ResponseCh chan A
}

// Dispatcher keeps in memory the channels of each node.
//
// Each channel is a structure containing a Response channel.
type Dispatcher[R, A any] struct {
	mutex             *sync.RWMutex
	dispatch          map[node.ID]chan Task[R, A]
	dispatchableNodes map[node.ID]bool
	nodesInventory    *inventory.Nodes
}

func NewDispatcher[R, A any](nodesInventory *inventory.Nodes) Dispatcher[R, A] {
	return Dispatcher[R, A]{
		mutex:             &sync.RWMutex{},
		dispatch:          make(map[node.ID]chan Task[R, A]),
		dispatchableNodes: make(map[node.ID]bool),
		nodesInventory:    nodesInventory,
	}
}

// RegisterNode creates a dispatch channel to distribute tasks to the node.
func (d *Dispatcher[R, A]) RegisterNode(nodeID node.ID) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if _, exists := d.dispatch[nodeID]; exists {
		return errors.New("cannot register: duplicate node")
	}
	d.dispatch[nodeID] = make(chan Task[R, A])
	d.dispatchableNodes[nodeID] = true
	return nil
}

// UnregisterNode deletes the dispatch channel assigned to the node.
func (d *Dispatcher[R, A]) UnregisterNode(nodeID node.ID) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	delete(d.dispatch, nodeID)
	d.dispatchableNodes[nodeID] = false
}

// Close closes the dispatch channel assigned to the node.
//
// It does not delete the channel yet, which is done by UnregisterNode.
func (d *Dispatcher[R, A]) Close(nodeID node.ID) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	close(d.dispatch[nodeID])
	d.dispatch[nodeID] = nil
	d.dispatchableNodes[nodeID] = false
}

// Forget removes any reference to an unregistered node.
//
// Jackadi will stop reporting that a disconnected node has been targeted.
func (d *Dispatcher[R, A]) Forget(nodeID node.ID) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if _, ok := d.dispatch[nodeID]; ok {
		return fmt.Errorf("node %s must be unregistered before being forgotten", nodeID)
	}

	delete(d.dispatchableNodes, nodeID)
	return nil
}

func (d *Dispatcher[R, A]) isReady(nodeID node.ID) bool {
	if state, ok := d.dispatchableNodes[nodeID]; ok {
		return state
	}
	return false
}

func (d *Dispatcher[R, A]) Send(nodeID node.ID, task Task[R, A], timeout time.Duration) error {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	ch, ok := d.dispatch[nodeID]
	if !ok {
		return ErrNodeNotFound
	}
	if ch == nil {
		return ErrClosedTaskChannel
	}

	select {
	case ch <- task:
	case <-time.After(timeout):
		return ErrTimeout
	}
	return nil
}

func (d *Dispatcher[R, A]) GetTasksChannel(nodeID node.ID) (chan Task[R, A], error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	ret, ok := d.dispatch[nodeID]
	if !ok {
		return nil, errors.New("node task channel closed")
	}
	return ret, nil
}

// TargetedNodes returns Task channels for all targets.
//
//   - The key in the returned map is the target ID.
//   - The mode in argument enables to filter target using different methods: exact match, list (sep: ','), glob, regex.
//   - For glob filter, please check filepath documentation: https://pkg.go.dev/path/filepath#Match
//   - For regex filter, please check regex documentation: https://pkg.go.dev/regexp
//   - For query filter, check Jackadi documentation
//
// Special note for regex filter: '^' and '$' are enforced to only do strict matching.
func (d *Dispatcher[R, A]) TargetedNodes(target string, mode proto.TargetMode) (map[string]bool, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	switch mode {
	case proto.TargetMode_EXACT:
		return map[string]bool{target: d.isReady(node.ID(target))}, nil

	case proto.TargetMode_LIST:
		return d.listMatching(target)

	case proto.TargetMode_GLOB:
		return d.globMatching(target)

	case proto.TargetMode_REGEX:
		return d.regexMatching(target)

	case proto.TargetMode_QUERY:
		return d.queryMatching(target)

	case proto.TargetMode_UNKNOWN:
		return nil, errors.New("unknown targetmode")
	}
	return nil, fmt.Errorf("not implemented targetMethod: '%s'", mode)
}

func (d *Dispatcher[R, A]) listMatching(list string) (map[string]bool, error) {
	nodes := make(map[string]bool)
	for id := range strings.SplitSeq(list, config.ListSeparator) {
		nodes[id] = d.isReady(node.ID(id))
	}

	if len(nodes) == 0 {
		return nil, errors.New("no connected node is matching with the list")
	}

	return nodes, nil
}

func (d *Dispatcher[R, A]) globMatching(pattern string) (map[string]bool, error) {
	nodes := make(map[string]bool)
	for id, ready := range d.dispatchableNodes {
		matched, err := filepath.Match(pattern, string(id))
		if err != nil {
			return nil, err
		}

		if matched {
			nodes[string(id)] = ready
		}
	}

	if len(nodes) == 0 {
		return nil, errors.New("no connected node is matching with the pattern")
	}

	return nodes, nil
}

func (d *Dispatcher[R, A]) regexMatching(pattern string) (map[string]bool, error) {
	regex, err := regexp.Compile(fmt.Sprintf("^%s$", pattern))
	if err != nil {
		return nil, err
	}

	nodes := map[string]bool{}
	for id, ready := range d.dispatchableNodes {
		if matched := regex.MatchString(string(id)); matched {
			nodes[string(id)] = ready
		}
	}

	if len(nodes) == 0 {
		return nil, errors.New("no connected node is matching with the pattern")
	}

	return nodes, nil
}

// queryMatching evaluates a filter expression and returns matching nodes.
func (d *Dispatcher[R, A]) queryMatching(expr string) (map[string]bool, error) {
	if strings.TrimSpace(expr) == "" {
		return nil, errors.New("empty filter expression")
	}

	result := make(map[string]bool)

	for orGroup := range strings.SplitSeq(expr, " or ") {
		orGroup = strings.TrimSpace(orGroup)
		if orGroup == "" {
			continue
		}

		andResult, err := d.evaluateAndGroup(orGroup)
		if err != nil {
			return nil, fmt.Errorf("OR group %q: %w", orGroup, err)
		}

		// Merge results (OR logic - add all matches)
		maps.Copy(result, andResult)
	}

	if len(result) == 0 {
		return nil, errors.New("no connected node matches the filter")
	}

	return result, nil
}

// evaluateAndGroup processes AND conditions within a group.
func (d *Dispatcher[R, A]) evaluateAndGroup(andGroup string) (map[string]bool, error) {
	conditions := strings.Split(andGroup, " and ")
	candidates := make(map[string]bool, len(d.dispatchableNodes))
	for k, v := range d.dispatchableNodes {
		candidates[string(k)] = v
	}

	// Apply each condition (AND logic)
	for _, condition := range conditions {
		condition = strings.TrimSpace(condition)
		if condition == "" {
			continue
		}

		matched, err := d.evaluateCondition(condition)
		if err != nil {
			return nil, fmt.Errorf("condition %q: %w", condition, err)
		}

		for id := range candidates {
			if _, ok := matched[id]; !ok {
				delete(candidates, id)
			}
		}
	}

	return candidates, nil
}

// evaluateCondition evaluates a single condition like "id==foo" or "specs.os==linux".
func (d *Dispatcher[R, A]) evaluateCondition(condition string) (map[string]bool, error) {
	var field, operator, value string
	var err error
	var matched map[string]bool

	switch {
	case strings.Contains(condition, "=="):
		parts := strings.SplitN(condition, "==", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid == condition: %q", condition)
		}
		field, operator, value = strings.TrimSpace(parts[0]), "==", strings.TrimSpace(parts[1])
	case strings.Contains(condition, "=~"):
		parts := strings.SplitN(condition, "=~", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid =~ condition: %q", condition)
		}
		field, operator, value = strings.TrimSpace(parts[0]), "=~", strings.TrimSpace(parts[1])
	default:
		return nil, fmt.Errorf("unsupported operator in condition: %q", condition)
	}

	switch {
	case field == "id":
		matched, err = d.evaluateIDCondition(operator, value)
		if err != nil {
			return nil, err
		}
	case strings.HasPrefix(field, "specs."):
		matched, err = d.evaluateSpecsCondition(field, operator, value)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported field: %q", field)
	}

	return matched, nil
}

// evaluateIDCondition handles ID-specific matching.
func (d *Dispatcher[R, A]) evaluateIDCondition(operator, value string) (map[string]bool, error) {
	switch operator {
	case "==":
		// Handle list (comma-separated) or single exact match
		return d.listMatching(value)

	case "=~":
		// Check if it's regex (enclosed in slashes) or glob
		if strings.HasPrefix(value, "/") && strings.HasSuffix(value, "/") {
			// Regex pattern
			pattern := value[1 : len(value)-1]
			return d.regexMatching(pattern)
		} else {
			// Glob pattern
			return d.globMatching(value)
		}

	default:
		return nil, fmt.Errorf("unsupported ID operator: %q", operator)
	}
}

// evaluateSpecsCondition handles specs field matching.
func (d *Dispatcher[R, A]) evaluateSpecsCondition(field, operator, value string) (map[string]bool, error) {
	matched := make(map[string]bool)

	// Extract specs path (remove "specs." prefix)
	specPath := strings.TrimPrefix(field, "specs.")
	for nd, ndSpecs := range d.nodesInventory.GetAllSpecs() {
		a, err := dotaccess.NewAccessorDot[any, map[string]any](&ndSpecs, specPath)
		if err != nil {
			continue
		}

		specAny := a.Get()
		if reflect.ValueOf(specAny).Kind() == reflect.Pointer {
			specAny = reflect.ValueOf(specAny).Elem()
		}

		// we only want to compare specs value, so we want only valid leaf.
		//nolint:exhaustive  // we do not support all types
		switch reflect.ValueOf(specAny).Kind() {
		case reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
			reflect.Pointer, reflect.Slice, reflect.Struct, reflect.UnsafePointer:
			slog.Debug("invalid spec", "error", "not a valid leaf", "type", reflect.ValueOf(specAny).Kind())
			continue
		}

		spec := fmt.Sprint(a.Get())
		switch operator {
		case "==":
			if spec == value {
				matched[string(nd)] = d.isReady(nd)
			}

		case "=~":
			// Check if it's regex (enclosed in slashes) or glob
			if strings.HasPrefix(value, "/") && strings.HasSuffix(value, "/") {
				// Regex pattern
				pattern := value[1 : len(value)-1]
				regex, err := regexp.Compile(pattern)
				if err != nil {
					return nil, fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
				}

				if regex.MatchString(spec) {
					matched[string(nd)] = d.isReady(nd)
				}
			} else {
				// Glob pattern
				globMatched, err := filepath.Match(value, spec)
				if err != nil {
					return nil, fmt.Errorf("invalid glob pattern %q: %w", value, err)
				}
				if globMatched {
					matched[string(nd)] = d.isReady(nd)
				}
			}

		default:
			return nil, fmt.Errorf("unsupported specs operator: %q", operator)
		}
	}

	return matched, nil
}
