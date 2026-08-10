package proxypool

import (
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTestProxyInvalidURL(t *testing.T) {
	res := TestProxy("not-a-valid-url", "http", "")
	if res.OK {
		t.Errorf("TestProxy returned OK for an invalid URL")
	}
}

func TestTestHTTPProxyWithInlineCredentials(t *testing.T) {
	var authHeader string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			authHeader = r.Header.Get("Proxy-Authorization")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()

			src := http.NewResponseController(w)
			srcConn, _, err := src.Hijack()
			if err != nil {
				return
			}
			defer srcConn.Close()

			dest, err := net.Dial("tcp", "ifconfig.co:443")
			if err != nil {
				return
			}
			defer dest.Close()
			go func() { _, _ = io.Copy(dest, srcConn) }()
			_, _ = io.Copy(srcConn, dest)
			return
		}
		http.Error(w, "expected CONNECT", http.StatusBadRequest)
	}))
	defer proxy.Close()

	// This test just verifies that credentials are sent during CONNECT.
	res := testHTTPSCONNECT(mustURL("http://alice:secret@"+stripScheme(proxy.URL)), 2*time.Second)
	if !res.OK {
		t.Fatalf("expected OK, got error: %s", res.Error)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	if authHeader != wantAuth {
		t.Fatalf("Proxy-Authorization = %q, want %q", authHeader, wantAuth)
	}
}

func TestTestHTTPProxyCONNECTFailureDetected(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
			return
		}
		http.Error(w, "expected CONNECT", http.StatusBadRequest)
	}))
	defer proxy.Close()

	res := TestHTTPProxy("http://"+stripScheme(proxy.URL), 2*time.Second)
	if res.OK || !strings.Contains(res.Error, "407") {
		t.Fatalf("expected 407 CONNECT error, got: %+v", res)
	}
}

func TestHTTPSProxyCONNECTTunnel(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ip":"203.0.113.1","country":"US","city":"Test","org":"TestOrg"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	var sawAuth string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" {
			http.Error(w, "expected CONNECT", http.StatusBadRequest)
			return
		}
		sawAuth = r.Header.Get("Proxy-Authorization")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()

		dest, err := net.Dial("tcp", stripHostPort(upstream.Listener.Addr().String()))
		if err != nil {
			return
		}
		defer dest.Close()
		src := http.NewResponseController(w)
		srcConn, _, err := src.Hijack()
		if err != nil {
			return
		}
		defer srcConn.Close()
		go func() {
			_, _ = io.Copy(dest, srcConn)
		}()
		_, _ = io.Copy(srcConn, dest)
	}))
	defer proxy.Close()

	proxyURL := "http://user:pass@" + stripScheme(proxy.URL)
	u, _ := url.Parse(proxyURL)
	transport := &http.Transport{
		Proxy:           http.ProxyURL(u),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	req, _ := http.NewRequest("GET", upstream.URL+"/json", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request through CONNECT proxy failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected OK, got %d", resp.StatusCode)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if sawAuth != wantAuth {
		t.Fatalf("Proxy-Authorization = %q, want %q", sawAuth, wantAuth)
	}
}

func stripScheme(raw string) string {
	if i := strings.Index(raw, "://"); i != -1 {
		return raw[i+3:]
	}
	return raw
}

func stripHostPort(addr string) string {
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
