package node

import (
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/task"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) startTasks(node *panel.NodeInfo) {
	// fetch node info task
	c.nodeInfoMonitorPeriodic = &task.Task{
		Interval: node.PullInterval,
		Execute:  c.nodeInfoMonitor,
	}
	// fetch user list task
	c.userReportPeriodic = &task.Task{
		Interval: node.PushInterval*4 + 1*time.Second,
		Execute:  c.reportUserTrafficTask,
	}
	// fetch onlineIp list task
	c.onlineIpReportPeriodic = &task.Task{
		Interval: node.PushInterval,
		Execute:  c.onlineIpReport,
	}
	// fetch getonlineIp list task
	c.getonlineIpReportPeriodic = &task.Task{
		Interval: node.PushInterval + 5*time.Second,
		Execute:  c.getonlineIpReport,
	}
	time.Sleep(time.Duration(int64(node.PushInterval.Seconds())-time.Now().Unix()%int64(node.PushInterval.Seconds())) * time.Second)
	log.WithField("tag", c.tag).Info("Start monitor node status")
	// delay to start nodeInfoMonitor
	_ = c.nodeInfoMonitorPeriodic.Start(false)
	log.WithField("tag", c.tag).Info("Start report node status")
	_ = c.userReportPeriodic.Start(false)
	log.WithField("tag", c.tag).Info("Start report online status")
	_ = c.onlineIpReportPeriodic.Start(false)
	log.WithField("tag", c.tag).Info("Start report getonline status")
	_ = c.getonlineIpReportPeriodic.Start(false)
	if node.Security == panel.Tls {
		switch c.CertConfig.CertMode {
		case "none", "", "file", "self":
		default:
			c.renewCertPeriodic = &task.Task{
				Interval: time.Hour * 24,
				Execute:  c.renewCertTask,
			}
			log.WithField("tag", c.tag).Info("Start renew cert")
			// delay to start renewCert
			_ = c.renewCertPeriodic.Start(true)
		}
	}
}

func (c *Controller) nodeInfoMonitor() (err error) {
	// get node info
	newN, err := c.apiClient.GetNodeInfo()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get node info failed")
		return nil
	}
	// get user info
	newU, err := c.apiClient.GetUserList()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get user list failed")
		return nil
	}
	if newN != nil {
		c.info = newN
		// nodeInfo changed
		if newU != nil {
			c.userList = newU
		}
		c.traffic = make(map[string]int64)
		// Remove old node
		log.WithField("tag", c.tag).Info("Node changed, reload")
		err = c.server.DelNode(c.tag)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Delete node failed")
			return nil
		}

		// Update limiter
		if len(c.Options.Name) == 0 {
			c.tag = c.buildNodeTag(newN)
			// Remove Old limiter
			limiter.DeleteLimiter(c.tag)
			// Add new Limiter
			l := limiter.AddLimiter(c.tag, &c.LimitConfig, c.userList)
			c.limiter = l
		}
		// Update rule
		err = c.limiter.UpdateRule(&newN.Rules)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Update Rule failed")
			return nil
		}

		// check cert
		if newN.Security == panel.Tls {
			err = c.requestCert()
			if err != nil {
				log.WithFields(log.Fields{
					"tag": c.tag,
					"err": err,
				}).Error("Request cert failed")
				return nil
			}
		}
		// add new node
		err = c.server.AddNode(c.tag, newN, c.Options)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add node failed")
			return nil
		}
		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			Users:    c.userList,
			NodeInfo: newN,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
			return nil
		}
		// Check interval
		if (c.nodeInfoMonitorPeriodic.Interval != newN.PullInterval && newN.PullInterval != 0) || (c.onlineIpReportPeriodic.Interval != newN.PushInterval &&
			newN.PushInterval != 0) {
			log.Printf("newN time:%s", time.Now())
			time.Sleep(time.Duration(int64(newN.PushInterval.Seconds())-time.Now().Unix()%int64(newN.PushInterval.Seconds())) * time.Second)
			c.onlineIpReportPeriodic.Interval = newN.PushInterval
			c.onlineIpReportPeriodic.Close()
			_ = c.onlineIpReportPeriodic.Start(false)
			c.getonlineIpReportPeriodic.Interval = newN.PushInterval + 5*time.Second
			c.getonlineIpReportPeriodic.Close()
			_ = c.getonlineIpReportPeriodic.Start(false)
			c.userReportPeriodic.Interval = newN.PushInterval*4 + 1*time.Second
			c.userReportPeriodic.Close()
			_ = c.userReportPeriodic.Start(false)
			c.nodeInfoMonitorPeriodic.Interval = newN.PullInterval
			c.nodeInfoMonitorPeriodic.Close()
			_ = c.nodeInfoMonitorPeriodic.Start(false)
		}
		log.WithField("tag", c.tag).Infof("Added %d new users", len(c.userList))
		// exit
		return nil
	}

	// node no changed, check users
	if len(newU) == 0 {
		return nil
	}
	deleted, added := compareUserList(c.userList, newU)
	if len(deleted) > 0 {
		// have deleted users
		err = c.server.DelUsers(deleted, c.tag)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Delete users failed")
			return nil
		}
	}
	if len(added) > 0 {
		// have added users
		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			NodeInfo: c.info,
			Users:    added,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
			return nil
		}
	}
	if len(added) > 0 || len(deleted) > 0 {
		// update Limiter
		c.limiter.UpdateUser(c.tag, added, deleted)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("limiter users failed")
			return nil
		}
		// clear traffic record
		for i := range deleted {
			delete(c.traffic, deleted[i].Uuid)
		}
	}
	c.userList = newU
	if len(added)+len(deleted) != 0 {
		log.WithField("tag", c.tag).
			Infof("%d user deleted, %d user added", len(deleted), len(added))
	}
	return nil
}
