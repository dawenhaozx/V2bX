package node

import (
	"strconv"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) reportUserTrafficTask() (err error) {
	c.trafficMu.Lock()
	defer c.trafficMu.Unlock()
	userTraffic, _ := c.server.GetUserTrafficSlice(c.tag, true)
	c.limiter.ResetOtraffic(userTraffic)
	if len(userTraffic) > 0 {
		err = c.apiClient.ReportUserTraffic(userTraffic)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Info("Report user traffic failed")
		} else {
			log.WithField("tag", c.tag).Infof("Report %d users traffic", len(userTraffic))
			log.WithField("tag", c.tag).Debugf("User traffic: %+v", userTraffic)
		}
	}
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
	c.trafficMu.Lock()
	defer c.trafficMu.Unlock()
	if seconds := int64(c.info.PushInterval.Seconds()); seconds > 0 && time.Now().Unix()%seconds != 0 {
		_ = c.restartonlineIpReport
	}

	// Get User traffic
	ATraffic := make(map[int]int64)
	userTraffic, err := c.server.GetUserTrafficSlice(c.tag, false)
	if err != nil {
		log.WithField("tag", c.tag).WithError(err).Error("Get online user traffic failed")
		return nil
	}
	for _, traffic := range userTraffic {
		ATraffic[traffic.UID] += traffic.Upload + traffic.Download
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
	waitForNextInterval(c.info.PushInterval)
	c.onlineIpReportPeriodic.Close()
	_ = c.onlineIpReportPeriodic.Start(false)
	return nil
}

func (c *Controller) restartgetonlineIpReport() (err error) {
	log.Infof("onlineIpReport time:%s", time.Now())
	c.onlineIpReportPeriodic.Interval = c.info.PushInterval + 5*time.Second
	waitForNextInterval(c.info.PushInterval)
	c.onlineIpReportPeriodic.Close()
	_ = c.onlineIpReportPeriodic.Start(false)
	return nil
}

func waitForNextInterval(interval time.Duration) {
	seconds := int64(interval.Seconds())
	if seconds > 0 {
		time.Sleep(time.Duration(seconds-time.Now().Unix()%seconds) * time.Second)
	}
}
