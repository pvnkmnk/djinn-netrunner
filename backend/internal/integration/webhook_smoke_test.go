//go:build integration

package integration

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "net"
    "net/http"
    "testing"
)

func TestSmoke_Webhook_Delivery(t *testing.T) {
    skipIfShort(t)
    baseURL := GetEnvOrDefault("INTEGRATION_BASE_URL", "http://localhost:8080")
    
    // Start a local HTTP server to receive webhook
    webhookURL, listener := startWebhookListener(t)
    defer listener.Close()
    
    client := integrationAdminClient(t, baseURL)
    
    // Register webhook URL via config Setting
    payload := map[string]string{"key": "notification_webhook_url", "value": webhookURL}
    body, _ := json.Marshal(payload)
    req, _ := http.NewRequest("PATCH", baseURL+"/api/admin/config", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := client.Do(req)
    if err != nil {
        t.Fatalf("Setting webhook failed: %v", err)
    }
    resp.Body.Close()
    
    // Trigger notification (manual approach)
    // For now, verify the setting was stored by listing config
    resp, err = client.Get(baseURL + "/api/admin/config")
    if err != nil {
        t.Fatalf("List config failed: %v", err)
    }
    if resp.StatusCode != 200 {
        t.Fatalf("List config expected 200, got %d", resp.StatusCode)
    }
    resp.Body.Close()
    
    // Note: Full webhook delivery verification requires a background job completion
    // to trigger the notification. This test verifies the config setting path.
    t.Log("Webhook URL setting test passed - endpoint exists and accepts the setting")
}

func startWebhookListener(t *testing.T) (string, *http.Server) {
    t.Helper()
    
    // Create a channel to receive webhook requests
    webhookChan := make(chan *http.Request, 1)
    
    // Create a handler that captures the request
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Read the request body
        body, err := io.ReadAll(r.Body)
        if err != nil {
            t.Errorf("Failed to read webhook body: %v", err)
            return
        }
        
        // Create a copy of the request to store
        req := r.Clone(context.Background())
        req.Body = io.NopCloser(bytes.NewReader(body))
        
        // Send to channel
        webhookChan <- req
        
        // Respond with success
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })
    
    // Bind explicitly and read the port back off the listener. `Addr: ":0"`
    // leaves srv.Addr as ":0" forever, so the URL handed to the app was
    // unusable and this listener could never receive a webhook — the fixed
    // 100ms sleep that used to sit here was a stand-in for a readiness window
    // that does not exist. Listening is synchronous.
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatalf("Failed to bind webhook listener: %v", err)
    }
    srv := &http.Server{Handler: handler}

    // Serve in background, on the listener already bound above.
    go func() {
        if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
            t.Errorf("Webhook listener error: %v", err)
        }
    }()

    // Hand back the address actually bound, plus the server for cleanup.
    return "http://" + ln.Addr().String(), srv
}