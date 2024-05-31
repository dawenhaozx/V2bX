package conf

import (
	"encoding/json"
)

type CoreConfig struct {
	Type       string      `json:"Type"`
	Name       string      `json:"Name"`
	SingConfig *SingConfig `json:"-"`
}

type _CoreConfig CoreConfig

func (c *CoreConfig) UnmarshalJSON(b []byte) error {
	err := json.Unmarshal(b, (*_CoreConfig)(c))
	if err != nil {
		return err
	}

	if c.Type == "sing" {
		c.SingConfig = NewSingConfig()
		return json.Unmarshal(b, c.SingConfig)
	}

	return nil
}
