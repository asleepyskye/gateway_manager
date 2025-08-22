package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"pluralkit/manager/internal/etcd"
	"pluralkit/manager/internal/k8s"
	"strconv"
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
	Paused   State = "paused"
	Shutdown State = "shutdown"
)

const (
	EventOk         Event = "ok"
	EventHealthy    Event = "healthy"
	EventNotHealthy Event = "not_healthy"
	EventRolloutCmd Event = "rollout_command"
	EventDeployCmd  Event = "deploy_command"
	EventError      Event = "error"
	EventRollback   Event = "rollback"
	EventPause      Event = "pause"
	EventResume     Event = "resume"
	EventSigterm    Event = "sigterm"
)

// helper struct for a gateway config
type GatewayConfig struct {
	NumShards     int
	NumClusters   int
	PodDefinition json.RawMessage
	RevisionID    string
}

type GatewayConfigPatch struct {
	NumShards     int
	PodDefinition json.RawMessage
}

type VersionedGatewayConfig struct {
	Prev *GatewayConfig
	Cur  *GatewayConfig
	Next *GatewayConfig
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

	confMu   sync.RWMutex
	statMu   sync.RWMutex
	config   ManagerConfig
	gwConfig VersionedGatewayConfig
	status   ManagerStatus
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

		eventChannel: eventChan,
		etcdClient:   etcdCli,
		k8sClient:    k8sCli,
	}

	m.stateFuncs = map[State]StateFunc{
		Monitor:  MonitorState,
		Rollout:  RolloutState,
		Deploy:   DeployState,
		Degraded: DegradedState,
		Paused:   PausedState,
		Rollback: RollbackState,
		Shutdown: ShutdownState,
	}

	m.transitions = map[State]map[Event]State{
		Monitor: {
			EventHealthy:    Monitor,
			EventNotHealthy: Degraded,
			EventRolloutCmd: Rollout,
			EventDeployCmd:  Deploy,
			EventRollback:   Rollback,
			EventSigterm:    Shutdown,
		},
		Rollout: {
			EventHealthy:  Monitor,
			EventError:    Degraded,
			EventRollback: Rollback,
			EventSigterm:  Shutdown,
			EventPause:    Degraded,
		},
		Deploy: {
			EventHealthy:  Monitor,
			EventError:    Degraded,
			EventRollback: Rollback,
			EventSigterm:  Shutdown,
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
		Paused: {
			EventResume:     Rollout,
			EventRolloutCmd: Rollout,
			EventRollback:   Rollback,
			EventDeployCmd:  Deploy,
			EventSigterm:    Shutdown,
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
			m.confMu.Lock()
			m.gwConfig = config
			m.confMu.Unlock()
		}
	}

	if m.gwConfig.Cur != nil {
		m.makeShardsSlice()
	}

	signal.Notify(m.sigChannel, syscall.SIGTERM, syscall.SIGINT)

	return m
}

// sends an event to the state machine through the event channel
func (m *Machine) SendEvent(event Event) {
	m.eventChannel <- event
}

func (m *Machine) saveConfigToEtcd() error {
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

// sets the next gateway config for use on next deploy/rollout
func (m *Machine) SetConfig(configStr []byte) error {
	config := GatewayConfigPatch{}

	if m.currentState != Monitor && m.currentState != Degraded {
		return errors.New("cannot set config in current state")
	}

	err := json.Unmarshal(configStr, &config)
	if err != nil {
		m.logger.Warn("error while unmarshaling config", slog.Any("error", err))
		return err
	}

	m.confMu.Lock()
	m.gwConfig.Next = &GatewayConfig{}
	m.gwConfig.Next.NumShards = config.NumShards
	m.gwConfig.Next.PodDefinition = config.PodDefinition
	m.gwConfig.Next.NumClusters = (config.NumShards + m.config.MaxConcurrency - 1) / m.config.MaxConcurrency
	m.confMu.Unlock()

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
func (m *Machine) GetCurrentConfig() *GatewayConfig {
	m.confMu.RLock()
	defer m.confMu.RUnlock()
	return m.gwConfig.Cur
}

// returns the specified next gw config
func (m *Machine) GetNextConfig() *GatewayConfig {
	m.confMu.RLock()
	defer m.confMu.RUnlock()
	return m.gwConfig.Next
}

// returns the specified prev gw config
func (m *Machine) GetPrevConfig() *GatewayConfig {
	m.confMu.RLock()
	defer m.confMu.RUnlock()
	return m.gwConfig.Prev
}

// returns the current number of shards
func (m *Machine) GetNumShards() int {
	m.confMu.RLock()
	defer m.confMu.RUnlock()
	return m.gwConfig.Cur.NumShards
}

// returns the current status of manager
func (m *Machine) GetStatus() ManagerStatus {
	m.statMu.RLock()
	defer m.statMu.RUnlock()
	return m.status
}

// returns the current proxy endpoints
func (m *Machine) GetEndpoints() (map[int]string, error) {
	if m.gwConfig.Cur == nil {
		return make(map[int]string, 0), nil
	}
	ctx := context.Background()
	endpoints := make(map[int]string, m.gwConfig.Cur.NumClusters)
	pods, err := m.k8sClient.GetPods(ctx, "app=pluralkit-gateway")
	if err != nil {
		return endpoints, err
	}

	for _, pod := range pods.Items {
		revision := pod.Annotations["revision-id"]
		index, err := strconv.Atoi(pod.Annotations["gateway-index"])
		if err != nil {
			return endpoints, err
		}

		target := fmt.Sprintf("http://%s.%s.%s:5000", pod.Spec.Hostname, pod.Spec.Subdomain, pod.Namespace)

		switch m.currentState {
		case Rollout:
			if m.gwConfig.Prev == nil {
				return endpoints, errors.New("prev config not set")
			}
			//just switch to the new pod now to avoid race conditions -- we shouldn't really be here anyways
			if revision == m.gwConfig.Prev.RevisionID && index == m.status.RolloutIDX {
				continue
			}
		case Rollback:
			if revision == m.gwConfig.Cur.RevisionID && index == m.status.RolloutIDX {
				continue
			}
		}

		endpoints[index] = target
	}
	return endpoints, nil
}

// updates/patches the status of a given shard
func (m *Machine) UpdateShardStatus(status ShardState) error {
	//seemingly the easiest way to 'patch' a struct?
	statJson, err := json.Marshal(status)
	if err != nil {
		return err
	}
	m.statMu.Lock()
	defer m.statMu.Unlock()
	err = json.Unmarshal(statJson, &(m.status.Shards[status.ShardID]))
	if err != nil {
		return err
	}
	return nil
}

func (m *Machine) makeShardsSlice() {
	m.statMu.Lock()
	defer m.statMu.Unlock()
	m.status.Shards = make([]ShardState, m.gwConfig.Cur.NumShards)
	for i := 0; i < m.gwConfig.Cur.NumShards; i++ {
		m.status.Shards[i].ShardID = int32(i)
		m.status.Shards[i].ClusterID = int32(i / m.config.MaxConcurrency)
	}
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
		m.status.Status = string(m.currentState)

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
