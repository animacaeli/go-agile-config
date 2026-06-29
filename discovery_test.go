package agileconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_ListServices(t *testing.T) {
	services := []ServiceInfo{
		{ServiceID: "orders", ServiceName: "orders-api", IP: "10.0.0.1", Status: ServiceStatusHealthy},
		{ServiceID: "billing", ServiceName: "billing-api", IP: "10.0.0.2", Status: ServiceStatusUnhealthy},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/RegisterCenter/services" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(services)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1")
	result, err := client.ListServices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 services, got %d", len(result))
	}
	if result[0].ServiceID != "orders" || result[1].ServiceID != "billing" {
		t.Fatalf("unexpected services: %+v", result)
	}
}

func TestClient_ServiceRegistrationLifecycleMethods(t *testing.T) {
	var registered bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/RegisterCenter":
			registered = true
			json.NewEncoder(w).Encode(RegisterResult{UniqueID: "service-unique-id"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/RegisterCenter/heartbeat":
			if !registered {
				t.Fatal("heartbeat received before register")
			}
			json.NewEncoder(w).Encode(HeartbeatResult{Module: "RegisterCenter", Action: "ping", Data: "md5"})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/RegisterCenter/service-unique-id":
			json.NewEncoder(w).Encode(RegisterResult{UniqueID: "service-unique-id"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1")
	registerResult, err := client.RegisterService(context.Background(), RegisterService{
		ServiceID:   "orders",
		ServiceName: "orders-api",
		IP:          "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if registerResult.UniqueID != "service-unique-id" {
		t.Fatalf("unexpected register result: %+v", registerResult)
	}

	heartbeatResult, err := client.Heartbeat(context.Background(), registerResult.UniqueID)
	if err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}
	if heartbeatResult.Action != "ping" {
		t.Fatalf("unexpected heartbeat result: %+v", heartbeatResult)
	}

	unregisterResult, err := client.UnregisterService(context.Background(), registerResult.UniqueID)
	if err != nil {
		t.Fatalf("unregister failed: %v", err)
	}
	if unregisterResult.UniqueID != "service-unique-id" {
		t.Fatalf("unexpected unregister result: %+v", unregisterResult)
	}
}
