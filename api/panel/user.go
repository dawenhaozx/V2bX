package panel

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"encoding/json/jsontext"
	"encoding/json/v2"

	log "github.com/sirupsen/logrus"
	"github.com/vmihailenco/msgpack/v5"
)

type OnlineUser struct {
	UID int
	IP  string
}

type UserInfo struct {
	Id          int    `json:"id" msgpack:"id"`
	Uuid        string `json:"uuid" msgpack:"uuid"`
	SpeedLimit  int    `json:"speed_limit" msgpack:"speed_limit"`
	DeviceLimit int    `json:"device_limit" msgpack:"device_limit"`
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
	Users []UserInfo `json:"users" msgpack:"users"`
}

// 用户UUID和其存活的IP地址映射关系的全局变量
var UserAliveIPsMap atomic.Pointer[sync.Map]

// 初始化全局变量
func init() {
	UserAliveIPsMap.Store(new(sync.Map))
}

// GetUserList will pull user from v2board
func (c *Client) GetUserList() (UserList []UserInfo, err error) {
	const path = "/api/v1/server/UniProxy/user"
	r, err := c.client.R().
		SetHeader("If-None-Match", c.userEtag).
		SetHeader("X-Response-Format", "msgpack").
		SetDoNotParseResponse(true).
		Get(path)
	if r == nil || r.RawResponse == nil {
		return nil, fmt.Errorf("received nil response or raw response")
	}
	defer r.RawResponse.Body.Close()

	if r.StatusCode() == 304 {
		return nil, nil
	}

	if err = c.checkResponse(r, path, err); err != nil {
		return nil, err
	}
	userlist := &UserListBody{}
	if strings.Contains(r.Header().Get("Content-Type"), "application/x-msgpack") {
		decoder := msgpack.NewDecoder(r.RawResponse.Body)
		if err := decoder.Decode(userlist); err != nil {
			return nil, fmt.Errorf("decode user list error: %w", err)
		}
	} else {
		dec := jsontext.NewDecoder(r.RawResponse.Body)
		for {
			tok, err := dec.ReadToken()
			if err != nil {
				return nil, fmt.Errorf("decode user list error: %w", err)
			}
			if tok.Kind() == '"' && tok.String() == "users" {
				break
			}
		}
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("decode user list error: %w", err)
		}
		if tok.Kind() != '[' {
			return nil, fmt.Errorf(`decode user list error: expected "users" array`)
		}
		for dec.PeekKind() != ']' {
			val, err := dec.ReadValue()
			if err != nil {
				return nil, fmt.Errorf("decode user list error: read user object: %w", err)
			}
			var u UserInfo
			if err := json.Unmarshal(val, &u); err != nil {
				return nil, fmt.Errorf("decode user list error: unmarshal user error: %w", err)
			}
			userlist.Users = append(userlist.Users, u)
		}
	}
	c.userEtag = r.Header().Get("ETag")
	return userlist.Users, nil
}

// GetIpsList fetches a snapshot of users' alive IP addresses.
func (c *Client) GetIpsList() error {
	const path = "/api/v1/server/UniProxy/aips"
	r, err := c.client.R().
		SetHeader("If-None-Match", c.ipsEtag).
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
	if IpsList == nil {
		return fmt.Errorf("received null IP list")
	}
	aliveIPs := new(sync.Map)
	for _, user := range IpsList.Users {
		if len(user.AliveIPs) > 0 {
			aliveIPs.Store(user.Id, user.AliveIPs)
			log.Infof("GetIpsList: userid=%d, aliveips=%s, lastOnline=%d", user.Id, user.AliveIPs, c.LastReportOnline[user.Id])
		}
	}
	UserAliveIPsMap.Store(aliveIPs)
	c.ipsEtag = r.Header().Get("ETag")
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
