package policy

import (
	"net/http"
)

// LoadBalancingPolicy is an interface for load balancing policies.
type LoadBalancingPolicy interface {
	SetReadyReplicas(replicas []string)
	SelectReplica(request *http.Request) *string
}
