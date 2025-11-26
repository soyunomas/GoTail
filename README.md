# GoTail

Herramienta en Go para visualizar múltiples archivos de log en tiempo real a través de un navegador web. Utiliza WebSockets para el streaming, organiza los logs en un dashboard tipo grid y permite alertas visuales críticas.

![GoTail Screenshot](screenshot.png)

## 🚀 Características

- **Multi-Log:** Visualización simultánea de múltiples archivos en un grid.
- **Tiempo Real:** Streaming eficiente vía WebSockets (`tail -f`).
- **Alertas Nucleares:** Sistema de alarmas visuales a pantalla completa para errores críticos.
- **Seguridad:** Autenticación mediante contraseña (SHA256).
- **Control Total:** Scroll automático inteligente, pausa con buffer y filtrado por texto/tags.
- **Resaltado Avanzado:** Colores, parpadeo y marcadores configurables vía JSON.

## 📥 Descarga e Instalación

Necesitas tener **Go** instalado (v1.16+).

```bash
# Clonar el repositorio
git clone https://github.com/soyunomas/gotail.git

# Entrar al directorio
cd gotail

# Descargar dependencias
go mod tidy
```

## ⚙️ Ejecución

El sistema ahora funciona leyendo un archivo de configuración maestro (`dashboard.json`).

### Compilación (Recomendado)
```bash
# Compilar el binario
go build -o gotail main.go

# Ejecutar
./gotail -config dashboard.json -port 9000
```

### Ejecución directa
```bash
go run main.go -config dashboard.json
```

### Parámetros
- `-config`: Ruta al archivo de definición del dashboard (Por defecto `dashboard.json`).
- `-port`: Puerto del servidor web (Por defecto `9000`).

## 🛠️ Configuración

La configuración se divide en dos partes: el dashboard general y los perfiles de resaltado.

### 1. Dashboard (`dashboard.json`)
Define la contraseña de acceso y la lista de archivos a monitorizar.

```json
{
  "server_password": "micontraseñasegura",
  "logs": [
    {
      "path": "/var/log/syslog",
      "profile": "syslog",
      "name": "Sistema Principal"
    },
    {
      "path": "/var/log/apache2/error.log",
      "profile": "apache2",
      "name": "Servidor Web"
    }
  ]
}
```

### 2. Perfiles (`configs/*.json`)
Reglas de color y alertas para cada tipo de log. Ejemplo con **Alerta Nuclear**:

```json
[
  {
    "keyword": "CRITICAL FAILURE", 
    "color": "#ff5555", 
    "dot": "red", 
    "blink": true,
    "alert_msg": "🚨 FALLO CRÍTICO DEL NÚCLEO 🚨"
  },
  {
    "keyword": "Connection accepted", 
    "color": "#50fa7b", 
    "dot": "green"
  }
]
```

# 📘 Guía de Uso y Configuración Avanzada

## 🖥️ Interfaz de Usuario

GoTail está diseñado para ser intuitivo, pero esconde varias funciones potentes:

### 1. Control del Flujo
*   **Pausa Global:** El botón superior "PAUSA GLOBAL" detiene el scroll de *todos* los paneles. Los logs siguen llegando en segundo plano (Buffer) y se mostrarán de golpe al reanudar.
*   **Pausa Individual:** Cada panel tiene su propio botón de pausa `||`. Útil para analizar un error específico sin detener el resto del sistema.
*   **Scroll Inteligente:** Si subes el scroll manualmente, el autoscroll se detiene. Aparecerá un botón flotante **"⬇ Nuevos Logs"** si llegan datos mientras revisas el historial.

### 2. Búsqueda y Filtrado
*   **Búsqueda Global:** La barra superior filtra líneas en *todos* los paneles simultáneamente.
*   **Chips de Filtro:** En la cabecera de cada panel verás etiquetas (e.g., "Error", "Warning"). Haz clic para mostrar/ocultar solo ese tipo de mensajes.

### 3. Selección y Copiado
*   **Copiar Línea:** Doble clic en una línea para copiar su contenido.
*   **Selección Múltiple:** Mantén presionado `Ctrl` (o `Cmd`) y haz clic para seleccionar varias líneas inconexas.
*   **Selección por Rango:** Selecciona una línea, mantén `Shift` y selecciona otra para marcar todo el bloque intermedio.
*   **Botón Copiar:** Al tener líneas seleccionadas, aparece un botón flotante "Copiar (N)" en la esquina inferior derecha.

### 4. Alertas Nucleares ☢️
Si una regla tiene configurado un `alert_msg`, la pantalla se oscurecerá y aparecerá una caja de alerta parpadeante. Pulsa "ENTENDIDO" o `Esc` para descartarla.

---

## ⚙️ Modificación de Configuración

### 1. El Archivo Maestro (`dashboard.json`)

Este archivo orquesta qué se monitoriza. Si cambias esto, debes reiniciar el servidor (`./gotail ...`).

```json
{
  "server_password": "clave_segura",  // Deja vacío "" para modo abierto
  "logs": [
    {
      "path": "/var/log/nginx/error.log", // Ruta absoluta al archivo
      "profile": "nginx",                 // Nombre del archivo en configs/ (sin .json)
      "name": "Nginx Errors"              // Título visible en la UI
    }
  ]
}
```

## 📂 Estructura

```text
/GoTail
│
├── main.go            # Lógica del servidor (WebSocket, Tail, Auth)
├── index.html         # Dashboard SPA (Grid, Alertas, Filtros)
├── login.html         # Pantalla de acceso
├── dashboard.json     # Configuración principal
├── configs/           # Perfiles de resaltado
│   ├── default.json
│   ├── auth.json
│   ├── apache2.json
│   └── ...
└── LICENSE            # Licencia MIT
```

## ⚖️ Licencia

Este proyecto está bajo la licencia **MIT**. Consulta el archivo `LICENSE` para más detalles.

