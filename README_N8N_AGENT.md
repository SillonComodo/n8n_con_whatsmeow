# 🤖 INSTRUCCIONES PARA AGENTES DE IA - n8n + WhatsMeow

**⚠️ LEER ANTES DE HACER CUALQUIER CAMBIO EN DOCKER ⚠️**

Este archivo contiene instrucciones críticas para agentes de IA que trabajen con este proyecto.
Los datos de n8n (workflows, credenciales, usuario) y las sesiones de WhatsApp son **PERSISTENTES** 
y se guardan en volúmenes Docker. Si no sigues estas instrucciones, **SE PERDERÁN LOS DATOS**.

---

## 🚨 RESUMEN RÁPIDO - LEER PRIMERO

### ✅ Para actualizar contenedores SIN perder datos:
```bash
cd /home/bryan/docker/n8n

# SEGURO - Reconstruir y reiniciar (mantiene volúmenes)
sudo docker-compose up --build -d

# SEGURO - Solo reiniciar
sudo docker-compose restart

# SEGURO - Detener y volver a iniciar
sudo docker-compose down && sudo docker-compose up -d
```

### ❌ NUNCA usar estos comandos:
```bash
# ❌ DESTRUYE TODOS LOS DATOS
sudo docker-compose down -v
sudo docker-compose down --volumes
sudo docker volume prune
sudo docker system prune -a --volumes
```

### 🔄 Si se borraron los workflows - RESTAURAR BACKUP:
```bash
# 1. Ver backups disponibles
ls -lht /home/bryan/n8n-backups/n8n-database-*.sqlite | head -5

# 2. Detener n8n
cd /home/bryan/docker/n8n && sudo docker-compose down

# 3. Restaurar el backup más reciente
sudo cp /home/bryan/n8n-backups/n8n-database-[MAS_RECIENTE].sqlite \
  /var/lib/docker/volumes/n8n_n8n_data/_data/database.sqlite

# 4. Reiniciar
sudo docker-compose up -d
```

---

## 🛡️ SISTEMA DE BACKUPS AUTOMÁTICOS (PROTEGIDO)

**Los backups se guardan FUERA de Docker en `/home/bryan/n8n-backups/`**
Incluso si se borran los volúmenes de Docker, los backups están protegidos.

```
/home/bryan/n8n-backups/
├── backup-n8n.sh              # Script de backup (NO MODIFICAR)
├── restore-n8n.sh             # Script de restauración
├── backup.log                 # Log de backups
├── cron.log                   # Log del cron
├── n8n-database-*.sqlite      # Backups de n8n (últimos 30 días)
└── whatsmeow-sessions-*.tar.gz # Backups de WhatsApp
```

### ⏰ Backup Automático
- **Frecuencia:** Cada hora (minuto 0)
- **Retención:** 30 días
- **Cron:** `/etc/cron.d/n8n-backup` (ejecuta como root)
- **Comando:** `0 * * * * root /home/bryan/n8n-backups/backup-n8n.sh`

### 🔄 Restaurar un Backup
```bash
# Ver backups disponibles (ordenados por fecha)
ls -lht /home/bryan/n8n-backups/n8n-database-*.sqlite | head -10

# MÉTODO RÁPIDO - Copiar directamente al volumen:
cd /home/bryan/docker/n8n
sudo docker-compose down
sudo cp /home/bryan/n8n-backups/n8n-database-XXXXXXXX_XXXXXX.sqlite \
  /var/lib/docker/volumes/n8n_n8n_data/_data/database.sqlite
sudo docker-compose up -d

# MÉTODO CON SCRIPT (alternativo):
/home/bryan/n8n-backups/restore-n8n.sh /home/bryan/n8n-backups/n8n-database-FECHA.sqlite
```

### 📋 Backup Manual
```bash
/home/bryan/n8n-backups/backup-n8n.sh
```

### ⚠️ IMPORTANTE PARA AGENTES DE IA
- **NUNCA borrar** `/home/bryan/n8n-backups/`
- **NUNCA modificar** los scripts de backup sin autorización
- **NUNCA eliminar** archivos `.sqlite` o `.tar.gz` de ese directorio
- Si hay un desastre, los backups están ahí para restaurar

---

## 📦 Arquitectura de Volúmenes

```
docker-compose.yml define estos volúmenes:

volumes:
  n8n_data:        # ← Contiene TODOS los datos de n8n (workflows, credenciales, usuario)
  whatsmeow_data:  # ← Contiene las sesiones de WhatsApp (NO requieren re-escanear QR)
```

**Ubicación de los volúmenes en el host:**
```bash
# Ver volúmenes
sudo docker volume ls | grep n8n

# Los datos están en:
# /var/lib/docker/volumes/n8n_n8n_data/_data/
# /var/lib/docker/volumes/n8n_whatsmeow_data/_data/
```

---

## ✅ COMANDOS SEGUROS (Usar estos)

### Actualizar código sin perder datos:
```bash
cd /home/bryan/docker/n8n

# Reconstruir y reiniciar (SEGURO - mantiene volúmenes)
sudo docker-compose up --build -d

# Solo reiniciar contenedores (SEGURO)
sudo docker-compose restart

# Reiniciar un contenedor específico (SEGURO)
sudo docker restart n8n
sudo docker restart whatsmeow

# Detener y volver a iniciar (SEGURO - mantiene volúmenes)
sudo docker-compose down
sudo docker-compose up -d

# Ver logs (SEGURO)
sudo docker logs n8n
sudo docker logs whatsmeow
```

### Copiar archivos actualizados al contenedor:
```bash
# Para actualizar nodos de n8n, los archivos están montados desde el host
# Ubicación: /home/bryan/docker/whatsmeow-bundle/nodes/WhatsMeow/

# Solo necesitas reiniciar n8n después de editar:
sudo docker restart n8n
```

---

## ❌ COMANDOS PELIGROSOS (NUNCA USAR)

```bash
# ❌ NUNCA - Elimina TODOS los volúmenes (workflows, credenciales, sesiones WhatsApp)
sudo docker-compose down -v
sudo docker-compose down --volumes

# ❌ NUNCA - Elimina volúmenes huérfanos incluyendo los de n8n
sudo docker system prune -a --volumes
sudo docker volume prune

# ❌ NUNCA - Eliminar volúmenes específicos
sudo docker volume rm n8n_n8n_data
sudo docker volume rm n8n_whatsmeow_data

# ❌ NUNCA - Eliminar contenedores con volúmenes
sudo docker rm -v n8n
sudo docker rm -v whatsmeow

# ❌ NUNCA - Borrar el directorio de backups
rm -rf /home/bryan/n8n-backups/
rm /home/bryan/n8n-backups/*.sqlite
rm /home/bryan/n8n-backups/*.tar.gz

# ❌ NUNCA - Modificar los scripts de backup sin autorización
nano /home/bryan/n8n-backups/backup-n8n.sh
```

---

## 🔄 Proceso Correcto de Actualización

### 1. Actualizar código del nodo WhatsMeow:
```bash
# 1. Editar archivos en:
#    /home/bryan/docker/whatsmeow-bundle/nodes/WhatsMeow/WhatsMeow.node.js
#    /home/bryan/docker/whatsmeow-bundle/nodes/WhatsMeow/WhatsMeowTrigger.node.js

# 2. Reiniciar n8n para cargar cambios:
sudo docker restart n8n
```

### 2. Actualizar código del servidor Go (whatsmeow):
```bash
# 1. Editar archivos en:
#    /home/bryan/docker/whatsmeow-bundle/server/main.go

# 2. Reconstruir y reiniciar:
cd /home/bryan/docker/n8n
sudo docker-compose up --build -d
```

### 3. Actualizar docker-compose.yml:
```bash
# 1. Editar /home/bryan/docker/n8n/docker-compose.yml

# 2. Aplicar cambios:
cd /home/bryan/docker/n8n
sudo docker-compose up -d
```

---

## 💾 Backup (Antes de cambios grandes)

### Backup rápido:
```bash
cd /home/bryan/docker/n8n

# Backup de n8n (workflows, credenciales)
sudo docker run --rm \
  -v n8n_n8n_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/n8n-backup-$(date +%Y%m%d).tar.gz -C /data .

# Backup de sesiones WhatsApp
sudo docker run --rm \
  -v n8n_whatsmeow_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/whatsmeow-backup-$(date +%Y%m%d).tar.gz -C /data .
```

### Restaurar backup:
```bash
cd /home/bryan/docker/n8n
sudo docker-compose down

# Restaurar n8n
sudo docker run --rm \
  -v n8n_n8n_data:/data \
  -v $(pwd):/backup \
  alpine sh -c "rm -rf /data/* && tar xzf /backup/n8n-backup-FECHA.tar.gz -C /data"

# Restaurar whatsmeow
sudo docker run --rm \
  -v n8n_whatsmeow_data:/data \
  -v $(pwd):/backup \
  alpine sh -c "rm -rf /data/* && tar xzf /backup/whatsmeow-backup-FECHA.tar.gz -C /data"

sudo docker-compose up -d
```

---

## 📁 Estructura de Archivos

```
/home/bryan/docker/
├── n8n/
│   ├── docker-compose.yml          # Configuración de Docker
│   └── local-files/                # Archivos accesibles desde n8n
│
└── whatsmeow-bundle/
    ├── server/
    │   ├── main.go                 # Servidor Go de WhatsApp
    │   ├── go.mod
    │   └── Dockerfile
    │
    └── nodes/
        └── WhatsMeow/
            ├── WhatsMeow.node.js       # Nodo principal
            ├── WhatsMeowTrigger.node.js # Trigger de mensajes
            ├── whatsapp.svg
            └── bin/
                └── whatsapp-cli-linux   # CLI compilado
```

---

## 🔧 Montajes de Volúmenes en docker-compose.yml

```yaml
services:
  n8n:
    volumes:
      - n8n_data:/home/node/.n8n                                    # Datos persistentes
      - ./local-files:/files                                        # Archivos compartidos
      - /home/bryan/docker/whatsmeow-bundle:/home/node/.n8n/custom/n8n-nodes-whatsmeow  # Código del nodo

  whatsmeow:
    volumes:
      - whatsmeow_data:/data/sessions                               # Sesiones WhatsApp
```

**Nota importante:** El directorio `whatsmeow-bundle` está montado directamente (bind mount),
por lo que los cambios en el código del nodo se reflejan inmediatamente después de reiniciar n8n.

---

## 🚨 Si Algo Sale Mal

### n8n no inicia:
```bash
sudo docker logs n8n 2>&1 | tail -50
```

### Workflows desaparecieron:
```bash
# 1. Verificar que el volumen existe
sudo docker volume ls | grep n8n_data

# 2. Si existe, reiniciar
sudo docker-compose down
sudo docker-compose up -d

# 3. Si no existe, restaurar desde backup
```

### Sesiones de WhatsApp perdidas:
```bash
# Verificar volumen
sudo docker volume ls | grep whatsmeow_data

# Las sesiones se pueden re-crear escaneando QR
# En n8n: Nodo WhatsMeow > Operation: Connect / Show QR
```

---

## 📋 Checklist para Agentes de IA

Antes de ejecutar cualquier comando Docker:

- [ ] ¿El comando incluye `-v` o `--volumes`? → **NO EJECUTAR**
- [ ] ¿El comando incluye `prune`? → **VERIFICAR QUE NO AFECTE VOLÚMENES**
- [ ] ¿El comando es `docker-compose down`? → **OK, pero sin `-v`**
- [ ] ¿El comando es `docker-compose up --build -d`? → **OK, SEGURO**
- [ ] ¿El comando es `docker restart`? → **OK, SEGURO**
- [ ] ¿El comando borra algo en `/home/bryan/n8n-backups/`? → **NO EJECUTAR**
- [ ] ¿El comando modifica scripts de backup? → **PEDIR CONFIRMACIÓN**

### 🚨 SI OCURRE UN DESASTRE (workflows borrados)
```bash
# 1. Ver backups disponibles
ls -lh /home/bryan/n8n-backups/n8n-database-*.sqlite

# 2. Restaurar el más reciente
/home/bryan/n8n-backups/restore-n8n.sh /home/bryan/n8n-backups/[ARCHIVO_MAS_RECIENTE].sqlite
```

---

## 🔑 Resumen Ejecutivo

| Acción | Comando Correcto |
|--------|------------------|
| Actualizar nodo JS | Editar archivo + `sudo docker restart n8n` |
| Actualizar servidor Go | `sudo docker-compose up --build -d` |
| Reiniciar todo | `sudo docker-compose restart` |
| Aplicar cambios docker-compose | `sudo docker-compose up -d` |
| Ver logs | `sudo docker logs n8n` o `sudo docker logs whatsmeow` |

**RECORDAR:** Los datos viven en los volúmenes. Mientras no elimines los volúmenes, los datos están seguros.
**BACKUP:** Hay backups automáticos cada hora en `/home/bryan/n8n-backups/`
**IMPORTANTE:** El docker-compose tiene `HOME=/home/node` - NO ELIMINAR esta variable o n8n usará /root/.n8n y perderá los datos.

---

*Última actualización: Enero 2026*
