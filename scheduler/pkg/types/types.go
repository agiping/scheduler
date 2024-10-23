package types

import (
	"net/http"
)

type Pod struct {
	IP               string // IP address of the pod: ip:port
	RejectStateless  bool   // Whether the pod rejects stateless requests
	NumberOfRequests int    // Number of requests handled by the pod
	TgiQueueSize     int    // The queue size of the TGI instance
	OwnerService     string // The service that owns the pod
}

type InferRequest struct {
	*http.Request
	SessionID string
	RequestID string
	// prompt length
	// currently, we just use the length of input string for prompt length.
	// we will support input tokens later.
	PromptLength int
}
