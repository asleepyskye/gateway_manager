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
	"strings"
	"sync"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Repair   State = "repair"
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
	PodDefinition json.RawMessage
}

// TODO: document this struct.
type Machine struct {
	currentState State
	stateFuncs   map[State]StateFunc
	transitions  map[State]map[Event]State
	sigChannel   chan os.Signal
	eventChannel chan Event
	config       ManagerConfig
	logger       *slog.Logger

	etcdClient     *etcd.Client
	k8sClient      *k8s.Client
	gwConfig       GatewayConfig
	cacheEndpoints []string
	numShards      int
	shardStatus    []ShardState
}

// TODO: document this function.
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
		Repair:   RepairState,
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
			EventHealthy:       Monitor,
			EventRecoverable:   Rollback,
			EventUnrecoverable: Deploy,
			EventError:         Degraded,
			EventSigterm:       Shutdown,
			EventRolloutCmd:    Rollout,
			EventDeployCmd:     Deploy,
		},
		Rollback: {
			EventOk:      Monitor,
			EventError:   Degraded,
			EventSigterm: Shutdown,
		},
		Repair: {
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
		m.gwConfig = GatewayConfig{}
	} else {
		var config GatewayConfig
		err = json.Unmarshal([]byte(val), &config)
		if err != nil {
			m.logger.Warn("error while parsing config! ", slog.Any("error", err))
		} else {
			m.gwConfig = config
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

	val, err = etcdCli.Get(ctxGet, "num_shards")
	if err != nil {
		m.logger.Info("num shards does not exist in etcd")
	} else {
		valInt, err := strconv.Atoi(val)
		if err != nil {
			m.logger.Warn("error while parsing num shards!", slog.Any("error", err))
		} else {
			m.numShards = valInt
			m.shardStatus = make([]ShardState, m.numShards)
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

	if m.currentState != Monitor && m.currentState != Degraded {
		return errors.New("cannot set config in current state")
	}

	err := json.Unmarshal(configStr, &config)
	if err != nil {
		m.logger.Warn("error while unmarshaling config", slog.Any("error", err))
		return err
	}

	m.gwConfig = config

	err = m.etcdClient.Put(context.Background(), "gateway_config", string(configStr))
	if err != nil {
		m.logger.Warn("error while putting config", slog.Any("error", err))
		return err
	}

	return nil
}

// TODO: document this function.
func (m *Machine) GetConfig() ([]byte, error) {
	jsonStr, err := json.Marshal(m.gwConfig)
	if err != nil {
		return nil, err
	}
	return jsonStr, nil
}

// TODO: document this function.
func (m *Machine) GetCacheEndpoints() *([]string) {
	return &m.cacheEndpoints
}

// TODO: document this function.
func (m *Machine) GetNumShards() *(int) {
	return &m.numShards
}

func (m *Machine) GetShardStatus() []ShardState {
	return m.shardStatus
}

func (m *Machine) UpdateShardStatus(status ShardState) error {
	//seemingly the easiest way to 'patch' a struct?
	statJson, err := json.Marshal(status)
	if err != nil {
		return err
	}
	err = json.Unmarshal(statJson, &(m.shardStatus[status.ShardID]))
	if err != nil {
		return err
	}
	return nil
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

// TODO: document this function.
func MonitorState(m *Machine) Event {
	ticker := time.NewTicker(m.config.MonitorPeriod)
	defer ticker.Stop()

	for {
		select {
		case sig := <-m.sigChannel:
			m.logger.Warn("os signal recieved while monitoring!", slog.String("signal", sig.String()))
			return EventSigterm
		case cmd := <-m.eventChannel:
			return cmd

		case <-ticker.C:
			m.logger.Debug("checking cluster health")

			//first check that we have a config
			if m.gwConfig.NumShards != 0 && m.gwConfig.PodDefinition != nil {
				ev, failures := RunChecks(m)
				if !ev {
					m.logger.Error("failed one or more health checks!", slog.Any("failures", failures))
					return EventNotHealthy
				}
			}
		}
	}
}

// TODO: document this function
func changeEventTarget(client http.Client, url string, eventTarget string) error {
	target := url + "/runtime_config/event_target"
	var req *http.Request
	if len(eventTarget) > 0 {
		req, _ = http.NewRequest("POST", target, strings.NewReader(eventTarget))
	} else {
		req, _ = http.NewRequest("DELETE", target, nil)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return err
	} else if resp.StatusCode != 302 {
		return errors.New("status code not 302")
	}
	resp.Body.Close()
	return nil
}

// TODO: document this function.
func RolloutState(m *Machine) Event {
	//should also check for sigterm here, but if we get a sigterm here, that could be problematic?
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pod corev1.Pod
	err := json.Unmarshal(m.gwConfig.PodDefinition, &pod)
	if err != nil {
		m.logger.Error("error while parsing config!", slog.Any("error", err))
		return EventError
	}

	if m.numShards != m.gwConfig.NumShards {
		m.logger.Error("shard count in config is different from current deployment!")
		return EventError
	}

	//TODO: inject shard count here?

	startIndex := 0
	status, err := m.etcdClient.Get(ctx, "rollout_status")
	if err != nil {
		m.logger.Warn("rollout state does not exist in etcd!")
		status = "done"
	}
	if status != "done" {
		startIndexStr, err := m.etcdClient.Get(ctx, "rollout_index")
		if err != nil {
			m.logger.Warn("error while getting rollout index!")
		} else {
			startIndex, err = strconv.Atoi(startIndexStr)
			if err != nil {
				m.logger.Warn("error while parsing rollout index!")
			}
		}
	}

	prevUid, err := m.etcdClient.Get(ctx, "current_uid")
	if err != nil {
		m.logger.Error("error while getting pod uid from etcd!")
		return EventError
	}

	uid := ""
	if status != "done" {
		uid, err = m.etcdClient.Get(ctx, "current_rollout_uid")
		if err != nil {
			m.logger.Warn("error while getting current rollout uid!")
		}
	}
	if uid == "" {
		uid = GenerateRandomID()
		for prevUid == uid {
			uid = GenerateRandomID() //ensure our uid is not the same, despite very very small odds
		}
		m.etcdClient.Put(ctx, "current_rollout_uid", uid)
	}

	httpClient := http.Client{}

	//begin rollout!
	numReplicas := m.gwConfig.NumShards / m.config.MaxConcurrency
	oldPod := ""
	for i := startIndex; i < numReplicas; i++ {
		m.etcdClient.Put(ctx, "rollout_index", strconv.Itoa(i))
		m.logger.Info("rolling out", slog.Int("rollout_index", i), slog.Int("num_replicas", numReplicas))

		oldPod = fmt.Sprintf("pluralkit-gateway-%s-%d", prevUid, i)

		pod.Name = fmt.Sprintf("pluralkit-gateway-%s-%d", uid, i)
		pod.Spec.Hostname = pod.Name
		pod.Spec.Subdomain = "gw-svc"
		if status != "waiting" {
			_, err := m.k8sClient.CreatePod(&pod)
			if err != nil {
				m.etcdClient.Put(ctx, "rollout_status", "error")
				return EventError
			}
		}

		m.etcdClient.Put(ctx, "rollout_status", "waiting")
		m.logger.Info("waiting for ready", slog.String("old_pod", oldPod), slog.String("new_pod", pod.Name))
		err = m.k8sClient.WaitForReady(ctx, []string{pod.Name}, m.config.EventWaitTimeout)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			return EventError
		}

		target := m.cacheEndpoints[i]
		err = changeEventTarget(httpClient, target, "")
		if err != nil {
			m.logger.Error("error while deleting old runtime_config!", slog.Any("err", err))
			m.etcdClient.Put(ctx, "rollout_status", "error")
			return EventError //TODO: should we return error here??
		}

		m.cacheEndpoints[i] = fmt.Sprintf("http://%s.%s:5000", pod.Spec.Hostname, pod.Spec.Subdomain)

		target = m.cacheEndpoints[i]
		err = changeEventTarget(httpClient, target, m.config.EventTarget)
		if err != nil {
			m.logger.Error("error while setting runtime_config!", slog.Any("err", err))
			m.etcdClient.Put(ctx, "rollout_status", "error")
			return EventError
		}

		//TODO: do this more efficient
		jsonData, err := json.Marshal(m.cacheEndpoints)
		if err != nil {
			m.logger.Warn("error while marshalling cache endpoints!", slog.Any("err", err))
		}
		m.etcdClient.Put(ctx, "cache_endpoints", string(jsonData))

		err = m.k8sClient.DeletePod(oldPod)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			return EventError
		}
		err = m.k8sClient.WaitForDeleted(ctx, []string{oldPod}, m.config.EventWaitTimeout)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			return EventError
		}

		m.etcdClient.Put(ctx, "rollout_status", "running")
		time.Sleep(50 * time.Millisecond) //sleep a short amount of time, just in case
	}

	m.etcdClient.Put(ctx, "rollout_status", "done")
	m.etcdClient.Put(ctx, "current_uid", uid)
	return EventHealthy
}

// TODO: document this function.
func DeployState(m *Machine) Event {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pod corev1.Pod
	err := json.Unmarshal(m.gwConfig.PodDefinition, &pod)
	if err != nil {
		m.logger.Error("error while parsing config!", slog.Any("error", err))
		return EventError
	}
	uid := GenerateRandomID()
	m.etcdClient.Put(ctx, "current_uid", uid)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gw-svc",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "pluralkit-gateway",
			},
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     5000,
				},
			},
			ClusterIP: corev1.ClusterIPNone,
		},
	}

	//delete all pods created by the manager in the namespace, if they exist and wait
	pods, err := m.k8sClient.GetAllPodsNames()
	if err != nil {
		return EventError
	}
	if len(pods) > 0 {
		err = m.k8sClient.DeleteAllPods()
		if err != nil {
			return EventError
		}
		err = m.k8sClient.WaitForDeleted(ctx, pods, m.config.EventWaitTimeout)
		if err != nil {
			return EventError
		}
	}

	//make sure the service exists
	_, err = m.k8sClient.CreateService(service)
	if err != nil && !k8sErrors.IsAlreadyExists(err) {
		return EventError
	}

	//begin deployment!
	numReplicas := m.gwConfig.NumShards / m.config.MaxConcurrency
	m.shardStatus = make([]ShardState, m.gwConfig.NumShards)
	m.cacheEndpoints = make([]string, numReplicas)
	podNames := make([]string, numReplicas)
	for i := range numReplicas {
		pod.Name = fmt.Sprintf("pluralkit-gateway-%s-%d", uid, i)
		pod.Spec.Hostname = pod.Name
		pod.Spec.Subdomain = "gw-svc"

		_, err := m.k8sClient.CreatePod(&pod)
		if err != nil {
			return EventError
		}
		podNames[i] = pod.Name

		m.cacheEndpoints[i] = fmt.Sprintf("http://%s.%s:5000", pod.Spec.Hostname, pod.Spec.Subdomain)
		time.Sleep(50 * time.Millisecond) //sleep a short amount of time, just in case
	}

	err = m.k8sClient.WaitForReady(ctx, podNames, m.config.EventWaitTimeout)
	if err != nil {
		return EventError
	}

	httpClient := http.Client{}
	for _, val := range m.cacheEndpoints {
		err = changeEventTarget(httpClient, val, m.config.EventTarget)
		if err != nil {
			m.logger.Error("error while setting runtime_config!", slog.Any("err", err))
			return EventError
		}
	}

	jsonData, err := json.Marshal(m.cacheEndpoints)
	if err != nil {
		m.logger.Warn("error while marshalling cache endpoints!", slog.Any("err", err))
	}
	m.etcdClient.Put(ctx, "cache_endpoints", string(jsonData))

	m.numShards = m.gwConfig.NumShards
	m.etcdClient.Put(ctx, "num_shards", strconv.Itoa(m.numShards))
	return EventHealthy
}

// TODO: document this function.
func DegradedState(m *Machine) Event {
	//just for now...
	ticker := time.NewTicker(m.config.MonitorPeriod)
	defer ticker.Stop()

	for {
		select {
		case sig := <-m.sigChannel:
			m.logger.Warn("os signal recieved while degraded!", slog.String("signal", sig.String()))
			return EventSigterm
		case cmd := <-m.eventChannel:
			return cmd

		case <-ticker.C:
			m.logger.Debug("checking cluster health")

			//first check that we have a config
			if m.gwConfig.NumShards != 0 && m.gwConfig.PodDefinition != nil {
				ev, failures := RunChecks(m)
				if !ev {
					m.logger.Warn("failed one or more health checks!", slog.Any("failures", failures))
					//TODO: determine how we get gateway healthy again based on the failed checks
					continue
				} else {
					return EventHealthy
				}
			}
		}
	}
}

// TODO: document this function.
func RollbackState(m *Machine) Event {
	return ""
}

// TODO: document this function.
func RepairState(m *Machine) Event {
	return ""
}

// TODO: document this function.
func ShutdownState(m *Machine) Event {
	//TODO: cleanup anything we need to before shutdown
	return ""
}
