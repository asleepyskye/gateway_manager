package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	err := json.Unmarshal(m.nextGWConfig.PodDefinition, &pod)
	if err != nil {
		m.logger.Error("error while parsing config!", slog.Any("error", err))
		return EventError
	}

	if m.numShards != m.nextGWConfig.NumShards {
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
	numReplicas := m.nextGWConfig.NumShards / m.config.MaxConcurrency
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
				m.logger.Error("error while creating pods in rollout!", slog.Any("error", err))
				return EventError
			}
		}

		m.etcdClient.Put(ctx, "rollout_status", "waiting")
		m.logger.Info("waiting for ready", slog.String("old_pod", oldPod), slog.String("new_pod", pod.Name))
		err = m.k8sClient.WaitForReady(ctx, []string{pod.Name}, m.config.EventWaitTimeout)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while waiting for pod in rollout!", slog.Any("error", err))
			m.k8sClient.DeletePod(pod.Name)
			return EventError
		}

		target := m.cacheEndpoints[i]
		err = changeEventTarget(httpClient, target, "")
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while deleting old runtime_config!", slog.Any("err", err))
			m.k8sClient.DeletePod(pod.Name)
			return EventError //TODO: should we return error here??
		}

		m.cacheEndpoints[i] = fmt.Sprintf("http://%s.%s:5000", pod.Spec.Hostname, pod.Spec.Subdomain)

		target = m.cacheEndpoints[i]
		err = changeEventTarget(httpClient, target, m.config.EventTarget)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while setting runtime_config!", slog.Any("err", err))
			m.k8sClient.DeletePod(pod.Name)
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
			m.logger.Error("error while deleting old pod!", slog.Any("err", err))
			return EventError
		}
		err = m.k8sClient.WaitForDeleted(ctx, []string{oldPod}, m.config.EventWaitTimeout)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while waiting for old pod to be deleted!", slog.Any("err", err))
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
	err := json.Unmarshal(m.nextGWConfig.PodDefinition, &pod)
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
			m.logger.Error("error while deleting pods in deploy!", slog.Any("err", err))
			return EventError
		}
		err = m.k8sClient.WaitForDeleted(ctx, pods, m.config.EventWaitTimeout)
		if err != nil {
			m.logger.Error("error while waiting for pods to be deleted in deploy!", slog.Any("err", err))
			return EventError
		}
	}

	//make sure the service exists
	_, err = m.k8sClient.CreateService(service)
	if err != nil && !k8sErrors.IsAlreadyExists(err) {
		return EventError
	}

	//begin deployment!
	numReplicas := m.nextGWConfig.NumShards / m.config.MaxConcurrency
	m.shardStatus = make([]ShardState, m.nextGWConfig.NumShards)
	m.cacheEndpoints = make([]string, numReplicas)
	podNames := make([]string, numReplicas)
	for i := range numReplicas {
		pod.Name = fmt.Sprintf("pluralkit-gateway-%s-%d", uid, i)
		pod.Spec.Hostname = pod.Name
		pod.Spec.Subdomain = "gw-svc"

		_, err := m.k8sClient.CreatePod(&pod)
		if err != nil {
			m.logger.Error("error while creating pod in deploy!", slog.Any("err", err))
			return EventError
		}
		podNames[i] = pod.Name

		m.cacheEndpoints[i] = fmt.Sprintf("http://%s.%s:5000", pod.Spec.Hostname, pod.Spec.Subdomain)
		time.Sleep(50 * time.Millisecond) //sleep a short amount of time, just in case
	}

	err = m.k8sClient.WaitForReady(ctx, podNames, m.config.EventWaitTimeout)
	if err != nil {
		m.logger.Error("error while waiting for pods to be ready in deploy!", slog.Any("err", err))
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

	m.numShards = m.nextGWConfig.NumShards
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
