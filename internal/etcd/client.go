package etcd

import (
	"context"
	"errors"
	"log/slog"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// yeah... this is kinda stupid
// we wrap the etcdClient so we can handle errors easily all in one place

//TODO: add error types to core for unified error handling here?

// TODO: document this struct.
type Client struct {
	etcdClient *clientv3.Client
}

// TODO: document this function.
func NewClient(addr string) *Client {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{addr},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		slog.Error("[etcd] error while setting up etcd!", slog.Any("error", err)) //TODO: properly handle etcd errors
		return nil
	}

	//apparently the etcd client can fail to connect without erroring above?
	//force sync to make sure we're actually connected
	ctxSync, cancelSync := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSync()
	err = cli.Sync(ctxSync)
	if err != nil {
		slog.Error("[etcd] error while setting up etcd!", slog.Any("error", err))
		return nil
	}

	return &Client{
		etcdClient: cli,
	}
}

// TODO: document this function.
func (c *Client) Close() error {
	if c.etcdClient == nil {
		return nil
	}

	slog.Info("[etcd] closing etcd client") //TODO: setup a logger for each part so we don't have to prepend [part]?
	err := c.etcdClient.Close()
	if err != nil {
		slog.Error("[etcd] error while closing etcd client!", slog.Any("error", err))
		return err
	}
	c.etcdClient = nil
	return nil
}

// TODO: document this function.
func (c *Client) Put(ctx context.Context, key string, val string) error {
	_, err := c.etcdClient.Put(ctx, key, val)
	if err != nil {
		slog.Error("[etcd] error in put", slog.Any("error", err))
		return err
	}
	return nil
}

// TODO: document this function.
func (c *Client) Get(ctx context.Context, key string) (value string, err error) {
	resp, e := c.etcdClient.Get(ctx, key)
	if e != nil {
		return "", e
	}
	if resp.Count == 0 {
		return "", errors.New("value not found")
	}
	return string(resp.Kvs[0].Value), nil
}
