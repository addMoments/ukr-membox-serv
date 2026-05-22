package routes

import (
	"membox-serv/src/env"
	"membox-serv/src/novaposhta"
	networkutils "membox-serv/src/network_utils"
	"net/http"
	"strconv"
)

type np_proxy_routes_typ struct{}

var NPProxyRoutes np_proxy_routes_typ

// GET /api/np/settlements?q=Kyiv&limit=20&page=1
func (np_proxy_routes_typ) Settlements(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}

	limit := 20
	page := 1
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	apiKey := env.Env().NovaPoshta.APIKey
	results, err := novaposhta.SearchSettlements(apiKey, q, limit, page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if results == nil {
		results = []novaposhta.Settlement{}
	}

	networkutils.SendJson(results, w)
}

// GET /api/np/warehouses?city_ref=UUID
func (np_proxy_routes_typ) Warehouses(w http.ResponseWriter, r *http.Request) {
	cityRef := r.URL.Query().Get("city_ref")
	if cityRef == "" {
		http.Error(w, "city_ref is required", http.StatusBadRequest)
		return
	}

	apiKey := env.Env().NovaPoshta.APIKey
	warehouses, err := novaposhta.GetWarehouses(apiKey, cityRef)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if warehouses == nil {
		warehouses = []novaposhta.Warehouse{}
	}

	networkutils.SendJson(warehouses, w)
}
