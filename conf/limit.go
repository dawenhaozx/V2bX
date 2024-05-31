package conf

type LimitConfig struct {
	SpeedLimit       int             `json:"SpeedLimit"`
	IPLimit          int             `json:"DeviceLimit"`
	EnableIpRecorder bool            `json:"EnableIpRecorder"`
	IpRecorderConfig *IpReportConfig `json:"IpRecorderConfig"`
}

type RecorderConfig struct {
	Url     string `json:"Url"`
	Token   string `json:"Token"`
	Timeout int    `json:"Timeout"`
}

type RedisConfig struct {
	Address  string `json:"Address"`
	Password string `json:"Password"`
	Db       int    `json:"Db"`
	Expiry   int    `json:"Expiry"`
}

type IpReportConfig struct {
	Periodic       int             `json:"Periodic"`
	Type           string          `json:"Type"`
	RecorderConfig *RecorderConfig `json:"RecorderConfig"`
	RedisConfig    *RedisConfig    `json:"RedisConfig"`
	EnableIpSync   bool            `json:"EnableIpSync"`
}
