package types

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type CargoSource struct {
	Base  string `yaml:"base"`
	Index string `yaml:"index"`
	DL    string `yaml:"dl"`
	API   string `yaml:"api"`
}

func (c *CargoSource) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		c.Base = value.Value
		return nil
	case yaml.MappingNode:
		type raw CargoSource
		var decoded raw
		if err := value.Decode(&decoded); err != nil {
			return err
		}
		*c = CargoSource(decoded)
		return nil
	default:
		return fmt.Errorf("cargo source must be string or map")
	}
}
