package sing

import (
	"context"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/sagernet/sing-box/adapter"
)

type testFlowHandle struct{ closed bool }

func (h *testFlowHandle) CloseFlow() { h.closed = true }

func TestManagedForwardingFlowDoesNotBypassConnectionPolicies(t *testing.T) {
	limiter.Init()
	limiter.AddLimiter(t.Name(), &conf.LimitConfig{}, []panel.UserInfo{{Id: 1, Uuid: "user"}})
	t.Cleanup(func() { limiter.DeleteLimiter(t.Name()) })
	hook := &HookServer{}
	tracker := hook.RoutedFlow(context.Background(), adapter.InboundContext{Inbound: t.Name(), User: "user"}, nil, nil)
	if tracker == nil {
		t.Fatal("managed forwarding flow bypassed connection policies")
	}
	handle := &testFlowHandle{}
	tracker.AttachFlow(handle)
	if !handle.closed {
		t.Fatal("unsupported managed forwarding flow was not closed")
	}
	if tracker := hook.RoutedFlow(context.Background(), adapter.InboundContext{Inbound: "unmanaged"}, nil, nil); tracker != nil {
		t.Fatal("custom unmanaged forwarding flow was unexpectedly changed")
	}
}
