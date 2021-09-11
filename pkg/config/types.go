package config

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// Config represents the configuration file of propagate-controller.
type Config struct {
	Targets []metav1.GroupVersionKind `json:"targets,omitempty"`
}

// Validate validates the configurations.
func (c *Config) Validate(mapper meta.RESTMapper) error {
	for _, gvk := range c.Targets {
		mapping, err := mapper.RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
		if err != nil {
			return fmt.Errorf("invalid gvk %s: %w", gvk.String(), err)
		}

		if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
			return fmt.Errorf("%s is not namespace-scoped", gvk.String())
		}
	}
	return nil
}

// Load loads configurations.
func (c *Config) Load(data []byte) error {
	return yaml.Unmarshal(data, c, yaml.DisallowUnknownFields)
}
