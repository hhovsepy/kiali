package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kiali/kiali/tests/integration/utils"
)

func TestNamespaceMetrics(t *testing.T) {
	assert := assert.New(t)
	params := map[string]string{"filters": "tcp_sent,tcp_received"}
	metrics, err := utils.NamespaceMetrics(utils.BOOKINFO, params)

	assert.Nil(err)
	assert.NotEmpty(metrics)
}

func TestServiceMetrics(t *testing.T) {
	assert := assert.New(t)
	name := "ratings"
	params := map[string]string{"filters": "request_count,request_duration_millis,request_error_count"}
	metrics, err := utils.ServiceMetrics(utils.BOOKINFO, name, params)

	assert.Nil(err)
	assert.NotEmpty(metrics)
}