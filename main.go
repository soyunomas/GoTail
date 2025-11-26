package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nxadm/tail"
)

// Incluimos TODOS los archivos HTML nuevos
//go:embed index.html login.html styles.html scripts_globals.html scripts_alerts.html scripts_actions.html scripts_core.html
var content embed.FS

// --- ESTRUCTURAS DE CONFIGURACIÓN ---

type GlobalConfig struct {
	ServerPassword string           `json:"server_password"`
	Logs           []LogEntryConfig `json:"logs"`
}

type LogEntryConfig struct {
	Path    string `json:"path"`
	Profile string `json:"profile"`
	Name    string `json:"name"`
}

type HighlightRule struct {
	Keyword  string `json:"keyword"`
	Color    string `json:"color"`
	Dot      string `json:"dot"`
	UseRegex bool   `json:"use_regex"`
	Label    string `json:"label,omitempty"`
	Blink    bool   `json:"blink,omitempty"`
	AlertMsg string `json:"alert_msg,omitempty"`
}

type FrontendLogData struct {
	Index int             `json:"index"`
	Name  string          `json:"name"`
	Rules []HighlightRule `json:"rules"`
}

type WebSocketMessage struct {
	LogIndex int    `json:"log_index"`
	Text     string `json:"text"`
}

type PageData struct {
	DashboardJSON template.JS
}

type LoginData struct {
	Error bool
}

// --- GLOBALES ---

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	loadedProfiles = make(map[string][]HighlightRule)

	currentConfig  GlobalConfig
	serverPassHash string
)

type Hub struct {
	clients   map[*websocket.Conn]bool
	broadcast chan WebSocketMessage
	history   map[int][]string
	mutex     sync.Mutex
}

var hub = Hub{
	clients:   make(map[*websocket.Conn]bool),
	broadcast: make(chan WebSocketMessage),
	history:   make(map[int][]string),
}

const (
	HISTORY_SIZE = 50
	COOKIE_NAME  = "gotail_session"
)

// --- MAIN ---

func main() {
	port := flag.String("port", "9000", "Puerto del servidor web")
	configPath := flag.String("config", "dashboard.json", "Ruta al dashboard JSON")

	flag.Parse()

	// 1. Cargar Configuración con valores por defecto y sanitización
	loadDashboardConfig(*configPath)

	// 2. Procesar Password
	if currentConfig.ServerPassword != "" {
		hash := sha256.Sum256([]byte(currentConfig.ServerPassword))
		serverPassHash = hex.EncodeToString(hash[:])
		fmt.Println("🔒 Modo Seguro ACTIVADO (Password cargado).")
	} else {
		// Si no hay password en el JSON, es modo abierto
		fmt.Println("⚠️  Modo Abierto: Sin password configurado.")
	}

	// Inicializar mapa de historial
	for i := range currentConfig.Logs {
		hub.history[i] = make([]string, 0)
	}

	startTailing()
	go handleMessages()

	http.HandleFunc("/", authMiddleware(serveHome))
	http.HandleFunc("/ws", authMiddleware(handleConnections))
	http.HandleFunc("/login", handleLogin)

	fmt.Printf("🚀 GoTail corriendo en puerto %s\n", *port)
	fmt.Printf("🌍 http://localhost:%s\n", *port)

	err := http.ListenAndServe(":"+*port, nil)
	if err != nil {
		log.Fatal("Error iniciando servidor: ", err)
	}
}

// --- LOGICA DE CARGA Y SANITIZACIÓN ---

func loadDashboardConfig(path string) {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Printf("⚠️  No se encontró '%s'. Iniciando con configuración vacía.\n", path)
		currentConfig = GlobalConfig{}
	} else {
		trimmed := bytes.TrimSpace(file)
		if len(trimmed) == 0 {
			fmt.Println("⚠️  El archivo de configuración está vacío.")
			currentConfig = GlobalConfig{}
		} else {
			// Soporte para Array (legacy) o Objeto (nuevo)
			if trimmed[0] == '[' {
				fmt.Println("ℹ️  Formato antiguo (Array) detectado.")
				var logs []LogEntryConfig
				if err := json.Unmarshal(file, &logs); err != nil {
					log.Printf("❌ Error leyendo JSON Array: %v. Iniciando vacío.", err)
					currentConfig = GlobalConfig{}
				} else {
					currentConfig.Logs = logs
				}
			} else {
				if err := json.Unmarshal(file, &currentConfig); err != nil {
					log.Printf("❌ Error leyendo JSON Objeto: %v. Iniciando vacío.", err)
					currentConfig = GlobalConfig{}
				}
			}
		}
	}

	// --- APLICAR VALORES POR DEFECTO ---
	if len(currentConfig.Logs) == 0 {
		fmt.Println("⚠️  ADVERTENCIA: No hay logs definidos para monitorear.")
	}

	for i := range currentConfig.Logs {
		// Usamos puntero para modificar el struct original
		entry := &currentConfig.Logs[i]

		// 1. Si falta "name", usar el nombre del archivo
		if entry.Name == "" {
			if entry.Path != "" {
				entry.Name = filepath.Base(entry.Path)
			} else {
				entry.Name = fmt.Sprintf("Log Sin Nombre #%d", i+1)
			}
		}

		// 2. Si falta "profile", usar "default"
		if entry.Profile == "" {
			entry.Profile = "default"
		}

		// Cargar perfil (si no existe, intentará cargar default)
		if _, exists := loadedProfiles[entry.Profile]; !exists {
			loadProfile(entry.Profile)
		}
	}

	fmt.Printf("📋 Configuración activa: %d logs.\n", len(currentConfig.Logs))
}

func loadProfile(profileName string) {
	path := filepath.Join("configs", profileName+".json")
	file, err := ioutil.ReadFile(path)

	if err != nil {
		// Si el perfil solicitado falla y no es 'default', intentamos cargar 'default'
		if profileName != "default" {
			fmt.Printf("⚠️  Perfil '%s' no encontrado. Intentando usar fallback 'default'.\n", profileName)
			// Verificar si ya cargamos default antes para no leer disco otra vez
			if defRules, ok := loadedProfiles["default"]; ok {
				loadedProfiles[profileName] = defRules
				return
			}
			// Intentar cargar default desde disco
			loadProfile("default")
			// Asignar lo que se haya cargado (o vacío)
			loadedProfiles[profileName] = loadedProfiles["default"]
			return
		}

		// Si estamos intentando cargar 'default' y falla, reglas vacías
		loadedProfiles[profileName] = []HighlightRule{}
		return
	}

	var rules []HighlightRule
	if err := json.Unmarshal(file, &rules); err != nil {
		fmt.Printf("❌ Error parseando perfil '%s': %v. Usando reglas vacías.\n", profileName, err)
		loadedProfiles[profileName] = []HighlightRule{}
		return
	}
	loadedProfiles[profileName] = rules
}

// --- LOGICA DEL SERVIDOR ---

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Si no hay hash de contraseña (modo abierto), pasar directo
		if serverPassHash == "" {
			next(w, r)
			return
		}

		cookie, err := r.Cookie(COOKIE_NAME)
		if err != nil || cookie.Value != serverPassHash {
			// Si es WebSocket, retornamos error 401, el JS manejará el cierre
			if r.URL.Path == "/ws" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			// Si es HTML normal, redirigir a login
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	// Si estamos en modo abierto, redirigir al home
	if serverPassHash == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if r.Method == "GET" {
		tmpl, _ := template.ParseFS(content, "login.html")
		tmpl.Execute(w, LoginData{Error: false})
		return
	}
	if r.Method == "POST" {
		pass := r.FormValue("password")
		hash := sha256.Sum256([]byte(pass))
		hashStr := hex.EncodeToString(hash[:])

		if hashStr == serverPassHash {
			http.SetCookie(w, &http.Cookie{
				Name:     COOKIE_NAME,
				Value:    hashStr,
				Path:     "/",
				HttpOnly: true,
				Expires:  time.Now().Add(24 * time.Hour),
			})
			http.Redirect(w, r, "/", http.StatusFound)
		} else {
			tmpl, _ := template.ParseFS(content, "login.html")
			tmpl.Execute(w, LoginData{Error: true})
		}
	}
}

func startTailing() {
	for i, entry := range currentConfig.Logs {
		if entry.Path == "" {
			continue // Skip logs sin path
		}

		go func(index int, path string) {
			// 1. Leer las últimas líneas antes de empezar el tail para rellenar historial
			initialLines := getLastLinesFromFile(path, HISTORY_SIZE)
			hub.mutex.Lock()
			hub.history[index] = append(hub.history[index], initialLines...)
			hub.mutex.Unlock()

			// 2. Iniciar el tailing en tiempo real
			t, err := tail.TailFile(path, tail.Config{
				Follow: true, ReOpen: true, Poll: true,
				Location: &tail.SeekInfo{Offset: 0, Whence: 2},
				Logger: tail.DiscardingLogger,
			})
			if err != nil {
				fmt.Printf("❌ Error abriendo log %s: %v\n", path, err)
				return
			}
			for line := range t.Lines {
				hub.broadcast <- WebSocketMessage{LogIndex: index, Text: line.Text}
			}
		}(i, entry.Path)
	}
}

func getLastLinesFromFile(filename string, n int) []string {
	file, err := os.Open(filename)
	if err != nil {
		return []string{}
	}
	defer file.Close()

	// Metodo simple: leer todo y quedarse con el final
	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	var frontendData []FrontendLogData

	for i, entry := range currentConfig.Logs {
		data := FrontendLogData{
			Index: i,
			Name:  entry.Name,
			Rules: loadedProfiles[entry.Profile],
		}
		frontendData = append(frontendData, data)
	}
	jsonBytes, _ := json.Marshal(frontendData)
	data := PageData{DashboardJSON: template.JS(jsonBytes)}

	// --- CARGAMOS TODOS LOS FRAGMENTOS ---
	tmpl, err := template.ParseFS(content,
		"index.html",
		"styles.html",
		"scripts_globals.html",
		"scripts_alerts.html",
		"scripts_actions.html",
		"scripts_core.html",
	)
	if err != nil {
		http.Error(w, "Error cargando templates HTML: "+err.Error(), 500)
		return
	}
	tmpl.Execute(w, data)
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	hub.mutex.Lock()
	hub.clients[ws] = true
	// Enviar historial completo al conectar
	for index, lines := range hub.history {
		for _, line := range lines {
			msg := WebSocketMessage{LogIndex: index, Text: line}
			jsonMsg, _ := json.Marshal(msg)
			ws.WriteMessage(websocket.TextMessage, jsonMsg)
		}
	}
	hub.mutex.Unlock()

	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			hub.mutex.Lock()
			delete(hub.clients, ws)
			hub.mutex.Unlock()
			break
		}
	}
}

func handleMessages() {
	for msg := range hub.broadcast {
		hub.mutex.Lock()
		// Guardar en historial
		hub.history[msg.LogIndex] = append(hub.history[msg.LogIndex], msg.Text)
		if len(hub.history[msg.LogIndex]) > HISTORY_SIZE {
			hub.history[msg.LogIndex] = hub.history[msg.LogIndex][1:]
		}
		// Enviar a clientes
		jsonMsg, _ := json.Marshal(msg)
		for client := range hub.clients {
			client.WriteMessage(websocket.TextMessage, jsonMsg)
		}
		hub.mutex.Unlock()
	}
}
