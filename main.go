package main

import (
	"bufio"
	"embed"
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

	"github.com/gorilla/websocket"
	"github.com/nxadm/tail"
)

//go:embed index.html
var content embed.FS

// Estructura para la configuración de colores
type HighlightRule struct {
	Keyword  string `json:"keyword"`
	Color    string `json:"color"`
	Dot      string `json:"dot"`
	UseRegex bool   `json:"use_regex"`
}

// Estructura para pasar datos al HTML
type PageData struct {
	ConfigJSON template.JS
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	loadedConfig []HighlightRule
)

type Hub struct {
	clients   map[*websocket.Conn]bool
	broadcast chan string
	history   []string
	mutex     sync.Mutex
}

var hub = Hub{
	clients:   make(map[*websocket.Conn]bool),
	broadcast: make(chan string),
	history:   make([]string, 0),
}

const HISTORY_SIZE = 50

func main() {
	// Definición de flags (parámetros)
	filePath := flag.String("file", "", "Ruta absoluta al archivo de log a monitorizar")
	port := flag.String("port", "9000", "Puerto del servidor web")
	profile := flag.String("profile", "default", "Nombre del perfil de configuración (busca en carpeta configs/)")
	
	flag.Parse()

	if *filePath == "" {
		fmt.Println("❌ Error: Debes especificar un archivo con -file")
		fmt.Println("👉 Ejemplo: go run main.go -file /var/log/syslog -profile syslog")
		os.Exit(1)
	}

	// 1. Construir la ruta al archivo JSON de configuración
	// Busca en la carpeta ./configs/[nombre].json
	configPath := filepath.Join("configs", *profile+".json")
	
	// 2. Cargar configuración
	loadConfig(configPath)

	// 3. Cargar historial inicial del log
	fmt.Printf("📂 Leyendo log: %s\n", *filePath)
	initialLines := getLastLinesFromFile(*filePath, HISTORY_SIZE)
	hub.history = append(hub.history, initialLines...)

	// 4. Iniciar Tail (lectura en tiempo real) y Hub (distribución de mensajes)
	go tailFile(*filePath)
	go handleMessages()

	// 5. Servidor Web
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleConnections)

	fmt.Printf("🚀 GoTail activo en puerto %s\n", *port)
	fmt.Printf("🎨 Perfil cargado: %s\n", configPath)
	fmt.Printf("🌍 Web Interface: http://localhost:%s\n", *port)
	
	err := http.ListenAndServe(":"+*port, nil)
	if err != nil {
		log.Fatal("Error iniciando servidor: ", err)
	}
}

func loadConfig(path string) {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Printf("⚠️  No se encontró el perfil '%s'. Usando configuración vacía.\n", path)
		// Si falla, intentamos cargar configs/default.json por si acaso, o dejamos vacío
		return
	}
	
	err = json.Unmarshal(file, &loadedConfig)
	if err != nil {
		fmt.Printf("❌ Error procesando el JSON '%s': %v\n", path, err)
		return
	}
	
	fmt.Printf("✅  Reglas cargadas: %d reglas desde %s\n", len(loadedConfig), path)
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	
	// Convertir la config cargada a JSON String para inyectarla en el JS del navegador
	configBytes, _ := json.Marshal(loadedConfig)
	data := PageData{
		ConfigJSON: template.JS(configBytes),
	}

	tmpl, err := template.ParseFS(content, "index.html")
	if err != nil {
		http.Error(w, "Error template", 500)
		return
	}
	tmpl.Execute(w, data)
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	defer ws.Close()

	hub.mutex.Lock()
	// Enviar historial al nuevo cliente
	for _, line := range hub.history {
		ws.WriteMessage(websocket.TextMessage, []byte(line))
	}
	hub.clients[ws] = true
	hub.mutex.Unlock()

	// Mantener conexión viva
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
		hub.history = append(hub.history, msg)
		if len(hub.history) > HISTORY_SIZE {
			hub.history = hub.history[1:]
		}
		for client := range hub.clients {
			client.WriteMessage(websocket.TextMessage, []byte(msg))
		}
		hub.mutex.Unlock()
	}
}

func tailFile(filename string) {
	t, err := tail.TailFile(filename, tail.Config{
		Follow: true, ReOpen: true, Poll: true,
		Location: &tail.SeekInfo{Offset: 0, Whence: 2},
	})
	if err != nil { log.Fatal(err) }
	for line := range t.Lines {
		hub.broadcast <- line.Text
	}
}

func getLastLinesFromFile(filename string, n int) []string {
	file, err := os.Open(filename)
	if err != nil { return []string{} }
	defer file.Close()
	
	// Método simplificado para leer líneas
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
