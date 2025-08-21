package Proxy

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
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

}

/*
Handler for POST /endpoints/{idx}/set

sets the endpoint for the specified index/instance
*/
func (p *Proxy) SetEndpoint(w http.ResponseWriter, r *http.Request) {

}

/*
Handler for PATCH /endpoints

patches the currently set endpoints
*/
func (p *Proxy) PatchEndpoints(w http.ResponseWriter, r *http.Request) {

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
	target := p.endpoints[clusterID] + path

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
