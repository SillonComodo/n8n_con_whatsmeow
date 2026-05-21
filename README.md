# 📱 WhatsMeow + n8n - Guía de Instalación

Sistema de integración WhatsApp para n8n usando WhatsMeow (Go).

## 📋 Tabla de Contenidos

- [Arquitectura](#arquitectura)
- [Requisitos](#requisitos)
- [Instalación Rápida](#instalación-rápida)
- [Instalación en Otra Computadora](#instalación-en-otra-computadora)
- [Configuración por Arquitectura](#configuración-por-arquitectura)
- [Uso del Nodo en n8n](#uso-del-nodo-en-n8n)
- [API del Servidor](#api-del-servidor)
- [Solución de Problemas](#solución-de-problemas)
- [Backup y Migración](#backup-y-migración)

---sudomanitas
sudomanitas
## 🏗️ Arquitectura

```
┌─────────────────────────────────────────────────────────────────┐
│                                                                 │
│   ┌─────────────┐         HTTP API         ┌─────────────────┐ │
│   │             │ ──────────────────────▶  │                 │ │
│   │    n8n      │   http://whatsmeow:3100  │  whatsmeow-     │ │
│   │  (puerto    │ ◀──────────────────────  │  server         │ │
│   │   5678)     │                          │  (puerto 3100)  │ │
│   └─────────────┘                          └────────┬────────┘ │
│                                                     │          │
│                                            ┌────────▼────────┐ │
│                                            │   WhatsApp      │ │
│                                            │   Servers       │ │
│                                            └─────────────────┘ │
│                                                                 │
│   VOLUMEN: whatsmeow_data                                      │
│   └── /data/sessions/{nombre}/whatsmeow.db                     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Componentes:**
- **whatsmeow-server**: Servidor Go que mantiene conexiones WhatsApp 24/7
- **n8n**: Orquestador de workflows con nodo WhatsMeow personalizado
- **Volumen compartido**: Sesiones persistentes en SQLite

---

## 📦 Requisitos

- Docker 20.10+
- Docker Compose v2+
- 512MB RAM mínimo
- Conexión a internet estable

---

## ⚡ Instalación Rápida

```bash
# Clonar o copiar la carpeta docker/
cd docker/n8n

# Construir y levantar
sudo docker-compose up --build -d

# Verificar que está corriendo
sudo docker ps
curl http://localhost:3100/health
```

---

## 🖥️ Instalación en Otra Computadora

### Paso 1: Copiar Archivos

Copia estas carpetas/archivos a la nueva máquina:

```
docker/
├── n8n/
│   └── docker-compose.yml
└── whatsmeow-bundle/
    ├── server/
    │   ├── main.go
    │   ├── go.mod
    │   └── Dockerfile
    └── nodes/
        └── WhatsMeow/
            └── WhatsMeow.node.js
```

**Método con rsync:**
```bash
rsync -avz --progress docker/ usuario@nueva-maquina:/ruta/destino/docker/
```

**Método con scp:**
```bash
scp -r docker/ usuario@nueva-maquina:/ruta/destino/
```

### Paso 2: Verificar Arquitectura

```bash
# En la nueva máquina
uname -m
```

| Resultado | Arquitectura | Acción |
|-----------|--------------|--------|
| `x86_64` | AMD64/Intel | Ver sección [AMD64/x86_64](#amd64x86_64-intel-y-amd) |
| `aarch64` | ARM64 | Listo, sin cambios |
| `armv7l` | ARM32 | Ver sección [ARM32](#arm32-raspberry-pi-antiguas) |

### Paso 3: Construir y Levantar

```bash
cd docker/n8n
sudo docker-compose up --build -d
```

### Paso 4: Verificar

```bash
# Verificar contenedores
sudo docker ps

# Probar servidor whatsmeow
curl http://localhost:3100/health
# Respuesta esperada: {"success":true,"data":"ok"}

# Acceder a n8n
# Abrir navegador: http://localhost:5678
```

---

## 🔧 Configuración por Arquitectura

### AMD64/x86_64 (Intel y AMD)

El Dockerfile ya es compatible. Solo verifica que docker-compose.yml use la imagen correcta de n8n:

```yaml
# docker-compose.yml
services:
  n8n:
    image: docker.n8n.io/n8nio/n8n:latest  # Cambia de :2.1.4-arm64 a :latest
```

**Editar docker-compose.yml:**
```bash
cd docker/n8n
nano docker-compose.yml
# Cambiar: image: docker.n8n.io/n8nio/n8n:2.1.4-arm64
# Por:     image: docker.n8n.io/n8nio/n8n:latest
```

### ARM64 (Raspberry Pi 4/5, Apple Silicon, AWS Graviton)

**Configuración actual, sin cambios necesarios.**

El Dockerfile usa `golang:1.24-alpine` que soporta ARM64 nativamente.

### ARM32 (Raspberry Pi antiguas)

⚠️ **Nota:** ARM32 tiene soporte limitado para Go moderno.

1. Usar imagen específica de n8n:
```yaml
image: docker.n8n.io/n8nio/n8n:latest  # Detecta arquitectura automáticamente
```

2. Modificar el Dockerfile del servidor:
```dockerfile
# Cambiar la primera línea de:
FROM golang:1.24-alpine AS builder
# A:
FROM golang:1.22-alpine AS builder
```

3. Si hay problemas de memoria, agregar swap:
```bash
sudo fallocate -l 1G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile
```

---

## 📱 Uso del Nodo en n8n

### Conectar WhatsApp (Primera vez)

1. Abre n8n: `http://localhost:5678`
2. Crea un workflow nuevo
3. Agrega el nodo **WhatsMeow**
4. Configura:
   - **Operation**: Connect / Show QR
   - **Session Scope**: Global o Custom
5. Ejecuta el nodo
6. Escanea el código QR con WhatsApp > Dispositivos vinculados
7. La sesión queda guardada permanentemente

### Enviar Mensaje de Texto

1. Agrega nodo **WhatsMeow**
2. Configura:
   - **Operation**: Send Text Message
   - **Target Type**: Phone Number o Group JID
   - **Phone Number**: `5491123456789` (sin + ni espacios)
   - **Message**: Tu mensaje

### Enviar Multimedia (Imagen, Video, Audio, Documento)

1. Agrega nodo **WhatsMeow**
2. Configura:
   - **Operation**: Send Media
   - **Media Type**: Image, Video, Audio o Document
   - **Media Source**: URL o Binary Data
   - **Media URL**: URL del archivo (si usas URL)
   - **Caption**: Texto opcional

### Recibir Mensajes (Trigger/Webhook)

Para recibir mensajes entrantes:

1. **Crear workflow con trigger:**
   - Agrega nodo **WhatsMeow Trigger**
   - Configura filtros opcionales (sesión, tipo de mensaje, grupo/privado)
   - Activa el workflow

2. **Configurar webhook en la sesión:**
   - Agrega nodo **WhatsMeow**
   - **Operation**: Configure Webhook
   - **Webhook Action**: Set Config
   - **Webhook URL**: La URL del trigger de n8n (ej: `http://n8n:5678/webhook/whatsmeow`)
   - **Webhook Enabled**: true

3. **Datos recibidos:**
   - `from`: Número del remitente
   - `fromName`: Nombre del remitente
   - `text`: Texto del mensaje
   - `type`: Tipo (text, media, sticker, location, contact)
   - `isGroup`: Si es mensaje de grupo
   - `mediaType`: Tipo de media si aplica

### Gestión de Grupos

| Operación | Descripción |
|-----------|-------------|
| List Groups | Lista todos los grupos donde estás |
| Get Group Info | Info detallada + participantes |
| Create Group | Crear grupo nuevo con participantes |
| Leave Group | Salir de un grupo |
| Get Group Invite Link | Obtener link de invitación |

### Tipos de Sesión

| Tipo | Nombre | Uso |
|------|--------|-----|
| Global | `global` | Una sesión compartida por todos los workflows |
| Workflow | `wf-{id}` | Una sesión aislada por workflow |
| Custom | `mi-bot`, `ventas` | Sesiones personalizadas por nombre |

---

## 🔌 API del Servidor

El servidor whatsmeow expone estos endpoints en el puerto 3100:

### Endpoints Principales

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/sessions` | Listar todas las sesiones |
| POST | `/session/{name}/connect` | Conectar/obtener QR |
| GET | `/session/{name}/status` | Estado de la sesión |
| POST | `/session/{name}/send` | Enviar mensaje de texto |
| POST | `/session/{name}/send-media` | Enviar multimedia |

### Endpoints de Webhook

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| GET | `/session/{name}/webhook` | Ver config de webhook |
| POST | `/session/{name}/webhook` | Configurar webhook |
| DELETE | `/session/{name}/webhook` | Eliminar webhook |

### Endpoints de Grupos

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| GET | `/session/{name}/groups` | Listar grupos |
| GET | `/session/{name}/group-info?jid=...` | Info de grupo |
| POST | `/session/{name}/create-group` | Crear grupo |
| POST | `/session/{name}/leave-group?jid=...` | Salir de grupo |
| GET | `/session/{name}/group-invite-link?jid=...` | Link de invitación |

### Endpoints de Gestión

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| POST | `/session/{name}/disconnect` | Desconectar sin borrar |
| POST | `/session/{name}/logout` | Cerrar sesión en WhatsApp |
| DELETE | `/session/{name}` | Eliminar sesión completamente |

### Ejemplos con curl

```bash
# Health check
curl http://localhost:3100/health

# Listar sesiones
curl http://localhost:3100/sessions

# Conectar y obtener QR
curl -X POST http://localhost:3100/session/mi-bot/connect

# Enviar mensaje de texto
curl -X POST http://localhost:3100/session/mi-bot/send \
  -H "Content-Type: application/json" \
  -d '{"phone":"5491123456789","message":"Hola desde API!"}'

# Enviar imagen por URL
curl -X POST http://localhost:3100/session/mi-bot/send-media \
  -H "Content-Type: application/json" \
  -d '{
    "phone": "5491123456789",
    "media_type": "image",
    "media_url": "https://example.com/imagen.jpg",
    "caption": "Mira esta imagen!"
  }'

# Enviar documento
curl -X POST http://localhost:3100/session/mi-bot/send-media \
  -H "Content-Type: application/json" \
  -d '{
    "phone": "5491123456789",
    "media_type": "document",
    "media_url": "https://example.com/archivo.pdf",
    "file_name": "documento.pdf",
    "caption": "Aquí está el archivo"
  }'

# Configurar webhook para recibir mensajes
curl -X POST http://localhost:3100/session/mi-bot/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "url": "http://n8n:5678/webhook/whatsmeow",
    "enabled": true
  }'

# Listar grupos
curl http://localhost:3100/session/mi-bot/groups

# Enviar mensaje a grupo
curl -X POST http://localhost:3100/session/mi-bot/send \
  -H "Content-Type: application/json" \
  -d '{"group_jid":"123456789@g.us","message":"Hola grupo!"}'

# Ver estado
curl http://localhost:3100/session/mi-bot/status

# Desconectar
curl -X POST http://localhost:3100/session/mi-bot/disconnect
```

---

## 🔥 Solución de Problemas

### El contenedor whatsmeow no inicia

```bash
# Ver logs
sudo docker logs whatsmeow

# Reconstruir desde cero
sudo docker-compose down
sudo docker-compose build --no-cache whatsmeow
sudo docker-compose up -d
```

### Error de compilación de Go

Si ves errores como `go: go.mod requires go >= 1.24`:

```bash
# Verifica que el Dockerfile use golang:1.24-alpine
cat whatsmeow-bundle/server/Dockerfile | grep FROM
```

### QR no aparece o está corrupto

El QR viene en formato texto. Si usas la API directamente:

```bash
curl -X POST http://localhost:3100/session/test/connect 2>/dev/null | jq -r '.data.qr'
```

Para mostrarlo gráficamente, usa un generador de QR online o la librería `qrcode-terminal`.

### Sesión se desconecta frecuentemente

1. Verifica conexión a internet estable
2. Revisa logs: `sudo docker logs -f whatsmeow`
3. WhatsApp puede desconectar si detecta actividad sospechosa

### Error "session not found"

La sesión no existe. Primero conecta:
```bash
curl -X POST http://localhost:3100/session/nombre/connect
```

### n8n no encuentra el nodo WhatsMeow

1. Verifica que el volumen esté montado correctamente:
```bash
sudo docker exec n8n ls -la /home/node/.n8n/custom/nodes/
```

2. Reinicia n8n:
```bash
sudo docker restart n8n
```

---

## 💾 Backup y Migración

### ⚠️ IMPORTANTE: Cómo Actualizar SIN Perder Datos

Los datos de n8n y WhatsApp se guardan en volúmenes Docker:

| Volumen | Contenido |
|---------|-----------|
| `n8n_n8n_data` | Cuenta de usuario, workflows, credenciales, historial |
| `n8n_whatsmeow_data` | Sesiones de WhatsApp |

**Para actualizar el código SIN perder datos, usa:**
```bash
cd /home/bryan/docker/n8n

# ✅ CORRECTO - Reconstruir sin eliminar volúmenes
sudo docker-compose up --build -d

# ✅ CORRECTO - Reiniciar contenedores
sudo docker-compose restart

# ⚠️ CUIDADO - Esto detiene pero NO elimina volúmenes
sudo docker-compose down
sudo docker-compose up --build -d
```

**❌ NUNCA uses estos comandos (eliminan los datos):**
```bash
# ❌ PELIGRO - Elimina TODOS los volúmenes
sudo docker-compose down -v

# ❌ PELIGRO - Elimina volúmenes huérfanos
sudo docker system prune -a --volumes
```

### Backup de Sesiones

Las sesiones están en el volumen Docker `whatsmeow_data`:

```bash
# Crear backup
sudo docker run --rm \
  -v n8n_whatsmeow_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/whatsmeow-sessions-backup.tar.gz -C /data .

# El archivo queda en: ./whatsmeow-sessions-backup.tar.gz
```

### Restaurar Sesiones

```bash
# En la nueva máquina, después de crear los contenedores:
sudo docker-compose down

sudo docker run --rm \
  -v n8n_whatsmeow_data:/data \
  -v $(pwd):/backup \
  alpine sh -c "cd /data && tar xzf /backup/whatsmeow-sessions-backup.tar.gz"

sudo docker-compose up -d
```

### Migración Completa

1. **En máquina origen:**
```bash
cd docker/n8n
sudo docker-compose down

# Backup de sesiones
sudo docker run --rm \
  -v n8n_whatsmeow_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/whatsmeow-backup.tar.gz -C /data .

# Backup de n8n (workflows, credenciales)
sudo docker run --rm \
  -v n8n_n8n_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/n8n-backup.tar.gz -C /data .
```

2. **Copiar a nueva máquina:**
```bash
scp -r docker/ usuario@nueva:/ruta/
scp whatsmeow-backup.tar.gz n8n-backup.tar.gz usuario@nueva:/ruta/docker/n8n/
```

3. **En máquina destino:**
```bash
cd docker/n8n

# Ajustar docker-compose.yml si es necesario (ver sección Arquitectura)
# Levantar para crear volúmenes
sudo docker-compose up -d
sudo docker-compose down

# Restaurar backups
sudo docker run --rm \
  -v n8n_whatsmeow_data:/data \
  -v $(pwd):/backup \
  alpine sh -c "cd /data && tar xzf /backup/whatsmeow-backup.tar.gz"

sudo docker run --rm \
  -v n8n_n8n_data:/data \
  -v $(pwd):/backup \
  alpine sh -c "cd /data && tar xzf /backup/n8n-backup.tar.gz"

# Levantar todo
sudo docker-compose up -d
```

---

## 📝 Notas Adicionales

### Puertos Utilizados

| Puerto | Servicio | Acceso |
|--------|----------|--------|
| 5678 | n8n Web UI | Público |
| 3100 | whatsmeow API | Interno (o público si necesitas) |

### Variables de Entorno

El nodo WhatsMeow usa:
- `WHATSMEOW_SERVER_URL`: URL del servidor (default: `http://whatsmeow:3100`)

### Estructura de Sesiones

```
/data/sessions/
├── global/
│   └── whatsmeow.db        # Base de datos SQLite con llaves y estado
├── mi-bot/
│   └── whatsmeow.db
└── otro-bot/
    └── whatsmeow.db
```

---

## 🆘 Soporte

Si encuentras problemas:

1. Revisa los logs: `sudo docker logs whatsmeow`
2. Verifica la conexión: `curl http://localhost:3100/health`
3. Reinicia los servicios: `sudo docker-compose restart`

---

*Última actualización: Diciembre 2025*
