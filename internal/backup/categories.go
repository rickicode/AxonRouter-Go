package backup

import "sort"

var CategoryTables = map[string][]string{
	"core": {
		"provider_types",
		"connections",
		"model_rate_limits",
		"api_keys",
		"settings",
		"rotation_state",
		"api_key_usage",
		"quota_cache",
		"proxy_pools",
		"proxy_groups",
		"model_pricing",
		"provider_models",
	},
	"combos": {
		"combos",
		"combo_steps",
	},
	"logs": {
		"request_logs",
		"compression_metrics",
	},
	"cache": {
		"response_cache",
	},
}

func AllCategories() []string {
	categories := make([]string, 0, len(CategoryTables))
	for category := range CategoryTables {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	return categories
}
