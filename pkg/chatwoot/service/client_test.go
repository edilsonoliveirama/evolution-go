package chatwoot_service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryDelay(t *testing.T) {
	if d := retryDelay("3", 0); d != 3*time.Second {
		t.Errorf("Retry-After 3 = %v", d)
	}
	if d := retryDelay("", 2); d != 4*time.Second {
		t.Errorf("backoff attempt 2 = %v", d)
	}
	if d := retryDelay("", 10); d != 60*time.Second {
		t.Errorf("backoff cap = %v", d)
	}
}

func TestSendRetriesOn429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"name":"x"}` {
			t.Errorf("body not resent on retry: %q", body)
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer srv.Close()

	c := newAPIClient(srv.URL, 1, "t")
	var out struct{ ID int }
	if err := c.doJSON(context.Background(), http.MethodPost, srv.URL+"/x", map[string]string{"name": "x"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != 7 || calls != 3 {
		t.Errorf("id=%d calls=%d", out.ID, calls)
	}
}
