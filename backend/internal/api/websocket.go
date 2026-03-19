package api

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/lib/pq"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type WebSocketManager struct {
	// jobID -> set of connections
	clients map[string]map[*websocket.Conn]struct{}
	mu      sync.RWMutex
}

func NewWebSocketManager() *WebSocketManager {
	return &WebSocketManager{
		clients: make(map[string]map[*websocket.Conn]struct{}),
	}
}

func (m *WebSocketManager) Subscribe(client *websocket.Conn, jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.clients[jobID]; !ok {
		m.clients[jobID] = make(map[*websocket.Conn]struct{})
	}
	m.clients[jobID][client] = struct{}{}
}

func (m *WebSocketManager) Unsubscribe(client *websocket.Conn, jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if conns, ok := m.clients[jobID]; ok {
		delete(conns, client)
		if len(conns) == 0 {
			delete(m.clients, jobID)
		}
	}
}

func (m *WebSocketManager) Broadcast(jobID string, message string) {
	m.mu.RLock()
	clients, ok := m.clients[jobID]
	m.mu.RUnlock()

	if !ok {
		return
	}

	for client := range clients {
		if err := client.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			log.Printf("[WS] broadcast error to job %s: %v", jobID, err)
		}
	}
}

type JobLogEvent struct {
	Event string `json:"event"`
	JobID uint64 `json:"job_id"`
	LogID uint64 `json:"log_id"`
}

func (m *WebSocketManager) ListenForJobLogs(dbURL string, db *gorm.DB) {
	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("[NOTIFY] Log listener error: %v", err)
		}
	}

	listener := pq.NewListener(dbURL, 10*time.Second, time.Minute, reportProblem)
	err := listener.Listen("opsevents")
	if err != nil {
		log.Fatalf("[NOTIFY] Failed to listen for logs: %v", err)
	}

	log.Println("[NOTIFY] Listening for 'opsevents' log notifications")

	for {
		select {
		case n := <-listener.Notify:
			var event JobLogEvent
			if err := json.Unmarshal([]byte(n.Extra), &event); err != nil {
				continue
			}

			if event.Event == "job_log" {
				var jobLog database.JobLog
				if err := db.First(&jobLog, event.LogID).Error; err == nil {
					// Format as HTML for HTMX compatibility
					// ✅ SECURITY: Escape log message to prevent XSS
					// ✅ ACCESSIBILITY: Use semantic time and ARIA labels
					logHTML := fmt.Sprintf(
						`<div class="log-line log-%s" id="log-%d" data-log-id="%d">`+
							`<time class="log-ts" datetime="%s" title="%s">%s</time> `+
							`<span class="log-level" aria-label="Log Level %s">[%s]</span> `+
							`<span class="log-msg">%s</span>`+
							`</div>`,
						stringsToLower(jobLog.Level),
						jobLog.ID,
						jobLog.ID,
						jobLog.CreatedAt.Format(time.RFC3339),
						jobLog.CreatedAt.Format(time.RFC3339),
						jobLog.CreatedAt.Format("15:04:05"),
						jobLog.Level,
						jobLog.Level,
						html.EscapeString(jobLog.Message),
					)
					m.Broadcast(strconv.FormatUint(event.JobID, 10), logHTML)
				}
			}
		case <-time.After(1 * time.Minute):
			go listener.Ping()
		}
	}
}

func stringsToLower(s string) string {
	return strings.ToLower(s)
}

func (m *WebSocketManager) HandleEvents(c *websocket.Conn) {
	// System-wide events use jobID "0"
	m.Subscribe(c, "0")
	defer m.Unsubscribe(c, "0")

	// Read loop to keep connection alive
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}

func (m *WebSocketManager) HandleConsole(c *websocket.Conn, db *gorm.DB) {
	jobIDStr := c.Params("job_id")
	jobIDUint, _ := strconv.ParseUint(jobIDStr, 10, 64)

	tailStr := c.Query("tail")
	sinceIDStr := c.Query("since_id")

	m.Subscribe(c, jobIDStr)
	defer m.Unsubscribe(c, jobIDStr)

	// Send initial backlog
	var logs []database.JobLog
	query := db.Where("job_id = ?", jobIDUint)

	if tailStr != "" {
		tail, _ := strconv.Atoi(tailStr)
		query = query.Order("id DESC").Limit(tail)
	} else if sinceIDStr != "" {
		sinceID, _ := strconv.ParseUint(sinceIDStr, 10, 64)
		query = query.Where("id > ?", sinceID).Order("id ASC")
	} else {
		query = query.Order("id DESC").Limit(50)
	}

	if err := query.Find(&logs).Error; err == nil {
		// If we queried DESC, we need to reverse for display
		if tailStr != "" || (tailStr == "" && sinceIDStr == "") {
			for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
				logs[i], logs[j] = logs[j], logs[i]
			}
		}

		for _, jobLog := range logs {
			// ✅ SECURITY: Escape log message to prevent XSS
			// ✅ ACCESSIBILITY: Use semantic time and ARIA labels
			logHTML := fmt.Sprintf(
				`<div class="log-line log-%s" id="log-%d" data-log-id="%d">`+
					`<time class="log-ts" datetime="%s" title="%s">%s</time> `+
					`<span class="log-level" aria-label="Log Level %s">[%s]</span> `+
					`<span class="log-msg">%s</span>`+
					`</div>`,
				stringsToLower(jobLog.Level),
				jobLog.ID,
				jobLog.ID,
				jobLog.CreatedAt.Format(time.RFC3339),
				jobLog.CreatedAt.Format(time.RFC3339),
				jobLog.CreatedAt.Format("15:04:05"),
				jobLog.Level,
				jobLog.Level,
				html.EscapeString(jobLog.Message),
			)
			c.WriteMessage(websocket.TextMessage, []byte(logHTML))
		}
	}

	// Read loop to keep connection alive
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}
