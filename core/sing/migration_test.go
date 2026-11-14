//go:build with_quic

package sing

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	"github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
)

func TestMigratedProtocolsDynamicUsersAndTraffic(t *testing.T) {
	certPath, keyPath := migrationCertificate(t)
	limiter.Init()
	for _, protocol := range []string{"vmess", "vless", "shadowsocks", "trojan", "tuic", "anytls", "hysteria", "hysteria2"} {
		t.Run(protocol, func(t *testing.T) {
			config := conf.NewSingConfig()
			config.LogConfig.Disabled = true
			created, err := New(&conf.CoreConfig{Type: "sing", SingConfig: config})
			if err != nil {
				t.Fatal(err)
			}
			server := created.(*Sing)
			t.Cleanup(func() { server.Close() })
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			port := migrationPort(t, protocol)
			info := migrationNode(protocol, port)
			options := &conf.Options{
				ListenIP: "127.0.0.1", SingOptions: conf.NewSingOptions(), ReportMinTraffic: 8,
				CertConfig: &conf.CertConfig{CertMode: "file", CertFile: certPath, KeyFile: keyPath},
			}
			if err := server.AddNode(protocol, info, options); err != nil {
				t.Fatalf("create live %s inbound: %v", protocol, err)
			}
			user := panel.UserInfo{Id: 7, Uuid: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}
			retained := panel.UserInfo{Id: 8, Uuid: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}
			limiter.AddLimiter(protocol, &conf.LimitConfig{}, []panel.UserInfo{user, retained})
			t.Cleanup(func() { limiter.DeleteLimiter(protocol) })
			for _, users := range [][]panel.UserInfo{{user}, {retained}} {
				if added, err := server.AddUsers(&core.AddUsersParams{Tag: protocol, Users: users, NodeInfo: info}); err != nil || added != len(users) {
					t.Fatalf("add dynamic users: added %d, error %v", added, err)
				}
			}
			echoAddress := migrationEcho(t)
			client := migrationOutbound(t, server, info, user, certPath, "client")
			connection := migrationDial(t, client, echoAddress)
			small := bytes.Repeat([]byte("s"), 2048)
			migrationExchange(t, connection, small)
			migrationWaitTraffic(t, server, protocol, user.Id, int64(len(small)))
			if traffic, err := server.GetUserTrafficSlice(protocol, true); err != nil || len(traffic) != 0 {
				t.Fatalf("report threshold was not preserved: %v, %v", traffic, err)
			}
			large := bytes.Repeat([]byte("l"), 16384)
			migrationExchange(t, connection, large)
			migrationWaitTraffic(t, server, protocol, user.Id, int64(len(small)+len(large)))
			traffic, err := server.GetUserTrafficSlice(protocol, true)
			if err != nil || len(traffic) != 1 || traffic[0].UID != user.Id || traffic[0].Upload < int64(len(large)) || traffic[0].Download < int64(len(large)) {
				t.Fatalf("unexpected reported traffic: %+v, %v", traffic, err)
			}
			traffic, err = server.GetUserTrafficSlice(protocol, false)
			if err != nil || len(traffic) != 1 || traffic[0].Upload != 0 || traffic[0].Download != 0 {
				t.Fatalf("reported counters were not reset: %+v, %v", traffic, err)
			}
			if err := server.DelUsers([]panel.UserInfo{user}, protocol, info); err != nil {
				t.Fatal(err)
			}
			if protocol == "vless" || protocol == "trojan" || protocol == "anytls" || protocol == "tuic" || protocol == "hysteria2" {
				connection.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, err := connection.Read(make([]byte, 1))
				if err == nil {
					t.Fatal("deleted user's existing connection remained open")
				}
				if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Fatalf("deleted user's existing connection was not revoked: %v", err)
				}
			}
			// A new client avoids treating a cached authenticated QUIC session as
			// a fresh credential check after deletion.
			deletedClient := migrationOutbound(t, server, info, user, certPath, "deleted-client")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if deletedConn, err := deletedClient.DialContext(ctx, "tcp", M.ParseSocksaddr(echoAddress)); err == nil {
				defer deletedConn.Close()
				deletedConn.SetDeadline(time.Now().Add(2 * time.Second))
				_, writeErr := deletedConn.Write([]byte("deleted"))
				if writeErr == nil {
					if _, readErr := deletedConn.Read(make([]byte, 7)); readErr == nil {
						t.Fatal("deleted user's credentials still authenticated")
					}
				}
			}
			otherClient := migrationOutbound(t, server, info, retained, certPath, "retained-client")
			migrationExchange(t, migrationDial(t, otherClient, echoAddress), []byte("retained user"))
			if err := server.DelNode(protocol); err != nil {
				t.Fatal(err)
			}
			if _, present := server.box.Inbound().Get(protocol); present {
				t.Fatal("deleted inbound remains registered")
			}
		})
	}
}

func migrationNode(protocol string, port int) *panel.NodeInfo {
	info := &panel.NodeInfo{Type: protocol, Common: &panel.CommonNode{ServerPort: port}}
	switch protocol {
	case "vmess", "vless":
		info.VAllss = &panel.VAllssNode{Network: "tcp"}
	case "shadowsocks":
		info.Shadowsocks = &panel.ShadowsocksNode{Cipher: "aes-128-gcm"}
	case "trojan":
		info.Security = panel.Tls
		info.Trojan = &panel.TrojanNode{Network: "tcp"}
	case "tuic":
		info.Security = panel.Tls
		info.Tuic = &panel.TuicNode{CongestionControl: "cubic"}
	case "anytls":
		info.Security = panel.Tls
		info.AnyTls = &panel.AnyTlsNode{}
	case "hysteria":
		info.Security = panel.Tls
		info.Hysteria = &panel.HysteriaNode{UpMbps: 100, DownMbps: 100}
	case "hysteria2":
		info.Security = panel.Tls
		info.Hysteria2 = &panel.Hysteria2Node{UpMbps: 100, DownMbps: 100}
	}
	return info
}

func migrationOutbound(t *testing.T, server *Sing, info *panel.NodeInfo, user panel.UserInfo, certPath, tag string) adapter.Outbound {
	t.Helper()
	config := map[string]any{"type": info.Type, "tag": tag, "server": "127.0.0.1", "server_port": info.Common.ServerPort}
	if info.Security == panel.Tls {
		config["tls"] = map[string]any{"enabled": true, "server_name": "localhost", "certificate_path": certPath}
	}
	switch info.Type {
	case "vmess", "vless":
		config["uuid"] = user.Uuid
	case "shadowsocks":
		config["method"], config["password"] = info.Shadowsocks.Cipher, user.Uuid
	case "trojan", "anytls", "hysteria2":
		config["password"] = user.Uuid
	case "tuic":
		config["uuid"], config["password"] = user.Uuid, user.Uuid
		config["congestion_control"] = "cubic"
		config["tls"].(map[string]any)["alpn"] = []string{"h3"}
	case "hysteria":
		config["auth_str"] = user.Uuid
		config["up_mbps"], config["down_mbps"] = 100, 100
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var outbound option.Outbound
	if err := sjson.UnmarshalContext(server.ctx, data, &outbound); err != nil {
		t.Fatal(err)
	}
	if err := server.box.Outbound().Create(server.ctx, server.router, server.logFactory.NewLogger(tag), tag, outbound.Type, outbound.Options); err != nil {
		t.Fatal(err)
	}
	result, found := server.box.Outbound().Outbound(tag)
	if !found {
		t.Fatal("created outbound was not registered")
	}
	// OutboundManager.Create registers an outbound; unlike dynamic inbound
	// creation it does not start components added after Box.Start.
	if lifecycle, ok := result.(adapter.Lifecycle); ok {
		scope := adapter.NewScope(server.ctx, server.logFactory.NewLogger(tag))
		t.Cleanup(func() { scope.Close() })
		for _, stage := range adapter.ListStartStages {
			if err := scope.Start(tag, lifecycle, stage); err != nil {
				t.Fatal(err)
			}
		}
	}
	return result
}

func migrationDial(t *testing.T, client adapter.Outbound, address string) net.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := client.DialContext(ctx, "tcp", M.ParseSocksaddr(address))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func migrationExchange(t *testing.T, connection net.Conn, payload []byte) {
	t.Helper()
	connection.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := connection.Write(payload); err != nil {
		t.Fatal(err)
	}
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, received); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, payload) {
		t.Fatal("proxied data changed")
	}
}

func migrationWaitTraffic(t *testing.T, server *Sing, tag string, uid int, minimum int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		traffic, err := server.GetUserTrafficSlice(tag, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range traffic {
			if entry.UID == uid && entry.Upload >= minimum && entry.Download >= minimum {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing non-resetting per-user traffic: %+v", traffic)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func migrationPort(t *testing.T, protocol string) int {
	t.Helper()
	if protocol == "tuic" || protocol == "hysteria" || protocol == "hysteria2" {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		return listener.LocalAddr().(*net.UDPAddr).Port
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func migrationEcho(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var access sync.Mutex
	var connections []net.Conn
	t.Cleanup(func() {
		listener.Close()
		access.Lock()
		defer access.Unlock()
		for _, connection := range connections {
			connection.Close()
		}
	})
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			access.Lock()
			connections = append(connections, connection)
			access.Unlock()
			go func() { defer connection.Close(); io.Copy(connection, connection) }()
		}
	}()
	return listener.Addr().String()
}

func migrationCertificate(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	certPath, keyPath := filepath.Join(directory, "certificate.pem"), filepath.Join(directory, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
