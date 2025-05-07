package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"pluralkit/manager/internal/etcd"
	"pluralkit/manager/internal/k8s"
	"sync"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// TODO: document this type.
type State string

// TODO: document this type.
type Event string

// TODO: document this type.
type StateFunc func(*Machine) Event

const (
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

// TODO: document this struct.
type GatewayConfig struct {
	NumShards     int
	PodDefinition string
}

// TODO: document this struct.
type Machine struct {
	currentState State
	stateFuncs   map[State]StateFunc
	transitions  map[State]map[Event]State
	sigChannel   chan os.Signal
	eventChannel chan Event

	etcdClient *etcd.Client
	k8sClient  *k8s.Client
	config     GatewayConfig
}

// TODO: document this function.
func NewController(etcdCli *etcd.Client, k8sCli *k8s.Client, eventChan chan Event) *Machine {
	m := &Machine{
		currentState: Monitor,
		stateFuncs:   make(map[State]StateFunc),
		transitions:  make(map[State]map[Event]State),
		sigChannel:   make(chan os.Signal, 1),

		eventChannel: eventChan,
		etcdClient:   etcdCli,
		k8sClient:    k8sCli,
	}

	ctxGet, cancelGet := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelGet()
	val, err := etcdCli.Get(ctxGet, "current_state")
	if err != nil {
		slog.Info("[control] current state does not exist in etcd")
	} else if State(val) != Shutdown {
		m.currentState = State(val)
	}

	m.stateFuncs = map[State]StateFunc{
		Monitor:  MonitorState,
		Rollout:  RolloutState,
		Deploy:   DeployState,
		Degraded: DegradedState,
		Rollback: RollbackState,
		Shutdown: ShutdownState,
	}

	m.transitions = map[State]map[Event]State{
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
			EventError:   Degraded,
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

	val, err = etcdCli.Get(ctxGet, "gateway_config")
	if err != nil {
		slog.Info("[control] gateway config does not exist in etcd")
		m.config = GatewayConfig{}
	} else {
		var config GatewayConfig
		err = json.Unmarshal([]byte(val), &config)
		if err != nil {
			slog.Error("[control] error while parsing config! ", slog.Any("error", err))
		} else {
			m.config = config
		}
	}

	signal.Notify(m.sigChannel, syscall.SIGTERM, syscall.SIGINT)

	return m
}

// TODO: document this function.
func (m *Machine) SendEvent(event Event) {
	m.eventChannel <- event
}

// TODO: document this function.
func (m *Machine) SetConfig(configStr []byte) error {
	var config GatewayConfig

	err := json.Unmarshal(configStr, &config)
	if err != nil {
		slog.Warn("[control] error while unmarshaling config", slog.Any("error", err))
		return err
	}

	m.config = config

	err = m.etcdClient.Put(context.Background(), "gateway_config", string(configStr))
	if err != nil {
		slog.Warn("[api] error while putting config", slog.Any("error", err))
		return err
	}

	return nil
}

// TODO: document this function.
func (m *Machine) GetConfig(configStr []byte) ([]byte, error) {
	jsonStr, err := json.Marshal(m.config)
	if err != nil {
		return nil, err
	}
	return jsonStr, nil
}

// TODO: document this function.
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

// TODO: document this function.
func (m *Machine) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	ctx := context.Background()

	for m.currentState != Shutdown {
		slog.Info("[control] running", slog.String("state", string(m.currentState)))

		//save our current state to etcd
		//we should probably check for errors here?
		//but then what do we do? we rely heavily on etcd for safety, should we shutdown?
		m.etcdClient.Put(ctx, "current_state", string(m.currentState))

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
		m.etcdClient.Put(ctx, "last_event", string(event))

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
	m.etcdClient.Put(ctx, "current_state", string(Shutdown))
}

// TODO: document this function.
func MonitorState(m *Machine) Event {
	//TODO: check health here
	//loop here, just make sure to exit if a new event is recieved
	//(or sigterm)
	//we should also probably check if a gateway instance isn't healthy because of an issue on discord's end here
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

		//first check that we have a config
		if m.config != (GatewayConfig{}) {
			ev, _ := RunChecks(m)
			if !ev {
				return EventNotHealthy
			}
		}

		time.Sleep(10 * time.Second) //TODO: don't hardcode this
	}
}

// TODO: document this function.
func RolloutState(m *Machine) Event {
	//TODO: implement rollout commands
	//wait until rollout complete before returning
	//should also check for sigterm here, but if we get a sigterm here, that could be problematic?
	//maybe check number of redeploys -- if it's over a certain amount, something is very broken
	return EventHealthy
}

// TODO: document this function.
func DeployState(m *Machine) Event {
	var pod corev1.Pod
	slog.Info("config:", slog.Any("val", m.config))
	err := json.Unmarshal([]byte(m.config.PodDefinition), &pod)
	if err != nil {
		slog.Error("[control] error while parsing config!", slog.Any("error", err))
	}

	//begin deployment!
	numReplicas := m.config.NumShards / 16
	for i := range numReplicas {
		pod.Name = fmt.Sprintf("pluralkit-gateway-%d", i)
		pod.ObjectMeta.Labels["created-by"] = "pluralkit-gateway_manager" //for now put this here, this should be moved to the k8s client
		_, err := m.k8sClient.CreatePod(&pod)                             //we don't really have a use for the created pod objects? we need to re-get them when fetching status so
		if err != nil {
			return EventError
		}
		time.Sleep(50 * time.Millisecond) //sleep a short amount of time, just in case
	}

	//TODO: make this non-blocking so we can recieve events in the meantime?
	err = m.k8sClient.WaitForReady(numReplicas, 8*time.Minute) //TODO: don't hardcode timeout values
	if err != nil {
		return EventError
	}

	return EventHealthy
}

// TODO: document this function.
func DegradedState(m *Machine) Event {
	return ""
}

// TODO: document this function.
func RollbackState(m *Machine) Event {
	return ""
}

// TODO: document this function.
func ShutdownState(m *Machine) Event {
	//TODO: cleanup anything we need to before shutdown
	return ""
}
