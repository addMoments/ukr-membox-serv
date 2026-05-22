package servereact

import (
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func ServeReact(live_location, local_host string, is_live bool) func(w http.ResponseWriter, r *http.Request) {
	var proxy *httputil.ReverseProxy
	if !is_live {
		target, _ := url.Parse(local_host)
		proxy = httputil.NewSingleHostReverseProxy(target)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if is_live {
			//dont touch this this is simple
			resp, err := http.Get(live_location + r.URL.Path)
			if err != nil {
				http.Error(w, "Failed to fetch", http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()

			for k, v := range resp.Header {
				w.Header()[k] = v
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
		} else {
			proxy.ServeHTTP(w, r)
		}
	}
}
