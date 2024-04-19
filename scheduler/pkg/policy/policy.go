package policy

import (
	"net/http"
)

// LoadBalancingPolicy is an interface for load balancing policies.
type LoadBalancingPolicy interface {
	SetReadyReplicas(replicas []interface{})
	SelectReplica(request *http.Request) interface{}
}
