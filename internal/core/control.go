package core

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type State string
type Event string
type StateFunc func(*Machine) Event

const (
	Startup  State = "startup"
	Monitor  State = "monitor"
	Rollout  State = "rollout"
	Rollback State = "rollback"
	Deploy   State = "deploy"
	Degraded State = "degraded"
	Shutdown State = "shutdown"
)

const (
	EventOk            Event = "ok"
	EventHealthy       Event = "healthy"
	EventNotHealthy    Event = "not_healthy"
	EventRolloutCmd    Event = "rollout_command"
	EventDeployCmd     Event = "deploy_command"
	EventError         Event = "error"
	EventRecoverable   Event = "recoverable"
	EventUnrecoverable Event = "unrecoverable"
	EventSigterm       Event = "sigterm"
)

type Machine struct {
	currentState State
	stateFuncs   map[State]StateFunc
	transitions  map[State]map[Event]State
	sigChannel   chan os.Signal
	eventChannel chan Event
}

func NewMachine() *Machine {
	m := &Machine{
		currentState: Startup,
		stateFuncs:   make(map[State]StateFunc),
		transitions:  make(map[State]map[Event]State),
		sigChannel:   make(chan os.Signal, 1),
		eventChannel: make(chan Event, 1),
	}

	m.stateFuncs = map[State]StateFunc{
		Startup: StartupState,
		Monitor: MonitorState,
		// Rollout:  RolloutState,
		// Deploy:   DeployState,
		// Degraded: DegradedState,
		// Rollback: RollbackState,
		// Shutdown: ShutdownState,
	}

	m.transitions = map[State]map[Event]State{
		Startup: {
			EventOk: Monitor,
		},
		Monitor: {
			EventHealthy:    Monitor,
			EventNotHealthy: Degraded,
			EventRolloutCmd: Rollout,
			EventDeployCmd:  Deploy,
			EventSigterm:    Shutdown,
		},
		Rollout: {
			EventHealthy: Monitor,
			EventError:   Degraded,
			EventSigterm: Shutdown,
		},
		Deploy: {
			EventHealthy: Monitor,
			EventSigterm: Shutdown,
		},
		Degraded: {
			EventHealthy:       Monitor,
			EventRecoverable:   Rollback,
			EventUnrecoverable: Deploy,
			EventError:         Degraded,
			EventSigterm:       Shutdown,
		},
		Rollback: {
			EventOk:      Monitor,
			EventError:   Degraded,
			EventSigterm: Shutdown,
		},
	}

	signal.Notify(m.sigChannel, syscall.SIGTERM, syscall.SIGINT)

	return m
}

func (m *Machine) SendEvent(event Event) {
	m.eventChannel <- event
}

func (m *Machine) lookupTransition(event Event) (State, bool) {
	//prioritize Sigterm -- if it has a sigterm state, process that first
	//is there a better way to do this?
	if event == EventSigterm {
		if nextState, ok := m.transitions[m.currentState][EventSigterm]; ok {
			return nextState, true
		}
	}

	//lookup the current state in the transitions table
	stateTransitions, ok := m.transitions[m.currentState]
	if !ok {
		return "", false
	}
	//get the next state from the current state
	nextState, ok := stateTransitions[event]
	if !ok {
		return "", false
	}

	return nextState, true
}

func (m *Machine) Run() {
	for m.currentState != Shutdown {
		slog.Info("[control] running", slog.String("state", string(m.currentState)))

		//get the next state
		stateHandler, ok := m.stateFuncs[m.currentState]
		if !ok {
			//if we have an error while getting the state function, that's a big problem, safely shutdown
			slog.Error("[control] state error", slog.String("state", string(m.currentState)))
			m.currentState = Shutdown
			continue
		}

		//execute the next state
		event := stateHandler(m)

		//lookup our next state
		nextState, ok := m.lookupTransition(event)
		if !ok {
			slog.Warn("[control] No transition defined",
				slog.String("from_state", string(m.currentState)),
				slog.String("event", string(event)))
			continue
			//no transition defined from our current state, so we're just gonna stay here and hope for the best!
			//this could be problematic if we reach here -- we should ensure our transitions are complete
		}

		slog.Info("[control] transitioning",
			slog.String("from_state", string(m.currentState)),
			slog.String("event", string(event)),
			slog.String("to_state", string(nextState)))
		m.currentState = nextState

		time.Sleep(1 * time.Second) //sleep for a second just to prevent transitions from being too fast, can probably remove this safely?
	}
}

func StartupState(m *Machine) Event {
	//TODO: actually do the startup stuff
	return EventOk
}

func MonitorState(m *Machine) Event {
	//TODO: check health here
	//loop here, just make sure to exit if a new event is recieved
	//(or sigterm)
	//we should also probably check if a gateway instance isn't healthy because of an issue on discord's end here
	// -> otherwise we could end up in a redeploy loop
	for {
		//is there some better way to time this so we still get events?
		select {
		case sig := <-m.sigChannel:
			slog.Warn("[state] os signal recieved while monitoring!", slog.String("signal", sig.String()))
			return EventSigterm
		case cmd := <-m.eventChannel:
			return cmd
		default:
		}

		slog.Debug("[control] checking cluster health")
		time.Sleep(10 * time.Second) //TODO: don't hardcode this
	}
}

func RolloutState(m *Machine) Event {
	//TODO: implement rollout commands
	//wait until rollout complete before returning
	//should also check for sigterm here, but if we get a sigterm here, that could be problematic?
	//maybe check number of redeploys -- if it's over a certain amount, something is very broken
	return EventHealthy
}

func ShutdownState(m *Machine) Event {
	//TODO: cleanup anything we need to before shutdown
	return ""
}
