# 🔐 Credenciales PostgreSQL para n8n Chat Memory

## Datos de Conexión

| Campo | Valor |
|-------|-------|
| **Host** | `n8n-postgres` |
| **Port** | `5432` |
| **Database** | `n8n_memory` |
| **User** | `n8n` |
| **Password** | `n8n_password_seguro` |
| **SSL** | `Disable` |

---

## 📋 Configuración en n8n

### Paso 1: Crear Credencial

1. Ve a **Settings** → **Credentials** → **Add Credential**
2. Busca **"Postgres"**
3. Llena los campos:

```
Host:     n8n-postgres
Database: n8n_memory
User:     n8n
Password: n8n_password_seguro
Port:     5432
SSL:      Disable
```

4. Click en **Save**

### Paso 2: Usar en Postgres Chat Memory

En el nodo **Postgres Chat Memory**:

| Campo | Valor |
|-------|-------|
| **Credential** | La credencial que creaste |
| **Session ID** | `{{ $json.sessionId }}` o `{{ $json.from }}` |
| **Table Name** | `n8n_chat_histories` |
| **Context Window Length** | `5` (mensajes previos) |

---

## 🔌 Conexión desde Terminal (para debugging)

```bash
# Conectar al contenedor
sudo docker exec -it n8n-postgres psql -U n8n -d n8n_memory

# Ver tablas
\dt

# Ver historial de chats
SELECT * FROM n8n_chat_histories LIMIT 10;

# Salir
\q
```

---

## 🧹 Limpiar Historial de Conversaciones

```bash
# Borrar TODO el historial
sudo docker exec n8n-postgres psql -U n8n -d n8n_memory -c "DELETE FROM n8n_chat_histories;"

# Borrar historial de una sesión específica
sudo docker exec n8n-postgres psql -U n8n -d n8n_memory -c "DELETE FROM n8n_chat_histories WHERE session_id = 'TU_SESSION_ID';"
```

---

## ⚠️ Seguridad

- Estas credenciales son solo para uso **interno** en Docker
- PostgreSQL NO está expuesto a internet (sin puerto público)
- Solo los contenedores en la misma red Docker pueden conectarse

---

*Generado: 18 de enero de 2026*
