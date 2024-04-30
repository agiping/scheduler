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

type SysPrompt struct {
	ID    string `json:"id"`
	Input string `json:"input"`
}

type History struct {
	ID     string `json:"id"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

type Cur struct {
	ID    string `json:"id"`
	Input string `json:"input"`
}

type Parameters struct {
	RepetitionPenalty float64 `json:"repetition_penalty"`
	Temperature       float64 `json:"temperature"`
	TopK              int     `json:"top_k"`
	TopP              float64 `json:"top_p"`
	MaxNewTokens      int     `json:"max_new_tokens"`
	DoSample          bool    `json:"do_sample"`
	Seed              int     `json:"seed"`
	Details           bool    `json:"details"`
}

type StructInput struct {
	SessionID string    `json:"session_id"`
	SysPrompt SysPrompt `json:"sys_prompt"`
	History   []History `json:"history"`
	Cur       Cur       `json:"cur"`
}

type RequestBody struct {
	StructInput StructInput `json:"struct_input"`
	Inputs      string      `json:"inputs"`
	Parameters  Parameters  `json:"parameters"`
}

type InferRequest struct {
	*http.Request
	Body      RequestBody
	RequestID string
}
