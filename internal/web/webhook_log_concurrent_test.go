package web

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestHandleGetLogs_Concurrent(t *testing.T) {
	srv := NewServer(Config{ListenAddr: ":0"})

	var wg sync.WaitGroup
	// Writer goroutines.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				srv.webhookMu.Lock()
				srv.webhookLog = append(srv.webhookLog, map[string]interface{}{
					"test": true,
				})
				srv.webhookMu.Unlock()
			}
		}()
	}

	// Reader goroutines.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/api/logs", nil)
				srv.handleGetLogs(w, r)
				if w.Code != http.StatusOK {
					t.Errorf("status = %d, want 200", w.Code)
				}
			}
		}()
	}

	wg.Wait()
}
