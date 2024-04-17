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

// NewRequest creates a new instance of Request.
func NewRequest(req *http.Request) *Request {
	headers := make(map[string]string)
	for k, v := range req.Header {
		headers[k] = v[0]
	}

	queryParams := make(map[string]string)
	for k, v := range req.URL.Query() {
		queryParams[k] = v[0]
	}

	return &Request{
		Method:      req.Method,
		URL:         req.URL.String(),
		Headers:     headers,
		QueryParams: queryParams,
	}
}
