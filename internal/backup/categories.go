package backup

import "sort"

// CategoryTables maps user-facing backup categories to the database tables
// they contain. This matches the dashboard checklist groups so operators can
// export, for example, only providers+combos or only configuration data.
var CategoryTables = map[string][]string{
	"providers": {
		"provider_types",
		"connections",
		"provider_models",
		"model_rate_limits",
		"combos",
		"combo_steps",
	},
	"config": {
		"settings",
		"model_pricing",
		"proxy_pools",
		"proxy_groups",
		"rotation_state",
		"compression_metrics",
	},
	"api_keys": {
		"api_keys",
		"api_key_usage",
	},
	"usage": {
		"request_logs",
	},
	"cache": {
		"response_cache",
		"quota_cache",
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
