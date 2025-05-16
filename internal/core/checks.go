package core

import "time"

type Check string
type CheckFunc func(*Machine) bool

const (
	NumPods   Check = "check_num_pods"
	Health    Check = "check_health"
	Network   Check = "check_network"
	Heartbeat Check = "check_heartbeat"
)

// check that we have the correct number of pods
func CheckNumPods(m *Machine) bool {
	numPods, err := m.k8sClient.GetNumPods()
	if err != nil {
		return false
	}
	if numPods != (*m.GetNumShards() / m.config.MaxConcurrency) {
		return false
	}
	return true
}

// check that all pods are healthy
func CheckHealth(m *Machine) bool {
	// TODO
	return true
}

// check that we can contact each pod
func CheckNetwork(m *Machine) bool {
	// TODO
	return true
}

// check that each cluster has heartbeated recently with at half the shards (maybe make this all?)
func CheckHeartbeat(m *Machine) bool {
	numClusters := *m.GetNumShards() / m.config.MaxConcurrency
	shardStatus := m.GetShardStatus()
	for c := range numClusters {
		numHeartbeated := 0
		for s := range m.config.MaxConcurrency {
			shard := shardStatus[(c*m.config.MaxConcurrency)+s]
			ht := time.Unix(int64(shard.LastHeartbeat), 0)
			if time.Now().Before(ht.Add(time.Duration(5) * time.Minute)) {
				numHeartbeated++
			}
		}
		if numHeartbeated < int(0.5*float64(m.config.MaxConcurrency)) {
			return false
		}
	}
	return true
}

var checkFuncs = map[Check]CheckFunc{
	NumPods:   CheckNumPods,
	Health:    CheckHealth,
	Network:   CheckNetwork,
	Heartbeat: CheckHeartbeat,
}

func RunChecks(m *Machine) (bool, []Check) {
	failures := []Check{}

	for name, f := range checkFuncs {
		if !f(m) {
			failures = append(failures, name)
		}
	}

	if len(failures) > 0 {
		return false, failures
	}
	return true, failures
}
