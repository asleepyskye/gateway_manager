package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"pluralkit/manager/internal/etcd"
	"pluralkit/manager/internal/k8s"
	"sync"
	"syscall"
	"time"
)

// type for state machine states
type State string

// type for state machine events
type Event string

// type for state machine functions
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

// helper struct for a gateway config
type GatewayConfig struct {
	NumShards     int
	PodDefinition json.RawMessage
}

type VersionedGatewayConfig struct {
	Prev GatewayConfig
	Cur  GatewayConfig
	Next GatewayConfig
}

// render helper function for GatewayConfig
func (i *GatewayConfig) Render(w http.ResponseWriter, r *http.Request) error { return nil }

// helper struct for defining the state machine
type Machine struct {
	currentState State
	stateFuncs   map[State]StateFunc
	transitions  map[State]map[Event]State
	sigChannel   chan os.Signal
	eventChannel chan Event
	logger       *slog.Logger
	etcdClient   *etcd.Client
	k8sClient    *k8s.Client

	mu             sync.RWMutex
	config         ManagerConfig
	gwConfig       VersionedGatewayConfig
	cacheEndpoints []string
	shardStatus    []ShardState
}

// helper function for creating a new state machine/controller
func NewController(etcdCli *etcd.Client, k8sCli *k8s.Client, eventChan chan Event, cfg ManagerConfig, logger *slog.Logger) *Machine {
	moduleLogger := logger.With("module", "control")
	m := &Machine{
		currentState: Monitor,
		stateFuncs:   make(map[State]StateFunc),
		transitions:  make(map[State]map[Event]State),
		sigChannel:   make(chan os.Signal, 1),
		config:       cfg,
		logger:       moduleLogger,

		eventChannel:   eventChan,
		etcdClient:     etcdCli,
		k8sClient:      k8sCli,
		cacheEndpoints: make([]string, 0, 256),
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
			EventError:   Rollback,
			EventSigterm: Shutdown,
		},
		Deploy: {
			EventHealthy: Monitor,
			EventError:   Degraded,
			EventSigterm: Shutdown,
		},
		Degraded: {
			EventHealthy:    Monitor,
			EventError:      Degraded,
			EventSigterm:    Shutdown,
			EventRolloutCmd: Rollout,
			EventDeployCmd:  Deploy,
		},
		Rollback: {
			EventOk:      Monitor,
			EventError:   Degraded,
			EventSigterm: Shutdown,
		},
	}

	ctxGet, cancelGet := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelGet()

	val, err := etcdCli.Get(ctxGet, "current_state")
	if err != nil {
		m.logger.Info("current state does not exist in etcd")
	} else if State(val) != Shutdown {
		m.currentState = State(val)
	}

	val, err = etcdCli.Get(ctxGet, "gateway_config")
	if err != nil {
		m.logger.Info("gateway config does not exist in etcd")
		m.gwConfig = VersionedGatewayConfig{}
	} else {
		var config VersionedGatewayConfig
		err = json.Unmarshal([]byte(val), &config)
		if err != nil {
			m.logger.Warn("error while parsing config! ", slog.Any("error", err))
		} else {
			m.mu.Lock()
			m.gwConfig = config
			m.mu.Unlock()
		}
	}

	val, err = etcdCli.Get(ctxGet, "cache_endpoints")
	if err != nil {
		m.logger.Info("cache endpoints does not exist in etcd")
	} else {
		err = json.Unmarshal([]byte(val), &m.cacheEndpoints)
		if err != nil {
			m.logger.Warn("error while parsing cache endpoints!", slog.Any("error", err))
		}
	}

	signal.Notify(m.sigChannel, syscall.SIGTERM, syscall.SIGINT)

	return m
}

// sends an event to the state machine through the event channel
func (m *Machine) SendEvent(event Event) {
	m.eventChannel <- event
}

// sets the next gateway config for use on next deploy/rollout
func (m *Machine) SetConfig(configStr []byte) error {
	var config GatewayConfig

	if m.currentState != Monitor && m.currentState != Degraded {
		return errors.New("cannot set config in current state")
	}

	err := json.Unmarshal(configStr, &config)
	if err != nil {
		m.logger.Warn("error while unmarshaling config", slog.Any("error", err))
		return err
	}

	m.mu.Lock()
	m.gwConfig.Next = config
	m.mu.Unlock()

	conf, err := json.Marshal(m.gwConfig)
	if err != nil {
		m.logger.Warn("error while marshalling config", slog.Any("error", err))
		return err
	}

	err = m.etcdClient.Put(context.Background(), "gateway_config", string(conf))
	if err != nil {
		m.logger.Warn("error while putting config", slog.Any("error", err))
		return err
	}

	return nil
}

// returns the current gw config
func (m *Machine) GetCurrentConfig() GatewayConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gwConfig.Cur
}

// returns the specified next gw config
func (m *Machine) GetNextConfig() GatewayConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gwConfig.Next
}

// returns the current cache endpoints as a pointer/reference
func (m *Machine) GetCacheEndpoint(i int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cacheEndpoints[i]
}

// returns the current number of shards
func (m *Machine) GetNumShards() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gwConfig.Cur.NumShards
}

// returns the current shard states
func (m *Machine) GetShardStatus() []ShardState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shardStatus
}

// updates/patches the status of a given shard
func (m *Machine) UpdateShardStatus(status ShardState) error {
	//seemingly the easiest way to 'patch' a struct?
	statJson, err := json.Marshal(status)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	err = json.Unmarshal(statJson, &(m.shardStatus[status.ShardID]))
	if err != nil {
		return err
	}
	return nil
}

// helper function for looking up the next state given an event
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

// runs the controller
func (m *Machine) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	ctx := context.Background()

	for m.currentState != Shutdown {
		m.logger.Info("running", slog.String("state", string(m.currentState)))

		//save our current state to etcd
		//we should probably check for errors here?
		//but then what do we do? we rely heavily on etcd for safety, should we shutdown?
		m.etcdClient.Put(ctx, "current_state", string(m.currentState))

		//get the next state
		stateHandler, ok := m.stateFuncs[m.currentState]
		if !ok {
			//if we have an error while getting the state function, that's a big problem, safely shutdown
			m.logger.Error("state error", slog.String("state", string(m.currentState)))
			m.currentState = Shutdown
			continue
		}

		//execute the next state
		event := stateHandler(m)
		m.etcdClient.Put(ctx, "last_event", string(event))

		//lookup our next state
		nextState, ok := m.lookupTransition(event)
		if !ok {
			m.logger.Warn("No transition defined",
				slog.String("from_state", string(m.currentState)),
				slog.String("event", string(event)))
			continue
			//no transition defined from our current state, so we're just gonna stay here and hope for the best!
			//this could be problematic if we reach here -- we should ensure our transitions are complete
		}

		m.logger.Info("transitioning",
			slog.String("from_state", string(m.currentState)),
			slog.String("event", string(event)),
			slog.String("to_state", string(nextState)))
		m.currentState = nextState

		time.Sleep(1 * time.Second) //sleep for a second just to prevent transitions from being too fast, can probably remove this safely?
	}
	m.etcdClient.Put(ctx, "current_state", string(Shutdown))
}
