package signal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LycheeOrg/Keep-Me-Alive/internal/config"
)

func TestClient_Send(t *testing.T) {
	var gotAuth string
	var gotBody sendMessageRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := config.SignalConfig{
		BaseURL:      srv.URL,
		Username:     "user",
		Password:     "pass",
		SenderNumber: "+15551234567",
		Recipients:   []string{"group.abc=="},
	}
	c := New(cfg, time.Second)

	if err := c.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if gotAuth != wantAuth {
		t.Errorf("Authorization header = %q, want %q", gotAuth, wantAuth)
	}
	if gotBody.Message != "hello" {
		t.Errorf("Message = %q, want %q", gotBody.Message, "hello")
	}
	if gotBody.Number != "+15551234567" {
		t.Errorf("Number = %q, want %q", gotBody.Number, "+15551234567")
	}
	if len(gotBody.Recipients) != 1 || gotBody.Recipients[0] != "group.abc==" {
		t.Errorf("Recipients = %v, want [group.abc==]", gotBody.Recipients)
	}
}

func TestClient_Send_NonTwoXX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("bad credentials"))
	}))
	defer srv.Close()

	cfg := config.SignalConfig{
		BaseURL:      srv.URL,
		SenderNumber: "+1",
		Recipients:   []string{"group.abc=="},
	}
	c := New(cfg, time.Second)

	err := c.Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("Send() expected error, got nil")
	}
}
