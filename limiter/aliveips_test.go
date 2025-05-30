package limiter

import (
	"sync"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/conf"
)

func newAliveIPsTestLimiter(t *testing.T, users []panel.UserInfo) (*Limiter, string) {
	t.Helper()
	Init()
	tag := t.Name()
	l := AddLimiter(tag, &conf.LimitConfig{}, users)
	previous := panel.UserAliveIPsMap.Load()
	panel.UserAliveIPsMap.Store(new(sync.Map))
	t.Cleanup(func() {
		panel.UserAliveIPsMap.Store(previous)
		DeleteLimiter(tag)
	})
	return l, tag
}

func TestCheckLimitAliveIPsAtDeviceLimit(t *testing.T) {
	user := panel.UserInfo{Id: 1, Uuid: "user", DeviceLimit: 2}
	l, tag := newAliveIPsTestLimiter(t, []panel.UserInfo{user})
	panel.UserAliveIPsMap.Load().Store(user.Id, []string{"192.0.2.1", "192.0.2.2"})
	userTag := format.UserTag(tag, user.Uuid)

	for _, ip := range []string{"192.0.2.1", "::ffff:192.0.2.1", "192.0.2.2"} {
		if _, rejected := l.CheckLimit(userTag, ip, true, true); rejected {
			t.Errorf("existing alive IP %q was rejected at the device limit", ip)
		}
	}
	if _, rejected := l.CheckLimit(userTag, "192.0.2.3", true, true); !rejected {
		t.Fatal("new IP was accepted after the remote alive IPs reached the device limit")
	}

	v, ok := l.UserOnlineIP.Load(userTag)
	if !ok {
		t.Fatal("accepted IPs were not recorded")
	}
	ips := v.(*sync.Map)
	if _, ok := ips.Load("192.0.2.1"); !ok {
		t.Error("IPv4 address was not recorded in normalized form")
	}
	if _, ok := ips.Load("::ffff:192.0.2.1"); ok {
		t.Error("IPv4-mapped IPv6 address was recorded as a separate device")
	}
	if _, ok := ips.Load("192.0.2.3"); ok {
		t.Error("rejected IP was recorded as online")
	}
}

func TestUpdateUserDeletesOnlineAndSpeedLimiter(t *testing.T) {
	deleted := panel.UserInfo{Id: 1, Uuid: "deleted", SpeedLimit: 10}
	retained := panel.UserInfo{Id: 2, Uuid: "retained", SpeedLimit: 10}
	l, tag := newAliveIPsTestLimiter(t, []panel.UserInfo{deleted, retained})
	deletedTag := format.UserTag(tag, deleted.Uuid)
	retainedTag := format.UserTag(tag, retained.Uuid)
	for _, userTag := range []string{deletedTag, retainedTag} {
		if bucket, rejected := l.CheckLimit(userTag, "192.0.2.1", true, true); rejected || bucket == nil {
			t.Fatalf("could not initialize online and speed limit state for %q", userTag)
		}
	}

	l.UpdateUser(tag, nil, []panel.UserInfo{deleted})

	for name, state := range map[string]*sync.Map{
		"user limit":    l.UserLimitInfo,
		"online IPs":    l.UserOnlineIP,
		"speed limiter": l.SpeedLimiter,
	} {
		if _, ok := state.Load(deletedTag); ok {
			t.Errorf("deleted user's %s state was retained", name)
		}
		if _, ok := state.Load(retainedTag); !ok {
			t.Errorf("other user's %s state was removed", name)
		}
	}
	if _, ok := l.UUIDtoUID[deleted.Uuid]; ok {
		t.Error("deleted user's UUID mapping was retained")
	}
	if _, rejected := l.CheckLimit(deletedTag, "192.0.2.1", true, true); !rejected {
		t.Error("deleted user was still allowed to connect")
	}
}

func TestResetOtrafficPreservesUnreportedBaselines(t *testing.T) {
	l := &Limiter{Otraffic: new(sync.Map)}
	l.Otraffic.Store(1, int64(100))
	l.Otraffic.Store(2, int64(200))
	l.Otraffic.Store(3, int64(300))

	l.ResetOtraffic([]panel.UserTraffic{
		{UID: 1, Upload: 100},
		{UID: 3, Download: 300},
	})

	for _, uid := range []int{1, 3} {
		if _, ok := l.Otraffic.Load(uid); ok {
			t.Errorf("reset user's traffic baseline was retained: UID %d", uid)
		}
	}
	if baseline, ok := l.Otraffic.Load(2); !ok || baseline != int64(200) {
		t.Fatalf("unreported user's baseline changed: got %v, present %v", baseline, ok)
	}
	l.ResetOtraffic(nil)
	if baseline, ok := l.Otraffic.Load(2); !ok || baseline != int64(200) {
		t.Error("empty traffic report removed an existing baseline")
	}
}
