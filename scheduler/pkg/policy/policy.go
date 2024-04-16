package policy

import (
	"net/http"
)

// Request is a placeholder for the HTTP request.
// We may need to modify this to suit the HTTP framework we are using.
type Request struct {
	Method      string
	URL         string
	Headers     map[string]string
	QueryParams map[string]string
}

// LoadBalancingPolicy is an interface for load balancing policies.
type LoadBalancingPolicy interface {
	SetReadyReplicas(replicas []string)
	SelectReplica(request *http.Request) *string
}
