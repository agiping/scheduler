package policy

// LoadBalancingPolicy is an interface for load balancing policies.
type LoadBalancingPolicy interface {
	SetReadyReplicas(replicas []interface{})
	SelectReplica(request []interface{}) interface{}
}
