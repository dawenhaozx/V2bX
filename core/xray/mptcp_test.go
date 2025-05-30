package xray

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	"github.com/xtls/xray-core/app/proxyman"
)

func TestBuildInboundMPTCP(t *testing.T) {
	for _, network := range []string{"tcp", "ws", "grpc"} {
		for _, enabled := range []bool{false, true} {
			t.Run(network+"/"+strconv.FormatBool(enabled), func(t *testing.T) {
				options := &conf.Options{
					ListenIP:    "127.0.0.1",
					XrayOptions: conf.NewXrayOptions(),
				}
				options.XrayOptions.EnableMPTCP = enabled
				options.XrayOptions.EnableProxyProtocol = true
				options.XrayOptions.EnableTFO = float64(256)
				node := &panel.NodeInfo{
					Type:   "vmess",
					Common: &panel.CommonNode{ServerPort: 12345},
					VAllss: &panel.VAllssNode{
						Network:         network,
						NetworkSettings: json.RawMessage(`{}`),
					},
				}
				inbound, err := buildInbound(options, node, "test")
				if err != nil {
					t.Fatal(err)
				}
				message, err := inbound.ReceiverSettings.GetInstance()
				if err != nil {
					t.Fatal(err)
				}
				receiver, ok := message.(*proxyman.ReceiverConfig)
				if !ok {
					t.Fatalf("unexpected receiver type %T", message)
				}
				socket := receiver.GetStreamSettings().GetSocketSettings()
				if got := socket.GetTcpMptcp(); got != enabled {
					t.Errorf("TcpMptcp = %v, want %v", got, enabled)
				}
				if network == "grpc" {
					if socket.GetTfo() != 256 || !socket.GetAcceptProxyProtocol() {
						t.Errorf("existing socket options changed: %v", socket)
					}
				}
			})
		}
	}
}
