package cache

import (
	"time"

	"github.com/kiali/kiali/kubernetes"
	"github.com/kiali/kiali/models"
	networking_v1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	security_v1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
)

type fakeInformer struct {
	cache.SharedIndexInformer
	Store *cache.FakeCustomStore
}

func (f *fakeInformer) GetStore() cache.Store {
	return f.Store
}

func createPAIndexInformer(getter cache.Getter, resourceType string, refreshDuration time.Duration, namespace string) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(cache.NewListWatchFromClient(getter, resourceType, namespace, fields.Everything()),
		&security_v1beta1.PeerAuthentication{},
		refreshDuration,
		cache.Indexers{},
	)
}

// Fake KialiCache used for TLS Scenarios
// It populates the Namespaces, Informers and Registry information needed
func FakeTlsKialiCache(token string, namespaces []string, pa []security_v1beta1.PeerAuthentication, dr []networking_v1alpha3.DestinationRule) KialiCache {
	kialiCacheImpl := kialiCacheImpl{
		nsCache: make(map[string]typeCache),
		cacheIstioTypes: map[string]bool{
			kubernetes.PluralType[kubernetes.PeerAuthentications]: true,
		},
		tokenNamespaces: make(map[string]namespaceCache),
	}
	// Populate namespaces and PeerAuthentication informers
	nss := []models.Namespace{}
	for _, ns := range namespaces {
		nss = append(nss, models.Namespace{Name: ns})
		kialiCacheImpl.nsCache[ns] = typeCache{
			kubernetes.PeerAuthenticationsType: &fakeInformer{
				SharedIndexInformer: createPAIndexInformer(nil, kubernetes.PeerAuthentications, time.Minute, "anyns"),
				Store: &cache.FakeCustomStore{
					ListFunc: func() []interface{} {
						rPa := []interface{}{}
						for _, p := range pa {
							if p.Namespace == ns {
								rPa = append(rPa, p.DeepCopyObject())
							}
						}
						return rPa
					},
				},
			},
		}
	}
	kialiCacheImpl.SetNamespaces(token, nss)

	// Populate all DestinationRules using the Registry
	registryStatus := kubernetes.RegistryStatus{
		Configuration: &kubernetes.RegistryConfiguration{
			DestinationRules: []networking_v1alpha3.DestinationRule{},
		},
	}

	registryStatus.Configuration.DestinationRules = append(registryStatus.Configuration.DestinationRules, dr...)

	kialiCacheImpl.SetRegistryStatus(&registryStatus)

	return &kialiCacheImpl
}
