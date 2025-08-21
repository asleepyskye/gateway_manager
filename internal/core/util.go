package core

import (
	"errors"
	"log/slog"
	"math/rand"
	"net/http"
	"time"
)

type SlogLevel slog.Level

var LevelMappings = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

func (l *SlogLevel) UnmarshalText(text []byte) error {
	lvl, ok := LevelMappings[string(text)]
	if !ok {
		return errors.New("invalid log level")
	}
	*l = SlogLevel(lvl)
	return nil
}

type ManagerConfig struct {
	MaxConcurrency int    `env:"pluralkit__discord__max_concurrency,required"`
	EtcdAddr       string `env:"pluralkit__manager__etcd_addr,required"`
	BindAddr       string `env:"pluralkit__manager__addr,required"`

	//times should be formatted to parse with time.ParseDuration
	EventWaitTimeout time.Duration `env:"pluralkit__manager__event_wait_timeout" envDefault:"8m"`
	MonitorPeriod    time.Duration `env:"pluralkit__manager__monitor_period" envDefault:"30s"`

	EventTarget      string `env:"pluralkit__manager__event_target_format" envDefault:"http://pluralkit-dotnet-bot.pluralkit.svc.cluster.local:5002/events"`
	ManagerNamespace string `env:"pluralkit__manager__namespace" envDefault:"pluralkit-gateway"`

	SentryURL      string    `env:"pluralkit__sentry_url"`
	LogLevel       SlogLevel `env:"pluralkit__consoleloglevel" envDefault:"info"`
	SentryLogLevel SlogLevel `env:"pluralkit__sentryloglevel" envDefault:"error"`

	ProxyReplicas  int32  `env:"pluralkit__manager__proxy_replicas" envDefault:"2"`
	ProxyImage     string `env:"pluralkit__manager__proxy_image,required"`
	ProxySentryURL string `env:"pluralkit__manager__proxy_sentry_url"`
}

type ProxyConfig struct {
	MaxConcurrency int    `env:"pluralkit__discord__max_concurrency,required"`
	BindAddr       string `env:"pluralkit__proxy__addr" envDefault:"0.0.0.0:5000"`

	SentryURL      string    `env:"pluralkit__sentry_url"`
	LogLevel       SlogLevel `env:"pluralkit__consoleloglevel" envDefault:"info"`
	SentryLogLevel SlogLevel `env:"pluralkit__sentryloglevel" envDefault:"error"`
}

type ShardState struct {
	ShardID            int32 `json:"shard_id"`
	Up                 bool  `json:"up"`
	DisconnectionCount int32 `json:"disconnection_count,omitempty"`
	Latency            int32 `json:"latency,omitempty"`
	LastHeartbeat      int32 `json:"last_heartbeat,omitempty"`
	LastConnection     int32 `json:"last_connection,omitempty"`
	ClusterID          int32 `json:"cluster_id"`
}

type ShardStateList struct {
	Shards []ShardState `json:"shards"`
}

// render helper function for ShardStateList
func (i *ShardStateList) Render(w http.ResponseWriter, r *http.Request) error { return nil }

type ProxyEndpoint struct {
	Index    int
	Endpoint string
}

// render helper function for ProxyEndpoint
func (i *ProxyEndpoint) Render(w http.ResponseWriter, r *http.Request) error { return nil }

const alphanumeric = "abcdefghijklmnopqrstuvwxyz0123456789"

func GenerateRandomID() string {
	bytes := make([]byte, 5)
	for i := range bytes {
		bytes[i] = alphanumeric[rand.Intn(len(alphanumeric))]
	}
	return string(bytes)
}
