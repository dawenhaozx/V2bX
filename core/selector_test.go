package core

import (
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
)

type testSingInstance struct {
	addedTag   string
	deletedTag string
	uid        int
}

func (c *testSingInstance) Start() error { return nil }
func (c *testSingInstance) Close() error { return nil }
func (c *testSingInstance) AddNode(tag string, _ *panel.NodeInfo, _ *conf.Options) error {
	c.addedTag = tag
	return nil
}
func (c *testSingInstance) DelNode(tag string) error {
	c.deletedTag = tag
	return nil
}
func (c *testSingInstance) AddUsers(p *AddUsersParams) (int, error) { return len(p.Users), nil }
func (c *testSingInstance) DelUsers([]panel.UserInfo, string, *panel.NodeInfo) error {
	return nil
}
func (c *testSingInstance) GetUserTrafficSlice(string, bool) ([]panel.UserTraffic, error) {
	return []panel.UserTraffic{{UID: c.uid, Upload: 1024}}, nil
}
func (c *testSingInstance) Protocols() []string { return []string{"vmess", "hysteria2"} }
func (c *testSingInstance) Type() string        { return "sing" }

func TestSelectorRoutesNamedSingInstances(t *testing.T) {
	previous := cores
	t.Cleanup(func() { cores = previous })
	instances := map[string]*testSingInstance{
		"primary":   {uid: 1},
		"secondary": {uid: 2},
	}
	cores = map[string]func(*conf.CoreConfig) (Core, error){
		"sing": func(config *conf.CoreConfig) (Core, error) {
			if config.Type != "sing" || config.SingConfig == nil {
				t.Fatalf("sing instance defaults were not initialized: %#v", config)
			}
			return instances[config.Name], nil
		},
	}
	c, err := NewCore([]conf.CoreConfig{{Name: "primary"}, {Type: "sing", Name: "secondary"}})
	if err != nil {
		t.Fatal(err)
	}
	options := &conf.Options{CoreName: "secondary"}
	if err := c.AddNode("hy2-node", &panel.NodeInfo{Type: "hysteria2"}, options); err != nil {
		t.Fatal(err)
	}
	if instances["primary"].addedTag != "" || instances["secondary"].addedTag != "hy2-node" {
		t.Fatal("CoreName did not route the node to the requested sing instance")
	}
	if options.Core != "sing" || options.SingOptions == nil {
		t.Fatal("node did not receive default sing options")
	}
	traffic, err := c.GetUserTrafficSlice("hy2-node", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(traffic) != 1 || traffic[0].UID != 2 {
		t.Fatalf("traffic came from the wrong sing instance: %#v", traffic)
	}
	if err := c.DelNode("hy2-node"); err != nil {
		t.Fatal(err)
	}
	if instances["secondary"].deletedTag != "hy2-node" {
		t.Fatal("node deletion was not routed to the selected sing instance")
	}
}
