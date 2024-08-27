package types

import (
	"net/http"
)

type Pod struct {
	IP               string // IP address of the pod: ip:port
	RejectStateless  bool   // Whether the pod rejects stateless requests
	NumberOfRequests int    // Number of requests handled by the pod
	TgiQueueSize     int    // The queue size of the TGI instance
}

type InferRequest struct {
	*http.Request
	SessionID string
	RequestID string
}
