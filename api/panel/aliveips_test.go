package panel

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/InazumaV/V2bX/conf"
	"github.com/vmihailenco/msgpack/v5"
)

func newAliveIPsTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := New(&conf.ApiConfig{
		APIHost:  server.URL,
		Key:      "test-token",
		NodeType: "vmess",
		NodeID:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.client.SetRetryCount(0)
	return c
}

func preserveAliveIPs(t *testing.T) {
	t.Helper()
	previous := UserAliveIPsMap.Load()
	t.Cleanup(func() { UserAliveIPsMap.Store(previous) })
}

func TestClient_GetIpsListUsesIndependentETag(t *testing.T) {
	preserveAliveIPs(t)
	c := newAliveIPsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/server/UniProxy/aips" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("If-None-Match"); got != "ips-v1" {
			t.Errorf("IP list ETag = %q, want ips-v1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", "ips-v2")
		fmt.Fprint(w, `{"users":[{"id":42,"alive_ips":["192.0.2.1","2001:db8::1"]}]}`)
	})
	c.userEtag = "users-v1"
	c.ipsEtag = "ips-v1"
	if err := c.GetIpsList(); err != nil {
		t.Fatal(err)
	}
	if c.userEtag != "users-v1" || c.ipsEtag != "ips-v2" {
		t.Fatalf("ETags after IP fetch: users=%q, ips=%q", c.userEtag, c.ipsEtag)
	}
	got, ok := UserAliveIPsMap.Load().Load(42)
	want := []string{"192.0.2.1", "2001:db8::1"}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("alive IPs = %v, present=%v; want %v", got, ok, want)
	}
}

func TestClient_GetIpsListNotModifiedPreservesCache(t *testing.T) {
	preserveAliveIPs(t)
	cached := new(sync.Map)
	cached.Store(42, []string{"192.0.2.1"})
	UserAliveIPsMap.Store(cached)
	c := newAliveIPsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("If-None-Match"); got != "ips-v1" {
			t.Errorf("IP list ETag = %q, want ips-v1", got)
		}
		w.WriteHeader(http.StatusNotModified)
	})
	c.ipsEtag = "ips-v1"
	if err := c.GetIpsList(); err != nil {
		t.Fatal(err)
	}
	if UserAliveIPsMap.Load() != cached {
		t.Fatal("304 response replaced the alive IP cache")
	}
	if c.ipsEtag != "ips-v1" {
		t.Fatalf("304 changed IP ETag to %q", c.ipsEtag)
	}
}

func TestClient_GetIpsListNullReturnsError(t *testing.T) {
	preserveAliveIPs(t)
	cached := new(sync.Map)
	cached.Store(42, []string{"192.0.2.1"})
	UserAliveIPsMap.Store(cached)
	c := newAliveIPsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", "invalid-response")
		fmt.Fprint(w, "null")
	})
	c.ipsEtag = "ips-v1"
	if err := c.GetIpsList(); err == nil {
		t.Fatal("null IP list should return an error")
	}
	if UserAliveIPsMap.Load() != cached || c.ipsEtag != "ips-v1" {
		t.Fatal("invalid response changed the cached IP list or ETag")
	}
}

func TestClient_GetUserListPreservesResponseFormats(t *testing.T) {
	want := []UserInfo{{Id: 42, Uuid: "test-uuid", SpeedLimit: 10, DeviceLimit: 2}}
	packed, err := msgpack.Marshal(UserListBody{Users: want})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		contentType string
		body        []byte
	}{
		{"msgpack", "application/x-msgpack", packed},
		{"json", "application/json", []byte(`{"users":[{"id":42,"uuid":"test-uuid","speed_limit":10,"device_limit":2}]}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newAliveIPsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/server/UniProxy/user" {
					t.Errorf("unexpected user list path: %s", r.URL.Path)
				}
				if got := r.Header.Get("X-Response-Format"); got != "msgpack" {
					t.Errorf("requested response format = %q, want msgpack", got)
				}
				if got := r.Header.Get("If-None-Match"); got != "users-v1" {
					t.Errorf("user list ETag = %q, want users-v1", got)
				}
				w.Header().Set("Content-Type", test.contentType)
				w.Header().Set("ETag", "users-v2")
				w.Write(test.body)
			})
			c.userEtag = "users-v1"
			c.ipsEtag = "ips-v1"
			got, err := c.GetUserList()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("user list = %#v, want %#v", got, want)
			}
			if c.userEtag != "users-v2" || c.ipsEtag != "ips-v1" {
				t.Fatalf("ETags after user fetch: users=%q, ips=%q", c.userEtag, c.ipsEtag)
			}
		})
	}
}
