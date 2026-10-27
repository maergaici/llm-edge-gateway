package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Routes []RouteRule `yaml:"routes"`
}

type RouteRule struct {
	PathPrefix  string `yaml:"path_prefix"`
	Upstream    string `yaml:"upstream"`
	StripPrefix bool   `yaml:"strip_prefix"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
