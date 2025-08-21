package Proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

/*
Handler for /ping

returns "pong!"
*/
func (p *Proxy) Ping(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong!"))
}

/*
Handler for GET /endpoints/{idx}/get

gets the endpoint for the specified index/instance
*/
func (p *Proxy) GetEndpoint(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(r, "idx"))
	if err != nil {
		http.Error(w, "error while reading param", 500)
		return
	}
	if err := render.Render(w, r, &p.endpoints[index]); err != nil {
		http.Error(w, "error while rendering response", 500)
		return
	}
}

/*
Handler for POST /endpoints/{idx}/set

sets the endpoint for the specified index/instance
*/
func (p *Proxy) SetEndpoint(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(r, "idx"))
	if err != nil {
		http.Error(w, "error while reading param", 500)
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error while reading request body", 500)
		p.Logger.Warn("error while reading request body", slog.Any("error", err))
		return
	}
	p.endpoints[index].Endpoint = string(data)
}

/*
Handler for PATCH /endpoints

patches the currently set endpoints
*/
func (p *Proxy) PatchEndpoints(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error while reading request body", 500)
		p.Logger.Warn("error while reading request body", slog.Any("error", err))
		return
	}

	err = json.Unmarshal(data, &p.endpoints)
	if err != nil {
		http.Error(w, "error while parsing endpoints data", 500)
		p.Logger.Warn("error while parsing endpoints data", slog.Any("error", err))
		return
	}
}

/*
Handler for /cache/guilds/:id/*path

Acts as a proxy to the appropriate gateway instance based on the guild ID.
*/
func (p *Proxy) GetCache(w http.ResponseWriter, r *http.Request) {
	guildID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "error while reading param", 500)
		return
	}

	reqPath := strings.TrimRight(chi.URLParam(r, "*"), "/")

	path := "/guilds/" + strconv.Itoa(guildID)
	if len(reqPath) > 0 {
		path += "/" + reqPath
	}

	shardID := (guildID >> 22) % p.numShards
	clusterID := shardID / p.Config.MaxConcurrency
	target := p.endpoints[clusterID].Endpoint + path

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

	resp, err := p.httpClient.Do(req)
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
