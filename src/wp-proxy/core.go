package wpproxy

import (
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func NewWPReverseProxy(wpRoot string) *httputil.ReverseProxy {
	target, err := url.Parse(wpRoot)
	if err != nil {
		log.Fatalf("invalid WP_UPSTREAM url %q: %v", wpRoot, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// Keep a reference to the original Director so we can extend it.
	origDirector := proxy.Director

	proxy.Director = func(r *http.Request) {
		// Save original values before origDirector mutates the request.
		origHost := r.Host
		origScheme := "http"
		if r.TLS != nil {
			origScheme = "https"
		}
		origIP := clientIPFromRequest(r)

		origDirector(r)

		// --- WordPress-friendly behavior ---
		// 1) Preserve the incoming path and query (NewSingleHostReverseProxy already does this)
		// 2) Set Host header to upstream host (helps WP if it expects its own host)
		r.Host = target.Host

		// 3) Forwarding headers
		// X-Forwarded-Host: what client requested
		r.Header.Set("X-Forwarded-Host", origHost)

		// X-Forwarded-Proto: what client used to reach us
		r.Header.Set("X-Forwarded-Proto", origScheme)

		// X-Forwarded-For: chain
		if origIP != "" {
			prior := r.Header.Get("X-Forwarded-For")
			if prior == "" {
				r.Header.Set("X-Forwarded-For", origIP)
			} else {
				r.Header.Set("X-Forwarded-For", prior+", "+origIP)
			}
		}

		// Optional: if your WP is behind HTTPS and you terminate TLS at the Go proxy,
		// some setups like to see:
		// r.Header.Set("X-Forwarded-Ssl", "on")

		// 4) Ensure User-Agent isn't blank (some upstreams behave oddly)
		if _, ok := r.Header["User-Agent"]; !ok {
			r.Header.Set("User-Agent", "go-reverse-proxy")
		}

		// 5) If you want WP to “think” it is served on the same public host,
		// you can ALSO keep r.Host as origHost, but then you must make sure WP
		// is configured with that host (WP_HOME/WP_SITEURL) and origin accepts it.
		// For most origin setups, setting r.Host = target.Host is safer.
	}

	// Transport with sane timeouts
	/*
		proxy.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          200,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}*/

	// Error handling
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("wp proxy error: %v", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}

	// Optional: rewrite redirects so Location headers come back to your public host.
	// This is useful if WP generates absolute redirects to the upstream host.
	proxy.ModifyResponse = func(resp *http.Response) error {
		loc := resp.Header.Get("Location")
		if loc == "" {
			return nil
		}

		u, err := url.Parse(loc)
		if err != nil {
			return nil
		}

		// If WP returns relative redirects like "/paywall", leave them.
		if !u.IsAbs() {
			return nil
		}

		xfh := resp.Request.Header.Get("X-Forwarded-Host")
		xfp := resp.Request.Header.Get("X-Forwarded-Proto")
		if xfh == "" {
			return nil
		}
		if xfp == "" {
			xfp = "http"
		}

		// Rewrite any upstream-ish redirect hosts back to the public host
		h := strings.ToLower(u.Hostname())
		th := strings.ToLower(target.Hostname())

		if h == th || h == "localhost" || h == "127.0.0.1" {
			u.Scheme = xfp
			u.Host = xfh
			resp.Header.Set("Location", u.String())
		}

		log.Printf("upstream redirect: %s  (req=%s host=%s xfh=%s xfp=%s)",
			loc, resp.Request.URL.String(), resp.Request.Host,
			resp.Request.Header.Get("X-Forwarded-Host"),
			resp.Request.Header.Get("X-Forwarded-Proto"),
		)

		return nil
	}

	return proxy
}

func clientIPFromRequest(r *http.Request) string {
	// Prefer the direct peer IP; if you're behind another proxy/LB, you might prefer
	// trusting X-Real-IP / X-Forwarded-For from that proxy instead.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
