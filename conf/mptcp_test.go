package conf

import (
	"encoding/json"
	"testing"
)

func TestMPTCPDefaults(t *testing.T) {
	if NewSingOptions().TCPMultiPath {
		t.Error("sing MPTCP must be disabled by default")
	}
}

func TestSingMPTCPJSON(t *testing.T) {
	var options Options
	if err := json.Unmarshal([]byte(`{"Core":"sing","TCPMultiPath":true}`), &options); err != nil {
		t.Fatal(err)
	}
	if !options.SingOptions.TCPMultiPath {
		t.Error("TCPMultiPath was not parsed")
	}
}
