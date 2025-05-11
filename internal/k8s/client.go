package k8s

import (
	"context"
	"errors"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	k8sClient     *kubernetes.Clientset
	namespace     string
	creatorName   string
	labelSelector string
}

// TODO: document this function.
func NewClient(namespace string, creatorName string) *Client {
	config, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("[k8s] error while setting up k8s client config!", slog.Any("error", err))
		return nil
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		slog.Error("[k8s] error while setting up k8s clientset!", slog.Any("error", err))
	}

	selector := "created-by=" + creatorName

	return &Client{
		k8sClient:     clientset,
		namespace:     namespace,
		creatorName:   creatorName,
		labelSelector: selector,
	}
}

// TODO: document this function.
func (c *Client) GetNumPods() (int, error) {
	podList, err := c.k8sClient.CoreV1().Pods(c.namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: c.labelSelector,
	})
	if err != nil {
		slog.Error("[k8s] error while getting num pods!", slog.Any("error", err))
		return 0, err
	}
	return len(podList.Items), nil
}

// TODO: document this function.
func (c *Client) DeleteAllPods() error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelSelector,
	}

	err := c.k8sClient.CoreV1().Pods(c.namespace).DeleteCollection(context.TODO(), deleteOptions, listOptions)
	if err != nil {
		slog.Error("[k8s] error while deleting pods in DeleteAll!", slog.Any("error", err))
		return err
	}

	return nil
}

// TODO: document this function.
func (c *Client) CreateService(service *corev1.Service) (*corev1.Service, error) {
	service.Namespace = c.namespace
	if service.ObjectMeta.Labels == nil {
		service.ObjectMeta.Labels = make(map[string]string)
	}
	service.ObjectMeta.Labels["created-by"] = c.creatorName

	createdService, err := c.k8sClient.CoreV1().Services(c.namespace).Create(context.TODO(), service, metav1.CreateOptions{})
	if err != nil {
		slog.Warn("[k8s] error while creating service!", slog.Any("error", err))
		return nil, err
	}
	return createdService, nil
}

// TODO: document this function.
func (c *Client) CreatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	pod.Namespace = c.namespace
	if pod.ObjectMeta.Labels == nil {
		pod.ObjectMeta.Labels = make(map[string]string)
	}
	pod.ObjectMeta.Labels["created-by"] = c.creatorName

	createdPod, err := c.k8sClient.CoreV1().Pods(c.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		slog.Warn("[k8s] error while creating pod!", slog.Any("error", err))
		return nil, err
	}
	return createdPod, nil
}

// TODO: document this function.
func (c *Client) DeletePod(name string) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	} //TODO: maybe don't hardcode this

	err := c.k8sClient.CoreV1().Pods(c.namespace).Delete(context.TODO(), name, deleteOptions)
	if err != nil {
		slog.Error("[k8s] error while deleting pod!", slog.Any("error", err))
		return err
	}

	return nil
}

// TODO: document this function.
// the more proper way to do this is probably with a watcher?
func (c *Client) WaitForReadyAll(expected int, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), 5*time.Second, timeout, false, func(ctx context.Context) (bool, error) {
		podList, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: c.labelSelector,
		})
		if err != nil {
			slog.Warn("[k8s] error while getting pod list in WaitForReadyAll!", slog.Any("error", err))
			return false, nil //keep polling
		}

		numReady := 0
		for _, pod := range podList.Items {
			if pod.Status.Phase == "Failed" {
				return true, errors.New("at least one failed pod")
			}
		}

		if numReady == expected {
			return true, nil
		}
		if numReady > expected {
			return true, errors.New("more pods than expected") //todo: combine these with unified errors for checks
		}
		return false, nil //keep polling
	})
}

// TODO: document this function.
// the more proper way to do this is probably with a watcher?
func (c *Client) WaitForReady(name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := c.k8sClient.CoreV1().Pods(c.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			slog.Warn("[k8s] error while getting pod in WaitForReady!", slog.Any("error", err))
			return false, err
		}

		if pod.Status.Phase == corev1.PodFailed {
			return true, errors.New("pod failed to start")
		}

		for _, condition := range pod.Status.Conditions {
			if condition.Type == "DisruptionTarget" && condition.Status == "True" {
				return true, errors.New("pod disrupted")
			}
			if condition.Type == "Ready" && condition.Status == "True" {
				return true, nil
			}
		}

		return false, nil
	})
}
