package limiter

import (
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/conf"
	"github.com/juju/ratelimit"
)

var limitLock sync.RWMutex
var limiter map[string]*Limiter

func Init() {
	limiter = map[string]*Limiter{}
}

type Limiter struct {
	DomainRules   []*regexp.Regexp
	ProtocolRules []string
	SpeedLimit    int
	UserOnlineIP  *sync.Map      // Key: Name, value: {Key: Ip, value: Uid}
	UUIDtoUID     map[string]int // Key: UUID, value: UID
	UserLimitInfo *sync.Map      // Key: Uid value: UserLimitInfo
	SpeedLimiter  *sync.Map      // key: Uid, value: *ratelimit.Bucket
	OnlineDevice  *sync.Map
	ipAllowedMap  *sync.Map
	Otraffic      *sync.Map
}

type UserLimitInfo struct {
	UID         int
	SpeedLimit  int
	DeviceLimit int
	ExpireTime  int64
}

func AddLimiter(tag string, l *conf.LimitConfig, users []panel.UserInfo) *Limiter {
	info := &Limiter{
		SpeedLimit:    l.SpeedLimit,
		UserOnlineIP:  new(sync.Map),
		UserLimitInfo: new(sync.Map),
		SpeedLimiter:  new(sync.Map),
		OnlineDevice:  new(sync.Map),
		ipAllowedMap:  new(sync.Map),
		Otraffic:      new(sync.Map),
	}
	uuidmap := make(map[string]int)
	for i := range users {
		uuidmap[users[i].Uuid] = users[i].Id
		userLimit := &UserLimitInfo{}
		userLimit.UID = users[i].Id
		if users[i].SpeedLimit != 0 {
			userLimit.SpeedLimit = users[i].SpeedLimit
		}
		if users[i].DeviceLimit != 0 {
			userLimit.DeviceLimit = users[i].DeviceLimit
		}
		info.UserLimitInfo.Store(format.UserTag(tag, users[i].Uuid), userLimit)
	}
	info.UUIDtoUID = uuidmap
	limitLock.Lock()
	limiter[tag] = info
	limitLock.Unlock()
	return info
}

func GetLimiter(tag string) (info *Limiter, err error) {
	limitLock.RLock()
	info, ok := limiter[tag]
	limitLock.RUnlock()
	if !ok {
		return nil, errors.New("not found")
	}
	return info, nil
}

func DeleteLimiter(tag string) {
	limitLock.Lock()
	delete(limiter, tag)
	limitLock.Unlock()
}

func (l *Limiter) UpdateUser(tag string, added []panel.UserInfo, deleted []panel.UserInfo) {
	for i := range deleted {
		l.UserLimitInfo.Delete(format.UserTag(tag, deleted[i].Uuid))
		delete(l.UUIDtoUID, deleted[i].Uuid)
	}
	for i := range added {
		userLimit := &UserLimitInfo{
			UID: added[i].Id,
		}
		if added[i].SpeedLimit != 0 {
			userLimit.SpeedLimit = added[i].SpeedLimit
			userLimit.ExpireTime = 0
		}
		if added[i].DeviceLimit != 0 {
			userLimit.DeviceLimit = added[i].DeviceLimit
		}
		l.UserLimitInfo.Store(format.UserTag(tag, added[i].Uuid), userLimit)
		l.UUIDtoUID[added[i].Uuid] = added[i].Id
	}
}

func GetUserAliveIPs(user int) []string {
	v, ok := panel.UserAliveIPsMap.Load(user)
	if !ok || v == nil {
		return nil
	}
	return v.([]string)
}

func ipAllowed(ip string, aliveIPs []string) int {
	if len(aliveIPs) == 0 {
		return 0 // AliveIPs为空
	}
	for _, aliveIP := range aliveIPs {
		if aliveIP == ip {
			return 1 // IP在AliveIPs中
		}
	}
	return 2 // IP不在AliveIPs中
}

func (l *Limiter) CheckLimit(taguuid string, ip string) (Bucket *ratelimit.Bucket, Reject bool) {
	// check and gen speed limit Bucket
	userLimit := 0
	deviceLimit := 0
	var uid int
	if v, ok := l.UserLimitInfo.Load(taguuid); ok {
		u := v.(*UserLimitInfo)
		deviceLimit = u.DeviceLimit
		uid = u.UID
		if u.ExpireTime < time.Now().Unix() && u.ExpireTime != 0 {
			if u.SpeedLimit != 0 {
				userLimit = u.SpeedLimit
				u.ExpireTime = 0
			} else {
				l.UserLimitInfo.Delete(taguuid)
			}
		}
	}
	ipMap := new(sync.Map)
	aliveIPs := GetUserAliveIPs(uid)
	ipStatus := ipAllowed(ip, aliveIPs)
	l.ipAllowedMap.Store(ip, ipStatus)
	// log.Infof("Check: ipStatus=%d, userid=%d, aliveips=%s, devicelimit=%d, speedlimit=%d", ipStatus, uid, ip, deviceLimit, userLimit)
	if ipStatus == 2 && deviceLimit > 0 && deviceLimit <= len(aliveIPs) {
		return nil, true
	}

	// Store online user for device limit
	ipMap.Store(ip, uid)
	// If any device is online
	if v, ok := l.UserOnlineIP.LoadOrStore(taguuid, ipMap); ok {
		ipMap := v.(*sync.Map)
		// If this is a new ip
		if _, ok := ipMap.LoadOrStore(ip, uid); !ok {
			counter := 0
			ipMap.Range(func(key, value interface{}) bool {
				counter++
				return true
			})
			if ipStatus != 1 && deviceLimit > 0 && deviceLimit < counter+len(aliveIPs) {
				ipMap.Delete(ip)
				return nil, true
			}
		}
	}

	limit := int64(userLimit) * 1000000 / 8 // If you need the Speed limit
	if limit > 0 {
		Bucket = ratelimit.NewBucketWithQuantum(time.Second, limit, limit) // Byte/s
		if v, ok := l.SpeedLimiter.LoadOrStore(taguuid, Bucket); ok {
			return v.(*ratelimit.Bucket), false
		} else {
			l.SpeedLimiter.Store(taguuid, Bucket)
			return Bucket, false
		}
	} else {
		return nil, false
	}
}

func (l *Limiter) GetOnlineDevice(tag string, userTraffic map[int]int64, T int64) (*[]panel.OnlineUser, bool, error) {
	var onlineUser []panel.OnlineUser

	PrevT := make(map[int]int64)
	PrevO := make(map[int]string)
	l.Otraffic.Range(func(key, value interface{}) bool {
		PrevT[key.(int)] = value.(int64)
		return true
	})
	l.OnlineDevice.Range(func(key, value interface{}) bool {
		PrevO[key.(int)] = value.(string)
		return true
	})
	l.OnlineDevice = new(sync.Map)
	l.Otraffic = new(sync.Map)
	diff := false
	l.UserOnlineIP.Range(func(key, value interface{}) bool {
		email := key.(string)
		ipMap := value.(*sync.Map)
		var uid int
		var X int64
		var A int
		var pip string
		ipMap.Range(func(key, value interface{}) bool {
			uid = value.(int)
			ip := key.(string)
			if a, aok := l.ipAllowedMap.Load(ip); aok {
				A = a.(int)
			}
			l.Otraffic.Store(uid, userTraffic[uid])
			X = userTraffic[uid] - PrevT[uid]
			pip = PrevO[uid]
			if A != 2 {
				if X <= T {
					ip = ""
				}
				if pip != ip {
					diff = true
				}
				onlineUser = append(onlineUser, panel.OnlineUser{UID: uid, IP: ip})
				l.OnlineDevice.Store(uid, ip)
				// log.Infof("onlineUser Store,UID: %d,IP: %s", uid, ip)
			}
			return true
		})
		if A == 2 || X <= T {
			// log.Infof("Delete email: %s, uid: %d", email, uid)
			l.UserOnlineIP.Delete(email) // Reset online device
		}
		return true
	})

	return &onlineUser, diff, nil
}

type UserIpList struct {
	Uid    int      `json:"Uid"`
	IpList []string `json:"Ips"`
}

func (l *Limiter) ResetOtraffic(tag string) error {
	limitLock.Lock()
	limiter[tag].Otraffic = new(sync.Map)
	limitLock.Unlock()
	return nil
}
