package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	var resp *http.Response
	var err error
	retries := 3
	for retries > 0 {
		resp, err = client.Do(req)
		if err != nil {
			retries -= 1
		} else {
			break
		}
	}
	if err != nil {
		return err
	}
	resp.Body.Close()

	return nil
}

func (m *Machine) generatePod(index int, config *GatewayConfig) (*corev1.Pod, error) {
	if config == nil {
		return nil, errors.New("config not set")
	}

	var pod corev1.Pod
	err := json.Unmarshal(config.PodDefinition, &pod)
	if err != nil {
		return nil, err
	}

	pod.Name = fmt.Sprintf("pluralkit-gateway-%s-%d", config.RevisionID, index)

	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Labels["app"] = "pluralkit-gateway"
	pod.Annotations["gateway-index"] = strconv.Itoa(index)
	pod.Annotations["revision-id"] = config.RevisionID

	pod.Spec.Hostname = pod.Name
	pod.Spec.Subdomain = "gw-svc"
	for i := 0; i < len(pod.Spec.Containers); i++ {
		if pod.Spec.Containers[i].Env == nil {
			pod.Spec.Containers[i].Env = make([]corev1.EnvVar, 0)
		}
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env,
			corev1.EnvVar{
				Name:  "pluralkit__discord__cluster__total_shards",
				Value: strconv.Itoa(config.NumShards),
			},
			corev1.EnvVar{
				Name:  "pluralkit__discord__cluster__total_nodes",
				Value: strconv.Itoa(config.NumClusters),
			},
			corev1.EnvVar{
				Name:  "pluralkit__discord__max_concurrency",
				Value: strconv.Itoa(m.config.MaxConcurrency),
			},
			corev1.EnvVar{
				Name:  "pluralkit__runtime_config_key",
				Value: "gateway-k8s",
			},
			corev1.EnvVar{
				Name:  "pluralkit__manager_url",
				Value: "pluralkit-manager.pluralkit-gateway.svc.cluster.local:5020", //TODO: don't hardcode this
			},
		)
	}

	return &pod, nil
}

func (m *Machine) ensureService(ctx context.Context) error {
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
	_, err := m.k8sClient.CreateService(ctx, service)
	if err != nil && !k8sErrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (m *Machine) EnsureProxy(ctx context.Context) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pluralkit-gateway-proxy",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "pluralkit-gateway-proxy",
			},
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     5000,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	_, err := m.k8sClient.CreateService(ctx, service)
	if err != nil && !k8sErrors.IsAlreadyExists(err) {
		return err
	}

	headless_service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pluralkit-gateway-proxy-headless",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "pluralkit-gateway-proxy",
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
	_, err = m.k8sClient.CreateService(ctx, headless_service)
	if err != nil && !k8sErrors.IsAlreadyExists(err) {
		return err
	}

	proxy_envs := []corev1.EnvVar{
		{
			Name:  "pluralkit__discord__max_concurrency",
			Value: strconv.Itoa(m.config.MaxConcurrency),
		},
		{
			Name:  "pluralkit__sentry_url",
			Value: m.config.ProxySentryURL,
		},
	}

	proxy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "pluralkit-gateway-proxy",
			Labels: map[string]string{"app": "pluralkit-gateway-proxy"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &m.config.ProxyReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "pluralkit-gateway-proxy"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "pluralkit-gateway-proxy"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "gateway-proxy",
							Image: m.config.ProxyImage,
							Env:   proxy_envs,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 5000,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
	}

	_, err = m.k8sClient.CreateDeployment(ctx, proxy)
	if err != nil && !k8sErrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func updateCacheEndpoint(ctx context.Context, m *Machine, index int, endpoint string) error {
	k8endpoints, err := m.k8sClient.GetServiceEndpoints(ctx, "pluralkit-gateway-proxy-headless")
	if err != nil {
		return err
	}

	cli := http.Client{Timeout: 3 * time.Second}
	var target string
	var req *http.Request
	for _, es := range k8endpoints.Items {
		for _, e := range es.Endpoints {
			target = fmt.Sprintf("http://%s:5000/endpoints/%d", e.Addresses[0], index)
			req, _ = http.NewRequest("POST", target, strings.NewReader(endpoint))
			req.Header.Set("Content-Type", "application/json")

			var resp *http.Response
			retries := 3
			for retries > 0 {
				resp, err = cli.Do(req)
				if err != nil {
					retries -= 1
				} else {
					break
				}
			}
			if err != nil {
				return err
			}
			resp.Body.Close()
		}
	}

	return nil
}

func updateCacheEndpointBulk(ctx context.Context, m *Machine, endpoints EndpointsConfig) error {
	k8endpoints, err := m.k8sClient.GetServiceEndpoints(ctx, "pluralkit-gateway-proxy-headless")
	if err != nil {
		return err
	}

	data, err := json.Marshal(endpoints)
	if err != nil {
		return err
	}

	cli := http.Client{Timeout: 3 * time.Second}
	var target string
	var req *http.Request
	for _, es := range k8endpoints.Items {
		for _, e := range es.Endpoints {
			target = fmt.Sprintf("http://%s:5000/endpoints", e.Addresses[0])
			req, _ = http.NewRequest("PATCH", target, bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")

			var resp *http.Response
			retries := 3
			for retries > 0 {
				resp, err = cli.Do(req)
				if err != nil {
					retries -= 1
				} else {
					break
				}
			}
			if err != nil {
				return err
			}
			resp.Body.Close()
		}
	}

	return nil
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
			if m.gwConfig.Cur != nil && m.gwConfig.Cur.NumShards != 0 && m.gwConfig.Cur.PodDefinition != nil {
				ev, failures := RunChecks(m)
				if !ev {
					m.logger.Error("failed one or more health checks!", slog.Any("failures", failures))
					return EventNotHealthy
				}
			}
		}
	}
}

type TraversalOrder string

const (
	Forwards  TraversalOrder = "forwards"
	Backwards TraversalOrder = "backwards"
)

func checkPause(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func rollout(ctx context.Context, m *Machine, curConfig *GatewayConfig, nextConfig *GatewayConfig, order TraversalOrder) error {
	numClusters := curConfig.NumClusters
	startIndex := 0
	if order == Backwards {
		startIndex = (numClusters - 1)
	}

	status, err := m.etcdClient.Get(ctx, "rollout_status")
	if err != nil {
		m.logger.Warn("rollout state does not exist in etcd!")
		status = "starting"
	}
	resume := !(status == "starting" || status == "done" || status == "error")
	if resume {
		startIndexStr, err := m.etcdClient.Get(ctx, "rollout_index")
		if err != nil {
			m.logger.Warn("error while getting rollout index")
		} else {
			startIndex, err = strconv.Atoi(startIndexStr)
			if err != nil {
				m.logger.Warn("error while parsing rollout index")
			}
		}
	} else {
		status = "starting"
	}

	pods, err := m.k8sClient.GetPods(ctx, "app=pluralkit-gateway")
	numPods := len(pods.Items)
	if err != nil {
		return err
	}
	if numPods != numClusters && numPods != numClusters+1 {
		return errors.New("unexpected number of pods")
	}

	if !CheckPodNames(ctx, m) {
		return errors.New("incorrect pod names, rollout cannot proceed")
	}

	if nextConfig.RevisionID == "" {
		nextConfig.RevisionID = GenerateRandomID()
		for curConfig.RevisionID == nextConfig.RevisionID {
			nextConfig.RevisionID = GenerateRandomID() //ensure our uid is not the same, despite very very small odds
		}
	}

	//update our config
	m.confMu.Lock()
	if m.gwConfig.Next != nil && !resume {
		m.gwConfig.Prev = curConfig
		m.gwConfig.Cur = nextConfig
		m.gwConfig.Next = nil
	}
	m.confMu.Unlock()
	m.saveConfigToEtcd()

	//begin rollout!
	m.status.RolloutTotal = &numClusters
	httpClient := http.Client{Timeout: 3 * time.Second}
	var oldPod, newPod, target string
	for i := startIndex; i < numClusters; i++ {
		m.etcdClient.Put(ctx, "rollout_index", strconv.Itoa(i))
		m.status.RolloutIDX = &i
		m.logger.Info("rolling out", slog.Int("rollout_index", i), slog.Int("num_replicas", numClusters))

		oldPod = fmt.Sprintf("pluralkit-gateway-%s-%d", curConfig.RevisionID, i)
		pod, err := m.generatePod(i, nextConfig)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			return err
		}
		newPod = pod.Name

		newPodExists, err := m.k8sClient.PodExists(ctx, newPod)
		if err != nil {
			return err
		}
		oldPodExists, err := m.k8sClient.PodExists(ctx, oldPod)
		if err != nil {
			return err
		}
		if newPodExists && oldPodExists {
			status = "switching_old"
		} else if newPodExists {
			status = "switching_new"
		}

		m.etcdClient.Put(ctx, "rollout_status", "creating")
		switch status {
		case "starting", "creating":
			m.logger.Info("creating pod", slog.String("pod", newPod))
			if pause := checkPause(ctx); pause {
				return ErrPaused
			}
			_, err = m.k8sClient.CreatePod(ctx, pod)
			if err != nil {
				m.etcdClient.Put(ctx, "rollout_status", "error")
				return err
			}

			m.etcdClient.Put(ctx, "rollout_status", "waiting")
			fallthrough

		case "waiting":
			m.logger.Info("waiting for ready", slog.String("old_pod", oldPod), slog.String("new_pod", newPod))
			if pause := checkPause(ctx); pause {
				return ErrPaused
			}
			err = m.k8sClient.WaitForReady(ctx, []string{newPod}, m.config.EventWaitTimeout)
			if err != nil {
				m.etcdClient.Put(ctx, "rollout_status", "error")
				m.k8sClient.DeletePod(ctx, newPod)
				return err
			}

			m.etcdClient.Put(ctx, "rollout_status", "switching_old")
			fallthrough

		case "switching_old":
			m.logger.Info("switching event target on old pod")
			if pause := checkPause(ctx); pause {
				return ErrPaused
			}
			target = fmt.Sprintf("http://%s.%s:5000", oldPod, pod.Spec.Subdomain)
			err = changeEventTarget(httpClient, target, "")
			if err != nil {
				m.etcdClient.Put(ctx, "rollout_status", "error")
				m.k8sClient.DeletePod(ctx, newPod)
				return err
			}

			m.etcdClient.Put(ctx, "rollout_status", "switching_new")
			fallthrough

		case "switching_new":
			m.logger.Info("switching event target on new pod")
			if pause := checkPause(ctx); pause {
				return ErrPaused
			}
			target = fmt.Sprintf("http://%s.%s.%s:5000", newPod, pod.Spec.Subdomain, pod.Namespace)
			updateCacheEndpoint(ctx, m, i, target)

			err = changeEventTarget(httpClient, target, m.config.EventTarget)
			if err != nil {
				m.etcdClient.Put(ctx, "rollout_status", "error")
				m.k8sClient.DeletePod(ctx, newPod)
				return err
			}

			m.etcdClient.Put(ctx, "rollout_status", "deleting")
			fallthrough

		case "deleting":
			if !oldPodExists {
				m.logger.Info("deleting pod", slog.String("pod", oldPod))
				if pause := checkPause(ctx); pause {
					return ErrPaused
				}
				err = m.k8sClient.DeletePod(ctx, oldPod)
				if err != nil {
					if resume {
						continue
					}
					m.etcdClient.Put(ctx, "rollout_status", "error")
					return err
				}
				err = m.k8sClient.WaitForDeleted(ctx, []string{oldPod}, m.config.EventWaitTimeout)
				if err != nil {
					if resume {
						continue
					}
					m.etcdClient.Put(ctx, "rollout_status", "error")
					return err
				}
			}

			resume = false
			m.etcdClient.Put(ctx, "rollout_status", "running")
			time.Sleep(50 * time.Millisecond) //sleep a short amount of time, just in case
		}
	}

	m.etcdClient.Put(ctx, "rollout_status", "done")
	m.status.RolloutIDX = nil
	m.status.RolloutTotal = nil

	return nil
}

// TODO: document this function.
func RolloutState(m *Machine) Event {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if m.gwConfig.Next == nil || m.gwConfig.Cur == nil {
		m.logger.Error("config is not set!")
		return EventError
	}

	if m.gwConfig.Cur.NumShards != m.gwConfig.Next.NumShards {
		m.logger.Error("shard count in config is different from current deployment!")
		return EventError
	}

	go func() {
		for {
			select {
			case sig := <-m.sigChannel:
				m.logger.Warn("os signal recieved in rollout!", slog.String("signal", sig.String()))
				cancel()
			case cmd := <-m.eventChannel:
				if cmd == EventPause {
					cancel()
				}
			}
		}
	}()

	err := rollout(ctx, m, m.gwConfig.Cur, m.gwConfig.Next, Forwards)
	if err != nil {
		m.logger.Error("error in rollout", slog.Any("error", err))
		if err == ErrPaused {
			return EventPause
		}
		return EventError
	}

	return EventHealthy
}

func deploy(ctx context.Context, m *Machine, config *GatewayConfig) error {
	if config == nil {
		return errors.New("config is nil")
	}

	//delete all pluralkit-gateway pods created by the manager in the namespace, if they exist and wait
	pods, err := m.k8sClient.GetPods(ctx, "app=pluralkit-gateway")
	if err != nil {
		return err
	}
	if len(pods.Items) > 0 {
		err = m.k8sClient.DeleteAllPods(ctx, "app=pluralkit-gateway")
		if err != nil {
			return err
		}
	}

	//make sure the service exists
	err = m.ensureService(ctx)
	if err != nil {
		return err
	}

	//make sure the proxy exists
	err = m.EnsureProxy(ctx)
	if err != nil {
		return err
	}

	//update our config
	m.confMu.Lock()
	m.gwConfig.Prev = m.gwConfig.Cur
	m.gwConfig.Cur = config
	m.gwConfig.Next = nil
	m.confMu.Unlock()
	m.saveConfigToEtcd()

	m.makeShardsSlice()
	podNames := make([]string, config.NumClusters)
	endpoints := make(map[int]string, config.NumClusters)
	for i := range config.NumClusters {
		pod, err := m.generatePod(i, config)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			return err
		}

		_, err = m.k8sClient.CreatePod(ctx, pod)
		if err != nil {
			return err
		}
		podNames[i] = pod.Name
		endpoints[i] = fmt.Sprintf("http://%s.%s.%s:5000", pod.Spec.Hostname, pod.Spec.Subdomain, pod.Namespace)
		time.Sleep(50 * time.Millisecond) //sleep a short amount of time
	}

	m.logger.Info("waiting for pods to be ready")
	err = m.k8sClient.WaitForReady(ctx, podNames, m.config.EventWaitTimeout)
	if err != nil {
		return err
	}

	m.logger.Info("updating cache endpoints")
	endpointsConfig := EndpointsConfig{
		Endpoints: endpoints,
		NumShards: config.NumShards,
	}
	err = updateCacheEndpointBulk(ctx, m, endpointsConfig)
	if err != nil {
		return err
	}

	m.logger.Info("setting event targets")
	httpClient := http.Client{Timeout: 3 * time.Second}
	for _, val := range endpoints {
		err = changeEventTarget(httpClient, val, m.config.EventTarget)
		if err != nil {
			return err
		}
	}

	return nil
}

// TODO: document this function.
func DeployState(m *Machine) Event {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if m.gwConfig.Next == nil {
		m.logger.Error("config is not set!")
		return EventError
	}

	m.gwConfig.Next.RevisionID = GenerateRandomID()

	err := deploy(ctx, m, m.gwConfig.Next)
	if err != nil {
		m.logger.Error("error in deploy", slog.Any("error", err))
		return EventError
	}

	return EventHealthy
}

// TODO: document this function.
func RollbackState(m *Machine) Event {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if m.gwConfig.Cur == nil || m.gwConfig.Prev == nil {
		m.logger.Error("config is not set!")
		return EventError
	}

	if m.gwConfig.Prev.NumShards != m.gwConfig.Cur.NumShards {
		m.logger.Error("shard count in config is different from current deployment!")
		return EventError
	}

	go func() {
		for {
			select {
			case sig := <-m.sigChannel:
				m.logger.Warn("os signal recieved in rollback!", slog.String("signal", sig.String()))
				cancel()
			case cmd := <-m.eventChannel:
				if cmd == EventPause {
					cancel()
				}
			}
		}
	}()

	err := rollout(ctx, m, m.gwConfig.Cur, m.gwConfig.Prev, Backwards)
	if err != nil {
		m.logger.Error("error in rollback", slog.Any("error", err))
		if err == ErrPaused {
			return EventPause
		}
		return EventError
	}

	return EventHealthy
}

// TODO: document this function.
func DegradedState(m *Machine) Event {
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
			if m.gwConfig.Cur != nil && m.gwConfig.Cur.NumShards != 0 && m.gwConfig.Cur.PodDefinition != nil {
				ev, failures := RunChecks(m)
				if !ev {
					m.logger.Warn("failed one or more health checks!", slog.Any("failures", failures))
					continue
				} else {
					return EventHealthy
				}
			}
		}
	}
}

func PausedState(m *Machine) Event {
	for {
		select {
		case sig := <-m.sigChannel:
			m.logger.Warn("os signal recieved while paused!", slog.String("signal", sig.String()))
			return EventSigterm
		case cmd := <-m.eventChannel:
			return cmd
		}
	}
}

// TODO: document this function.
func ShutdownState(m *Machine) Event {
	m.saveConfigToEtcd()
	return ""
}
