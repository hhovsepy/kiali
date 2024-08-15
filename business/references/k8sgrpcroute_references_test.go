package references

import (
	"testing"

	"github.com/stretchr/testify/assert"

	k8s_networking_v1 "sigs.k8s.io/gateway-api/apis/v1"
	k8s_networking_v1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kiali/kiali/config"
	"github.com/kiali/kiali/kubernetes"
	"github.com/kiali/kiali/models"
	"github.com/kiali/kiali/tests/data"
)

func prepareTestForK8sGRPCRoute(route *k8s_networking_v1.GRPCRoute) models.IstioReferences {
	routeReferences := K8sGRPCRouteReferences{
		Namespaces: models.Namespaces{
			{Name: "bookinfo"},
			{Name: "bookinfo2"},
			{Name: "bookinfo3"},
		},
		K8sGRPCRoutes:      []*k8s_networking_v1.GRPCRoute{route},
		K8sReferenceGrants: []*k8s_networking_v1beta1.ReferenceGrant{data.CreateReferenceGrantByKind("rg", route.Namespace, "bookinfo", kubernetes.K8sGRPCRouteType)},
	}
	return *routeReferences.References()[models.IstioReferenceKey{ObjectType: "k8sgrpcroute", Namespace: route.Namespace, Name: route.Name}]
}

func TestK8sGRPCRouteReferences(t *testing.T) {
	assert := assert.New(t)
	conf := config.NewConfig()
	config.Set(conf)

	// Setup mocks
	references := prepareTestForK8sGRPCRoute(data.AddBackendRefToGRPCRoute("reviews2", "bookinfo2", data.AddBackendRefToGRPCRoute("reviews", "bookinfo", data.CreateGRPCRoute("route1", "bookinfo", "gatewayapi", []string{"bookinfo"}))))
	assert.NotEmpty(references.ServiceReferences)

	// Check Service references
	assert.Len(references.ServiceReferences, 2)
	assert.Equal(references.ServiceReferences[0].Name, "reviews")
	assert.Equal(references.ServiceReferences[0].Namespace, "bookinfo")
	assert.Equal(references.ServiceReferences[1].Name, "reviews2")
	assert.Equal(references.ServiceReferences[1].Namespace, "bookinfo2")

	assert.Len(references.ObjectReferences, 2)
	// Check Gateway references
	assert.Equal(references.ObjectReferences[0].Name, "gatewayapi")
	assert.Equal(references.ObjectReferences[0].Namespace, "bookinfo")
	assert.Equal(references.ObjectReferences[0].ObjectType, "k8sgateway")
	// Reference Grant
	assert.Equal(references.ObjectReferences[1].Name, "rg")
	assert.Equal(references.ObjectReferences[1].Namespace, "bookinfo")
	assert.Equal(references.ObjectReferences[1].ObjectType, "k8sreferencegrant")
}

func TestK8sGRPCRouteNoReferences(t *testing.T) {
	assert := assert.New(t)
	conf := config.NewConfig()
	config.Set(conf)

	// Setup mocks
	references := prepareTestForK8sGRPCRoute(data.CreateEmptyGRPCRoute("route1", "bookinfo", []string{"details"}))
	assert.Empty(references.ServiceReferences)
}
