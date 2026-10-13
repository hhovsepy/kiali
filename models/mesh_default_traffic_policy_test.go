package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDefaultTrafficPolicyFromYAML(t *testing.T) {
	meshYAML := `
defaultConfig:
  discoveryAddress: istiod.istio-system.svc:15012
defaultTrafficPolicy:
  connectionPool:
    http:
      http1MaxPendingRequests: 1024
  outlierDetection:
    consecutive5xxErrors: 5
    interval: 10s
`

	policy, err := ParseDefaultTrafficPolicyFromYAML(meshYAML)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotNil(t, policy.ConnectionPool)
	require.NotNil(t, policy.ConnectionPool.Http)
	assert.EqualValues(t, 1024, policy.ConnectionPool.Http.Http1MaxPendingRequests)
	require.NotNil(t, policy.OutlierDetection)
	assert.EqualValues(t, 5, policy.OutlierDetection.Consecutive5xxErrors)
	assert.Equal(t, "10s", policy.OutlierDetection.Interval)
}

func TestParseDefaultTrafficPolicyFromYAML_Absent(t *testing.T) {
	policy, err := ParseDefaultTrafficPolicyFromYAML("trustDomain: cluster.local")
	require.NoError(t, err)
	assert.Nil(t, policy)
}
