package conf

import (
	"encoding/json"
	"testing"
)

func TestMPTCPDefaults(t *testing.T) {
	if NewXrayOptions().EnableMPTCP {
		t.Error("Xray MPTCP must be disabled by default")
	}
	if NewSingOptions().TCPMultiPath {
		t.Error("sing MPTCP must be disabled by default")
	}
}

func TestXraySocketOptionsJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		tfo  any
	}{
		{"enabled", `{"Core":"xray","EnableTFO":true,"EnableMPTCP":true}`, true},
		{"disabled", `{"Core":"xray","EnableTFO":false,"EnableMPTCP":true}`, false},
		{"numeric", `{"Core":"xray","EnableTFO":256,"EnableMPTCP":true}`, float64(256)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var options Options
			if err := json.Unmarshal([]byte(test.data), &options); err != nil {
				t.Fatal(err)
			}
			if options.XrayOptions.EnableTFO != test.tfo {
				t.Errorf("EnableTFO = %v, want %v", options.XrayOptions.EnableTFO, test.tfo)
			}
			if !options.XrayOptions.EnableMPTCP {
				t.Error("EnableMPTCP was not parsed")
			}
		})
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
