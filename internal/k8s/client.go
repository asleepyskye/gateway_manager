package k8s

import (
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
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
func NewClient(namespace string, creatorName string) (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	selector := "created-by=" + creatorName

	return &Client{
		k8sClient:     clientset,
		namespace:     namespace,
		creatorName:   creatorName,
		labelSelector: selector,
	}, nil
}

// TODO: document this function.
func (c *Client) GetAllPodsNames() ([]string, error) {
	podList, err := c.k8sClient.CoreV1().Pods(c.namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: c.labelSelector,
	})
	if err != nil {
		return make([]string, 0), err
	}

	podNames := make([]string, len(podList.Items))
	for i, pod := range podList.Items {
		podNames[i] = pod.Name
	}
	return podNames, nil
}

// TODO: document this function.
func (c *Client) GetNumPods() (int, error) {
	podList, err := c.k8sClient.CoreV1().Pods(c.namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: c.labelSelector,
	})
	if err != nil {
		return 0, err
	}
	return len(podList.Items), nil
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
		return nil, err
	}
	return createdPod, nil
}

// TODO: document this function.
func (c *Client) DeletePod(name string) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	err := c.k8sClient.CoreV1().Pods(c.namespace).Delete(context.TODO(), name, deleteOptions)
	if err != nil {
		return err
	}

	return nil
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
		return err
	}

	return nil
}

// TODO: document this function.
// the more proper way to do this is probably with a watcher?
func (c *Client) WaitForReady(names []string, timeout time.Duration) error {
	remaining := make(map[string]bool, len(names))
	for _, v := range names {
		remaining[v] = false
	} //seems like this is the best/only way to do this in golang???

	//not the most resource efficient but whatevs
	return wait.PollUntilContextTimeout(context.Background(), 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		for name := range remaining {
			pod, err := c.k8sClient.CoreV1().Pods(c.namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return true, err
			}

			if pod.Status.Phase == corev1.PodFailed {
				return true, errors.New("pod failed to start")
			}

			for _, condition := range pod.Status.Conditions {
				if condition.Type == "DisruptionTarget" && condition.Status == "True" {
					return true, errors.New("pod disrupted")
				}
				if condition.Type == "Ready" && condition.Status == "True" {
					delete(remaining, name)
					continue
				}
			}
		}
		if len(remaining) == 0 {
			return true, nil
		}
		return false, nil
	})
}

func (c *Client) WaitForDeleted(names []string, timeout time.Duration) error {
	if len(names) == 0 {
		return nil
	}
	remaining := make(map[string]bool, len(names))
	for _, v := range names {
		remaining[v] = false
	} //seems like this is the best/only way to do this in golang???

	return wait.PollUntilContextTimeout(context.Background(), 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		for name := range remaining {
			_, err := c.k8sClient.CoreV1().Pods(c.namespace).Get(ctx, name, metav1.GetOptions{})
			if k8sErrors.IsNotFound(err) {
				delete(remaining, name)
			} else if err != nil {
				return true, err
			}
		}
		if len(remaining) == 0 {
			return true, nil
		}
		return false, nil
	})
}
