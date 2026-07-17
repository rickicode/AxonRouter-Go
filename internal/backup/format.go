package backup

const (
	FormatName    = "axonrouter-backup"
	FormatVersion = 1
)

type Header struct {
	Format     string   `json:"format"`
	Version    int      `json:"version"`
	Categories []string `json:"categories"`
	CreatedAt  int64    `json:"created_at"`
}

type Row struct {
	Table string         `json:"table"`
	Data  map[string]any `json:"data"`
}
