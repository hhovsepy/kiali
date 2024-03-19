package handlers

import (
	"fmt"
	"github.com/kiali/kiali/models"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/kiali/kiali/business"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// appParams holds the path and query parameters for appList and appDetails
//
// swagger:parameters appList AppDetails
type appParams struct {
	baseHealthParams
	// The target workload
	//
	// in: path
	Namespace   string `json:"namespace"`
	ClusterName string `json:"clusterName"`
	AppName     string `json:"app"`
	// Optional
	IncludeHealth         bool `json:"health"`
	IncludeIstioResources bool `json:"istioResources"`
}

func (p *appParams) extract(r *http.Request) {
	vars := mux.Vars(r)
	query := r.URL.Query()
	p.baseExtract(r, vars)
	p.Namespace = vars["namespace"]
	p.ClusterName = clusterNameFromQuery(query)
	p.AppName = vars["app"]
	var err error
	p.IncludeHealth, err = strconv.ParseBool(query.Get("health"))
	if err != nil {
		p.IncludeHealth = true
	}
	p.IncludeIstioResources, err = strconv.ParseBool(query.Get("istioResources"))
	if err != nil {
		p.IncludeIstioResources = true
	}
}

// ClustersApps is the API handler to fetch all the apps to be displayed, related to a single cluster
func ClustersApps(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	namespaces := query.Get("namespaces") // csl of namespaces
	nss := []string{}
	if len(namespaces) > 0 {
		nss = strings.Split(namespaces, ",")
	}
	p := appParams{}
	p.extract(r)

	criteria := business.AppCriteria{
		Cluster: p.ClusterName,
		// Load services from all namespaces cache for particular cluster, then will filter them
		Namespace:             meta_v1.NamespaceAll,
		IncludeIstioResources: p.IncludeIstioResources,
		IncludeHealth:         p.IncludeHealth,
		RateInterval:          p.RateInterval,
		QueryTime:             p.QueryTime,
	}

	// Get business layer
	business, err := getBusiness(r)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Apps initialization error: "+err.Error())
		return
	}

	if criteria.IncludeHealth {
		namespaces, _ := business.Namespace.GetClusterNamespaces(r.Context(), criteria.Cluster)
		if len(namespaces) == 0 {
			err = fmt.Errorf("No namespaces found for cluster  [%s]", criteria.Cluster)
			handleErrorResponse(w, err, "Error looking for namespaces: "+err.Error())
			return
		}
		ns := GetOldestNamespace(namespaces)
		rateInterval, err := adjustRateInterval(r.Context(), business, ns.Name, p.RateInterval, p.QueryTime, ns.Cluster)
		if err != nil {
			handleErrorResponse(w, err, "Adjust rate interval error: "+err.Error())
			return
		}
		criteria.RateInterval = rateInterval
	}

	// Fetch and build apps
	appList, err := business.App.GetAppList(r.Context(), criteria)
	if err != nil {
		handleErrorResponse(w, err)
		return
	}

	// filter apps by provided namespaces before returning
	if len(nss) != 0 {
		result := models.AppList{
			Apps:    []models.AppListItem{},
			Cluster: appList.Cluster,
		}
		for _, app := range appList.Apps {
			if app.Cluster == criteria.Cluster {
				result.Apps = append(result.Apps, app)
			}
		}
		RespondWithJSON(w, http.StatusOK, result)
	} else {
		// return services from all namespaces
		RespondWithJSON(w, http.StatusOK, appList)
	}
}

// AppList is the API handler to fetch all the apps to be displayed, related to a single namespace
// @TODO should be removed as ClustersApps is added, left for Backstage plugin integration
func AppList(w http.ResponseWriter, r *http.Request) {
	p := appParams{}
	p.extract(r)

	criteria := business.AppCriteria{Namespace: p.Namespace, IncludeIstioResources: p.IncludeIstioResources,
		IncludeHealth: p.IncludeHealth, RateInterval: p.RateInterval, QueryTime: p.QueryTime}

	// Get business layer
	business, err := getBusiness(r)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Apps initialization error: "+err.Error())
		return
	}

	if criteria.IncludeHealth {
		// When the cluster is not specified, we need to get it. If there are more than one,
		// get the one for which the namespace creation time is oldest
		namespaces, _ := business.Namespace.GetNamespaceClusters(r.Context(), p.Namespace)
		if len(namespaces) == 0 {
			err = fmt.Errorf("No clusters found for namespace  [%s]", p.Namespace)
			handleErrorResponse(w, err, err.Error())
			return
		}
		ns := GetOldestNamespace(namespaces)
		rateInterval, err := adjustRateInterval(r.Context(), business, p.Namespace, p.RateInterval, p.QueryTime, ns.Cluster)
		if err != nil {
			handleErrorResponse(w, err, "Adjust rate interval error: "+err.Error())
			return
		}
		criteria.RateInterval = rateInterval
	}

	// Fetch and build apps
	appList, err := business.App.GetAppList(r.Context(), criteria)
	if err != nil {
		handleErrorResponse(w, err)
		return
	}

	RespondWithJSON(w, http.StatusOK, appList)
}

// AppDetails is the API handler to fetch all details to be displayed, related to a single app
func AppDetails(w http.ResponseWriter, r *http.Request) {
	p := appParams{}
	p.extract(r)

	criteria := business.AppCriteria{Namespace: p.Namespace, AppName: p.AppName, IncludeIstioResources: true, IncludeHealth: p.IncludeHealth,
		RateInterval: p.RateInterval, QueryTime: p.QueryTime, Cluster: p.ClusterName}

	// Get business layer
	business, err := getBusiness(r)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "Services initialization error: "+err.Error())
		return
	}

	// Fetch and build app
	appDetails, err := business.App.GetAppDetails(r.Context(), criteria)
	if err != nil {
		handleErrorResponse(w, err)
		return
	}

	RespondWithJSON(w, http.StatusOK, appDetails)
}
