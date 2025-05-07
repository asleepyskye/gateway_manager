package k8s

import (
	"context"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	k8sClient *kubernetes.Clientset
	namespace string
}

func NewClient(namespace string) *Client {
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
		k8sClient: clientset,
		namespace: namespace,
	}
}

func (c *Client) GetNumPods() (int, error) {
	podList, err := c.k8sClient.CoreV1().Pods(c.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	return len(podList.Items), nil
}
