package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/InazumaV/V2bX/conf"
)

var (
	cores = map[string]func(c *conf.CoreConfig) (Core, error){}
)

func NewCore(c []conf.CoreConfig) (Core, error) {
	if len(c) == 0 {
		return nil, errors.New("no have vail core")
	}
	for i := range c {
		if err := normalizeCoreConfig(&c[i]); err != nil {
			return nil, err
		}
	}
	// Multiple named instances of the sing core.
	if len(c) > 1 {
		return NewSelector(c)
	}
	// One sing instance.
	if f, ok := cores[c[0].Type]; ok {
		return f(&c[0])
	} else {
		return nil, errors.New("unknown core type")
	}
}

func normalizeCoreConfig(c *conf.CoreConfig) error {
	c.Type = strings.ToLower(c.Type)
	if c.Type == "" {
		c.Type = "sing"
	}
	if c.Type != "sing" {
		return fmt.Errorf("unsupported core type %q: only sing is available", c.Type)
	}
	if c.SingConfig == nil {
		c.SingConfig = conf.NewSingConfig()
	}
	return nil
}

func RegisterCore(t string, f func(c *conf.CoreConfig) (Core, error)) {
	cores[t] = f
}

func RegisteredCore() []string {
	cs := make([]string, 0, len(cores))
	for k := range cores {
		cs = append(cs, k)
	}
	return cs
}
