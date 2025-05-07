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
	labelSelector string
}

// TODO: document this function.
func NewClient(namespace string, labelSelector string) *Client {
	config, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("[k8s] error while setting up k8s client config!", slog.Any("error", err))
		return nil
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		slog.Error("[k8s] error while setting up k8s clientset!", slog.Any("error", err))
	}
	return &Client{
		k8sClient:     clientset,
		namespace:     namespace,
		labelSelector: labelSelector,
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
func (c *Client) CreatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	pod.Namespace = c.namespace //ensure our namespace is properly set
	createdPod, err := c.k8sClient.CoreV1().Pods(c.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		slog.Error("[k8s] error while creating pod!", slog.Any("error", err))
		return nil, err
	}
	return createdPod, nil
}

// TODO: document this function.
// the more proper way to do this is probably with a watcher?
func (c *Client) WaitForReady(numReplicas int, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		podList, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: c.labelSelector,
		})
		if err != nil {
			slog.Warn("[k8s] error while getting pod list in WaitForReady!", slog.Any("error", err))
			return false, nil //keep polling
		}

		numReady := 0
		for _, pod := range podList.Items {
			if pod.Status.Phase == "Failed" {
				return true, errors.New("at least one failed pod")
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == "DisruptionTarget" && condition.Status == "True" {
					return true, errors.New("at least one disrupted pod")
				}
				if condition.Type == "Ready" && condition.Status == "True" {
					numReady++
					break
				}
			}
		}

		if numReady == numReplicas {
			return true, nil
		}
		if numReady > numReplicas {
			return true, errors.New("more pods than expected") //todo: combine these with unified errors for checks
		}
		return false, nil //keep polling
	})
}
