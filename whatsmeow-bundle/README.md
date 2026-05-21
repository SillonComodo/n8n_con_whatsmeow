# WhatsMeow n8n Node - Documentación Técnica

## 📋 Descripción

Nodo personalizado de n8n para interactuar con WhatsApp usando la biblioteca Go [whatsmeow](https://github.com/tulir/whatsmeow). Permite enviar mensajes, vincular dispositivos mediante QR y gestionar múltiples sesiones.

---

## 🏗️ Arquitectura

```
whatsmeow-bundle/
├── main.go                     # CLI wrapper en Go para whatsmeow
├── go.mod                      # Dependencias de Go
├── package.json                # Manifiesto del nodo n8n
├── index.js                    # Entry point del paquete
├── nodes/
│   └── WhatsMeow/
│       ├── WhatsMeow.node.js   # Implementación del nodo (compilado)
│       ├── whatsapp.svg        # Ícono del nodo
│       └── bin/
│           └── whatsapp-cli-linux  # Binario compilado para ARM64/musl
├── scan-qr.sh                  # Script para escanear QR desde terminal
├── get-qr-image.sh             # Script para generar imagen QR
└── manage-sessions.sh          # Script para gestionar sesiones
```

---

## 🐛 Problemas Encontrados y Soluciones

### 1. **Contenedor n8n no arrancaba - "failed switching to node"**

**Problema:** El contenedor de n8n crasheaba al iniciar con el error "failed switching to node".

**Causa:** Un docker-compose.yml corrupto o con configuración inválida de usuario.

**Solución:** Crear un docker-compose.yml limpio con la configuración correcta:
```yaml
services:
  n8n:
    image: n8nio/n8n:2.1.4-arm64
    container_name: n8n
    user: "0:0"  # Ejecutar como root para evitar problemas de permisos
    # ... resto de configuración
```

---

### 2. **Nodo no aparecía en n8n UI**

**Problema:** El nodo WhatsMeow no aparecía en la búsqueda de nodos de n8n.

**Causa múltiple:**
- El `package.json` no tenía la configuración correcta de n8n
- Faltaba `n8nNodesApiVersion`
- El formato de `inputs`/`outputs` era incompatible con n8n 2.x

**Solución en `package.json`:**
```json
{
  "name": "n8n-nodes-whatsmeow",
  "version": "1.0.0",
  "main": "index.js",
  "n8n": {
    "n8nNodesApiVersion": 1,
    "nodes": ["nodes/WhatsMeow/WhatsMeow.node.js"]
  },
  "peerDependencies": {
    "n8n-workflow": "^2.2.1"
  }
}
```

**Solución en el nodo (inputs/outputs):**
```javascript
// ANTES (no funcionaba en n8n 2.x):
inputs: ['main'],
outputs: ['main'],

// DESPUÉS (correcto para n8n 2.x):
inputs: [n8n_workflow_1.NodeConnectionType.Main],
outputs: [n8n_workflow_1.NodeConnectionType.Main],
```

---

### 3. **Binario Go no funcionaba en Alpine Linux (musl)**

**Problema:** El binario compilado daba error de "not found" o "exec format error" dentro del contenedor n8n (que usa Alpine Linux).

**Causa:** Alpine usa musl libc en lugar de glibc. Los binarios compilados con CGO contra glibc no funcionan.

**Solución:** Compilar el binario con musl:
```bash
# En sistema con musl-gcc instalado
CGO_ENABLED=1 \
CC=aarch64-linux-musl-gcc \
GOOS=linux \
GOARCH=arm64 \
go build -ldflags '-linkmode external -extldflags "-static"' -o nodes/WhatsMeow/bin/whatsapp-cli-linux main.go
```

**Nota:** En Raspberry Pi (aarch64), necesitas instalar el cross-compiler de musl:
```bash
# Opción 1: Usar musl-tools
sudo apt install musl-tools

# Opción 2: Descargar musl cross-compiler
wget https://musl.cc/aarch64-linux-musl-cross.tgz
tar -xzf aarch64-linux-musl-cross.tgz
export PATH=$PATH:$(pwd)/aarch64-linux-musl-cross/bin
```

---

### 4. **Sesiones no coincidían entre terminal y n8n**

**Problema:** Al escanear QR desde terminal funcionaba, pero n8n mostraba `logged_in: false`.

**Causa:** El contenedor corre como root (`user: "0:0"`), entonces:
- `os.homedir()` devuelve `/root`
- El nodo buscaba sesiones en `/root/.n8n/binaryData/whatsmeow/`
- Los scripts de terminal creaban sesiones en `/home/node/.n8n/binaryData/whatsmeow/`

**Solución:** Usar ruta fija en lugar de `os.homedir()`:
```javascript
// ANTES:
const baseN8n = process.env.N8N_USER_FOLDER ?? path.join(os.homedir(), '.n8n');

// DESPUÉS:
const baseN8n = process.env.N8N_USER_FOLDER ?? '/home/node/.n8n';
```

---

### 5. **QR Code expiraba antes de poder escanearlo**

**Problema:** El código QR de WhatsApp expira en ~60 segundos, y copiar URLs era muy lento.

**Solución:** Múltiples métodos para escanear rápidamente:

1. **Imagen binaria en n8n:** El nodo genera un PNG del QR que se muestra en la pestaña "Binary"

2. **Script para servidor HTTP:**
```bash
# Genera imagen y crea HTML
./get-qr-image.sh

# Sirve en http://192.168.100.13:8888/qr-code.html
cd /home/bryan/docker/n8n/local-files
python3 -m http.server 8888 --bind 0.0.0.0
```

3. **Escaneo interactivo desde terminal:**
```bash
./scan-qr.sh
```

---

## 🔧 Configuración de Docker

### docker-compose.yml
```yaml
services:
  n8n:
    image: n8nio/n8n:2.1.4-arm64
    container_name: n8n
    restart: unless-stopped
    ports:
      - "5678:5678"
    environment:
      - N8N_HOST=0.0.0.0
      - N8N_PORT=5678
      - N8N_PROTOCOL=http
      - WEBHOOK_URL=http://192.168.100.13:5678
      - TZ=America/Mexico_City
      - NODE_ENV=production
      - N8N_SECURE_COOKIE=false
      - N8N_RUNNERS_ENABLED=true
      - N8N_USER_MANAGEMENT_DISABLED=true
      - N8N_CUSTOM_EXTENSIONS=/home/node/.n8n/custom/n8n-nodes-whatsmeow
      - EXTRA_NODE_MODULES_PATH=/home/node/.n8n/custom
      - N8N_LOG_LEVEL=debug
    volumes:
      - n8n_data:/home/node/.n8n
      - ./local-files:/files
      - /home/bryan/docker/whatsmeow-bundle:/home/node/.n8n/custom/n8n-nodes-whatsmeow
    user: "0:0"  # IMPORTANTE: Ejecutar como root

volumes:
  n8n_data:
```

### Variables de entorno clave:
| Variable | Propósito |
|----------|-----------|
| `N8N_CUSTOM_EXTENSIONS` | Ruta al nodo personalizado |
| `EXTRA_NODE_MODULES_PATH` | Ruta adicional para módulos |
| `N8N_LOG_LEVEL=debug` | Ver logs detallados de carga de nodos |
| `user: "0:0"` | Ejecutar como root (evita problemas de permisos) |

---

## 📱 Sistema de Sesiones

### Tipos de sesiones:
| Tipo | Ubicación | Uso |
|------|-----------|-----|
| Global | `/home/node/.n8n/binaryData/whatsmeow/` | Compartida por todos los workflows |
| Workflow | `/home/node/.n8n/binaryData/whatsmeow/wf-{id}/` | Aislada por workflow |
| Custom | `/home/node/.n8n/binaryData/whatsmeow/{nombre}/` | Nombre personalizado |

### Gestión de sesiones:
```bash
# Listar sesiones
./manage-sessions.sh list

# Ver estado de una sesión
./manage-sessions.sh status global
./manage-sessions.sh status bryan

# Eliminar sesión específica
./manage-sessions.sh clean global
./manage-sessions.sh clean bryan

# Eliminar TODAS las sesiones
./manage-sessions.sh clean-all
```

---

## 🔄 Operaciones del Nodo

| Operación | Descripción |
|-----------|-------------|
| **Connect / Show QR** | Genera código QR para vincular dispositivo |
| **Send Message** | Envía mensaje de texto a un número |
| **Check Status** | Verifica si la sesión está conectada |
| **List Sessions** | Lista todas las sesiones disponibles |
| **Delete Session** | Elimina la sesión configurada |

---

## 🧪 Verificación de Funcionamiento

### Verificar que el nodo carga:
```bash
sudo docker compose logs n8n 2>&1 | grep -i whats
# Debe mostrar: "No codex available for: whatsMeow" (esto es normal)
```

### Verificar sesión desde terminal:
```bash
sudo docker exec n8n /home/node/.n8n/custom/n8n-nodes-whatsmeow/nodes/WhatsMeow/bin/whatsapp-cli-linux \
  --session-dir /home/node/.n8n/binaryData/whatsmeow \
  --action check-status
```

### Generar QR desde terminal:
```bash
sudo docker exec n8n /home/node/.n8n/custom/n8n-nodes-whatsmeow/nodes/WhatsMeow/bin/whatsapp-cli-linux \
  --session-dir /home/node/.n8n/binaryData/whatsmeow \
  --action login \
  --wait-after-qr \
  --timeout 120
```

---

## ⚠️ Notas Importantes

1. **Reiniciar n8n después de cambios:**
   ```bash
   cd /home/bryan/docker/n8n && sudo docker compose restart n8n
   ```

2. **El QR expira en ~60 segundos.** Usa el servidor HTTP para escanear rápido.

3. **La sesión se guarda en SQLite.** El archivo `whatsmeow.db` contiene las credenciales.

4. **Si el nodo no aparece:**
   - Verificar que el volumen está montado correctamente
   - Revisar logs: `sudo docker compose logs n8n | grep -i error`
   - Asegurar que `package.json` tiene `n8nNodesApiVersion: 1`

5. **Si da error "Class could not be found":**
   - Verificar que `exports.WhatsMeow = WhatsMeow;` está al final del archivo
   - Verificar que el nombre de la clase coincide con lo exportado

---

## 📂 Archivos Importantes

| Archivo | Propósito |
|---------|-----------|
| `/home/bryan/docker/whatsmeow-bundle/nodes/WhatsMeow/WhatsMeow.node.js` | Código del nodo |
| `/home/bryan/docker/whatsmeow-bundle/package.json` | Manifiesto n8n |
| `/home/bryan/docker/n8n/docker-compose.yml` | Configuración Docker |
| `/home/node/.n8n/binaryData/whatsmeow/whatsmeow.db` | Base de datos de sesión (dentro del contenedor) |

---

## 🔗 Dependencias

- **n8n:** 2.1.4-arm64
- **Go:** Para compilar el CLI (whatsmeow v0.0.0-20251217143725-11cf47c62d32)
- **musl-gcc:** Para compilación estática compatible con Alpine
- **SQLite3:** Almacenamiento de sesiones

---

## 👤 Autor

Desarrollado para Raspberry Pi 4 (ARM64) con n8n en Docker.

Última actualización: 30 de diciembre de 2025
