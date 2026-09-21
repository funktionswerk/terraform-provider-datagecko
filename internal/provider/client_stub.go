package provider

import "net/http"

// Client is replaced by the real implementation in Task 2. This stub exists
// only so Task 1 compiles and its own test can run in isolation.
type Client struct {
	Endpoint string
	AdminKey string
}

func NewClient(endpoint, adminKey string, httpClient *http.Client) *Client {
	return &Client{Endpoint: endpoint, AdminKey: adminKey}
}
