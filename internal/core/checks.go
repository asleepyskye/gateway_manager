package core

import (
	"context"
	"net/http"
	"strings"
	"time"
)

type Check string
type CheckFunc func(*Machine) bool

const (
	NumPods       Check = "check_num_pods"
	HealthNetwork Check = "check_health_network"
	Heartbeat     Check = "check_heartbeat"
	PodNames      Check = "check_pod_names"
)

// check that we have the correct number of pods
func CheckNumPods(m *Machine) bool {
	numPods, err := m.k8sClient.GetNumPods()
	if err != nil {
		return false
	}
	if numPods != (m.GetNumShards() / m.config.MaxConcurrency) {
		return false
	}
	return true
}

// check that we can contact each pod
func CheckHealthNetwork(m *Machine) bool {
	client := http.Client{}
	for _, v := range m.cacheEndpoints {
		target := v + "/up"
		req, _ := http.NewRequest("GET", target, nil)
		resp, err := client.Do(req)
		if err != nil {
			return false
		} else if resp.StatusCode != 200 {
			return false
		}
		resp.Body.Close()
	}
	return true
}

// check that each cluster has heartbeated recently with at half the shards (maybe make this all?)
func CheckHeartbeat(m *Machine) bool {
	numClusters := m.gwConfig.Cur.NumShards / m.config.MaxConcurrency
	for c := range numClusters {
		numHeartbeated := 0
		for s := range m.config.MaxConcurrency {
			shard := m.shardStatus[(c*m.config.MaxConcurrency)+s]
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

// check that each pod has the expected UID (check that all pods match iter)
func CheckPodNames(m *Machine) bool {
	pods, err := m.k8sClient.GetAllPodsNames()
	if err != nil {
		return false
	}
	expectedUID, err := m.etcdClient.Get(context.Background(), "current_uid")
	if err != nil {
		return false
	}
	for _, val := range pods {
		if !strings.Contains(val, expectedUID) {
			return false
		}
	}
	return true
}

var checkFuncs = map[Check]CheckFunc{
	NumPods:       CheckNumPods,
	HealthNetwork: CheckHealthNetwork,
	Heartbeat:     CheckHeartbeat,
	PodNames:      CheckPodNames,
}

// helper function to run all checks
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
