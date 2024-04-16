package node

import (
	"fmt"
	"strconv"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	log "github.com/sirupsen/logrus"
)

var onlineuserTraffic = make(map[int]panel.UserTraffic)

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
	onlineuserTraffic = make(map[int]panel.UserTraffic)
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
	log.Printf("onlineIpReport time:%s", time.Now())
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
	// Get Online info
	c.apiClient.GetIpsList()

	// Get User traffic
	userTraffic := make(map[int]panel.UserTraffic)
	for i := range c.userList {
		up, down := c.server.GetUserTraffic(c.tag, c.userList[i].Uuid, false)
		preuserTraffic := onlineuserTraffic[(c.userList)[i].Id]
		if (up > 0 || down > 0) && (up+down-preuserTraffic.Upload-preuserTraffic.Download > 30000) {
			userTraffic[(c.userList)[i].Id] = panel.UserTraffic{
				UID:      (c.userList)[i].Id,
				Upload:   up,
				Download: down,
			}
		}
		onlineuserTraffic[(c.userList)[i].Id] = panel.UserTraffic{
			UID:      (c.userList)[i].Id,
			Upload:   up,
			Download: down,
		}
	}
	if onlineDevice, err := c.limiter.GetOnlineDevice(userTraffic); err != nil {
		log.Print(err)
	} else if len(*onlineDevice) > 0 || c.initialZeroed {
		// Only report user has traffic > 100kb to allow ping test
		reportOnline := make(map[int]int)
		data := make(map[int][]string)
		for _, onlineuser := range *onlineDevice {
			// json structure: { UID1:["ip1","ip2"],UID2:["ip3","ip4"] }
			data[onlineuser.UID] = append(data[onlineuser.UID], fmt.Sprintf("%s_%d", onlineuser.IP, onlineuser.OT))
			reportOnline[onlineuser.UID]++
		}
		if err = c.apiClient.ReportNodeOnlineUsers(&data, &reportOnline); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Info("Report online users failed")
		} else {
			log.WithField("tag", c.tag).Infof("Total %d online users, %d Reported", len(*onlineDevice), len(*onlineDevice))
		}
		// If onlineDevice becomes non-zero for the first time, execute the steps inside this block
		if !c.initialZeroed && len(*onlineDevice) > 0 {
			log.Println("onlineDevice becomes non-zero for the first time. Executing the steps.")
			c.initialZeroed = true
		}
		// If onlineDevice becomes zeroed out after being non-zero, execute the steps inside this block
		if c.initialZeroed && len(*onlineDevice) == 0 {
			log.Println("onlineDevice becomes zero after being non-zero. Executing the steps.")
			c.initialZeroed = false
		}
	}
	return nil
}
