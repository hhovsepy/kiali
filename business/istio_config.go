package business

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	extentions_v1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	networking_v1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	networking_v1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	security_v1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/client-go/pkg/apis/telemetry/v1alpha1"
	api_errors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api_types "k8s.io/apimachinery/pkg/types"
	k8s_networking_v1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kiali/kiali/config"
	"github.com/kiali/kiali/kubernetes"
	"github.com/kiali/kiali/log"
	"github.com/kiali/kiali/models"
	"github.com/kiali/kiali/observability"
)

const allResources string = "*"

type IstioConfigService struct {
	k8s           kubernetes.ClientInterface
	businessLayer *Layer
}

type IstioConfigCriteria struct {
	// When AllNamespaces is true the IstioConfigService will use the Istio registry to return the configuration
	// from all namespaces directly from the Istio registry instead of the individual API
	// This usecase should be reserved for validations use cases only where cross-namespace validation may create a
	// penalty
	AllNamespaces                 bool
	Namespace                     string
	IncludeGateways               bool
	IncludeK8sGateways            bool
	IncludeK8sHTTPRoutes          bool
	IncludeVirtualServices        bool
	IncludeDestinationRules       bool
	IncludeServiceEntries         bool
	IncludeSidecars               bool
	IncludeAuthorizationPolicies  bool
	IncludePeerAuthentications    bool
	IncludeWorkloadEntries        bool
	IncludeWorkloadGroups         bool
	IncludeRequestAuthentications bool
	IncludeEnvoyFilters           bool
	IncludeWasmPlugins            bool
	IncludeTelemetry              bool
	LabelSelector                 string
	WorkloadSelector              string
}

func (icc IstioConfigCriteria) Include(resource string) bool {
	// Flag used to skip object that are not used in a query when a WorkloadSelector is present
	isWorkloadSelector := icc.WorkloadSelector != ""
	switch resource {
	case kubernetes.Gateways:
		return icc.IncludeGateways
	case kubernetes.K8sGateways:
		return icc.IncludeK8sGateways
	case kubernetes.K8sHTTPRoutes:
		return icc.IncludeK8sHTTPRoutes
	case kubernetes.VirtualServices:
		return icc.IncludeVirtualServices && !isWorkloadSelector
	case kubernetes.DestinationRules:
		return icc.IncludeDestinationRules && !isWorkloadSelector
	case kubernetes.ServiceEntries:
		return icc.IncludeServiceEntries && !isWorkloadSelector
	case kubernetes.Sidecars:
		return icc.IncludeSidecars
	case kubernetes.AuthorizationPolicies:
		return icc.IncludeAuthorizationPolicies
	case kubernetes.PeerAuthentications:
		return icc.IncludePeerAuthentications
	case kubernetes.WorkloadEntries:
		return icc.IncludeWorkloadEntries && !isWorkloadSelector
	case kubernetes.WorkloadGroups:
		return icc.IncludeWorkloadGroups && !isWorkloadSelector
	case kubernetes.RequestAuthentications:
		return icc.IncludeRequestAuthentications
	case kubernetes.EnvoyFilters:
		return icc.IncludeEnvoyFilters
	case kubernetes.WasmPlugins:
		return icc.IncludeWasmPlugins
	case kubernetes.Telemetries:
		return icc.IncludeTelemetry
	}
	return false
}

// IstioConfig types used in the IstioConfig New Page Form
// networking.istio.io
var newNetworkingConfigTypes = []string{
	kubernetes.Sidecars,
	kubernetes.Gateways,
	kubernetes.ServiceEntries,
}

// security.istio.io
var newSecurityConfigTypes = []string{
	kubernetes.AuthorizationPolicies,
	kubernetes.PeerAuthentications,
	kubernetes.RequestAuthentications,
}

// GetIstioConfigList returns a list of Istio routing objects, Mixer Rules, (etc.)
// per a given Namespace.
func (in *IstioConfigService) GetIstioConfigList(ctx context.Context, criteria IstioConfigCriteria) (models.IstioConfigList, error) {
	var end observability.EndFunc
	ctx, end = observability.StartSpan(ctx, "GetIstioConfigList",
		observability.Attribute("package", "business"),
	)
	defer end()

	if criteria.Namespace == "" && !criteria.AllNamespaces {
		return models.IstioConfigList{}, errors.New("GetIstioConfigList needs a non empty Namespace")
	}
	istioConfigList := models.IstioConfigList{
		Namespace: models.Namespace{Name: criteria.Namespace},

		DestinationRules: []*networking_v1beta1.DestinationRule{},
		EnvoyFilters:     []*networking_v1alpha3.EnvoyFilter{},
		Gateways:         []*networking_v1beta1.Gateway{},
		VirtualServices:  []*networking_v1beta1.VirtualService{},
		ServiceEntries:   []*networking_v1beta1.ServiceEntry{},
		Sidecars:         []*networking_v1beta1.Sidecar{},
		WorkloadEntries:  []*networking_v1beta1.WorkloadEntry{},
		WorkloadGroups:   []*networking_v1beta1.WorkloadGroup{},
		WasmPlugins:      []*extentions_v1alpha1.WasmPlugin{},
		Telemetries:      []*v1alpha1.Telemetry{},

		K8sGateways:   []*k8s_networking_v1alpha2.Gateway{},
		K8sHTTPRoutes: []*k8s_networking_v1alpha2.HTTPRoute{},

		AuthorizationPolicies:  []*security_v1beta1.AuthorizationPolicy{},
		PeerAuthentications:    []*security_v1beta1.PeerAuthentication{},
		RequestAuthentications: []*security_v1beta1.RequestAuthentication{},
	}

	// Use the Istio Registry when AllNamespaces is present
	if criteria.AllNamespaces {
		registryCriteria := RegistryCriteria{
			AllNamespaces: true,
		}
		registryConfiguration, err := in.businessLayer.RegistryStatus.GetRegistryConfiguration(registryCriteria)
		if err != nil {
			return istioConfigList, err
		}
		if registryConfiguration == nil {
			log.Warningf("RegistryConfiguration is nil. This is an unexpected case. Is the Kiali cache disabled ?")
			return istioConfigList, nil
		}
		// AllNamespaces will return an empty namespace
		istioConfigList.Namespace.Name = ""

		if criteria.Include(kubernetes.DestinationRules) {
			istioConfigList.DestinationRules = registryConfiguration.DestinationRules
		}
		if criteria.Include(kubernetes.EnvoyFilters) {
			istioConfigList.EnvoyFilters = registryConfiguration.EnvoyFilters
		}
		if criteria.Include(kubernetes.Gateways) {
			istioConfigList.Gateways = kubernetes.FilterSupportedGateways(registryConfiguration.Gateways)
		}
		if criteria.Include(kubernetes.K8sGateways) {
			istioConfigList.K8sGateways = registryConfiguration.K8sGateways
		}
		if criteria.Include(kubernetes.K8sHTTPRoutes) {
			istioConfigList.K8sHTTPRoutes = registryConfiguration.K8sHTTPRoutes
		}
		if criteria.Include(kubernetes.VirtualServices) {
			istioConfigList.VirtualServices = registryConfiguration.VirtualServices
		}
		if criteria.Include(kubernetes.ServiceEntries) {
			istioConfigList.ServiceEntries = registryConfiguration.ServiceEntries
		}
		if criteria.Include(kubernetes.Sidecars) {
			istioConfigList.Sidecars = registryConfiguration.Sidecars
		}
		if criteria.Include(kubernetes.WorkloadEntries) {
			istioConfigList.WorkloadEntries = registryConfiguration.WorkloadEntries
		}
		if criteria.Include(kubernetes.WorkloadGroups) {
			istioConfigList.WorkloadGroups = registryConfiguration.WorkloadGroups
		}
		if criteria.Include(kubernetes.WasmPlugins) {
			istioConfigList.WasmPlugins = registryConfiguration.WasmPlugins
		}
		if criteria.Include(kubernetes.Telemetries) {
			istioConfigList.Telemetries = registryConfiguration.Telemetries
		}
		if criteria.Include(kubernetes.AuthorizationPolicies) {
			istioConfigList.AuthorizationPolicies = registryConfiguration.AuthorizationPolicies
		}
		if criteria.Include(kubernetes.PeerAuthentications) {
			istioConfigList.PeerAuthentications = registryConfiguration.PeerAuthentications
		}
		if criteria.Include(kubernetes.RequestAuthentications) {
			istioConfigList.RequestAuthentications = registryConfiguration.RequestAuthentications
		}

		return istioConfigList, nil
	}

	// Check if user has access to the namespace (RBAC) in cache scenarios and/or
	// if namespace is accessible from Kiali (Deployment.AccessibleNamespaces)
	if _, err := in.businessLayer.Namespace.GetNamespace(ctx, criteria.Namespace); err != nil {
		return models.IstioConfigList{}, err
	}

	isWorkloadSelector := criteria.WorkloadSelector != ""
	workloadSelector := ""
	if isWorkloadSelector {
		workloadSelector = criteria.WorkloadSelector
	}

	errChan := make(chan error, 15)

	var wg sync.WaitGroup
	wg.Add(15)

	listOpts := meta_v1.ListOptions{LabelSelector: criteria.LabelSelector}

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.DestinationRules) {
			var err error
			// Check if namespace is cached
			if IsResourceCached(criteria.Namespace, kubernetes.DestinationRules) {
				istioConfigList.DestinationRules, err = kialiCache.GetDestinationRules(criteria.Namespace, criteria.LabelSelector)
			} else {
				drl, e := in.k8s.Istio().NetworkingV1beta1().DestinationRules(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.DestinationRules = drl.Items
				err = e
			}
			if err != nil {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.EnvoyFilters) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.EnvoyFilters) {
				istioConfigList.EnvoyFilters, err = kialiCache.GetEnvoyFilters(criteria.Namespace, criteria.LabelSelector)
			} else {
				efl, e := in.k8s.Istio().NetworkingV1alpha3().EnvoyFilters(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.EnvoyFilters = efl.Items
				err = e
			}
			if err == nil {
				if isWorkloadSelector {
					istioConfigList.EnvoyFilters = kubernetes.FilterEnvoyFiltersBySelector(workloadSelector, istioConfigList.EnvoyFilters)
				}
			} else {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.Gateways) {
			var err error
			// Check if namespace is cached
			if IsResourceCached(criteria.Namespace, kubernetes.Gateways) {
				istioConfigList.Gateways, err = kialiCache.GetGateways(criteria.Namespace, criteria.LabelSelector)
			} else {
				gwl, e := in.k8s.Istio().NetworkingV1beta1().Gateways(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.Gateways = gwl.Items
				err = e
			}
			if err == nil {
				if isWorkloadSelector {
					istioConfigList.Gateways = kubernetes.FilterGatewaysBySelector(workloadSelector, istioConfigList.Gateways)
				}
			} else {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if in.k8s.IsGatewayAPI() && criteria.Include(kubernetes.K8sGateways) {
			// ignore an error as system could not be configured to support K8s Gateway API
			// Check if namespace is cached
			if IsResourceCached(criteria.Namespace, kubernetes.K8sGateways) {
				istioConfigList.K8sGateways, _ = kialiCache.GetK8sGateways(criteria.Namespace, criteria.LabelSelector)
			}
			// TODO gwl.Items
			//else {
			//	if gwl, e := in.k8s.GatewayAPI().GatewayV1alpha2().Gateways(criteria.Namespace).List(ctx, listOpts); e == nil {
			//		istioConfigList.K8sGateways = gwl.Items
			//	}
			//}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if in.k8s.IsGatewayAPI() && criteria.Include(kubernetes.K8sHTTPRoutes) {
			// ignore an error as system could not be configured to support K8s Gateway API
			// Check if namespace is cached
			if IsResourceCached(criteria.Namespace, kubernetes.K8sHTTPRoutes) {
				istioConfigList.K8sHTTPRoutes, _ = kialiCache.GetK8sHTTPRoutes(criteria.Namespace, criteria.LabelSelector)
			}
			// TODO gwl.Items
			//else {
			//	if gwl, e := in.k8s.GatewayAPI().GatewayV1alpha2().HTTPRoutes(criteria.Namespace).List(ctx, listOpts); e == nil {
			//		istioConfigList.K8sHTTPRoutes = gwl.Items
			//	}
			//}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.ServiceEntries) {
			var err error
			// Check if namespace is cached
			if IsResourceCached(criteria.Namespace, kubernetes.ServiceEntries) {
				istioConfigList.ServiceEntries, err = kialiCache.GetServiceEntries(criteria.Namespace, criteria.LabelSelector)
			} else {
				sel, e := in.k8s.Istio().NetworkingV1beta1().ServiceEntries(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.ServiceEntries = sel.Items
				err = e
			}
			if err != nil {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.Sidecars) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.Sidecars) {
				istioConfigList.Sidecars, err = kialiCache.GetSidecars(criteria.Namespace, criteria.LabelSelector)
			} else {
				scl, e := in.k8s.Istio().NetworkingV1beta1().Sidecars(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.Sidecars = scl.Items
				err = e
			}
			if err == nil {
				if isWorkloadSelector {
					istioConfigList.Sidecars = kubernetes.FilterSidecarsBySelector(workloadSelector, istioConfigList.Sidecars)
				}
			} else {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.VirtualServices) {
			var err error
			// Check if namespace is cached
			if IsResourceCached(criteria.Namespace, kubernetes.VirtualServices) {
				istioConfigList.VirtualServices, err = kialiCache.GetVirtualServices(criteria.Namespace, criteria.LabelSelector)
			} else {
				vsl, e := in.k8s.Istio().NetworkingV1beta1().VirtualServices(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.VirtualServices = vsl.Items
				err = e
			}
			if err != nil {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.WorkloadEntries) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.WorkloadEntries) {
				istioConfigList.WorkloadEntries, err = kialiCache.GetWorkloadEntries(criteria.Namespace, criteria.LabelSelector)
			} else {
				wel, e := in.k8s.Istio().NetworkingV1beta1().WorkloadEntries(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.WorkloadEntries = wel.Items
				err = e
			}
			if err != nil {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.WorkloadGroups) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.WorkloadGroups) {
				istioConfigList.WorkloadGroups, err = kialiCache.GetWorkloadGroups(criteria.Namespace, criteria.LabelSelector)
			} else {
				wgl, e := in.k8s.Istio().NetworkingV1beta1().WorkloadGroups(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.WorkloadGroups = wgl.Items
				err = e
			}
			if err != nil {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.WasmPlugins) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.WasmPlugins) {
				istioConfigList.WasmPlugins, err = kialiCache.GetWasmPlugins(criteria.Namespace, criteria.LabelSelector)
			} else {
				wgl, e := in.k8s.Istio().ExtensionsV1alpha1().WasmPlugins(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.WasmPlugins = wgl.Items
				err = e
			}
			if err != nil {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.Telemetries) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.Telemetries) {
				istioConfigList.Telemetries, err = kialiCache.GetTelemetries(criteria.Namespace, criteria.LabelSelector)
			} else {
				wgl, e := in.k8s.Istio().TelemetryV1alpha1().Telemetries(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.Telemetries = wgl.Items
				err = e
			}
			if err != nil {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.AuthorizationPolicies) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.AuthorizationPolicies) {
				istioConfigList.AuthorizationPolicies, err = kialiCache.GetAuthorizationPolicies(criteria.Namespace, criteria.LabelSelector)
			} else {
				apl, e := in.k8s.Istio().SecurityV1beta1().AuthorizationPolicies(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.AuthorizationPolicies = apl.Items
				err = e
			}
			if err == nil {
				if isWorkloadSelector {
					istioConfigList.AuthorizationPolicies = kubernetes.FilterAuthorizationPoliciesBySelector(workloadSelector, istioConfigList.AuthorizationPolicies)
				}
			} else {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.PeerAuthentications) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.PeerAuthentications) {
				istioConfigList.PeerAuthentications, err = kialiCache.GetPeerAuthentications(criteria.Namespace, criteria.LabelSelector)
			} else {
				pal, e := in.k8s.Istio().SecurityV1beta1().PeerAuthentications(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.PeerAuthentications = pal.Items
				err = e
			}
			if err == nil {
				if isWorkloadSelector {
					istioConfigList.PeerAuthentications = kubernetes.FilterPeerAuthenticationsBySelector(workloadSelector, istioConfigList.PeerAuthentications)
				}
			} else {
				errChan <- err
			}
		}
	}(ctx, errChan)

	go func(ctx context.Context, errChan chan error) {
		defer wg.Done()
		if criteria.Include(kubernetes.RequestAuthentications) {
			var err error
			if IsResourceCached(criteria.Namespace, kubernetes.RequestAuthentications) {
				istioConfigList.RequestAuthentications, err = kialiCache.GetRequestAuthentications(criteria.Namespace, criteria.LabelSelector)
			} else {
				ral, e := in.k8s.Istio().SecurityV1beta1().RequestAuthentications(criteria.Namespace).List(ctx, listOpts)
				istioConfigList.RequestAuthentications = ral.Items
				err = e
			}
			if err == nil {
				if isWorkloadSelector {
					istioConfigList.RequestAuthentications = kubernetes.FilterRequestAuthenticationsBySelector(workloadSelector, istioConfigList.RequestAuthentications)
				}
			} else {
				errChan <- err
			}
		}
	}(ctx, errChan)

	wg.Wait()

	close(errChan)
	for e := range errChan {
		if e != nil { // Check that default value wasn't returned
			err := e // To update the Kiali metric
			return models.IstioConfigList{}, err
		}
	}

	return istioConfigList, nil
}

// GetIstioConfigDetails returns a specific Istio configuration object.
// It uses following parameters:
// - "namespace": 		namespace where configuration is stored
// - "objectType":		type of the configuration
// - "object":			name of the configuration
func (in *IstioConfigService) GetIstioConfigDetails(ctx context.Context, namespace, objectType, object string) (models.IstioConfigDetails, error) {
	var end observability.EndFunc
	ctx, end = observability.StartSpan(ctx, "GetIstioConfigDetails",
		observability.Attribute("package", "business"),
		observability.Attribute("namespace", namespace),
		observability.Attribute("objectType", objectType),
		observability.Attribute("object", object),
	)
	defer end()

	var err error

	istioConfigDetail := models.IstioConfigDetails{}
	istioConfigDetail.Namespace = models.Namespace{Name: namespace}
	istioConfigDetail.ObjectType = objectType

	// Check if user has access to the namespace (RBAC) in cache scenarios and/or
	// if namespace is accessible from Kiali (Deployment.AccessibleNamespaces)
	if _, err := in.businessLayer.Namespace.GetNamespace(ctx, namespace); err != nil {
		return istioConfigDetail, err
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func(ctx context.Context) {
		defer wg.Done()
		canCreate, canUpdate, canDelete := getPermissions(ctx, in.k8s, namespace, objectType)
		istioConfigDetail.Permissions = models.ResourcePermissions{
			Create: canCreate,
			Update: canUpdate,
			Delete: canDelete,
		}
	}(ctx)

	getOpts := meta_v1.GetOptions{}

	switch objectType {
	case kubernetes.DestinationRules:
		istioConfigDetail.DestinationRule, err = in.k8s.Istio().NetworkingV1beta1().DestinationRules(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.DestinationRule.Kind = kubernetes.DestinationRuleType
			istioConfigDetail.DestinationRule.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
		}
	case kubernetes.EnvoyFilters:
		istioConfigDetail.EnvoyFilter, err = in.k8s.Istio().NetworkingV1alpha3().EnvoyFilters(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.EnvoyFilter.Kind = kubernetes.EnvoyFilterType
			istioConfigDetail.EnvoyFilter.APIVersion = kubernetes.ApiNetworkingVersionV1Alpha3
		}
	case kubernetes.Gateways:
		istioConfigDetail.Gateway, err = in.k8s.Istio().NetworkingV1beta1().Gateways(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.Gateway.Kind = kubernetes.GatewayType
			istioConfigDetail.Gateway.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
		}
	case kubernetes.K8sGateways:
		istioConfigDetail.K8sGateway, err = in.k8s.GatewayAPI().GatewayV1alpha2().Gateways(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.K8sGateway.Kind = kubernetes.K8sActualGatewayType
			istioConfigDetail.K8sGateway.APIVersion = kubernetes.K8sApiNetworkingVersionV1Alpha2
		}
	case kubernetes.K8sHTTPRoutes:
		istioConfigDetail.K8sHTTPRoute, err = in.k8s.GatewayAPI().GatewayV1alpha2().HTTPRoutes(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.K8sHTTPRoute.Kind = kubernetes.K8sActualHTTPRouteType
			istioConfigDetail.K8sHTTPRoute.APIVersion = kubernetes.K8sApiNetworkingVersionV1Alpha2
		}
	case kubernetes.ServiceEntries:
		istioConfigDetail.ServiceEntry, err = in.k8s.Istio().NetworkingV1beta1().ServiceEntries(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.ServiceEntry.Kind = kubernetes.ServiceEntryType
			istioConfigDetail.ServiceEntry.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
		}
	case kubernetes.Sidecars:
		istioConfigDetail.Sidecar, err = in.k8s.Istio().NetworkingV1beta1().Sidecars(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.Sidecar.Kind = kubernetes.SidecarType
			istioConfigDetail.Sidecar.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
		}
	case kubernetes.VirtualServices:
		istioConfigDetail.VirtualService, err = in.k8s.Istio().NetworkingV1beta1().VirtualServices(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.VirtualService.Kind = kubernetes.VirtualServiceType
			istioConfigDetail.VirtualService.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
		}
	case kubernetes.WorkloadEntries:
		istioConfigDetail.WorkloadEntry, err = in.k8s.Istio().NetworkingV1beta1().WorkloadEntries(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.WorkloadEntry.Kind = kubernetes.WorkloadEntryType
			istioConfigDetail.WorkloadEntry.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
		}
	case kubernetes.WorkloadGroups:
		istioConfigDetail.WorkloadGroup, err = in.k8s.Istio().NetworkingV1beta1().WorkloadGroups(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.WorkloadGroup.Kind = kubernetes.WorkloadGroupType
			istioConfigDetail.WorkloadGroup.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
		}
	case kubernetes.WasmPlugins:
		istioConfigDetail.WasmPlugin, err = in.k8s.Istio().ExtensionsV1alpha1().WasmPlugins(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.WasmPlugin.Kind = kubernetes.WasmPluginType
			istioConfigDetail.WasmPlugin.APIVersion = kubernetes.ApiExtensionV1Alpha1
		}
	case kubernetes.Telemetries:
		istioConfigDetail.Telemetry, err = in.k8s.Istio().TelemetryV1alpha1().Telemetries(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.Telemetry.Kind = kubernetes.TelemetryType
			istioConfigDetail.Telemetry.APIVersion = kubernetes.ApiTelemetryV1Alpha1
		}
	case kubernetes.AuthorizationPolicies:
		istioConfigDetail.AuthorizationPolicy, err = in.k8s.Istio().SecurityV1beta1().AuthorizationPolicies(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.AuthorizationPolicy.Kind = kubernetes.AuthorizationPoliciesType
			istioConfigDetail.AuthorizationPolicy.APIVersion = kubernetes.ApiSecurityVersion
		}
	case kubernetes.PeerAuthentications:
		istioConfigDetail.PeerAuthentication, err = in.k8s.Istio().SecurityV1beta1().PeerAuthentications(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.PeerAuthentication.Kind = kubernetes.PeerAuthenticationsType
			istioConfigDetail.PeerAuthentication.APIVersion = kubernetes.ApiSecurityVersion
		}
	case kubernetes.RequestAuthentications:
		istioConfigDetail.RequestAuthentication, err = in.k8s.Istio().SecurityV1beta1().RequestAuthentications(namespace).Get(ctx, object, getOpts)
		if err == nil {
			istioConfigDetail.RequestAuthentication.Kind = kubernetes.RequestAuthenticationsType
			istioConfigDetail.RequestAuthentication.APIVersion = kubernetes.ApiSecurityVersion
		}
	default:
		err = fmt.Errorf("object type not found: %v", objectType)
	}

	wg.Wait()

	return istioConfigDetail, err
}

// GetIstioConfigDetailsFromRegistry returns a specific Istio configuration object from Istio Registry.
// The returned object is Read only.
// It uses following parameters:
// - "namespace": 		namespace where configuration is stored
// - "objectType":		type of the configuration
// - "object":			name of the configuration
func (in *IstioConfigService) GetIstioConfigDetailsFromRegistry(ctx context.Context, namespace, objectType, object string) (models.IstioConfigDetails, error) {
	var err error

	istioConfigDetail := models.IstioConfigDetails{}
	istioConfigDetail.Namespace = models.Namespace{Name: namespace}
	istioConfigDetail.ObjectType = objectType

	istioConfigDetail.Permissions = models.ResourcePermissions{
		Create: false,
		Update: false,
		Delete: false,
	}

	registryCriteria := RegistryCriteria{
		AllNamespaces: true,
	}
	registryConfiguration, err := in.businessLayer.RegistryStatus.GetRegistryConfiguration(registryCriteria)
	if err != nil {
		return istioConfigDetail, err
	}
	if registryConfiguration == nil {
		return istioConfigDetail, errors.New("RegistryConfiguration is nil. This is an unexpected case. Is the Kiali cache disabled ?")
	}

	switch objectType {
	case kubernetes.DestinationRules:
		configs := registryConfiguration.DestinationRules
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.DestinationRule = cfg
				istioConfigDetail.DestinationRule.Kind = kubernetes.DestinationRuleType
				istioConfigDetail.DestinationRule.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.EnvoyFilters:
		configs := registryConfiguration.EnvoyFilters
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.EnvoyFilter = cfg
				istioConfigDetail.EnvoyFilter.Kind = kubernetes.EnvoyFilterType
				istioConfigDetail.EnvoyFilter.APIVersion = kubernetes.ApiNetworkingVersionV1Alpha3
				return istioConfigDetail, nil
			}
		}
	case kubernetes.Gateways:
		configs := registryConfiguration.Gateways
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.Gateway = cfg
				istioConfigDetail.Gateway.Kind = kubernetes.GatewayType
				istioConfigDetail.Gateway.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.K8sGateways:
		configs := registryConfiguration.K8sGateways
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.K8sGateway = cfg
				istioConfigDetail.K8sGateway.Kind = kubernetes.K8sGatewayType
				istioConfigDetail.K8sGateway.APIVersion = kubernetes.K8sApiNetworkingVersionV1Alpha2
				return istioConfigDetail, nil
			}
		}
	case kubernetes.K8sHTTPRoutes:
		configs := registryConfiguration.K8sHTTPRoutes
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.K8sHTTPRoute = cfg
				istioConfigDetail.K8sHTTPRoute.Kind = kubernetes.K8sHTTPRouteType
				istioConfigDetail.K8sHTTPRoute.APIVersion = kubernetes.K8sApiNetworkingVersionV1Alpha2
				return istioConfigDetail, nil
			}
		}
	case kubernetes.ServiceEntries:
		configs := registryConfiguration.ServiceEntries
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.ServiceEntry = cfg
				istioConfigDetail.ServiceEntry.Kind = kubernetes.ServiceEntryType
				istioConfigDetail.ServiceEntry.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.Sidecars:
		configs := registryConfiguration.Sidecars
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.Sidecar = cfg
				istioConfigDetail.Sidecar.Kind = kubernetes.SidecarType
				istioConfigDetail.Sidecar.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.VirtualServices:
		configs := registryConfiguration.VirtualServices
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.VirtualService = cfg
				istioConfigDetail.VirtualService.Kind = kubernetes.VirtualServiceType
				istioConfigDetail.VirtualService.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.WorkloadEntries:
		configs := registryConfiguration.WorkloadEntries
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.WorkloadEntry = cfg
				istioConfigDetail.WorkloadEntry.Kind = kubernetes.WorkloadEntryType
				istioConfigDetail.WorkloadEntry.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.WorkloadGroups:
		configs := registryConfiguration.WorkloadGroups
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.WorkloadGroup = cfg
				istioConfigDetail.WorkloadGroup.Kind = kubernetes.WorkloadGroupType
				istioConfigDetail.WorkloadGroup.APIVersion = kubernetes.ApiNetworkingVersionV1Beta1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.WasmPlugins:
		configs := registryConfiguration.WasmPlugins
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.WasmPlugin = cfg
				istioConfigDetail.WasmPlugin.Kind = kubernetes.WasmPluginType
				istioConfigDetail.WasmPlugin.APIVersion = kubernetes.ApiExtensionV1Alpha1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.Telemetries:
		configs := registryConfiguration.Telemetries
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.Telemetry = cfg
				istioConfigDetail.Telemetry.Kind = kubernetes.TelemetryType
				istioConfigDetail.Telemetry.APIVersion = kubernetes.ApiTelemetryV1Alpha1
				return istioConfigDetail, nil
			}
		}
	case kubernetes.AuthorizationPolicies:
		configs := registryConfiguration.AuthorizationPolicies
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.AuthorizationPolicy = cfg
				istioConfigDetail.AuthorizationPolicy.Kind = kubernetes.AuthorizationPoliciesType
				istioConfigDetail.AuthorizationPolicy.APIVersion = kubernetes.ApiSecurityVersion
				return istioConfigDetail, nil
			}
		}
	case kubernetes.PeerAuthentications:
		configs := registryConfiguration.PeerAuthentications
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.PeerAuthentication = cfg
				istioConfigDetail.PeerAuthentication.Kind = kubernetes.PeerAuthenticationsType
				istioConfigDetail.PeerAuthentication.APIVersion = kubernetes.ApiSecurityVersion
				return istioConfigDetail, nil
			}
		}
	case kubernetes.RequestAuthentications:
		configs := registryConfiguration.RequestAuthentications
		for _, cfg := range configs {
			if cfg.Name == object && cfg.Namespace == namespace {
				istioConfigDetail.RequestAuthentication = cfg
				istioConfigDetail.RequestAuthentication.Kind = kubernetes.RequestAuthenticationsType
				istioConfigDetail.RequestAuthentication.APIVersion = kubernetes.ApiSecurityVersion
				return istioConfigDetail, nil
			}
		}
	default:
		err = fmt.Errorf("object type not found: %v", objectType)
	}

	if err == nil {
		err = errors.New("Object is not found in registry")
	}

	return istioConfigDetail, err
}

// GetIstioAPI provides the Kubernetes API that manages this Istio resource type
// or empty string if it's not managed
func GetIstioAPI(resourceType string) bool {
	return kubernetes.ResourceTypesToAPI[resourceType] != ""
}

// DeleteIstioConfigDetail deletes the given Istio resource
func (in *IstioConfigService) DeleteIstioConfigDetail(namespace, resourceType, name string) error {
	var err error
	delOpts := meta_v1.DeleteOptions{}
	ctx := context.TODO()
	switch resourceType {
	case kubernetes.DestinationRules:
		err = in.k8s.Istio().NetworkingV1beta1().DestinationRules(namespace).Delete(ctx, name, delOpts)
	case kubernetes.EnvoyFilters:
		err = in.k8s.Istio().NetworkingV1alpha3().EnvoyFilters(namespace).Delete(ctx, name, delOpts)
	case kubernetes.Gateways:
		err = in.k8s.Istio().NetworkingV1beta1().Gateways(namespace).Delete(ctx, name, delOpts)
	case kubernetes.K8sGateways:
		err = in.k8s.GatewayAPI().GatewayV1alpha2().Gateways(namespace).Delete(ctx, name, delOpts)
	case kubernetes.K8sHTTPRoutes:
		err = in.k8s.GatewayAPI().GatewayV1alpha2().HTTPRoutes(namespace).Delete(ctx, name, delOpts)
	case kubernetes.ServiceEntries:
		err = in.k8s.Istio().NetworkingV1beta1().ServiceEntries(namespace).Delete(ctx, name, delOpts)
	case kubernetes.Sidecars:
		err = in.k8s.Istio().NetworkingV1beta1().Sidecars(namespace).Delete(ctx, name, delOpts)
	case kubernetes.VirtualServices:
		err = in.k8s.Istio().NetworkingV1beta1().VirtualServices(namespace).Delete(ctx, name, delOpts)
	case kubernetes.WorkloadEntries:
		err = in.k8s.Istio().NetworkingV1beta1().WorkloadEntries(namespace).Delete(ctx, name, delOpts)
	case kubernetes.WorkloadGroups:
		err = in.k8s.Istio().NetworkingV1beta1().WorkloadGroups(namespace).Delete(ctx, name, delOpts)
	case kubernetes.AuthorizationPolicies:
		err = in.k8s.Istio().SecurityV1beta1().AuthorizationPolicies(namespace).Delete(ctx, name, delOpts)
	case kubernetes.PeerAuthentications:
		err = in.k8s.Istio().SecurityV1beta1().PeerAuthentications(namespace).Delete(ctx, name, delOpts)
	case kubernetes.RequestAuthentications:
		err = in.k8s.Istio().SecurityV1beta1().RequestAuthentications(namespace).Delete(ctx, name, delOpts)
	case kubernetes.WasmPlugins:
		err = in.k8s.Istio().ExtensionsV1alpha1().WasmPlugins(namespace).Delete(ctx, name, delOpts)
	case kubernetes.Telemetries:
		err = in.k8s.Istio().TelemetryV1alpha1().Telemetries(namespace).Delete(ctx, name, delOpts)
	default:
		err = fmt.Errorf("object type not found: %v", resourceType)
	}

	// Cache is stopped after a Create/Update/Delete operation to force a refresh
	if kialiCache != nil && err == nil {
		kialiCache.Refresh(namespace)
	}
	return err
}

func (in *IstioConfigService) UpdateIstioConfigDetail(namespace, resourceType, name, jsonPatch string) (models.IstioConfigDetails, error) {
	istioConfigDetail := models.IstioConfigDetails{}
	istioConfigDetail.Namespace = models.Namespace{Name: namespace}
	istioConfigDetail.ObjectType = resourceType

	patchOpts := meta_v1.PatchOptions{}
	ctx := context.TODO()
	patchType := api_types.MergePatchType
	bytePatch := []byte(jsonPatch)

	var err error
	switch resourceType {
	case kubernetes.DestinationRules:
		istioConfigDetail.DestinationRule = &networking_v1beta1.DestinationRule{}
		istioConfigDetail.DestinationRule, err = in.k8s.Istio().NetworkingV1beta1().DestinationRules(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.EnvoyFilters:
		istioConfigDetail.EnvoyFilter = &networking_v1alpha3.EnvoyFilter{}
		istioConfigDetail.EnvoyFilter, err = in.k8s.Istio().NetworkingV1alpha3().EnvoyFilters(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.Gateways:
		istioConfigDetail.Gateway = &networking_v1beta1.Gateway{}
		istioConfigDetail.Gateway, err = in.k8s.Istio().NetworkingV1beta1().Gateways(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.K8sGateways:
		istioConfigDetail.K8sGateway = &k8s_networking_v1alpha2.Gateway{}
		istioConfigDetail.K8sGateway, err = in.k8s.GatewayAPI().GatewayV1alpha2().Gateways(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.K8sHTTPRoutes:
		istioConfigDetail.K8sHTTPRoute = &k8s_networking_v1alpha2.HTTPRoute{}
		istioConfigDetail.K8sHTTPRoute, err = in.k8s.GatewayAPI().GatewayV1alpha2().HTTPRoutes(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.ServiceEntries:
		istioConfigDetail.ServiceEntry = &networking_v1beta1.ServiceEntry{}
		istioConfigDetail.ServiceEntry, err = in.k8s.Istio().NetworkingV1beta1().ServiceEntries(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.Sidecars:
		istioConfigDetail.Sidecar = &networking_v1beta1.Sidecar{}
		istioConfigDetail.Sidecar, err = in.k8s.Istio().NetworkingV1beta1().Sidecars(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.VirtualServices:
		istioConfigDetail.VirtualService = &networking_v1beta1.VirtualService{}
		istioConfigDetail.VirtualService, err = in.k8s.Istio().NetworkingV1beta1().VirtualServices(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.WorkloadEntries:
		istioConfigDetail.WorkloadEntry = &networking_v1beta1.WorkloadEntry{}
		istioConfigDetail.WorkloadEntry, err = in.k8s.Istio().NetworkingV1beta1().WorkloadEntries(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.WorkloadGroups:
		istioConfigDetail.WorkloadGroup = &networking_v1beta1.WorkloadGroup{}
		istioConfigDetail.WorkloadGroup, err = in.k8s.Istio().NetworkingV1beta1().WorkloadGroups(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.AuthorizationPolicies:
		istioConfigDetail.AuthorizationPolicy = &security_v1beta1.AuthorizationPolicy{}
		istioConfigDetail.AuthorizationPolicy, err = in.k8s.Istio().SecurityV1beta1().AuthorizationPolicies(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.PeerAuthentications:
		istioConfigDetail.PeerAuthentication = &security_v1beta1.PeerAuthentication{}
		istioConfigDetail.PeerAuthentication, err = in.k8s.Istio().SecurityV1beta1().PeerAuthentications(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.RequestAuthentications:
		istioConfigDetail.RequestAuthentication = &security_v1beta1.RequestAuthentication{}
		istioConfigDetail.RequestAuthentication, err = in.k8s.Istio().SecurityV1beta1().RequestAuthentications(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.WasmPlugins:
		istioConfigDetail.WasmPlugin = &extentions_v1alpha1.WasmPlugin{}
		istioConfigDetail.WasmPlugin, err = in.k8s.Istio().ExtensionsV1alpha1().WasmPlugins(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	case kubernetes.Telemetries:
		istioConfigDetail.Telemetry = &v1alpha1.Telemetry{}
		istioConfigDetail.Telemetry, err = in.k8s.Istio().TelemetryV1alpha1().Telemetries(namespace).Patch(ctx, name, patchType, bytePatch, patchOpts)
	default:
		err = fmt.Errorf("object type not found: %v", resourceType)
	}

	// Cache is stopped after a Create/Update/Delete operation to force a refresh
	if kialiCache != nil && err == nil {
		kialiCache.Refresh(namespace)
	}
	return istioConfigDetail, err
}

func (in *IstioConfigService) CreateIstioConfigDetail(namespace, resourceType string, body []byte) (models.IstioConfigDetails, error) {
	istioConfigDetail := models.IstioConfigDetails{}
	istioConfigDetail.Namespace = models.Namespace{Name: namespace}
	istioConfigDetail.ObjectType = resourceType

	createOpts := meta_v1.CreateOptions{}
	ctx := context.TODO()

	var err error
	switch resourceType {
	case kubernetes.DestinationRules:
		istioConfigDetail.DestinationRule = &networking_v1beta1.DestinationRule{}
		err = json.Unmarshal(body, istioConfigDetail.DestinationRule)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.DestinationRule, err = in.k8s.Istio().NetworkingV1beta1().DestinationRules(namespace).Create(ctx, istioConfigDetail.DestinationRule, createOpts)
	case kubernetes.EnvoyFilters:
		istioConfigDetail.EnvoyFilter = &networking_v1alpha3.EnvoyFilter{}
		err = json.Unmarshal(body, istioConfigDetail.EnvoyFilter)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.EnvoyFilter, err = in.k8s.Istio().NetworkingV1alpha3().EnvoyFilters(namespace).Create(ctx, istioConfigDetail.EnvoyFilter, createOpts)
	case kubernetes.Gateways:
		istioConfigDetail.Gateway = &networking_v1beta1.Gateway{}
		err = json.Unmarshal(body, istioConfigDetail.Gateway)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.Gateway, err = in.k8s.Istio().NetworkingV1beta1().Gateways(namespace).Create(ctx, istioConfigDetail.Gateway, createOpts)
	case kubernetes.K8sGateways:
		istioConfigDetail.K8sGateway = &k8s_networking_v1alpha2.Gateway{}
		err = json.Unmarshal(body, istioConfigDetail.K8sGateway)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.K8sGateway, err = in.k8s.GatewayAPI().GatewayV1alpha2().Gateways(namespace).Create(ctx, istioConfigDetail.K8sGateway, createOpts)
	case kubernetes.K8sHTTPRoutes:
		istioConfigDetail.K8sHTTPRoute = &k8s_networking_v1alpha2.HTTPRoute{}
		err = json.Unmarshal(body, istioConfigDetail.K8sHTTPRoute)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.K8sHTTPRoute, err = in.k8s.GatewayAPI().GatewayV1alpha2().HTTPRoutes(namespace).Create(ctx, istioConfigDetail.K8sHTTPRoute, createOpts)
	case kubernetes.ServiceEntries:
		istioConfigDetail.ServiceEntry = &networking_v1beta1.ServiceEntry{}
		err = json.Unmarshal(body, istioConfigDetail.ServiceEntry)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.ServiceEntry, err = in.k8s.Istio().NetworkingV1beta1().ServiceEntries(namespace).Create(ctx, istioConfigDetail.ServiceEntry, createOpts)
	case kubernetes.Sidecars:
		istioConfigDetail.Sidecar = &networking_v1beta1.Sidecar{}
		err = json.Unmarshal(body, istioConfigDetail.Sidecar)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.Sidecar, err = in.k8s.Istio().NetworkingV1beta1().Sidecars(namespace).Create(ctx, istioConfigDetail.Sidecar, createOpts)
	case kubernetes.VirtualServices:
		istioConfigDetail.VirtualService = &networking_v1beta1.VirtualService{}
		err = json.Unmarshal(body, istioConfigDetail.VirtualService)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.VirtualService, err = in.k8s.Istio().NetworkingV1beta1().VirtualServices(namespace).Create(ctx, istioConfigDetail.VirtualService, createOpts)
	case kubernetes.WorkloadEntries:
		istioConfigDetail.WorkloadEntry = &networking_v1beta1.WorkloadEntry{}
		err = json.Unmarshal(body, istioConfigDetail.WorkloadEntry)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.WorkloadEntry, err = in.k8s.Istio().NetworkingV1beta1().WorkloadEntries(namespace).Create(ctx, istioConfigDetail.WorkloadEntry, createOpts)
	case kubernetes.WorkloadGroups:
		istioConfigDetail.WorkloadGroup = &networking_v1beta1.WorkloadGroup{}
		err = json.Unmarshal(body, istioConfigDetail.WorkloadGroup)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.WorkloadGroup, err = in.k8s.Istio().NetworkingV1beta1().WorkloadGroups(namespace).Create(ctx, istioConfigDetail.WorkloadGroup, createOpts)
	case kubernetes.WasmPlugins:
		istioConfigDetail.WasmPlugin = &extentions_v1alpha1.WasmPlugin{}
		err = json.Unmarshal(body, istioConfigDetail.WasmPlugin)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.WasmPlugin, err = in.k8s.Istio().ExtensionsV1alpha1().WasmPlugins(namespace).Create(ctx, istioConfigDetail.WasmPlugin, createOpts)
	case kubernetes.Telemetries:
		istioConfigDetail.Telemetry = &v1alpha1.Telemetry{}
		err = json.Unmarshal(body, istioConfigDetail.Telemetry)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.Telemetry, err = in.k8s.Istio().TelemetryV1alpha1().Telemetries(namespace).Create(ctx, istioConfigDetail.Telemetry, createOpts)
	case kubernetes.AuthorizationPolicies:
		istioConfigDetail.AuthorizationPolicy = &security_v1beta1.AuthorizationPolicy{}
		err = json.Unmarshal(body, istioConfigDetail.AuthorizationPolicy)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.AuthorizationPolicy, err = in.k8s.Istio().SecurityV1beta1().AuthorizationPolicies(namespace).Create(ctx, istioConfigDetail.AuthorizationPolicy, createOpts)
	case kubernetes.PeerAuthentications:
		istioConfigDetail.PeerAuthentication = &security_v1beta1.PeerAuthentication{}
		err = json.Unmarshal(body, istioConfigDetail.PeerAuthentication)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.PeerAuthentication, err = in.k8s.Istio().SecurityV1beta1().PeerAuthentications(namespace).Create(ctx, istioConfigDetail.PeerAuthentication, createOpts)
	case kubernetes.RequestAuthentications:
		istioConfigDetail.RequestAuthentication = &security_v1beta1.RequestAuthentication{}
		err = json.Unmarshal(body, istioConfigDetail.RequestAuthentication)
		if err != nil {
			return istioConfigDetail, api_errors.NewBadRequest(err.Error())
		}
		istioConfigDetail.RequestAuthentication, err = in.k8s.Istio().SecurityV1beta1().RequestAuthentications(namespace).Create(ctx, istioConfigDetail.RequestAuthentication, createOpts)
	default:
		err = fmt.Errorf("object type not found: %v", resourceType)
	}
	// Cache is stopped after a Create/Update/Delete operation to force a refresh
	if kialiCache != nil && err == nil {
		kialiCache.Refresh(namespace)
	}
	return istioConfigDetail, err
}

func (in *IstioConfigService) GetIstioConfigPermissions(ctx context.Context, namespaces []string) models.IstioConfigPermissions {
	var end observability.EndFunc
	ctx, end = observability.StartSpan(ctx, "GetIstioConfigPermissions",
		observability.Attribute("package", "business"),
		observability.Attribute("namespaces", namespaces),
	)
	defer end()

	istioConfigPermissions := make(models.IstioConfigPermissions, len(namespaces))

	if len(namespaces) > 0 {
		networkingPermissions := make(models.IstioConfigPermissions, len(namespaces))
		securityPermissions := make(models.IstioConfigPermissions, len(namespaces))

		wg := sync.WaitGroup{}
		// We will query 2 times per namespace (networking.istio.io and security.istio.io)
		wg.Add(len(namespaces) * 2)
		for _, ns := range namespaces {
			networkingRP := make(models.ResourcesPermissions, len(newNetworkingConfigTypes))
			securityRP := make(models.ResourcesPermissions, len(newSecurityConfigTypes))
			networkingPermissions[ns] = &networkingRP
			securityPermissions[ns] = &securityRP
			/*
				We can optimize this logic.
				Instead of query all editable objects of networking.istio.io and security.istio.io we can query
				only one per API, that will save several queries to the backend.

				Synced with:
				https://github.com/kiali/kiali-operator/blob/master/roles/default/kiali-deploy/templates/kubernetes/role.yaml#L62
			*/
			go func(ctx context.Context, namespace string, wg *sync.WaitGroup, networkingPermissions *models.ResourcesPermissions) {
				defer wg.Done()
				canCreate, canUpdate, canDelete := getPermissionsApi(ctx, in.k8s, namespace, kubernetes.NetworkingGroupVersionV1Beta1.Group, allResources)
				for _, rs := range newNetworkingConfigTypes {
					networkingRP[rs] = &models.ResourcePermissions{
						Create: canCreate,
						Update: canUpdate,
						Delete: canDelete,
					}
				}
			}(ctx, ns, &wg, &networkingRP)

			go func(ctx context.Context, namespace string, wg *sync.WaitGroup, securityPermissions *models.ResourcesPermissions) {
				defer wg.Done()
				canCreate, canUpdate, canDelete := getPermissionsApi(ctx, in.k8s, namespace, kubernetes.SecurityGroupVersion.Group, allResources)
				for _, rs := range newSecurityConfigTypes {
					securityRP[rs] = &models.ResourcePermissions{
						Create: canCreate,
						Update: canUpdate,
						Delete: canDelete,
					}
				}
			}(ctx, ns, &wg, &securityRP)
		}
		wg.Wait()

		// Join networking and security permissions into a single result
		for _, ns := range namespaces {
			allRP := make(models.ResourcesPermissions, len(newNetworkingConfigTypes)+len(newSecurityConfigTypes))
			istioConfigPermissions[ns] = &allRP
			for resource, permissions := range *networkingPermissions[ns] {
				(*istioConfigPermissions[ns])[resource] = permissions
			}
			for resource, permissions := range *securityPermissions[ns] {
				(*istioConfigPermissions[ns])[resource] = permissions
			}
		}
	}
	return istioConfigPermissions
}

func getPermissions(ctx context.Context, k8s kubernetes.ClientInterface, namespace, objectType string) (bool, bool, bool) {
	var canCreate, canPatch, canDelete bool

	if api, ok := kubernetes.ResourceTypesToAPI[objectType]; ok {
		resourceType := objectType
		return getPermissionsApi(ctx, k8s, namespace, api, resourceType)
	}
	return canCreate, canPatch, canDelete
}

func getPermissionsApi(ctx context.Context, k8s kubernetes.ClientInterface, namespace, api, resourceType string) (bool, bool, bool) {
	var canCreate, canPatch, canDelete bool

	// In view only mode, there is not need to check RBAC permissions, return false early
	if config.Get().Deployment.ViewOnlyMode {
		log.Debug("View only mode configured, skipping RBAC checks")
		return canCreate, canPatch, canDelete
	}

	/*
		Kiali only uses create,patch,delete as WRITE permissions

		"update" creates an extra call to the API that we know that it will always fail, introducing extra latency

		Synced with:
		https://github.com/kiali/kiali-operator/blob/master/roles/default/kiali-deploy/templates/kubernetes/role.yaml#L62
	*/
	ssars, permErr := k8s.GetSelfSubjectAccessReview(ctx, namespace, api, resourceType, []string{"create", "patch", "delete"})
	if permErr == nil {
		for _, ssar := range ssars {
			if ssar.Spec.ResourceAttributes != nil {
				switch ssar.Spec.ResourceAttributes.Verb {
				case "create":
					canCreate = ssar.Status.Allowed
				case "patch":
					canPatch = ssar.Status.Allowed
				case "delete":
					canDelete = ssar.Status.Allowed
				}
			}
		}
	} else {
		log.Errorf("Error getting permissions [namespace: %s, api: %s, resourceType: %s]: %v", namespace, api, "*", permErr)
	}
	return canCreate, canPatch, canDelete
}

func checkType(types []string, name string) bool {
	for _, typeName := range types {
		if typeName == name {
			return true
		}
	}
	return false
}

func ParseIstioConfigCriteria(namespace, objects, labelSelector, workloadSelector string, allNamespaces bool) IstioConfigCriteria {
	defaultInclude := objects == ""
	criteria := IstioConfigCriteria{}
	criteria.IncludeGateways = defaultInclude
	criteria.IncludeK8sGateways = defaultInclude
	criteria.IncludeK8sHTTPRoutes = defaultInclude
	criteria.IncludeVirtualServices = defaultInclude
	criteria.IncludeDestinationRules = defaultInclude
	criteria.IncludeServiceEntries = defaultInclude
	criteria.IncludeSidecars = defaultInclude
	criteria.IncludeAuthorizationPolicies = defaultInclude
	criteria.IncludePeerAuthentications = defaultInclude
	criteria.IncludeWorkloadEntries = defaultInclude
	criteria.IncludeWorkloadGroups = defaultInclude
	criteria.IncludeRequestAuthentications = defaultInclude
	criteria.IncludeEnvoyFilters = defaultInclude
	criteria.IncludeWasmPlugins = defaultInclude
	criteria.IncludeTelemetry = defaultInclude
	criteria.LabelSelector = labelSelector
	criteria.WorkloadSelector = workloadSelector

	if allNamespaces {
		criteria.AllNamespaces = true
	} else {
		criteria.Namespace = namespace
	}

	if defaultInclude {
		return criteria
	}

	types := strings.Split(objects, ",")
	if checkType(types, kubernetes.Gateways) {
		criteria.IncludeGateways = true
	}
	if checkType(types, kubernetes.K8sGateways) {
		criteria.IncludeK8sGateways = true
	}
	if checkType(types, kubernetes.K8sHTTPRoutes) {
		criteria.IncludeK8sHTTPRoutes = true
	}
	if checkType(types, kubernetes.VirtualServices) {
		criteria.IncludeVirtualServices = true
	}
	if checkType(types, kubernetes.DestinationRules) {
		criteria.IncludeDestinationRules = true
	}
	if checkType(types, kubernetes.ServiceEntries) {
		criteria.IncludeServiceEntries = true
	}
	if checkType(types, kubernetes.Sidecars) {
		criteria.IncludeSidecars = true
	}
	if checkType(types, kubernetes.AuthorizationPolicies) {
		criteria.IncludeAuthorizationPolicies = true
	}
	if checkType(types, kubernetes.PeerAuthentications) {
		criteria.IncludePeerAuthentications = true
	}
	if checkType(types, kubernetes.WorkloadEntries) {
		criteria.IncludeWorkloadEntries = true
	}
	if checkType(types, kubernetes.WorkloadGroups) {
		criteria.IncludeWorkloadGroups = true
	}
	if checkType(types, kubernetes.WasmPlugins) {
		criteria.IncludeWasmPlugins = true
	}
	if checkType(types, kubernetes.Telemetries) {
		criteria.IncludeTelemetry = true
	}
	if checkType(types, kubernetes.RequestAuthentications) {
		criteria.IncludeRequestAuthentications = true
	}
	if checkType(types, kubernetes.EnvoyFilters) {
		criteria.IncludeEnvoyFilters = true
	}
	return criteria
}
