package etcd

import (
	"context"
	"errors"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// TODO: document this struct.
type Client struct {
	etcdClient *clientv3.Client
}

// TODO: document this function.
func NewClient(addr string) (*Client, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{addr},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	//apparently the etcd client can fail to connect without erroring above?
	//force sync to make sure we're actually connected
	ctxSync, cancelSync := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSync()
	err = cli.Sync(ctxSync)
	if err != nil {
		return nil, err
	}

	return &Client{
		etcdClient: cli,
	}, nil
}

// TODO: document this function.
func (c *Client) Close() error {
	if c.etcdClient == nil {
		return nil
	}

	err := c.etcdClient.Close()
	if err != nil {
		return err
	}
	c.etcdClient = nil
	return nil
}

// TODO: document this function.
func (c *Client) Put(ctx context.Context, key string, val string) error {
	_, err := c.etcdClient.Put(ctx, key, val)
	if err != nil {
		return err
	}
	return nil
}

// TODO: document this function.
func (c *Client) Get(ctx context.Context, key string) (value string, err error) {
	resp, err := c.etcdClient.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if resp.Count == 0 {
		return "", errors.New("value not found")
	}
	return string(resp.Kvs[0].Value), nil
}
