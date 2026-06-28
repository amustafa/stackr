package store

// Config holds the stackr configuration persisted to config.json.
type Config struct {
	Trunk  string `json:"trunk"`
	Remote string `json:"remote"`
}
