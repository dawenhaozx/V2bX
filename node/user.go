package node

import (
	"strconv"
	"sync"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	log "github.com/sirupsen/logrus"
)

var onlineUserTraffic *sync.Map
var nextsend time.Time

func init() {
	onlineUserTraffic = new(sync.Map)
	nextsend = time.Now()
}

func (c *Controller) reportUserTrafficTask() (err error) {
	// Get User traffic
	userTraffic := make([]panel.UserTraffic, 0)
	for i := range c.userList {
		up, down := c.server.GetUserTraffic(c.tag, c.userList[i].Uuid, true)
		if up > 0 || down > 0 {
			if c.LimitConfig.EnableDynamicSpeedLimit {
				c.traffic[c.userList[i].Uuid] += up + down
			}
			userTraffic = append(userTraffic, panel.UserTraffic{
				UID:      (c.userList)[i].Id,
				Upload:   up,
				Download: down})
		}
	}
	if len(userTraffic) > 0 {
		err = c.apiClient.ReportUserTraffic(userTraffic)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Info("Report user traffic failed")
		} else {
			log.WithField("tag", c.tag).Infof("Report %d users traffic", len(userTraffic))
		}
	}
	onlineUserTraffic = new(sync.Map)
	userTraffic = nil
	return nil
}

func compareUserList(old, new []panel.UserInfo) (deleted, added []panel.UserInfo) {
	oldMap := make(map[string]int)
	for i, user := range old {
		key := user.Uuid + strconv.Itoa(user.SpeedLimit)
		oldMap[key] = i
	}

	for _, user := range new {
		key := user.Uuid + strconv.Itoa(user.SpeedLimit)
		if _, exists := oldMap[key]; !exists {
			added = append(added, user)
		} else {
			delete(oldMap, key)
		}
	}

	for _, index := range oldMap {
		deleted = append(deleted, old[index])
	}

	return deleted, added
}

func (c *Controller) restartonlineIpReport() (err error) {
	log.Infof("onlineIpReport time:%s", time.Now())
	c.onlineIpReportPeriodic.Interval = c.info.PushInterval
	time.Sleep(time.Duration(int64(c.info.PushInterval.Seconds())-time.Now().Unix()%int64(c.info.PushInterval.Seconds())) * time.Second)
	c.onlineIpReportPeriodic.Close()
	_ = c.onlineIpReportPeriodic.Start(false)
	return nil
}

func (c *Controller) onlineIpReport() (err error) {
	if time.Now().Unix()%int64(c.info.PushInterval.Seconds()) != 0 {
		_ = c.restartonlineIpReport
	}
	b := int64(c.info.PushInterval.Seconds()) * 1000
	// Get Online info
	c.apiClient.GetIpsList()

	// Get User traffic
	ATraffic := new(sync.Map)
	for i := range c.userList {
		up, down := c.server.GetUserTraffic(c.tag, c.userList[i].Uuid, false)
		nud := up + down
		v, ok := onlineUserTraffic.Load(c.userList[i].Id)
		if !ok || v == nil {
			// 如果值为 nil，则设为零
			v = int64(0)
		}
		pud := v.(int64)
		npud := nud - pud
		log.Infof("UID: %d, nud - pud: %d, prevup&down: %d ", c.userList[i].Id, npud, pud)
		if npud > b {
			ATraffic.Store(c.userList[i].Id, nud)
		}
		onlineUserTraffic.Store(c.userList[i].Id, nud)
	}

	onlineDevice, diff, err := c.limiter.GetOnlineDevice(ATraffic)
	if err != nil {
		log.Print(err)
	} else if diff || (len(*onlineDevice) > 0 && time.Since(nextsend) >= 120*time.Second) {
		// Only report user has traffic > 100kb to allow ping test
		reportOnline := make(map[int]int)
		data := make(map[int][]string)
		for _, onlineuser := range *onlineDevice {
			// json structure: { UID1:["ip1","ip2"],UID2:["ip3","ip4"] }
			data[onlineuser.UID] = append(data[onlineuser.UID], onlineuser.IP)
			reportOnline[onlineuser.UID]++
		}
		if err = c.apiClient.ReportNodeOnlineUsers(&data, &reportOnline); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Info("Report online users failed")
		} else {
			nextsend = time.Now()
			log.WithField("tag", c.tag).Infof("Total %d online users, %d Reported", len(*onlineDevice), len(*onlineDevice))
		}
	}
	return nil
}
