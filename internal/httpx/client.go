package httpx

import (
	"net/http"
	"sync"
)

var (
	clientMu sync.Mutex
	client   = http.DefaultClient
)

func Client() *http.Client {
	clientMu.Lock()
	defer clientMu.Unlock()
	return client
}

func SetClient(c *http.Client) func() {
	clientMu.Lock()
	old := client
	client = c
	clientMu.Unlock()
	return func() {
		clientMu.Lock()
		client = old
		clientMu.Unlock()
	}
}
