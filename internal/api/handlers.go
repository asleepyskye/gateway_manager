package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"pluralkit/manager/internal/core"

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
	render.JSON(w, r, shards)
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
	val := a.Controller.GetCurrentConfig()
	if val == nil {
		w.Write([]byte("config not set"))
		return
	}

	if err := render.Render(w, r, val); err != nil {
		http.Error(w, "error while rendering response", 500)
		return
	}
}

/*
Handler for /config/next

returns the next configuration for manager
*/
func (a *API) GetNextConfig(w http.ResponseWriter, r *http.Request) {
	val := a.Controller.GetNextConfig()
	if val == nil {
		w.Write([]byte("config not set"))
		return
	}

	if err := render.Render(w, r, val); err != nil {
		http.Error(w, "error while rendering response", 500)
		return
	}
}

/*
Handler for /config/prev

returns the next configuration for manager
*/
func (a *API) GetPrevConfig(w http.ResponseWriter, r *http.Request) {
	val := a.Controller.GetPrevConfig()
	if val == nil {
		w.Write([]byte("config not set"))
		return
	}

	if err := render.Render(w, r, val); err != nil {
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
Handler for /actions/pause

Sends a deploy event command to start a deploy on manager
*/
func (a *API) SetPause(w http.ResponseWriter, r *http.Request) {
	a.Controller.SendEvent(core.EventPause)
	w.Write([]byte(""))
}

/*
Handler for /actions/resume

Sends a deploy event command to start a deploy on manager
*/
func (a *API) SetResume(w http.ResponseWriter, r *http.Request) {
	a.Controller.SendEvent(core.EventResume)
	w.Write([]byte(""))
}
