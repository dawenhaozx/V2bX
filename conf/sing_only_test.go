package conf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSingConfigDefaults(t *testing.T) {
	var core CoreConfig
	if err := json.Unmarshal([]byte(`{}`), &core); err != nil {
		t.Fatal(err)
	}
	if core.Type != "sing" || core.SingConfig == nil {
		t.Fatalf("missing core type did not select sing: %#v", core)
	}
	var options Options
	if err := json.Unmarshal([]byte(`{"EnableTFO":true,"TCPMultiPath":true}`), &options); err != nil {
		t.Fatal(err)
	}
	if options.Core != "sing" || options.SingOptions == nil {
		t.Fatalf("missing node core did not initialize sing options: %#v", options)
	}
	if !options.SingOptions.TCPFastOpen || !options.SingOptions.TCPMultiPath {
		t.Fatal("default sing selection lost the node socket options")
	}
}

func TestRemovedCoreTypesAreRejected(t *testing.T) {
	for _, coreType := range []string{"xray", "hysteria2", "unknown"} {
		t.Run(coreType, func(t *testing.T) {
			var core CoreConfig
			data, err := json.Marshal(map[string]string{"Type": coreType})
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &core); err == nil {
				t.Errorf("removed core type %q was accepted", coreType)
			}
			var node Options
			data, err = json.Marshal(map[string]string{"Core": coreType})
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &node); err == nil {
				t.Errorf("node using removed core %q was accepted", coreType)
			}
		})
	}
}

func TestLoadNamedSingInstancesAndHysteria2Node(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{
		"Cores": [
			{"Type":"sing","Name":"primary","Log":{"Level":"warn"}},
			{"Name":"secondary","Log":{"Level":"debug"}}
		],
		"Nodes": [
			{"ApiHost":"http://127.0.0.1","NodeType":"hysteria2","CoreName":"secondary","EnableTFO":true,"TCPMultiPath":true},
			{"ApiConfig":{"NodeType":"vmess"},"Options":{"Core":"sing","CoreName":"primary","EnableSniff":false}}
		]
	}`)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	c := New()
	if err := c.LoadFromPath(configPath); err != nil {
		t.Fatal(err)
	}
	if len(c.CoresConfig) != 2 || len(c.NodeConfig) != 2 {
		t.Fatalf("lost named instances or nodes: %d cores, %d nodes", len(c.CoresConfig), len(c.NodeConfig))
	}
	for i, name := range []string{"primary", "secondary"} {
		if c.CoresConfig[i].Type != "sing" || c.CoresConfig[i].Name != name || c.CoresConfig[i].SingConfig == nil {
			t.Errorf("sing instance %d was not retained: %#v", i, c.CoresConfig[i])
		}
	}
	if c.CoresConfig[0].SingConfig.LogConfig.Level != "warn" || c.CoresConfig[1].SingConfig.LogConfig.Level != "debug" {
		t.Error("named sing instances lost their separate log configurations")
	}
	first := c.NodeConfig[0]
	if first.ApiConfig.NodeType != "hysteria2" || first.Options.Core != "sing" || first.Options.CoreName != "secondary" {
		t.Fatalf("hysteria2 protocol no longer selects a named sing instance: %#v", first)
	}
	if first.Options.SingOptions == nil || !first.Options.SingOptions.TCPFastOpen || !first.Options.SingOptions.TCPMultiPath {
		t.Fatal("flat node configuration lost sing socket options")
	}
	second := c.NodeConfig[1]
	if second.ApiConfig.NodeType != "vmess" || second.Options.CoreName != "primary" || second.Options.SingOptions == nil || second.Options.SingOptions.SniffEnabled {
		t.Fatalf("nested ApiConfig/Options configuration was not retained: %#v", second)
	}
}
