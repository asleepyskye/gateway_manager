package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/discovery/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sLabels "k8s.io/apimachinery/pkg/labels"
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

func (c *Client) getSelector(labels string) (k8sLabels.Selector, error) {
	var selector k8sLabels.Selector
	var err error
	if len(labels) != 0 {
		selector, err = k8sLabels.Parse(c.labelSelector + "," + labels)
		if err != nil {
			return nil, err
		}
	} else {
		selector, err = k8sLabels.Parse(c.labelSelector)
		if err != nil {
			return nil, err
		}
	}
	return selector, nil
}

// TODO: document this function.
func (c *Client) GetPods(ctx context.Context, labels string) (*corev1.PodList, error) {
	selector, err := c.getSelector(labels)
	if err != nil {
		return nil, err
	}
	podList, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}
	return podList, nil
}

func (c *Client) PodExists(ctx context.Context, name string) (bool, error) {
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelSelector,
		FieldSelector: fmt.Sprintf("metadata.name=%s", name),
	}
	pods, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, listOptions)
	if err != nil {
		return false, err
	}
	return (len(pods.Items) > 0), nil
}

// TODO: document this function.
func (c *Client) GetServiceEndpoints(ctx context.Context, name string) (*v1.EndpointSliceList, error) {
	disc := c.k8sClient.DiscoveryV1()
	endpoints, err := disc.EndpointSlices(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kubernetes.io/service-name=%s", name),
	})
	return endpoints, err
}

// TODO: document this function.
func (c *Client) CreateService(ctx context.Context, service *corev1.Service) (*corev1.Service, error) {
	service.Namespace = c.namespace
	if service.ObjectMeta.Labels == nil {
		service.ObjectMeta.Labels = make(map[string]string)
	}
	service.ObjectMeta.Labels["created-by"] = c.creatorName

	createdService, err := c.k8sClient.CoreV1().Services(c.namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return createdService, nil
}

// TODO: document this function.
func (c *Client) CreateDeployment(ctx context.Context, deployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	deployment.Namespace = c.namespace
	if deployment.ObjectMeta.Labels == nil {
		deployment.ObjectMeta.Labels = make(map[string]string)
	}
	deployment.ObjectMeta.Labels["created-by"] = c.creatorName

	createdDeployment, err := c.k8sClient.AppsV1().Deployments(c.namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return createdDeployment, nil
}

// TODO: document this function.
func (c *Client) CreatePod(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	pod.Namespace = c.namespace
	if pod.ObjectMeta.Labels == nil {
		pod.ObjectMeta.Labels = make(map[string]string)
	}
	pod.ObjectMeta.Labels["created-by"] = c.creatorName

	createdPod, err := c.k8sClient.CoreV1().Pods(c.namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return createdPod, nil
}

// TODO: document this function.
func (c *Client) DeletePod(ctx context.Context, name string) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	err := c.k8sClient.CoreV1().Pods(c.namespace).Delete(ctx, name, deleteOptions)
	if err != nil {
		return err
	}

	return nil
}

// TODO: document this function.
func (c *Client) DeleteAllPods(ctx context.Context, labels string) error {
	selector, err := c.getSelector(labels)
	if err != nil {
		return err
	}
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}
	listOptions := metav1.ListOptions{
		LabelSelector: selector.String(),
	}

	err = c.k8sClient.CoreV1().Pods(c.namespace).DeleteCollection(ctx, deleteOptions, listOptions)
	if err != nil {
		return err
	}

	return nil
}

// TODO: document this function.
// the more proper way to do this is probably with a watcher?
func (c *Client) WaitForReady(ctx context.Context, names []string, timeout time.Duration) error {
	remaining := make(map[string]bool, len(names))
	for _, v := range names {
		remaining[v] = false
	} //seems like this is the best/only way to do this in golang???

	//not the most resource efficient but whatevs
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
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

func (c *Client) WaitForDeleted(ctx context.Context, names []string, timeout time.Duration) error {
	if len(names) == 0 {
		return nil
	}
	remaining := make(map[string]bool, len(names))
	for _, v := range names {
		remaining[v] = false
	} //seems like this is the best/only way to do this in golang???

	return wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
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

func (c *Client) ValidatePod(ctx context.Context, pod *corev1.Pod) error {
	_, err := c.k8sClient.CoreV1().Pods(c.namespace).Create(ctx, pod, metav1.CreateOptions{
		DryRun: []string{metav1.DryRunAll},
	})

	if err != nil {
		return err
	}
	return nil
}
