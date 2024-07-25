package node

import (
	"strconv"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	log "github.com/sirupsen/logrus"
)

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
	c.limiter.ResetOtraffic(c.tag)
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

func (c *Controller) onlineIpReport() (err error) {
	if time.Now().Unix()%int64(c.info.PushInterval.Seconds()) != 0 {
		_ = c.restartonlineIpReport
	}

	// Get User traffic
	ATraffic := make(map[int]int64)
	for i := range c.userList {
		up, down := c.server.GetUserTraffic(c.tag, c.userList[i].Uuid, false)
		nud := up + down
		ATraffic[c.userList[i].Id] = nud
	}

	onlineDevice, diff, err := c.limiter.GetOnlineDevice(c.tag, ATraffic, c.Options.DeviceOnlineMinTraffic*1000)
	if err != nil {
		log.Print(err)
	} else if diff || (len(*onlineDevice) > 0 && time.Since(c.nextsend) >= 120*time.Second) {
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
			c.nextsend = time.Now()
			// log.WithField("tag", c.tag).Infof("Total %d online users, %d Reported", len(*onlineDevice), len(*onlineDevice))
		}
	}
	return nil
}

func (c *Controller) getonlineIpReport() (err error) {
	if time.Now().Unix()%int64(c.info.PushInterval.Seconds()+5) != 0 {
		_ = c.restartgetonlineIpReport
	}
	// Get Online info
	c.apiClient.GetIpsList()

	return nil
}

func (c *Controller) restartonlineIpReport() (err error) {
	log.Infof("onlineIpReport time:%s", time.Now())
	c.onlineIpReportPeriodic.Interval = c.info.PushInterval
	time.Sleep(time.Duration(int64(c.info.PushInterval.Seconds())-time.Now().Unix()%int64(c.info.PushInterval.Seconds())) * time.Second)
	c.onlineIpReportPeriodic.Close()
	_ = c.onlineIpReportPeriodic.Start(false)
	return nil
}

func (c *Controller) restartgetonlineIpReport() (err error) {
	log.Infof("onlineIpReport time:%s", time.Now())
	c.onlineIpReportPeriodic.Interval = c.info.PushInterval + 5*time.Second
	time.Sleep(time.Duration(int64(c.info.PushInterval.Seconds())-time.Now().Unix()%int64(c.info.PushInterval.Seconds())) * time.Second)
	c.onlineIpReportPeriodic.Close()
	_ = c.onlineIpReportPeriodic.Start(false)
	return nil
}
