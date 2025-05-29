package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"pluralkit/manager/internal/core"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

/*
Handler for /ping

returns "pong!"
*/
func (a *API) Ping(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong!"))
}

/*
Handler for /status

returns the status of all shards
*/
func (a *API) GetStatus(w http.ResponseWriter, r *http.Request) {
	shards := a.Controller.GetShardStatus()
	if err := render.Render(w, r, &core.ShardStateList{Shards: shards}); err != nil {
		http.Error(w, "error while rendering response", 500)
		return
	}
}

/*
Handler for /shard/status

Patches the status for an individual shard.
*/
func (a *API) SetShardStatus(w http.ResponseWriter, r *http.Request) {
	var state core.ShardState
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error while reading request body", 500)
		a.Logger.Warn("error while reading request body in SetClusterStatus", slog.Any("error", err))
		return
	}
	err = json.Unmarshal(data, &state)
	if err != nil {
		http.Error(w, "error while parsing status", 500)
		a.Logger.Warn("error while parsing cluster status", slog.Any("error", err))
		return
	}
	a.Controller.UpdateShardStatus(state)
	w.Write([]byte(""))
}

/*
Handler for /config

returns the current running configuration for manager
*/
func (a *API) GetConfig(w http.ResponseWriter, r *http.Request) {
	val := a.Controller.GetConfig()

	if err := render.Render(w, r, &val); err != nil {
		http.Error(w, "error while rendering response", 500)
		return
	}
}

/*
Handler for /actions/config

Sets the next running configuration for manager
*/
func (a *API) SetConfig(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error while getting body data", 500)
		a.Logger.Warn("error while getting body data", slog.Any("error", err))
		return
	}

	err = a.Controller.SetConfig(data)
	if err != nil {
		http.Error(w, "error while setting config", 500)
		a.Logger.Warn("error while setting config", slog.Any("error", err))
		return
	}

	w.Write([]byte(""))
}

/*
Handler for /actions/rollout

Sends a rollout event command to start a rollout on manager
*/
func (a *API) SetRollout(w http.ResponseWriter, r *http.Request) {
	a.Controller.SendEvent(core.EventRolloutCmd)
	w.Write([]byte(""))
}

/*
Handler for /actions/deploy

Sends a deploy event command to start a deploy on manager
*/
func (a *API) SetDeploy(w http.ResponseWriter, r *http.Request) {
	a.Controller.SendEvent(core.EventDeployCmd)
	w.Write([]byte(""))
}

/*
Handler for /cache/guilds/:id/*path

Acts as a proxy to the appropriate gateway instance based on the guild ID.
*/
func (a *API) GetCache(w http.ResponseWriter, r *http.Request) {
	guildID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "error while reading param", 500)
		return
	}

	reqPath := strings.TrimRight(chi.URLParam(r, "*"), "/")

	path := "/guilds/" + strconv.Itoa(guildID)
	if (len(reqPath) > 0) {
		path += "/" + reqPath
	}

	shardID := (guildID >> 22) % *a.NumShards
	clusterID := shardID / a.Config.MaxConcurrency

	target := (*a.CacheEndpoints)[clusterID] + path

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "error while creating request", 500)
		return
	}
	for header, values := range r.Header {
		for _, value := range values {
			req.Header.Add(header, value)
		}
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		http.Error(w, "error while requesting data", 500)
		return
	}
	defer resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
	for header, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		http.Error(w, "error while copying response", 500)
		return
	}
}
