package models

import (
	"encoding/json"

	networkingv1 "istio.io/api/networking/v1"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

// MeshDefaultTrafficPolicy is meshConfig.defaultTrafficPolicy (Istio 1.31+).
// Istio DestinationRules inherit these baseline connectionPool and outlierDetection settings
// unless they define their own trafficPolicy blocks.
type MeshDefaultTrafficPolicy struct {
	ConnectionPool   *networkingv1.ConnectionPoolSettings `json:"connectionPool,omitempty"`
	OutlierDetection *networkingv1.OutlierDetection       `json:"outlierDetection,omitempty"`
}

// IsConfigured reports whether any baseline traffic policy settings are present.
func (p *MeshDefaultTrafficPolicy) IsConfigured() bool {
	if p == nil {
		return false
	}
	return p.ConnectionPool != nil || p.OutlierDetection != nil
}

// ParseDefaultTrafficPolicyFromYAML extracts defaultTrafficPolicy from raw Istio mesh YAML.
// The field is not present on older istio.io/api MeshConfig protos, so typed unmarshal drops it.
func ParseDefaultTrafficPolicyFromYAML(meshConfigYAML string) (*MeshDefaultTrafficPolicy, error) {
	var raw map[string]any
	if err := k8syaml.Unmarshal([]byte(meshConfigYAML), &raw); err != nil {
		return nil, err
	}

	dtpRaw, ok := raw["defaultTrafficPolicy"]
	if !ok || dtpRaw == nil {
		return nil, nil
	}

	// util/yaml has no Marshal; JSON round-trip is enough for map[string]any -> typed struct.
	dtpJSON, err := json.Marshal(dtpRaw)
	if err != nil {
		return nil, err
	}

	policy := &MeshDefaultTrafficPolicy{}
	if err := k8syaml.Unmarshal(dtpJSON, policy); err != nil {
		return nil, err
	}
	if !policy.IsConfigured() {
		return nil, nil
	}

	return policy, nil
}
