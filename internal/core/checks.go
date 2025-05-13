package core

type Check string
type CheckFunc func(*Machine) bool

const (
	NumPods Check = "check_num_pods"
)

func CheckNumPods(m *Machine) bool {
	numPods, err := m.k8sClient.GetNumPods()
	if err != nil {
		return false
	}
	if numPods != (m.gwConfig.NumShards / 16) { //maybe don't hardcode this
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
