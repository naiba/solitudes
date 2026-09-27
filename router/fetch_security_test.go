package router

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchImageRejectsInternalAddresses(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte("secret"))
	}))
	defer server.Close()
	for _, raw := range []string{
		server.URL,
		strings.Replace(server.URL, "127.0.0.1", "localhost", 1),
		"file:///etc/passwd",
		"http://user:pass@example.com/image.png",
	} {
		if _, _, err := fetchImage(context.Background(), raw); err == nil {
			t.Errorf("fetchImage(%q) accepted unsafe URL", raw)
		}
	}
	if called {
		t.Fatal("private HTTP server was contacted")
	}
}

func TestPublicFetchIP(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"169.254.169.254", false},
		{"100.64.0.1", false},
		{"240.0.0.1", false},
		{"192.168.1.1", false},
		{"::ffff:127.0.0.1", false},
		{"::1", false},
		{"2001:db8::1", false},
	} {
		if got := publicFetchIP(net.ParseIP(tc.ip)); got != tc.want {
			t.Errorf("publicFetchIP(%s) = %t, want %t", tc.ip, got, tc.want)
		}
	}
}
