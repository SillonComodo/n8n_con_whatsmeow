package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bytes"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// ============================================================================
// TIPOS Y ESTRUCTURAS
// ============================================================================

// IncomingMessage representa un mensaje entrante para webhook
type IncomingMessage struct {
	SessionName   string            `json:"session_name"`
	MessageID     string            `json:"message_id"`
	From          string            `json:"from"`
	FromJID       string            `json:"from_jid"`
	Phone         string            `json:"phone"`
	FromName      string            `json:"from_name"`
	Chat          string            `json:"chat"`
	ChatJID       string            `json:"chat_jid"`
	ChatName      string            `json:"chat_name"`
	IsGroup       bool              `json:"is_group"`
	Timestamp     int64             `json:"timestamp"`
	Type          string            `json:"type"`
	Text          string            `json:"text,omitempty"`
	Caption       string            `json:"caption,omitempty"`
	MediaType     string            `json:"media_type,omitempty"`
	MimeType      string            `json:"mime_type,omitempty"`
	MediaURL      string            `json:"media_url,omitempty"`
	FileName      string            `json:"file_name,omitempty"`
	FileSize      uint64            `json:"file_size,omitempty"`
	HasMedia      bool              `json:"has_media,omitempty"`
	MediaCacheKey string            `json:"media_cache_key,omitempty"`
	QuotedID      string            `json:"quoted_id,omitempty"`
	QuotedText    string            `json:"quoted_text,omitempty"`
	Mentions      []string          `json:"mentions,omitempty"`
	Raw           map[string]string `json:"raw,omitempty"`
}

// WebhookConfig configuración de webhook para una sesión
type WebhookConfig struct {
	ID         string            `json:"id,omitempty"` // Identificador único del webhook
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers,omitempty"`
	Enabled    bool              `json:"enabled"`
	BatchDelay int               `json:"batch_delay,omitempty"` // Segundos para agrupar mensajes del mismo remitente (0 = deshabilitado)
}

// WebhooksConfig lista de webhooks para una sesión (soporta múltiples)
type WebhooksConfig struct {
	Webhooks []*WebhookConfig `json:"webhooks"`
}

// StoredMessage representa un mensaje almacenado en historial
type StoredMessage struct {
	MessageID   string `json:"message_id"`
	From        string `json:"from"`
	FromJID     string `json:"from_jid"`
	FromName    string `json:"from_name"`
	Chat        string `json:"chat"`
	ChatJID     string `json:"chat_jid"`
	IsGroup     bool   `json:"is_group"`
	Timestamp   int64  `json:"timestamp"`
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Caption     string `json:"caption,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
	HasMedia    bool   `json:"has_media,omitempty"`
	QuotedID    string `json:"quoted_id,omitempty"`
	QuotedText  string `json:"quoted_text,omitempty"`
	SessionName string `json:"session_name"`
}

// MessageStore almacena mensajes recibidos (últimos 1000 por chat)
type MessageStore struct {
	messages   map[string][]StoredMessage // key: chatJID
	mu         sync.RWMutex
	maxPerChat int
}

// NewMessageStore crea un nuevo almacén de mensajes
func NewMessageStore() *MessageStore {
	return &MessageStore{
		messages:   make(map[string][]StoredMessage),
		maxPerChat: 1000,
	}
}

// Add agrega un mensaje al almacén
func (ms *MessageStore) Add(chatJID string, msg StoredMessage) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	msgs := ms.messages[chatJID]
	msgs = append(msgs, msg)

	// Mantener solo los últimos maxPerChat mensajes
	if len(msgs) > ms.maxPerChat {
		msgs = msgs[len(msgs)-ms.maxPerChat:]
	}
	ms.messages[chatJID] = msgs
}

// GetHistory obtiene historial de un chat
func (ms *MessageStore) GetHistory(chatJID string, limit int) []StoredMessage {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	msgs, exists := ms.messages[chatJID]
	if !exists {
		return []StoredMessage{}
	}

	if limit <= 0 || limit > len(msgs) {
		limit = len(msgs)
	}

	// Retornar los últimos 'limit' mensajes
	start := len(msgs) - limit
	if start < 0 {
		start = 0
	}

	result := make([]StoredMessage, limit)
	copy(result, msgs[start:])
	return result
}

// GetAllChats obtiene lista de todos los chats con historial
func (ms *MessageStore) GetAllChats() []string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	chats := make([]string, 0, len(ms.messages))
	for chat := range ms.messages {
		chats = append(chats, chat)
	}
	return chats
}

var messageStore *MessageStore

// ============================================================================
// MESSAGE BATCHER - Agrupa mensajes del mismo remitente
// ============================================================================

// BatchedWebhookPayload representa el payload cuando se envían mensajes agrupados
type BatchedWebhookPayload struct {
	SessionName  string            `json:"session_name"`
	From         string            `json:"from"`
	FromJID      string            `json:"from_jid"`
	FromName     string            `json:"from_name"`
	Chat         string            `json:"chat"`
	ChatJID      string            `json:"chat_jid"`
	IsGroup      bool              `json:"is_group"`
	IsBatched    bool              `json:"is_batched"`
	MessageCount int               `json:"message_count"`
	Messages     []IncomingMessage `json:"messages"`
	// Campos de conveniencia para el primer mensaje (compatibilidad)
	Text      string `json:"text,omitempty"`
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`
}

// pendingBatch representa mensajes pendientes de un remitente para un webhook específico
type pendingBatch struct {
	messages []IncomingMessage
	timer    *time.Timer
	session  *Session
	webhook  *WebhookConfig
}

// MessageBatcher agrupa mensajes del mismo remitente por webhook
type MessageBatcher struct {
	pending map[string]*pendingBatch // key: sessionName_fromJID_webhookID
	mu      sync.Mutex
}

// NewMessageBatcher crea un nuevo batcher
func NewMessageBatcher() *MessageBatcher {
	return &MessageBatcher{
		pending: make(map[string]*pendingBatch),
	}
}

// AddWithWebhook agrega un mensaje al batch para un webhook específico
func (mb *MessageBatcher) AddWithWebhook(session *Session, msg IncomingMessage, webhook *WebhookConfig) {
	delaySeconds := webhook.BatchDelay
	if delaySeconds <= 0 {
		go session.sendToWebhook(webhook, msg)
		return
	}

	// Key incluye el ID/URL del webhook para separar batches por webhook
	webhookID := webhook.ID
	if webhookID == "" {
		webhookID = webhook.URL
	}
	key := fmt.Sprintf("%s_%s_%s", session.Name, msg.FromJID, webhookID)

	mb.mu.Lock()
	defer mb.mu.Unlock()

	batch, exists := mb.pending[key]
	if exists {
		// Ya hay un batch pendiente, agregar mensaje y reiniciar timer
		batch.messages = append(batch.messages, msg)
		batch.timer.Reset(time.Duration(delaySeconds) * time.Second)
		fmt.Printf("[%s] Message batched from %s for webhook %s (total: %d, waiting %ds)\n",
			session.Name, msg.From, webhookID, len(batch.messages), delaySeconds)
	} else {
		// Crear nuevo batch
		batch = &pendingBatch{
			messages: []IncomingMessage{msg},
			session:  session,
			webhook:  webhook,
		}
		mb.pending[key] = batch

		// Crear timer que enviará el batch cuando expire
		batch.timer = time.AfterFunc(time.Duration(delaySeconds)*time.Second, func() {
			mb.flush(key)
		})
		fmt.Printf("[%s] Message batch started from %s for webhook %s (waiting %ds for more)\n",
			session.Name, msg.From, webhookID, delaySeconds)
	}
}

// flush envía todos los mensajes pendientes de un remitente
func (mb *MessageBatcher) flush(key string) {
	mb.mu.Lock()
	batch, exists := mb.pending[key]
	if !exists {
		mb.mu.Unlock()
		return
	}
	delete(mb.pending, key)
	mb.mu.Unlock()

	if len(batch.messages) == 0 {
		return
	}

	// Enviar mensajes agrupados al webhook específico
	go batch.session.sendBatchToWebhook(batch.webhook, batch.messages)
}

var messageBatcher *MessageBatcher

// Session representa una sesión de WhatsApp activa
type Session struct {
	Client    *whatsmeow.Client
	Container *sqlstore.Container
	Name      string
	JID       string
	Connected bool
	QRCode    string
	QRASCII   string
	QRDataURL string
	Webhooks  []*WebhookConfig // Múltiples webhooks por sesión
	mu        sync.RWMutex
}

// SessionManager administra múltiples sesiones
type SessionManager struct {
	sessions map[string]*Session
	baseDir  string
	mu       sync.RWMutex
}

// SendMediaRequest request para enviar multimedia
type SendMediaRequest struct {
	Phone       string `json:"phone"`
	GroupJID    string `json:"group_jid,omitempty"`
	MediaType   string `json:"media_type"` // image, video, audio, document
	MediaURL    string `json:"media_url,omitempty"`
	MediaBase64 string `json:"media_base64,omitempty"`
	Caption     string `json:"caption,omitempty"`
	FileName    string `json:"file_name,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
	Ptt         bool   `json:"ptt,omitempty"` // Para audio: true = mensaje de voz (Push To Talk)
}

// GroupRequest request para operaciones de grupo
type GroupRequest struct {
	Name         string   `json:"name,omitempty"`
	Participants []string `json:"participants,omitempty"`
}

// CachedMediaMessage almacena un mensaje con media para descarga posterior
type CachedMediaMessage struct {
	SessionName string
	Message     *events.Message
	Timestamp   time.Time
}

// MediaCache almacena mensajes con media para descarga posterior (expiran en 10 minutos)
type MediaCache struct {
	messages map[string]*CachedMediaMessage
	mu       sync.RWMutex
}

var manager *SessionManager
var mediaCache *MediaCache

// NewMediaCache crea un nuevo cache de media
func NewMediaCache() *MediaCache {
	cache := &MediaCache{
		messages: make(map[string]*CachedMediaMessage),
	}
	// Iniciar goroutine para limpiar mensajes expirados cada minuto
	go cache.cleanupLoop()
	return cache
}

// Add agrega un mensaje al cache
func (mc *MediaCache) Add(key string, sessionName string, msg *events.Message) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.messages[key] = &CachedMediaMessage{
		SessionName: sessionName,
		Message:     msg,
		Timestamp:   time.Now(),
	}
}

// Get obtiene un mensaje del cache
func (mc *MediaCache) Get(key string) (*CachedMediaMessage, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	msg, exists := mc.messages[key]
	return msg, exists
}

// cleanupLoop limpia mensajes expirados (más de 10 minutos)
func (mc *MediaCache) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		mc.mu.Lock()
		now := time.Now()
		for key, cached := range mc.messages {
			if now.Sub(cached.Timestamp) > 10*time.Minute {
				delete(mc.messages, key)
			}
		}
		mc.mu.Unlock()
	}
}

// ============================================================================
// SESSION MANAGER
// ============================================================================

// NewSessionManager crea un nuevo administrador de sesiones
func NewSessionManager(baseDir string) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		baseDir:  baseDir,
	}
}

// GetOrCreateSession obtiene o crea una sesión
func (m *SessionManager) GetOrCreateSession(name string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.sessions[name]; exists {
		return session, nil
	}

	// Crear nueva sesión
	sessionDir := filepath.Join(m.baseDir, name)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session dir: %v", err)
	}

	dbPath := filepath.Join(sessionDir, "whatsmeow.db")
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", dbPath)

	container, err := sqlstore.New(context.Background(), "sqlite3", dsn, waLog.Noop)
	if err != nil {
		return nil, fmt.Errorf("db init: %v", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get device: %v", err)
	}

	client := whatsmeow.NewClient(deviceStore, waLog.Noop)

	session := &Session{
		Client:    client,
		Container: container,
		Name:      name,
		Connected: false,
	}

	// Cargar webhook config si existe
	session.loadWebhookConfig()

	// Event handler para actualizar estado y enviar webhooks
	client.AddEventHandler(func(evt interface{}) {
		session.handleEvent(evt)
	})

	// Si ya tiene sesión guardada, conectar automáticamente
	if client.Store.ID != nil {
		session.JID = client.Store.ID.String()
		go func() {
			if err := client.Connect(); err != nil {
				fmt.Printf("[%s] Auto-connect failed: %v\n", name, err)
			} else {
				fmt.Printf("[%s] Auto-connected\n", name)
			}
		}()
	}

	m.sessions[name] = session
	return session, nil
}

// GetSession obtiene una sesión existente sin crearla
func (m *SessionManager) GetSession(name string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[name]
}

// ListSessions lista todas las sesiones disponibles
func (m *SessionManager) ListSessions() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []map[string]interface{}

	// Sesiones activas en memoria
	for name, session := range m.sessions {
		session.mu.RLock()
		// Verificar si hay al menos un webhook habilitado
		webhookEnabled := false
		for _, wh := range session.Webhooks {
			if wh.Enabled {
				webhookEnabled = true
				break
			}
		}
		result = append(result, map[string]interface{}{
			"name":            name,
			"jid":             session.JID,
			"connected":       session.Connected,
			"active":          true,
			"webhook_enabled": webhookEnabled,
			"webhook_count":   len(session.Webhooks),
		})
		session.mu.RUnlock()
	}

	// Buscar sesiones en disco que no están cargadas
	entries, err := os.ReadDir(m.baseDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				name := entry.Name()
				if _, exists := m.sessions[name]; !exists {
					dbPath := filepath.Join(m.baseDir, name, "whatsmeow.db")
					if _, err := os.Stat(dbPath); err == nil {
						result = append(result, map[string]interface{}{
							"name":            name,
							"jid":             "",
							"connected":       false,
							"active":          false,
							"webhook_enabled": false,
						})
					}
				}
			}
		}
	}

	return result
}

// DeleteSession elimina una sesión
func (m *SessionManager) DeleteSession(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.sessions[name]; exists {
		session.Client.Disconnect()
		delete(m.sessions, name)
	}

	sessionDir := filepath.Join(m.baseDir, name)
	return os.RemoveAll(sessionDir)
}

// ============================================================================
// SESSION METHODS
// ============================================================================

// handleEvent maneja eventos de WhatsApp
func (s *Session) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		s.mu.Lock()
		s.Connected = true
		if s.Client.Store.ID != nil {
			s.JID = s.Client.Store.ID.String()
		}
		fmt.Printf("[%s] Connected: %s\n", s.Name, s.JID)
		s.mu.Unlock()

	case *events.Disconnected:
		s.mu.Lock()
		s.Connected = false
		fmt.Printf("[%s] Disconnected - will attempt auto-reconnect in 5 seconds\n", s.Name)
		s.mu.Unlock()
		// Auto-reconectar en una goroutine separada
		go s.autoReconnect()

	case *events.LoggedOut:
		s.mu.Lock()
		s.Connected = false
		s.JID = ""
		fmt.Printf("[%s] Logged out: %v (no auto-reconnect for logout)\n", s.Name, v.Reason)
		s.mu.Unlock()

	case *events.StreamError:
		s.mu.Lock()
		fmt.Printf("[%s] Stream error: %v - will attempt reconnect\n", s.Name, v.Code)
		s.Connected = false
		s.mu.Unlock()
		go s.autoReconnect()

	case *events.Message:
		// Manejar mensaje sin mantener lock (handleIncomingMessage maneja su propio locking)
		s.handleIncomingMessage(v)
	}
}

// autoReconnect intenta reconectar la sesión automáticamente
func (s *Session) autoReconnect() {
	// Esperar antes de intentar reconectar
	time.Sleep(5 * time.Second)

	s.mu.RLock()
	if s.Connected {
		s.mu.RUnlock()
		return // Ya está conectado
	}
	client := s.Client
	name := s.Name
	s.mu.RUnlock()

	if client == nil {
		fmt.Printf("[%s] Cannot auto-reconnect: client is nil\n", name)
		return
	}

	// Intentar reconectar con backoff exponencial
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		s.mu.RLock()
		if s.Connected {
			s.mu.RUnlock()
			return // Ya se conectó
		}
		s.mu.RUnlock()

		fmt.Printf("[%s] Auto-reconnect attempt %d/%d\n", name, i+1, maxRetries)

		err := client.Connect()
		if err == nil {
			fmt.Printf("[%s] Auto-reconnect successful\n", name)
			return
		}

		fmt.Printf("[%s] Auto-reconnect failed: %v\n", name, err)

		// Backoff exponencial: 5s, 10s, 20s, 40s, 80s
		waitTime := time.Duration(5*(1<<i)) * time.Second
		if waitTime > 2*time.Minute {
			waitTime = 2 * time.Minute
		}
		fmt.Printf("[%s] Waiting %v before next reconnect attempt\n", name, waitTime)
		time.Sleep(waitTime)
	}

	fmt.Printf("[%s] Auto-reconnect failed after %d attempts. Manual reconnect required.\n", name, maxRetries)
}

// handleIncomingMessage procesa mensajes entrantes
func (s *Session) handleIncomingMessage(msg *events.Message) {
	// Verificar si hay webhooks habilitados
	s.mu.RLock()
	if len(s.Webhooks) == 0 {
		s.mu.RUnlock()
		return
	}
	// Copiar lista de webhooks para no mantener lock
	webhooks := make([]*WebhookConfig, len(s.Webhooks))
	copy(webhooks, s.Webhooks)
	s.mu.RUnlock()

	// Verificar si al menos uno está habilitado
	hasEnabled := false
	for _, wh := range webhooks {
		if wh.Enabled && wh.URL != "" {
			hasEnabled = true
			break
		}
	}
	if !hasEnabled {
		return
	}

	// Extraer número de teléfono del remitente
	// El JID puede ser: número@s.whatsapp.net o lid:xxx@lid
	senderJID := msg.Info.Sender
	senderPhone := senderJID.User

	// Si es un servidor LID, el número real podría no estar disponible directamente
	// pero lo guardamos en raw para debugging
	isLID := senderJID.Server == "lid"

	// Extraer info del chat
	chatJID := msg.Info.Chat
	chatPhone := chatJID.User

	// Construir mensaje para webhook
	incoming := IncomingMessage{
		SessionName: s.Name,
		MessageID:   msg.Info.ID,
		From:        senderPhone,
		FromJID:     senderJID.String(),
		Phone:       senderPhone,
		Chat:        chatPhone,
		ChatJID:     chatJID.String(),
		IsGroup:     msg.Info.IsGroup,
		Timestamp:   msg.Info.Timestamp.Unix(),
		Raw:         make(map[string]string),
	}

	// Guardar JIDs originales en raw para debugging
	incoming.Raw["sender_jid"] = senderJID.String()
	incoming.Raw["chat_jid"] = chatJID.String()
	incoming.Raw["sender_server"] = senderJID.Server
	if isLID {
		incoming.Raw["is_lid"] = "true"
	}

	// Obtener nombre del remitente
	if msg.Info.PushName != "" {
		incoming.FromName = msg.Info.PushName
	}

	// Determinar tipo de mensaje y contenido
	if msg.Message.GetConversation() != "" {
		incoming.Type = "text"
		incoming.Text = msg.Message.GetConversation()
	} else if extMsg := msg.Message.GetExtendedTextMessage(); extMsg != nil {
		incoming.Type = "text"
		incoming.Text = extMsg.GetText()
		// Menciones
		if extMsg.ContextInfo != nil {
			for _, jid := range extMsg.ContextInfo.GetMentionedJID() {
				incoming.Mentions = append(incoming.Mentions, jid)
			}
			// Mensaje citado
			if extMsg.ContextInfo.QuotedMessage != nil {
				incoming.QuotedID = extMsg.ContextInfo.GetStanzaID()
				if extMsg.ContextInfo.QuotedMessage.GetConversation() != "" {
					incoming.QuotedText = extMsg.ContextInfo.QuotedMessage.GetConversation()
				}
			}
		}
	} else if imgMsg := msg.Message.GetImageMessage(); imgMsg != nil {
		incoming.Type = "media"
		incoming.MediaType = "image"
		incoming.Caption = imgMsg.GetCaption()
		incoming.MimeType = imgMsg.GetMimetype()
		incoming.FileSize = imgMsg.GetFileLength()
		incoming.HasMedia = true
	} else if vidMsg := msg.Message.GetVideoMessage(); vidMsg != nil {
		incoming.Type = "media"
		incoming.MediaType = "video"
		incoming.Caption = vidMsg.GetCaption()
		incoming.MimeType = vidMsg.GetMimetype()
		incoming.FileSize = vidMsg.GetFileLength()
		incoming.HasMedia = true
	} else if audioMsg := msg.Message.GetAudioMessage(); audioMsg != nil {
		incoming.Type = "media"
		incoming.MediaType = "audio"
		incoming.MimeType = audioMsg.GetMimetype()
		incoming.FileSize = audioMsg.GetFileLength()
		incoming.HasMedia = true
	} else if docMsg := msg.Message.GetDocumentMessage(); docMsg != nil {
		incoming.Type = "media"
		incoming.MediaType = "document"
		incoming.Caption = docMsg.GetCaption()
		incoming.FileName = docMsg.GetFileName()
		incoming.MimeType = docMsg.GetMimetype()
		incoming.FileSize = docMsg.GetFileLength()
		incoming.HasMedia = true
	} else if stickerMsg := msg.Message.GetStickerMessage(); stickerMsg != nil {
		incoming.Type = "sticker"
		incoming.MimeType = stickerMsg.GetMimetype()
		incoming.FileSize = stickerMsg.GetFileLength()
		incoming.HasMedia = true
	} else if locMsg := msg.Message.GetLocationMessage(); locMsg != nil {
		incoming.Type = "location"
		incoming.Raw["latitude"] = fmt.Sprintf("%f", locMsg.GetDegreesLatitude())
		incoming.Raw["longitude"] = fmt.Sprintf("%f", locMsg.GetDegreesLongitude())
		incoming.Raw["name"] = locMsg.GetName()
		incoming.Raw["address"] = locMsg.GetAddress()
	} else if contactMsg := msg.Message.GetContactMessage(); contactMsg != nil {
		incoming.Type = "contact"
		incoming.Raw["display_name"] = contactMsg.GetDisplayName()
		incoming.Raw["vcard"] = contactMsg.GetVcard()
	} else {
		incoming.Type = "unknown"
	}

	// Si hay media, guardar en cache para permitir descarga posterior
	if incoming.HasMedia {
		cacheKey := fmt.Sprintf("%s_%s", s.Name, incoming.MessageID)
		incoming.MediaCacheKey = cacheKey
		mediaCache.Add(cacheKey, s.Name, msg)
	}

	// Guardar mensaje en el historial (messageStore)
	storedMsg := StoredMessage{
		MessageID:   incoming.MessageID,
		From:        incoming.From,
		FromJID:     incoming.FromJID,
		FromName:    incoming.FromName,
		Chat:        incoming.Chat,
		ChatJID:     incoming.ChatJID,
		IsGroup:     incoming.IsGroup,
		Timestamp:   incoming.Timestamp,
		Type:        incoming.Type,
		Text:        incoming.Text,
		Caption:     incoming.Caption,
		MediaType:   incoming.MediaType,
		HasMedia:    incoming.HasMedia,
		QuotedID:    incoming.QuotedID,
		QuotedText:  incoming.QuotedText,
		SessionName: s.Name,
	}
	messageStore.Add(incoming.ChatJID, storedMsg)

	// Enviar a TODOS los webhooks habilitados
	s.mu.RLock()
	webhooksCopy := make([]*WebhookConfig, len(s.Webhooks))
	copy(webhooksCopy, s.Webhooks)
	s.mu.RUnlock()

	// Contar webhooks activos para logging
	activeCount := 0
	for _, wh := range webhooksCopy {
		if wh.Enabled && wh.URL != "" {
			activeCount++
		}
	}
	if activeCount > 0 {
		fmt.Printf("[%s] Dispatching message to %d webhook(s)\n", s.Name, activeCount)
	}

	for _, webhook := range webhooksCopy {
		if webhook.Enabled && webhook.URL != "" {
			// Usar batcher si hay delay configurado, sino enviar directo
			if webhook.BatchDelay > 0 {
				messageBatcher.AddWithWebhook(s, incoming, webhook)
			} else {
				go s.sendToWebhook(webhook, incoming)
			}
		}
	}
}

// sendToWebhook envía un mensaje a un webhook específico
func (s *Session) sendToWebhook(webhook *WebhookConfig, msg IncomingMessage) {
	jsonData, err := json.Marshal(msg)
	if err != nil {
		fmt.Printf("[%s] Webhook marshal error: %v\n", s.Name, err)
		return
	}
	s.doWebhookRequestTo(webhook, jsonData, msg.Type)
}

// sendBatchToWebhook envía mensajes agrupados a un webhook específico
func (s *Session) sendBatchToWebhook(webhook *WebhookConfig, messages []IncomingMessage) {
	if len(messages) == 0 {
		return
	}

	// Si solo hay un mensaje, enviar formato normal
	if len(messages) == 1 {
		s.sendToWebhook(webhook, messages[0])
		return
	}

	// Combinar textos
	var combinedText string
	for i, msg := range messages {
		if msg.Text != "" {
			if i > 0 && combinedText != "" {
				combinedText += "\n"
			}
			combinedText += msg.Text
		}
	}

	firstMsg := messages[0]
	payload := BatchedWebhookPayload{
		SessionName:  s.Name,
		From:         firstMsg.From,
		FromJID:      firstMsg.FromJID,
		FromName:     firstMsg.FromName,
		Chat:         firstMsg.Chat,
		ChatJID:      firstMsg.ChatJID,
		IsGroup:      firstMsg.IsGroup,
		IsBatched:    true,
		MessageCount: len(messages),
		Messages:     messages,
		Text:         combinedText,
		Timestamp:    firstMsg.Timestamp,
		Type:         "batched",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("[%s] Webhook marshal error: %v\n", s.Name, err)
		return
	}

	fmt.Printf("[%s] Sending batched webhook: %d messages to %s\n", s.Name, len(messages), webhook.URL)
	s.doWebhookRequestTo(webhook, jsonData, "batched")
}

// doWebhookRequestTo envía una petición a un webhook específico
func (s *Session) doWebhookRequestTo(webhook *WebhookConfig, jsonData []byte, msgType string) {
	req, err := http.NewRequest("POST", webhook.URL, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("[%s] Webhook request error: %v\n", s.Name, err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range webhook.Headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[%s] Webhook send error: %v\n", s.Name, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		fmt.Printf("[%s] Webhook response error: %d -> %s\n", s.Name, resp.StatusCode, webhook.URL)
	} else {
		fmt.Printf("[%s] Webhook sent: %s -> %s\n", s.Name, msgType, webhook.URL)
	}
}

// loadWebhookConfig carga la configuración de webhooks desde archivo
func (s *Session) loadWebhookConfig() {
	configPath := filepath.Join(manager.baseDir, s.Name, "webhook.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	// Primero intentar cargar como lista (nuevo formato)
	var webhooksConfig WebhooksConfig
	if err := json.Unmarshal(data, &webhooksConfig); err == nil && len(webhooksConfig.Webhooks) > 0 {
		s.Webhooks = webhooksConfig.Webhooks
		return
	}

	// Fallback: intentar cargar como webhook individual (formato antiguo)
	var config WebhookConfig
	if err := json.Unmarshal(data, &config); err == nil && config.URL != "" {
		// Migrar al nuevo formato
		if config.ID == "" {
			config.ID = config.URL
		}
		s.Webhooks = []*WebhookConfig{&config}
	}
}

// saveWebhookConfig guarda la configuración de webhooks
func (s *Session) saveWebhookConfig() error {
	configPath := filepath.Join(manager.baseDir, s.Name, "webhook.json")

	if len(s.Webhooks) == 0 {
		// Si no hay webhooks, eliminar archivo
		os.Remove(configPath)
		return nil
	}

	webhooksConfig := WebhooksConfig{Webhooks: s.Webhooks}
	data, err := json.MarshalIndent(webhooksConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// ============================================================================
// QR CODE HELPERS
// ============================================================================

func generateQRAssets(code string) (ascii, dataURL string, err error) {
	var buf bytes.Buffer
	qrterminal.GenerateHalfBlock(code, qrterminal.L, &buf)
	ascii = buf.String()

	png, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		return ascii, "", err
	}
	dataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)

	return ascii, dataURL, nil
}

// ============================================================================
// MEDIA HELPERS
// ============================================================================

// getMediaBytes obtiene los bytes del media desde URL, base64, o archivo local
// Soporta:
// - URLs HTTP/HTTPS: https://example.com/image.jpg
// - Archivos locales: /files/imagen.jpg, file:///files/documento.pdf
// - Base64: datos codificados en base64
func getMediaBytes(mediaURL, mediaBase64 string) ([]byte, error) {
	if mediaBase64 != "" {
		return base64.StdEncoding.DecodeString(mediaBase64)
	}

	if mediaURL != "" {
		// Detectar si es archivo local
		// Formatos soportados:
		// - /files/archivo.jpg (ruta absoluta dentro del contenedor)
		// - file:///files/archivo.jpg (URI de archivo)
		// - local:/files/archivo.jpg (esquema personalizado)
		localPath := ""

		if strings.HasPrefix(mediaURL, "file://") {
			localPath = strings.TrimPrefix(mediaURL, "file://")
		} else if strings.HasPrefix(mediaURL, "local:") {
			localPath = strings.TrimPrefix(mediaURL, "local:")
		} else if strings.HasPrefix(mediaURL, "/files/") {
			localPath = mediaURL
		}

		// Si es archivo local, leerlo directamente
		if localPath != "" {
			data, err := os.ReadFile(localPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read local file %s: %v", localPath, err)
			}
			return data, nil
		}

		// Si es URL HTTP/HTTPS, descargarlo
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Get(mediaURL)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("failed to download media: status %d", resp.StatusCode)
		}

		return io.ReadAll(resp.Body)
	}

	return nil, fmt.Errorf("no media source provided")
}

// convertToOpusForPTT convierte audio a formato OGG Opus para mensajes de voz PTT
// WhatsApp requiere OGG con codec Opus para que PTT funcione correctamente
// Retorna los bytes convertidos y el nuevo mimetype
func convertToOpusForPTT(audioData []byte, originalMimeType string) ([]byte, string, error) {
	// Verificar si ffmpeg está disponible
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		// ffmpeg no está disponible, retornar datos originales
		fmt.Println("[PTT] ffmpeg not available, using original audio")
		return audioData, originalMimeType, nil
	}

	// Si ya es audio/ogg con opus, no necesita conversión
	// Detectar por magic bytes o por mimetype
	if strings.Contains(originalMimeType, "opus") {
		return audioData, "audio/ogg; codecs=opus", nil
	}

	// Crear archivos temporales
	tmpInput, err := os.CreateTemp("", "audio_input_*")
	if err != nil {
		return audioData, originalMimeType, fmt.Errorf("failed to create temp input: %v", err)
	}
	defer os.Remove(tmpInput.Name())
	defer tmpInput.Close()

	tmpOutput := tmpInput.Name() + ".ogg"
	defer os.Remove(tmpOutput)

	// Escribir audio de entrada
	if _, err := tmpInput.Write(audioData); err != nil {
		return audioData, originalMimeType, fmt.Errorf("failed to write temp input: %v", err)
	}
	tmpInput.Close()

	// Convertir a OGG Opus usando ffmpeg
	// -y: sobrescribir sin preguntar
	// -i: archivo de entrada
	// -c:a libopus: usar codec opus
	// -b:a 64k: bitrate de 64kbps (óptimo para voz)
	// -ar 48000: sample rate de 48kHz (requerido por opus)
	// -ac 1: mono (mejor para voz)
	// -application voip: optimizado para voz
	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", tmpInput.Name(),
		"-c:a", "libopus",
		"-b:a", "64k",
		"-ar", "48000",
		"-ac", "1",
		"-application", "voip",
		tmpOutput,
	)

	// Capturar salida de error para debugging
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("[PTT] ffmpeg conversion failed: %v, stderr: %s\n", err, stderr.String())
		// Retornar datos originales si la conversión falla
		return audioData, originalMimeType, nil
	}

	// Leer archivo convertido
	convertedData, err := os.ReadFile(tmpOutput)
	if err != nil {
		return audioData, originalMimeType, fmt.Errorf("failed to read converted file: %v", err)
	}

	fmt.Printf("[PTT] Audio converted to OGG Opus: %d bytes -> %d bytes\n", len(audioData), len(convertedData))
	return convertedData, "audio/ogg; codecs=opus", nil
}

// parsePhoneOrLID convierte un string (teléfono o LID) a JID
// Soporta: números de teléfono normales (5215646404427), LIDs (180238604570868),
// o JIDs completos (5215646404427@s.whatsapp.net, 180238604570868@lid)
// También limpia el "device part" (ej: 5215646404427:32 -> 5215646404427)
func parsePhoneOrLID(input string) (types.JID, error) {
	input = strings.TrimSpace(input)

	// Si ya contiene @, es un JID completo
	if strings.Contains(input, "@") {
		// Parsear el JID
		jid, err := types.ParseJID(input)
		if err != nil {
			return jid, err
		}
		// Limpiar device part si existe (el :XX después del número)
		// WhatsApp no acepta JIDs con device part como destinatarios
		jid.Device = 0
		return jid, nil
	}

	// Limpiar caracteres no numéricos y device part
	cleaned := strings.ReplaceAll(input, "+", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")

	// Remover device part si existe (ej: 5215646404427:32 -> 5215646404427)
	if idx := strings.Index(cleaned, ":"); idx > 0 {
		cleaned = cleaned[:idx]
	}

	// Detectar si es un LID o un número de teléfono
	// Los LIDs típicamente son números muy largos (>15 dígitos)
	// Los números de teléfono normalmente tienen 10-15 dígitos
	if len(cleaned) > 15 {
		// Es un LID, usar servidor "lid"
		return types.NewJID(cleaned, "lid"), nil
	}

	// Es un número de teléfono normal
	return types.NewJID(cleaned, types.DefaultUserServer), nil
}

// detectMimeType detecta el tipo MIME basado en los primeros bytes
func detectMimeType(data []byte, fallback string) string {
	if fallback != "" {
		return fallback
	}

	// Detectar por magic bytes
	if len(data) >= 4 {
		switch {
		case data[0] == 0xFF && data[1] == 0xD8:
			return "image/jpeg"
		case data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47:
			return "image/png"
		case data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46:
			return "image/gif"
		case data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46:
			return "image/webp"
		case data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00:
			return "video/mp4"
		case data[0] == 0x25 && data[1] == 0x50 && data[2] == 0x44 && data[3] == 0x46:
			return "application/pdf"
		}
	}

	return "application/octet-stream"
}

// ============================================================================
// HTTP HANDLERS
// ============================================================================

func jsonResponse(w http.ResponseWriter, status int, success bool, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]interface{}{
		"success": success,
		"data":    data,
	}
	json.NewEncoder(w).Encode(response)
}

// Health check
func handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, true, "ok")
}

// Listar sesiones
func handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := manager.ListSessions()
	jsonResponse(w, http.StatusOK, true, sessions)
}

// Conectar sesión (obtener QR si es necesario)
func handleSessionConnect(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	session.mu.RLock()
	if session.Connected {
		session.mu.RUnlock()
		jsonResponse(w, http.StatusOK, true, map[string]interface{}{
			"status":  "connected",
			"jid":     session.JID,
			"message": "Already connected",
		})
		return
	}
	session.mu.RUnlock()

	// Si ya tiene credenciales, solo conectar
	if session.Client.Store.ID != nil {
		if err := session.Client.Connect(); err != nil {
			jsonResponse(w, http.StatusInternalServerError, false, err.Error())
			return
		}

		// Esperar conexión
		time.Sleep(2 * time.Second)

		session.mu.RLock()
		connected := session.Connected
		jid := session.JID
		session.mu.RUnlock()

		jsonResponse(w, http.StatusOK, true, map[string]interface{}{
			"status":    "reconnected",
			"jid":       jid,
			"connected": connected,
		})
		return
	}

	// Necesita escanear QR
	qrChan, err := session.Client.GetQRChannel(context.Background())
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	go func() {
		if err := session.Client.Connect(); err != nil {
			fmt.Printf("[%s] Connect error: %v\n", sessionName, err)
		}
	}()

	// Esperar primer QR
	timeout := time.After(30 * time.Second)
	for {
		select {
		case evt, ok := <-qrChan:
			if !ok {
				// Canal cerrado sin evento reconocido
				fmt.Printf("[%s] QR channel closed unexpectedly\n", sessionName)
				jsonResponse(w, http.StatusInternalServerError, false, "QR channel closed unexpectedly")
				return
			}

			switch evt.Event {
			case "code":
				ascii, dataURL, _ := generateQRAssets(evt.Code)
				session.mu.Lock()
				session.QRCode = evt.Code
				session.QRASCII = ascii
				session.QRDataURL = dataURL
				session.mu.Unlock()

				fmt.Printf("[%s] QR code generated successfully\n", sessionName)
				jsonResponse(w, http.StatusOK, true, map[string]interface{}{
					"status":   "qr_ready",
					"qr_code":  evt.Code,
					"qr_ascii": ascii,
					"qr_image": dataURL,
					"message":  "Scan QR code with WhatsApp",
				})
				return

			case "success":
				session.mu.Lock()
				session.Connected = true
				if session.Client.Store.ID != nil {
					session.JID = session.Client.Store.ID.String()
				}
				session.mu.Unlock()

				fmt.Printf("[%s] QR pairing successful\n", sessionName)
				jsonResponse(w, http.StatusOK, true, map[string]interface{}{
					"status":  "connected",
					"jid":     session.JID,
					"message": "Successfully connected",
				})
				return

			case "timeout":
				fmt.Printf("[%s] QR pairing timed out (server disconnected before pairing)\n", sessionName)
				jsonResponse(w, http.StatusRequestTimeout, false, "QR pairing timed out - WhatsApp server disconnected before pairing completed. Try again.")
				return

			case "err-client-outdated":
				fmt.Printf("[%s] QR pairing failed: client outdated - need to update whatsmeow\n", sessionName)
				jsonResponse(w, http.StatusInternalServerError, false, "WhatsApp client is outdated. The whatsmeow library needs to be updated.")
				return

			case "err-unexpected-state":
				fmt.Printf("[%s] QR pairing failed: unexpected state (session may already exist)\n", sessionName)
				jsonResponse(w, http.StatusConflict, false, "Unexpected state - session may already be paired. Try checking status or deleting the session first.")
				return

			case "err-scanned-without-multidevice":
				fmt.Printf("[%s] QR scanned without multi-device enabled\n", sessionName)
				jsonResponse(w, http.StatusBadRequest, false, "QR scanned but multi-device is not enabled on the phone. Enable it in WhatsApp settings.")
				return

			case "error":
				errMsg := "Unknown pairing error"
				if evt.Error != nil {
					errMsg = evt.Error.Error()
				}
				fmt.Printf("[%s] QR pairing error: %s\n", sessionName, errMsg)
				jsonResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Pairing error: %s", errMsg))
				return

			default:
				fmt.Printf("[%s] QR channel unknown event: %s\n", sessionName, evt.Event)
				// Continuar esperando un evento reconocido
			}

		case <-timeout:
			fmt.Printf("[%s] QR timeout after 30 seconds (no QR code generated)\n", sessionName)
			jsonResponse(w, http.StatusRequestTimeout, false, "QR timeout - no QR code was generated within 30 seconds. Check server logs and whatsmeow version.")
			return
		}
	}
}

// Estado de sesión
func handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	// Verificar si hay al menos un webhook habilitado
	webhookEnabled := false
	for _, wh := range session.Webhooks {
		if wh.Enabled {
			webhookEnabled = true
			break
		}
	}

	jsonResponse(w, http.StatusOK, true, map[string]interface{}{
		"name":            session.Name,
		"jid":             session.JID,
		"connected":       session.Connected,
		"webhook_enabled": webhookEnabled,
		"webhook_count":   len(session.Webhooks),
	})
}

// Enviar mensaje de texto
func handleSessionSend(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	var req struct {
		Phone    string `json:"phone"`
		GroupJID string `json:"group_jid"`
		Message  string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, false, "invalid JSON")
		return
	}

	if req.Message == "" {
		jsonResponse(w, http.StatusBadRequest, false, "message required")
		return
	}

	// Determinar destinatario
	var targetJID types.JID
	if req.GroupJID != "" {
		// Es un grupo
		parsed, err := types.ParseJID(req.GroupJID)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, false, "invalid group JID")
			return
		}
		targetJID = parsed
	} else if req.Phone != "" {
		// Es un chat privado - puede ser teléfono o LID
		parsed, err := parsePhoneOrLID(req.Phone)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, false, "invalid phone/LID")
			return
		}
		targetJID = parsed
	} else {
		jsonResponse(w, http.StatusBadRequest, false, "phone or group_jid required")
		return
	}

	// Auto-conectar si no está conectado
	session.mu.RLock()
	connected := session.Connected
	session.mu.RUnlock()

	if !connected {
		if err := session.Client.Connect(); err != nil {
			jsonResponse(w, http.StatusServiceUnavailable, false, "not connected")
			return
		}
		time.Sleep(2 * time.Second)
	}

	// Enviar mensaje
	msg := &waProto.Message{
		Conversation: proto.String(req.Message),
	}

	resp, err := session.Client.SendMessage(context.Background(), targetJID, msg)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, true, map[string]interface{}{
		"message_id": resp.ID,
		"timestamp":  resp.Timestamp.Unix(),
	})
}

// Enviar multimedia
func handleSessionSendMedia(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	var req SendMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, false, "invalid JSON")
		return
	}

	// Determinar destinatario
	var targetJID types.JID
	if req.GroupJID != "" {
		parsed, err := types.ParseJID(req.GroupJID)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, false, "invalid group JID")
			return
		}
		targetJID = parsed
	} else if req.Phone != "" {
		// Es un chat privado - puede ser teléfono o LID
		parsed, err := parsePhoneOrLID(req.Phone)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, false, "invalid phone/LID")
			return
		}
		targetJID = parsed
	} else {
		jsonResponse(w, http.StatusBadRequest, false, "phone or group_jid required")
		return
	}

	// Obtener bytes del media
	mediaData, err := getMediaBytes(req.MediaURL, req.MediaBase64)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, false, fmt.Sprintf("media error: %v", err))
		return
	}

	// Auto-conectar si no está conectado
	session.mu.RLock()
	connected := session.Connected
	session.mu.RUnlock()

	if !connected {
		if err := session.Client.Connect(); err != nil {
			jsonResponse(w, http.StatusServiceUnavailable, false, "not connected")
			return
		}
		time.Sleep(2 * time.Second)
	}

	// Detectar MIME type
	mimeType := detectMimeType(mediaData, req.MimeType)

	// Si es audio con PTT, convertir a OGG Opus para mejor compatibilidad
	if req.MediaType == "audio" && req.Ptt {
		convertedData, convertedMime, convErr := convertToOpusForPTT(mediaData, mimeType)
		if convErr != nil {
			fmt.Printf("[PTT] Conversion warning: %v, using original\n", convErr)
		} else {
			mediaData = convertedData
			mimeType = convertedMime
		}
	}

	// Subir media a WhatsApp
	var mediaType whatsmeow.MediaType
	switch req.MediaType {
	case "image":
		mediaType = whatsmeow.MediaImage
	case "video":
		mediaType = whatsmeow.MediaVideo
	case "audio":
		mediaType = whatsmeow.MediaAudio
	case "document":
		mediaType = whatsmeow.MediaDocument
	case "sticker":
		mediaType = whatsmeow.MediaImage // Los stickers se suben como imagen
	default:
		jsonResponse(w, http.StatusBadRequest, false, "invalid media_type (image, video, audio, document, sticker)")
		return
	}

	uploaded, err := session.Client.Upload(context.Background(), mediaData, mediaType)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("upload error: %v", err))
		return
	}

	// Construir mensaje según tipo
	var msg *waProto.Message
	switch req.MediaType {
	case "image":
		msg = &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Mimetype:      proto.String(mimeType),
				Caption:       proto.String(req.Caption),
			},
		}
	case "video":
		msg = &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Mimetype:      proto.String(mimeType),
				Caption:       proto.String(req.Caption),
			},
		}
	case "audio":
		msg = &waProto.Message{
			AudioMessage: &waProto.AudioMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Mimetype:      proto.String(mimeType),
				PTT:           proto.Bool(req.Ptt), // true = mensaje de voz
			},
		}
	case "document":
		fileName := req.FileName
		if fileName == "" {
			fileName = "document"
		}
		msg = &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Mimetype:      proto.String(mimeType),
				FileName:      proto.String(fileName),
				Caption:       proto.String(req.Caption),
			},
		}
	case "sticker":
		// Los stickers deben ser WebP de 512x512, sin caption
		msg = &waProto.Message{
			StickerMessage: &waProto.StickerMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Mimetype:      proto.String("image/webp"),
			},
		}
	}

	resp, err := session.Client.SendMessage(context.Background(), targetJID, msg)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, true, map[string]interface{}{
		"message_id": resp.ID,
		"timestamp":  resp.Timestamp.Unix(),
	})
}

// ============================================================================
// WEBHOOK HANDLERS
// ============================================================================

// Configurar webhook (soporta múltiples webhooks por sesión)
func handleSessionWebhook(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	switch r.Method {
	case "GET":
		session.mu.RLock()
		defer session.mu.RUnlock()

		if len(session.Webhooks) > 0 {
			jsonResponse(w, http.StatusOK, true, map[string]interface{}{
				"webhooks": session.Webhooks,
				"count":    len(session.Webhooks),
			})
		} else {
			jsonResponse(w, http.StatusOK, true, map[string]interface{}{
				"webhooks": []WebhookConfig{},
				"count":    0,
			})
		}

	case "POST", "PUT":
		var config WebhookConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			jsonResponse(w, http.StatusBadRequest, false, "invalid JSON")
			return
		}

		if config.URL == "" {
			jsonResponse(w, http.StatusBadRequest, false, "url is required")
			return
		}

		// Usar URL como ID si no se proporciona
		if config.ID == "" {
			config.ID = config.URL
		}

		session.mu.Lock()

		// Buscar si ya existe un webhook con este ID/URL
		found := false
		for i, wh := range session.Webhooks {
			if wh.ID == config.ID || wh.URL == config.URL {
				// Actualizar existente
				session.Webhooks[i] = &config
				found = true
				break
			}
		}

		if !found {
			// Agregar nuevo webhook
			session.Webhooks = append(session.Webhooks, &config)
		}

		if err := session.saveWebhookConfig(); err != nil {
			session.mu.Unlock()
			jsonResponse(w, http.StatusInternalServerError, false, err.Error())
			return
		}
		session.mu.Unlock()

		action := "added"
		if found {
			action = "updated"
		}
		jsonResponse(w, http.StatusOK, true, map[string]interface{}{
			"message":       fmt.Sprintf("webhook %s", action),
			"webhook_count": len(session.Webhooks),
		})

	case "DELETE":
		// Obtener ID/URL del webhook a eliminar desde query params
		webhookID := r.URL.Query().Get("id")
		webhookURL := r.URL.Query().Get("url")
		deleteAll := r.URL.Query().Get("all") == "true"

		session.mu.Lock()

		if deleteAll {
			// Eliminar todos los webhooks
			session.Webhooks = nil
			configPath := filepath.Join(manager.baseDir, session.Name, "webhook.json")
			os.Remove(configPath)
			session.mu.Unlock()
			jsonResponse(w, http.StatusOK, true, "all webhooks removed")
			return
		}

		if webhookID == "" && webhookURL == "" {
			session.mu.Unlock()
			jsonResponse(w, http.StatusBadRequest, false, "provide 'id', 'url', or 'all=true' query parameter")
			return
		}

		// Buscar y eliminar el webhook específico
		found := false
		for i, wh := range session.Webhooks {
			if wh.ID == webhookID || wh.URL == webhookURL {
				session.Webhooks = append(session.Webhooks[:i], session.Webhooks[i+1:]...)
				found = true
				break
			}
		}

		if !found {
			session.mu.Unlock()
			jsonResponse(w, http.StatusNotFound, false, "webhook not found")
			return
		}

		if err := session.saveWebhookConfig(); err != nil {
			session.mu.Unlock()
			jsonResponse(w, http.StatusInternalServerError, false, err.Error())
			return
		}
		session.mu.Unlock()

		jsonResponse(w, http.StatusOK, true, map[string]interface{}{
			"message":       "webhook removed",
			"webhook_count": len(session.Webhooks),
		})

	default:
		jsonResponse(w, http.StatusMethodNotAllowed, false, "method not allowed")
	}
}

// ============================================================================
// GROUP HANDLERS
// ============================================================================

// Listar grupos
func handleSessionGroups(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	session.mu.RLock()
	connected := session.Connected
	session.mu.RUnlock()

	if !connected {
		jsonResponse(w, http.StatusServiceUnavailable, false, "not connected")
		return
	}

	groups, err := session.Client.GetJoinedGroups(context.Background())
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	var result []map[string]interface{}
	for _, g := range groups {
		result = append(result, map[string]interface{}{
			"jid":               g.JID.String(),
			"name":              g.Name,
			"topic":             g.Topic,
			"participant_count": len(g.Participants),
			"created_at":        g.GroupCreated.Unix(),
		})
	}

	jsonResponse(w, http.StatusOK, true, result)
}

// Obtener info de grupo específico
func handleSessionGroupInfo(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	groupJID := r.URL.Query().Get("jid")

	if sessionName == "" || groupJID == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name and group jid required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, false, "invalid group JID")
		return
	}

	info, err := session.Client.GetGroupInfo(context.Background(), jid)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	var participants []map[string]interface{}
	for _, p := range info.Participants {
		participants = append(participants, map[string]interface{}{
			"jid":      p.JID.String(),
			"is_admin": p.IsAdmin,
			"is_super": p.IsSuperAdmin,
		})
	}

	jsonResponse(w, http.StatusOK, true, map[string]interface{}{
		"jid":          info.JID.String(),
		"name":         info.Name,
		"topic":        info.Topic,
		"created_at":   info.GroupCreated.Unix(),
		"participants": participants,
	})
}

// Crear grupo
func handleSessionCreateGroup(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	var req GroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, false, "invalid JSON")
		return
	}

	if req.Name == "" {
		jsonResponse(w, http.StatusBadRequest, false, "group name required")
		return
	}

	// Convertir participantes a JIDs
	var participants []types.JID
	for _, phone := range req.Participants {
		phone = strings.ReplaceAll(phone, "+", "")
		phone = strings.ReplaceAll(phone, " ", "")
		phone = strings.ReplaceAll(phone, "-", "")
		participants = append(participants, types.NewJID(phone, types.DefaultUserServer))
	}

	createReq := whatsmeow.ReqCreateGroup{
		Name:         req.Name,
		Participants: participants,
	}

	info, err := session.Client.CreateGroup(context.Background(), createReq)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, true, map[string]interface{}{
		"jid":  info.JID.String(),
		"name": info.Name,
	})
}

// Salir de grupo
func handleSessionLeaveGroup(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	groupJID := r.URL.Query().Get("jid")

	if sessionName == "" || groupJID == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name and group jid required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, false, "invalid group JID")
		return
	}

	if err := session.Client.LeaveGroup(context.Background(), jid); err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, true, "left group")
}

// Obtener link de invitación del grupo
func handleSessionGroupInviteLink(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	groupJID := r.URL.Query().Get("jid")

	if sessionName == "" || groupJID == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name and group jid required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, false, "invalid group JID")
		return
	}

	link, err := session.Client.GetGroupInviteLink(context.Background(), jid, false)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, true, map[string]interface{}{
		"invite_link": link,
	})
}

// ============================================================================
// SESSION MANAGEMENT HANDLERS
// ============================================================================

// Desconectar sesión
func handleSessionDisconnect(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	session.Client.Disconnect()

	session.mu.Lock()
	session.Connected = false
	session.mu.Unlock()

	jsonResponse(w, http.StatusOK, true, "disconnected")
}

// Logout de sesión
func handleSessionLogout(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session, err := manager.GetOrCreateSession(sessionName)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	if err := session.Client.Logout(context.Background()); err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	session.mu.Lock()
	session.Connected = false
	session.JID = ""
	session.mu.Unlock()

	jsonResponse(w, http.StatusOK, true, "logged out")
}

// DownloadMediaRequest request para descargar media
type DownloadMediaRequest struct {
	CacheKey string `json:"cache_key"`
}

// DownloadMediaResponse respuesta con el media descargado
type DownloadMediaResponse struct {
	Success     bool   `json:"success"`
	MediaBase64 string `json:"media_base64"`
	MimeType    string `json:"mime_type"`
	FileName    string `json:"file_name"`
	FileSize    int    `json:"file_size"`
	MediaType   string `json:"media_type"`
}

// handleDownloadMedia descarga el contenido de un mensaje con media
func handleDownloadMedia(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	session := manager.GetSession(sessionName)
	if session == nil {
		jsonResponse(w, http.StatusNotFound, false, "session not found")
		return
	}

	var req DownloadMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, false, "invalid JSON")
		return
	}

	if req.CacheKey == "" {
		jsonResponse(w, http.StatusBadRequest, false, "cache_key required")
		return
	}

	// Buscar en cache
	cached, exists := mediaCache.Get(req.CacheKey)
	if !exists {
		jsonResponse(w, http.StatusNotFound, false, "media not found in cache (may have expired after 10 minutes)")
		return
	}

	// Verificar que la sesión coincida
	if cached.SessionName != sessionName {
		jsonResponse(w, http.StatusForbidden, false, "cache key does not belong to this session")
		return
	}

	msg := cached.Message.Message

	// Determinar el tipo de media y obtener la información necesaria para descargar
	var downloadable whatsmeow.DownloadableMessage
	var mimeType, fileName, mediaType string

	if img := msg.GetImageMessage(); img != nil {
		downloadable = img
		mimeType = img.GetMimetype()
		fileName = "image"
		if ext := getExtensionFromMime(mimeType); ext != "" {
			fileName += ext
		}
		mediaType = "image"
	} else if video := msg.GetVideoMessage(); video != nil {
		downloadable = video
		mimeType = video.GetMimetype()
		fileName = "video"
		if ext := getExtensionFromMime(mimeType); ext != "" {
			fileName += ext
		}
		mediaType = "video"
	} else if audio := msg.GetAudioMessage(); audio != nil {
		downloadable = audio
		mimeType = audio.GetMimetype()
		fileName = "audio"
		if ext := getExtensionFromMime(mimeType); ext != "" {
			fileName += ext
		}
		mediaType = "audio"
	} else if doc := msg.GetDocumentMessage(); doc != nil {
		downloadable = doc
		mimeType = doc.GetMimetype()
		fileName = doc.GetFileName()
		if fileName == "" {
			fileName = "document"
			if ext := getExtensionFromMime(mimeType); ext != "" {
				fileName += ext
			}
		}
		mediaType = "document"
	} else if sticker := msg.GetStickerMessage(); sticker != nil {
		downloadable = sticker
		mimeType = sticker.GetMimetype()
		fileName = "sticker.webp"
		mediaType = "sticker"
	} else {
		jsonResponse(w, http.StatusBadRequest, false, "message does not contain downloadable media")
		return
	}

	// Descargar el media
	data, err := session.Client.Download(context.Background(), downloadable)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("download error: %v", err))
		return
	}

	// Convertir a base64
	base64Data := base64.StdEncoding.EncodeToString(data)

	// Responder con el media
	response := DownloadMediaResponse{
		Success:     true,
		MediaBase64: base64Data,
		MimeType:    mimeType,
		FileName:    fileName,
		FileSize:    len(data),
		MediaType:   mediaType,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getExtensionFromMime devuelve la extensión de archivo para un MIME type
func getExtensionFromMime(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/3gpp":
		return ".3gp"
	case "audio/ogg", "audio/ogg; codecs=opus":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4", "audio/aac":
		return ".m4a"
	case "application/pdf":
		return ".pdf"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.ms-excel":
		return ".xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	default:
		return ""
	}
}

// Eliminar sesión
func handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	if err := manager.DeleteSession(sessionName); err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, true, "session deleted")
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func getSessionName(r *http.Request) string {
	// Intentar obtener de path: /session/{name}/...
	path := r.URL.Path
	parts := strings.Split(path, "/")

	for i, part := range parts {
		if part == "session" && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	// Fallback a query param
	return r.URL.Query().Get("session")
}

// ============================================================================
// HISTORY HANDLER
// ============================================================================

// handleSessionHistory obtiene el historial de mensajes de un chat
func handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	sessionName := getSessionName(r)
	if sessionName == "" {
		jsonResponse(w, http.StatusBadRequest, false, "session name required")
		return
	}

	// Verificar que la sesión exista
	session := manager.GetSession(sessionName)
	if session == nil {
		jsonResponse(w, http.StatusNotFound, false, "session not found")
		return
	}

	// Obtener parámetros
	chatJID := r.URL.Query().Get("chat")
	limitStr := r.URL.Query().Get("limit")

	limit := 50 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
			if limit > 1000 {
				limit = 1000
			}
		}
	}

	// Si no se especifica chat, devolver lista de chats disponibles
	if chatJID == "" {
		chats := messageStore.GetAllChats()
		// Filtrar solo chats de esta sesión
		sessionChats := []string{}
		for _, chat := range chats {
			msgs := messageStore.GetHistory(chat, 1)
			if len(msgs) > 0 && msgs[0].SessionName == sessionName {
				sessionChats = append(sessionChats, chat)
			}
		}
		jsonResponse(w, http.StatusOK, true, map[string]interface{}{
			"session": sessionName,
			"chats":   sessionChats,
			"count":   len(sessionChats),
		})
		return
	}

	// Obtener historial del chat específico
	messages := messageStore.GetHistory(chatJID, limit)

	// Filtrar solo mensajes de esta sesión
	filteredMessages := []StoredMessage{}
	for _, msg := range messages {
		if msg.SessionName == sessionName {
			filteredMessages = append(filteredMessages, msg)
		}
	}

	jsonResponse(w, http.StatusOK, true, map[string]interface{}{
		"session":  sessionName,
		"chat":     chatJID,
		"messages": filteredMessages,
		"count":    len(filteredMessages),
	})
}

// ============================================================================
// ROUTER
// ============================================================================

func setupRoutes() http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", handleHealth)

	// Sessions
	mux.HandleFunc("/sessions", handleListSessions)

	// Session operations
	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Route based on path suffix
		switch {
		case strings.HasSuffix(path, "/connect"):
			if r.Method == "POST" {
				handleSessionConnect(w, r)
			} else {
				jsonResponse(w, http.StatusMethodNotAllowed, false, "POST only")
			}

		case strings.HasSuffix(path, "/status"):
			handleSessionStatus(w, r)

		case strings.HasSuffix(path, "/send"):
			if r.Method == "POST" {
				handleSessionSend(w, r)
			} else {
				jsonResponse(w, http.StatusMethodNotAllowed, false, "POST only")
			}

		case strings.HasSuffix(path, "/send-media"):
			if r.Method == "POST" {
				handleSessionSendMedia(w, r)
			} else {
				jsonResponse(w, http.StatusMethodNotAllowed, false, "POST only")
			}

		case strings.HasSuffix(path, "/download-media"):
			if r.Method == "POST" {
				handleDownloadMedia(w, r)
			} else {
				jsonResponse(w, http.StatusMethodNotAllowed, false, "POST only")
			}

		case strings.HasSuffix(path, "/webhook"):
			handleSessionWebhook(w, r)

		case strings.HasSuffix(path, "/groups"):
			handleSessionGroups(w, r)

		case strings.HasSuffix(path, "/group-info"):
			handleSessionGroupInfo(w, r)

		case strings.HasSuffix(path, "/create-group"):
			if r.Method == "POST" {
				handleSessionCreateGroup(w, r)
			} else {
				jsonResponse(w, http.StatusMethodNotAllowed, false, "POST only")
			}

		case strings.HasSuffix(path, "/leave-group"):
			if r.Method == "POST" {
				handleSessionLeaveGroup(w, r)
			} else {
				jsonResponse(w, http.StatusMethodNotAllowed, false, "POST only")
			}

		case strings.HasSuffix(path, "/group-invite-link"):
			handleSessionGroupInviteLink(w, r)

		case strings.HasSuffix(path, "/history"):
			handleSessionHistory(w, r)

		case strings.HasSuffix(path, "/disconnect"):
			if r.Method == "POST" {
				handleSessionDisconnect(w, r)
			} else {
				jsonResponse(w, http.StatusMethodNotAllowed, false, "POST only")
			}

		case strings.HasSuffix(path, "/logout"):
			if r.Method == "POST" {
				handleSessionLogout(w, r)
			} else {
				jsonResponse(w, http.StatusMethodNotAllowed, false, "POST only")
			}

		default:
			// DELETE /session/{name} - eliminar sesión
			if r.Method == "DELETE" {
				handleSessionDelete(w, r)
			} else if r.Method == "GET" {
				// GET /session/{name} - status
				handleSessionStatus(w, r)
			} else {
				jsonResponse(w, http.StatusNotFound, false, "endpoint not found")
			}
		}
	})

	return mux
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	// Directorio de sesiones
	sessionsDir := os.Getenv("SESSIONS_DIR")
	if sessionsDir == "" {
		sessionsDir = "/data/sessions"
	}

	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		fmt.Printf("Failed to create sessions dir: %v\n", err)
		os.Exit(1)
	}

	// Inicializar manager, cache de media y almacén de mensajes
	manager = NewSessionManager(sessionsDir)
	mediaCache = NewMediaCache()
	messageStore = NewMessageStore()
	messageBatcher = NewMessageBatcher()

	// Cargar sesiones existentes
	entries, err := os.ReadDir(sessionsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				name := entry.Name()
				dbPath := filepath.Join(sessionsDir, name, "whatsmeow.db")
				if _, err := os.Stat(dbPath); err == nil {
					fmt.Printf("Loading session: %s\n", name)
					if _, err := manager.GetOrCreateSession(name); err != nil {
						fmt.Printf("Failed to load session %s: %v\n", name, err)
					}
				}
			}
		}
	}

	// Puerto
	port := os.Getenv("PORT")
	if port == "" {
		port = "3100"
	}

	// Servidor HTTP
	server := &http.Server{
		Addr:    ":" + port,
		Handler: setupRoutes(),
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		fmt.Println("\nShutting down...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Desconectar todas las sesiones
		manager.mu.RLock()
		for name, session := range manager.sessions {
			fmt.Printf("Disconnecting session: %s\n", name)
			session.Client.Disconnect()
		}
		manager.mu.RUnlock()

		server.Shutdown(ctx)
	}()

	fmt.Printf("WhatsMeow Server v2.0 listening on :%s\n", port)
	fmt.Printf("Sessions directory: %s\n", sessionsDir)
	fmt.Println("Ready to accept connections...")

	// Iniciar monitor de conexiones en background
	go connectionMonitor()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Printf("Server error: %v\n", err)
		os.Exit(1)
	}
}

// connectionMonitor verifica periódicamente el estado de las conexiones
// y reconecta sesiones que se hayan desconectado silenciosamente
func connectionMonitor() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		manager.mu.RLock()
		sessions := make([]*Session, 0, len(manager.sessions))
		for _, s := range manager.sessions {
			sessions = append(sessions, s)
		}
		manager.mu.RUnlock()

		for _, session := range sessions {
			session.mu.RLock()
			name := session.Name
			client := session.Client
			connected := session.Connected
			// Verificar si hay al menos un webhook habilitado
			hasWebhook := false
			for _, wh := range session.Webhooks {
				if wh.Enabled {
					hasWebhook = true
					break
				}
			}
			session.mu.RUnlock()

			if client == nil {
				continue
			}

			// Verificar si el cliente reporta estar conectado
			isClientConnected := client.IsConnected()

			// Si hay discrepancia o está desconectado pero tiene webhook, intentar reconectar
			if hasWebhook && !isClientConnected {
				fmt.Printf("[%s] Connection monitor: detected disconnected session with active webhook, attempting reconnect\n", name)
				go session.autoReconnect()
			} else if connected != isClientConnected {
				// Sincronizar estado
				session.mu.Lock()
				session.Connected = isClientConnected
				session.mu.Unlock()
				if !isClientConnected {
					fmt.Printf("[%s] Connection monitor: state sync - marked as disconnected\n", name)
				}
			}
		}
	}
}
