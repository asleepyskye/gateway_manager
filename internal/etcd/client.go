package etcd

import (
	"context"
	"log/slog"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// yeah... this is kinda stupid
// we wrap the etcdClient so we can handle errors easily all in one place

//TODO: add error types to core for unified error handling here?

type Client struct {
	etcdClient *clientv3.Client
}

func NewClient(addr string) *Client {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{addr},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		slog.Error("[etcd] error while setting up etcd!", err) //TODO: properly handle etcd errors
		return nil
	}

	return &Client{
		etcdClient: cli,
	}
}

func (c *Client) Close() error {
	if c.etcdClient == nil {
		return nil
	}

	slog.Info("[etcd] closing etcd client") //TODO: setup a logger for each part so we don't have to prepend [part]?
	err := c.etcdClient.Close()
	if err != nil {
		slog.Error("[etcd] error while closing etcd client!", err)
		return err
	}
	c.etcdClient = nil
	return nil
}

func (c *Client) Put(ctx context.Context, key string, val string) error {
	_, err := c.etcdClient.Put(ctx, key, val)
	if err != nil {
		slog.Error("[etcd] error in put", err)
		return err
	}
	return nil
}
