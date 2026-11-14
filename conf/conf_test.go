package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConf_LoadFromPath(t *testing.T) {
	c := New()
	if err := c.LoadFromPath("../example/config.json"); err != nil {
		t.Fatal(err)
	}
}

func TestConf_Watch(t *testing.T) {
	for _, changedFile := range []string{"config", "dns"} {
		t.Run(changedFile, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			dnsPath := filepath.Join(dir, "dns.json")
			writeConfig := func(level string) {
				t.Helper()
				data := fmt.Sprintf(`{"Log":{"Level":%q},"Cores":[{}],"Nodes":[]}`, level)
				if err := os.WriteFile(configPath, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeConfig("info")
			if err := os.WriteFile(dnsPath, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			c := New()
			if changedFile == "dns" {
				// The DNS event must cause a fresh read of the main configuration.
				writeConfig("debug")
			}
			reloaded := make(chan string, 1)
			watcher, err := c.watchFiles(configPath, dnsPath, func() {
				reloaded <- c.LogConfig.Level
			}, 20*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { watcher.Close() })
			if changedFile == "config" {
				writeConfig("debug")
			} else if err := os.WriteFile(dnsPath, []byte(`{"servers":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case level := <-reloaded:
				if level != "debug" {
					t.Fatalf("reloaded log level = %q, want debug", level)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("file change did not trigger a configuration reload")
			}
		})
	}
}

func TestConf_WatchMissingFile(t *testing.T) {
	c := New()
	missing := filepath.Join(t.TempDir(), "missing.json")
	if err := c.Watch(missing, "", func() { t.Error("missing file triggered reload") }); err == nil {
		t.Fatal("watching a missing file should fail")
	}
}
