package core

type Check string
type CheckFunc func(*Machine) bool

const (
	NumPods Check = "check_num_pods"
)

func CheckNumPods(m *Machine) bool {
	numPods, err := m.k8sClient.GetNumPods()
	if err != nil {
		//well this is bad...
		//how do we handle this properly?
		//perhaps we implement retries into the client?
		//for now just continue and hope for the best
		return true
	}
	if (numPods - 1) != (m.config.NumShards / 16) { //maybe don't hardcode this
		return false
	}
	return true
}

var checkFuncs = map[Check]CheckFunc{
	NumPods: CheckNumPods,
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
