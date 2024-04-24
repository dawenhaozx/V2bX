package panel

import (
	"fmt"
	"sync"

	"github.com/goccy/go-json"
	log "github.com/sirupsen/logrus"
)

type OnlineUser struct {
	UID int
	IP  string
}

type UserInfo struct {
	Id          int    `json:"id"`
	Uuid        string `json:"uuid"`
	SpeedLimit  int    `json:"speed_limit"`
	DeviceLimit int    `json:"device_limit"`
}
type IpsInfo struct {
	Id       int      `json:"id"`
	AliveIPs []string `json:"alive_ips"`
}
type IpsListBody struct {
	//Msg  string `json:"msg"`
	Users []IpsInfo `json:"users"`
}
type UserListBody struct {
	//Msg  string `json:"msg"`
	Users []UserInfo `json:"users"`
}

// 用户UUID和其存活的IP地址映射关系的全局变量
var UserAliveIPsMap *sync.Map

// 初始化全局变量
func init() {
	UserAliveIPsMap = new(sync.Map)
}

// GetUserList will pull user form sspanel
func (c *Client) GetUserList() (UserList []UserInfo, err error) {
	const path = "/api/v1/server/UniProxy/user"
	r, err := c.client.R().
		SetHeader("If-None-Match", c.userEtag).
		ForceContentType("application/json").
		Get(path)
	if err = c.checkResponse(r, path, err); err != nil {
		return nil, err
	}

	if r != nil {
		defer func() {
			if r.RawBody() != nil {
				r.RawBody().Close()
			}
		}()
		if r.StatusCode() == 304 {
			return nil, nil
		}
	} else {
		return nil, fmt.Errorf("received nil response")
	}
	var userList *UserListBody
	if err != nil {
		return nil, fmt.Errorf("read body error: %s", err)
	}
	if err := json.Unmarshal(r.Body(), &userList); err != nil {
		return nil, fmt.Errorf("unmarshal userlist error: %s", err)
	}
	c.userEtag = r.Header().Get("ETag")

	return userList.Users, nil
}

// GetUserList will pull user form sspanel
func (c *Client) GetIpsList() error {
	const path = "/api/v1/server/UniProxy/aips"
	r, err := c.client.R().
		SetHeader("If-None-Match", c.userEtag).
		ForceContentType("application/json").
		Get(path)
	if err = c.checkResponse(r, path, err); err != nil {
		return err
	}

	if r != nil {
		defer func() {
			if r.RawBody() != nil {
				r.RawBody().Close()
			}
		}()
		if r.StatusCode() == 304 {
			return nil
		}
	} else {
		return fmt.Errorf("received nil response")
	}
	var IpsList *IpsListBody
	if err := json.Unmarshal(r.Body(), &IpsList); err != nil {
		return fmt.Errorf("unmarshal Ipslist error: %s", err)
	}
	c.userEtag = r.Header().Get("ETag")
	UserAliveIPsMap = new(sync.Map)
	for _, user := range IpsList.Users {
		if len(user.AliveIPs) > 0 {
			UserAliveIPsMap.Store(user.Id, user.AliveIPs)
			log.Infof("GetIpsList: userid=%d, aliveips=%s, lastOnline=%d", user.Id, user.AliveIPs, c.LastReportOnline[user.Id])
		}
	}
	return nil
}

type UserTraffic struct {
	UID      int
	Upload   int64
	Download int64
}

// ReportUserTraffic reports the user traffic
func (c *Client) ReportUserTraffic(userTraffic []UserTraffic) error {
	data := make(map[int][]int64, len(userTraffic))
	for i := range userTraffic {
		data[userTraffic[i].UID] = []int64{userTraffic[i].Upload, userTraffic[i].Download}
	}
	const path = "/api/v1/server/UniProxy/push"
	r, err := c.client.R().
		SetBody(data).
		ForceContentType("application/json").
		Post(path)
	err = c.checkResponse(r, path, err)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) ReportNodeOnlineUsers(data *map[int][]string, reportOnline *map[int]int) error {
	c.LastReportOnline = *reportOnline
	const path = "/api/v1/server/UniProxy/alive"
	r, err := c.client.R().
		SetBody(data).
		ForceContentType("application/json").
		Post(path)
	err = c.checkResponse(r, path, err)

	log.Infof("Sending data to %s: %v", "/api/v1/server/UniProxy/alive", data)
	if err != nil {
		return nil
	}

	return nil
}
