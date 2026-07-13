package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheck_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := Check(context.Background(), srv.URL, time.Second)
	if !res.Up {
		t.Fatalf("Check() Up = false, want true; err=%v", res.Err)
	}
}

func TestCheck_NonTwoXX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := Check(context.Background(), srv.URL, time.Second)
	if res.Up {
		t.Fatal("Check() Up = true, want false for 500 status")
	}
	if res.Err == nil {
		t.Fatal("Check() Err = nil, want non-nil")
	}
}

func TestCheck_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	res := Check(context.Background(), url, time.Second)
	if res.Up {
		t.Fatal("Check() Up = true, want false for closed server")
	}
}

func TestCheck_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := Check(context.Background(), srv.URL, 20*time.Millisecond)
	if res.Up {
		t.Fatal("Check() Up = true, want false for timeout")
	}
	if res.Err == nil {
		t.Fatal("Check() Err = nil, want non-nil (timeout reason)")
	}
}
