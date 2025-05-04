package core

import (
	"log/slog"
	"time"
)

type State string
const (
	Startup  State = "startup"
	Monitor        = "monitor"
	Rollout        = "rollout"
	Deploy         = "deploy"
	Degraded       = "degraded"
	Shutdown       = "shutdown"
)

type StateFunc func(*Machine)

type Machine struct {
	currentState State
	stateFunc StateFunc
}

func NewMachine() *Machine {
	return &Machine {
		currentState: Startup,
		stateFunc: StartupState,
	}
}

func (m *Machine) Run() {
	slog.Info("[control] running", m.currentState)

	m.stateFunc(m)
	//TODO: next state logic lol

	time.Sleep(10 * time.Second) //TODO: don't hardcode this, maybe don't sleep here, sleep in monitor?
}

func StartupState(m *Machine) {
	return
}