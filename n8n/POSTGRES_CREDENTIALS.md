# Credenciales PostgreSQL para n8n

## Conexión desde n8n

### Para "Postgres Chat Memory" Node (Memoria de Agente IA)
| Campo     | Valor                      |
|-----------|----------------------------|
| Host      | `n8n-postgres`             |
| Port      | `5432`                     |
| Database  | `n8n_memory`               |
| User      | `n8n`                      |
| Password  | `n8n_password_seguro`      |
| SSL       | `Disabled`                 |

### Para "Postgres PGVector Store" Node (RAG - Vector Store)
| Campo      | Valor                      |
|------------|----------------------------|
| Host       | `n8n-postgres`             |
| Port       | `5432`                     |
| Database   | `n8n_memory`               |
| User       | `n8n`                      |
| Password   | `n8n_password_seguro`      |
| Table Name | `document_vectors`         |

**Nota:** La extensión `pgvector` (v0.8.1) ya está habilitada.

---

## Conexión Externa (desde tu PC u otras herramientas)

| Campo     | Valor                      |
|-----------|----------------------------|
| Host      | `IP_de_tu_Raspberry_Pi`    |
| Port      | `5432`                     |
| Database  | `n8n_memory`               |
| User      | `n8n`                      |
| Password  | `n8n_password_seguro`      |

---

## Cómo eliminar historial de chat

```sql
-- Borrar memoria de un session_id específico
DELETE FROM n8n_chat_histories WHERE session_id = 'tu_session_id';

-- Borrar toda la memoria de chat
TRUNCATE TABLE n8n_chat_histories;
```

---

## Cómo eliminar vectores (para RAG)

```sql
-- Borrar todos los vectores de una tabla
TRUNCATE TABLE document_vectors;

-- O borrar la tabla completamente
DROP TABLE IF EXISTS document_vectors;
```

---

## Funcionalidades disponibles

- ✅ **Postgres Chat Memory**: Memoria persistente para agentes IA
- ✅ **PGVector Store**: Almacenamiento de vectores para RAG
- ✅ **Soporte de embeddings**: Compatible con OpenAI, Cohere, etc.
