package inventory

import "github.com/kpetremann/jackadi/internal/node"

func (n *Nodes) GetMatchingAccepted(id node.ID, address, certificate *string) []NodeIdentity {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	accepted := []NodeIdentity{}
	for _, candidate := range n.registry.Accepted {
		if candidate.ID != id {
			continue
		}
		if address != nil && candidate.Address != *address {
			continue
		}
		if certificate != nil && candidate.Certificate != *certificate {
			continue
		}
		accepted = append(accepted, candidate)
	}

	return accepted
}

func (n *Nodes) GetMatchingCandidates(id node.ID, address, certificate *string) []NodeIdentity {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	candidates := []NodeIdentity{}
	for _, candidate := range n.registry.candidates {
		if candidate.ID != id {
			continue
		}
		if address != nil && candidate.Address != *address {
			continue
		}
		if certificate != nil && candidate.Certificate != *certificate {
			continue
		}
		candidates = append(candidates, candidate)
	}

	return candidates
}

func (n *Nodes) GetMatchingRejected(id node.ID, address, certificate *string) []NodeIdentity {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	rejected := []NodeIdentity{}
	for _, candidate := range n.registry.Rejected {
		if candidate.ID != id {
			continue
		}
		if address != nil && candidate.Address != *address {
			continue
		}
		if certificate != nil && candidate.Certificate != *certificate {
			continue
		}
		rejected = append(rejected, candidate)
	}

	return rejected
}
