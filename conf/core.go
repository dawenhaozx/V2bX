package conf

import (
	"encoding/json"
	"fmt"
	"strings"
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
	c.Type = strings.ToLower(c.Type)
	if c.Type == "" {
		c.Type = "sing"
	}
	if c.Type != "sing" {
		return fmt.Errorf("unsupported core type %q: only sing is available", c.Type)
	}
	c.SingConfig = NewSingConfig()
	return json.Unmarshal(b, c.SingConfig)
}
