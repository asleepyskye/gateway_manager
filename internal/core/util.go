package core

import (
	"math/rand"
	"time"
)

type ManagerConfig struct {
	MaxConcurrency int    `env:"pluralkit__discord__max_concurrency,required"`
	EtcdAddr       string `env:"pluralkit__manager__etcd_addr,required"`
	BindAddr       string `env:"pluralkit__manager__addr,required"`

	//times should be formatted to parse with time.ParseDuration
	EventWaitTimeout time.Duration `env:"pluralkit__manager__event_wait_timeout" envDefault:"8m"`
	MonitorPeriod    time.Duration `env:"pluralkit__manager__monitor_period" envDefault:"10s"`

	EventTarget      string `env:"pluralkit__manager__event_target_format" envDefault:"http://pluralkit-dotnet-bot.pluralkit.svc.cluster.local:5002/events"`
	ManagerNamespace string `env:"pluralkit__manager__namespace" envDefault:"pluralkit-gateway"`
}

type ShardState struct {
	ShardID            int32
	Up                 bool
	DisconnectionCount int32
	Latency            int32
	LastHeartbeat      int32
	LastConnection     int32
	ClusterID          int32
}

const alphanumeric = "abcdefghijklmnopqrstuvwxyz0123456789"

func GenerateRandomID() string {
	bytes := make([]byte, 5)
	for i := range bytes {
		bytes[i] = alphanumeric[rand.Intn(len(alphanumeric))]
	}
	return string(bytes)
}
