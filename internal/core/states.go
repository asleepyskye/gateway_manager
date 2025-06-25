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
			if m.gwConfig.Cur.NumShards != 0 && m.gwConfig.Cur.PodDefinition != nil {
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
// TODO: try again on error?
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

func addEnvs(m *Machine, pod *corev1.Pod) {
	pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "pluralkit__discord__cluster__total_shards",
		Value: strconv.Itoa(m.gwConfig.Cur.NumShards),
	})
	pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "pluralkit__discord__max_concurrency",
		Value: strconv.Itoa(m.config.MaxConcurrency),
	})
	pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "pluralkit__runtime_config_key",
		Value: "gateway-k8s",
	})
}

// TODO: document this function.
func RolloutState(m *Machine) Event {
	// TODO: check for sigterm
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pod corev1.Pod
	err := json.Unmarshal(m.gwConfig.Next.PodDefinition, &pod)
	if err != nil {
		m.logger.Error("error while parsing config!", slog.Any("error", err))
		return EventError
	}
	addEnvs(m, &pod)

	if m.gwConfig.Cur.NumShards != m.gwConfig.Next.NumShards {
		m.logger.Error("shard count in config is different from current deployment!")
		return EventError
	}

	startIndex := 0
	status, err := m.etcdClient.Get(ctx, "rollout_status")
	if err != nil {
		m.logger.Warn("rollout state does not exist in etcd!")
		if !CheckNumPods(m) {
			m.logger.Error("unexpected number of pods!")
			return EventError
		}
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
			expectedNum := (m.gwConfig.Cur.NumShards / m.config.MaxConcurrency)
			numPods, err := m.k8sClient.GetNumPods()
			if err != nil {
				m.logger.Error("error while getting num pods!")
				return EventError
			}

			if numPods != expectedNum || numPods != expectedNum+1 {
				m.logger.Error("unexpected number of pods!")
				return EventError
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
			m.logger.Error("error while getting current rollout uid!")
			return EventError
		}
	}
	if uid == "" {
		uid = GenerateRandomID()
		for prevUid == uid {
			uid = GenerateRandomID() //ensure our uid is not the same, despite very very small odds
		}
		m.etcdClient.Put(ctx, "current_rollout_uid", uid)
	}

	//update our 'previous' config
	m.mu.Lock()
	m.gwConfig.Prev = m.gwConfig.Cur
	m.mu.Unlock()

	//begin rollout!
	numReplicas := m.gwConfig.Next.NumShards / m.config.MaxConcurrency
	httpClient := http.Client{}
	oldPod := ""
	target := ""
	resume := (status != "done")
	var jsonData []byte
	for i := startIndex; i < numReplicas; i++ {
		m.etcdClient.Put(ctx, "rollout_index", strconv.Itoa(i))
		m.logger.Info("rolling out", slog.Int("rollout_index", i), slog.Int("num_replicas", numReplicas))

		oldPod = fmt.Sprintf("pluralkit-gateway-%s-%d", prevUid, i)
		pod.Name = fmt.Sprintf("pluralkit-gateway-%s-%d", uid, i)
		pod.Spec.Hostname = pod.Name
		pod.Spec.Subdomain = "gw-svc"

		//theres uh probably a better way to do this?
		if resume {
			switch status {
			case "creating":
				goto creating
			case "waiting":
				goto waiting
			case "switching_old":
				goto switching_old
			case "switching_new":
				goto switching_new
			case "deleting":
				goto deleting
			}
		}

		m.etcdClient.Put(ctx, "rollout_status", "creating")
	creating:
		_, err = m.k8sClient.CreatePod(&pod)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while creating pods in rollout!", slog.Any("error", err))
			return EventError
		}

		m.etcdClient.Put(ctx, "rollout_status", "waiting")
	waiting:
		m.logger.Info("waiting for ready", slog.String("old_pod", oldPod), slog.String("new_pod", pod.Name))
		err = m.k8sClient.WaitForReady(ctx, []string{pod.Name}, m.config.EventWaitTimeout)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while waiting for pod in rollout!", slog.Any("error", err))
			m.k8sClient.DeletePod(pod.Name)
			return EventError
		}

		m.etcdClient.Put(ctx, "rollout_status", "switching_old")
	switching_old:
		target = m.cacheEndpoints[i]
		err = changeEventTarget(httpClient, target, "")
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while deleting old runtime_config!", slog.Any("err", err))
			m.k8sClient.DeletePod(pod.Name)
			return EventError
		}

		m.etcdClient.Put(ctx, "rollout_status", "switching_new")
	switching_new:
		m.mu.Lock()
		m.cacheEndpoints[i] = fmt.Sprintf("http://%s.%s:5000", pod.Spec.Hostname, pod.Spec.Subdomain)
		m.mu.Unlock()

		target = m.cacheEndpoints[i]
		err = changeEventTarget(httpClient, target, m.config.EventTarget)
		if err != nil {
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while setting runtime_config!", slog.Any("err", err))
			m.k8sClient.DeletePod(pod.Name)
			return EventError
		}

		//TODO: do this more efficient
		jsonData, err = json.Marshal(m.cacheEndpoints)
		if err != nil {
			m.logger.Warn("error while marshalling cache endpoints!", slog.Any("err", err))
		}
		m.etcdClient.Put(ctx, "cache_endpoints", string(jsonData))

		m.etcdClient.Put(ctx, "rollout_status", "deleting")
	deleting:
		err = m.k8sClient.DeletePod(oldPod)
		if err != nil {
			if resume {
				continue
			}
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while deleting old pod!", slog.Any("err", err))
			return EventError
		}
		err = m.k8sClient.WaitForDeleted(ctx, []string{oldPod}, m.config.EventWaitTimeout)
		if err != nil {
			if resume {
				continue
			}
			m.etcdClient.Put(ctx, "rollout_status", "error")
			m.logger.Error("error while waiting for old pod to be deleted!", slog.Any("err", err))
			return EventError
		}

		resume = false
		m.etcdClient.Put(ctx, "rollout_status", "running")
		time.Sleep(50 * time.Millisecond) //sleep a short amount of time, just in case
	}

	//update our current config
	m.mu.Lock()
	m.gwConfig.Cur = m.gwConfig.Next
	m.mu.Unlock()

	m.etcdClient.Put(ctx, "rollout_status", "done")
	m.etcdClient.Put(ctx, "current_uid", uid)
	return EventHealthy
}

// TODO: document this function.
func DeployState(m *Machine) Event {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pod corev1.Pod
	err := json.Unmarshal(m.gwConfig.Next.PodDefinition, &pod)
	if err != nil {
		m.logger.Error("error while parsing config!", slog.Any("error", err))
		return EventError
	}
	addEnvs(m, &pod)
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

	//update our 'previous' config
	m.mu.Lock()
	m.gwConfig.Prev = m.gwConfig.Cur
	m.mu.Unlock()

	//begin deployment!
	m.mu.Lock()
	numReplicas := m.gwConfig.Next.NumShards / m.config.MaxConcurrency
	m.shardStatus = make([]ShardState, m.gwConfig.Next.NumShards)
	m.cacheEndpoints = make([]string, numReplicas)
	podNames := make([]string, numReplicas)
	m.mu.Unlock()
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

		m.mu.Lock()
		m.cacheEndpoints[i] = fmt.Sprintf("http://%s.%s:5000", pod.Spec.Hostname, pod.Spec.Subdomain)
		m.mu.Unlock()
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

	m.mu.Lock()
	m.gwConfig.Cur = m.gwConfig.Next
	m.mu.Unlock()
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
			if m.gwConfig.Cur.NumShards != 0 && m.gwConfig.Cur.PodDefinition != nil {
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

// TODO: document this function.
func RollbackState(m *Machine) Event {
	return ""
}

// TODO: document this function.
func ShutdownState(m *Machine) Event {
	//TODO: cleanup anything we need to before shutdown
	return ""
}
