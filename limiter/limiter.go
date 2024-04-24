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
	log "github.com/sirupsen/logrus"
	"github.com/xtls/xray-core/common/task"
)

var nOnlineDevice *sync.Map
var ipAllowedMap *sync.Map
var Acount int

func init() {
	ipAllowedMap = new(sync.Map)
	nOnlineDevice = new(sync.Map)
}

var limitLock sync.RWMutex
var limiter map[string]*Limiter

func Init() {
	limiter = map[string]*Limiter{}
	c := task.Periodic{
		Interval: time.Minute * 2,
		Execute:  ClearOnlineIP,
	}
	go func() {
		log.WithField("Type", "Limiter").
			Debug("ClearOnlineIP started")
		time.Sleep(time.Minute * 2)
		_ = c.Start()
	}()
}

type Limiter struct {
	DomainRules   []*regexp.Regexp
	ProtocolRules []string
	SpeedLimit    int
	UserOnlineIP  *sync.Map      // Key: Name, value: {Key: Ip, value: Uid}
	UUIDtoUID     map[string]int // Key: UUID, value: UID
	UserLimitInfo *sync.Map      // Key: Uid value: UserLimitInfo
	ConnLimiter   *ConnLimiter   // Key: Uid value: ConnLimiter
	SpeedLimiter  *sync.Map      // key: Uid, value: *ratelimit.Bucket
}

type UserLimitInfo struct {
	UID               int
	SpeedLimit        int
	DeviceLimit       int
	DynamicSpeedLimit int
	ExpireTime        int64
}

func AddLimiter(tag string, l *conf.LimitConfig, users []panel.UserInfo) *Limiter {
	info := &Limiter{
		SpeedLimit:    l.SpeedLimit,
		UserOnlineIP:  new(sync.Map),
		UserLimitInfo: new(sync.Map),
		ConnLimiter:   NewConnLimiter(l.ConnLimit, l.IPLimit, l.EnableRealtime),
		SpeedLimiter:  new(sync.Map),
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

func (l *Limiter) UpdateDynamicSpeedLimit(tag, uuid string, limit int, expire time.Time) error {
	if v, ok := l.UserLimitInfo.Load(format.UserTag(tag, uuid)); ok {
		info := v.(*UserLimitInfo)
		info.DynamicSpeedLimit = limit
		info.ExpireTime = expire.Unix()
	} else {
		return errors.New("not found")
	}
	return nil
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

func (l *Limiter) CheckLimit(taguuid string, ip string, isTcp bool) (Bucket *ratelimit.Bucket, Reject bool) {
	// check and gen speed limit Bucket
	nodeLimit := l.SpeedLimit
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
				u.DynamicSpeedLimit = 0
				u.ExpireTime = 0
			} else {
				l.UserLimitInfo.Delete(taguuid)
			}
		} else {
			userLimit = determineSpeedLimit(u.SpeedLimit, u.DynamicSpeedLimit)
		}
	}

	ipMap := new(sync.Map)
	aliveIPs := GetUserAliveIPs(uid)
	ipStatus := ipAllowed(ip, aliveIPs)
	ipAllowedMap.Store(ip, ipStatus)
	log.Infof("Check: ipStatus=%d, userid=%d, aliveips=%s, devicelimit=%d, speedlimit=%d", ipStatus, uid, ip, deviceLimit, userLimit)
	if ipStatus == 2 && deviceLimit > 0 && deviceLimit <= len(aliveIPs) {
		return nil, true
	}
	// ip and conn limiter
	if l.ConnLimiter.AddConnCount(taguuid, ip, isTcp) {
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

	limit := int64(determineSpeedLimit(nodeLimit, userLimit)) * 1000000 / 8 // If you need the Speed limit
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

func onlineDevicesEqual(C int, D int, A *sync.Map, B *sync.Map) bool {
	if C == 0 && D == 0 {
		log.Infof("compare AB, [Same] prev nil & now nil")
		return false
	}
	if C != D {
		log.Infof("compare ABcount, [different] A:%d & D:%d", C, D)
		return true
	}
	diff := true
	A.Range(func(key, valueA interface{}) bool {
		if valueB, ok := B.Load(key); ok && valueB == valueA {
			log.Infof("compare AB, [Same] UID:%d, prev:%s now:%s", key, valueA, valueB)
			diff = false
			return false
		}
		return true
	})
	return diff
}

func (l *Limiter) GetOnlineDevice(userTraffic *sync.Map) (*[]panel.OnlineUser, bool, error) {
	var onlineUser []panel.OnlineUser
	prevonlineUser := new(sync.Map)
	nOnlineDevice.Range(func(key, value interface{}) bool {
		prevonlineUser.Store(key, value)
		return true
	})
	nOnlineDevice = new(sync.Map)
	Bcount := 0

	l.UserOnlineIP.Range(func(key, value interface{}) bool {
		email := key.(string)
		ipMap := value.(*sync.Map)
		var uid int
		var X bool
		ipMap.Range(func(key, value interface{}) bool {
			uid = value.(int)
			ip := key.(string)
			a, _ := ipAllowedMap.Load(ip)
			if _, b := userTraffic.Load(uid); b {
				X = b
			}
			if a.(int) != 2 && X {
				onlineUser = append(onlineUser, panel.OnlineUser{UID: uid, IP: ip})
				nOnlineDevice.Store(uid, ip)
				Bcount++
				log.Infof("onlineUser Store,UID: %d,IP: %s", uid, ip)
			}
			return true
		})
		if !X {
			log.Infof("Delete email: %s, uid: %d", email, uid)
			l.UserOnlineIP.Delete(email) // Reset online device
		}
		return true
	})
	diff := onlineDevicesEqual(Acount, Bcount, prevonlineUser, nOnlineDevice)
	Acount = Bcount
	return &onlineUser, diff, nil
}

type UserIpList struct {
	Uid    int      `json:"Uid"`
	IpList []string `json:"Ips"`
}

func determineDeviceLimit(nodeLimit, userLimit int) (limit int) {
	if nodeLimit == 0 || userLimit == 0 {
		if nodeLimit > userLimit {
			return nodeLimit
		} else if nodeLimit < userLimit {
			return userLimit
		} else {
			return 0
		}
	} else {
		if nodeLimit > userLimit {
			return userLimit
		} else if nodeLimit < userLimit {
			return nodeLimit
		} else {
			return nodeLimit
		}
	}
}
