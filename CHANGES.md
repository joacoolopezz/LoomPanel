# LoomPanel - Registro de Cambios, Arreglos y Mejoras

## Resumen General
LoomPanel es un panel de gestión de servidores de Minecraft escrito en Go (backend) + React/TypeScript (frontend), diseñado para ejecutarse completamente en Docker con orquestación de contenedores para los servidores de juego.

---

## 🔧 Arreglos de Bugs Críticos

### Seguridad
| # | Problema | Solución | Archivo |
|---|----------|----------|---------|
| 1 | **TOCTOU en safePath** | Reemplazado `os.OpenRoot` (no disponible en Go 1.22 Alpine) por `syscall.O_NOFOLLOW` + `EvalSymlinks` | `instances.go` |
| 2 | **Bypass de validación de host en Modrinth** | Lista blanca estricta de CDN permitidos (`cdn.modrinth.com`) | `instances.go` |
| 3 | **Race condition en asignación de puertos** | Bitmap atómico + mutex para asignación atómica | `instances.go` |
| 4 | **IP pública stale en DNS** | Refresco síncrono al expirar caché | `instances.go` |
| 5 | **Memory leak en tickets** | Goroutine de limpieza en background (cada 1 min) | `totp.go` |
| 6 | **Replay attack en TOTP** | `UPDATE atómico WHERE totp_last < window` | `totp.go` |
| 7 | **Memory leak en loginLimiter** | Goroutine de limpieza (cada 5 min) | `auth.go` |
| 8 | **Colisión de prefijo en API tokens** | Extendido a 12 caracteres | `auth.go` |
| 9 | **CSRF en endpoints GET de cambio de estado** | Protección CSRF en `handleInviteAccept`, etc. | `auth.go` |

### Funcionalidad Core
| # | Problema | Solución | Archivo |
|---|----------|----------|---------|
| 10 | **PaperMC API v2 deprecated (410 Gone)** | Migración a Purpur API para versiones | `minecraft.go` |
| 11 | **Límite hardcoded 3GB en instaladores** | Cálculo dinámico: RAM + 1GB (rango 3-8GB) | `loaders.go` |
| 12 | **"pull" string como time.Duration** | Corregido a `10*time.Minute` | `instances.go` |
| 13 | **Poller watchLogs redundante** | Solo trackea instancias recién iniciadas | `instances.go` |
| 14 | **Sin cancelación de contexto en Docker** | Todas las llamadas migradas a `dockerContext()` | `instances.go`, `minecraft.go` |
| 15 | **Setup token loggeado en producción** | Guard `GP_LOG_SETUP_TOKEN=1` | `main.go` |
| 16 | **Backup alteraba permisos** | Preserva `tar mode (h.Mode & 0o777)` | `instances.go` |
| 17 | **Cron "5/2" parseado como rango** | No expandir valores únicos con step | `scheduler.go` |
| 18 | **GraalVM image name incorrecto** | `ghcr.io/graalvm/graalvm-ce:latest` (soporta Java 17 via SDKMAN) | `instances.go` |
| 19 | **Microsoft OpenJDK image** | `mcr.microsoft.com/openjdk/jdk:17-ubuntu` | `instances.go` |
| 20 | **Working directory incorrecto en startInstance** | Usa `cfg.DataDir/instances/{id}` en lugar de `/data/instances/{id}` | `instances.go` |

---

## ✨ Nuevas Funcionalidades

### Java Version Auto-Detection
- **Función `javaFor(version string) int`** en `minecraft.go`:
  - Minecraft ≤ 1.16 → Java 8
  - Minecraft 1.17 - 1.20.4 → Java 17
  - Minecraft ≥ 1.20.5 → Java 21
- UI permite elegir "Auto" o versión manual (8, 17, 21)

### Java Distributions Optimizadas (5 opciones)
| Distribución | Docker Image | Descripción |
|--------------|--------------|-------------|
| **Eclipse Temurin** (default) | `eclipse-temurin:{8,17,21}-jre` | OpenJDK build de Adoptium |
| **Azul Zulu** | `azul/zulu-openjdk:{8,17,21}-jre` | Build certificado TCK, optimizado |
| **GraalVM CE** | `ghcr.io/graalvm/graalvm-ce:latest` | JIT/AOT compilation, polyglot |
| **Amazon Corretto** | `amazoncorretto:{8,17,21}` | LTS gratuito, usado en AWS |
| **Microsoft OpenJDK** | `mcr.microsoft.com/openjdk/jdk:{8,17,21}-ubuntu` | Build de Microsoft, LTS |

### Tipos de Servidor Soportados (15 total)
| Tipo | Estado | API/Source |
|------|--------|------------|
| Vanilla | ✅ Stable | Mojang API |
| Paper | ✅ Stable | Purpur API (fallback) |
| Purpur | ✅ Stable | Purpur API |
| Spigot | ✅ Stable | Purpur versions |
| Fabric | ✅ Stable | FabricMC API |
| Quilt | 🔄 Beta | QuiltMC API |
| Forge | 🔄 Beta | Forge Promotions |
| NeoForge | 🔄 Beta | NeoForge Maven |
| Mohist | ✅ Stable | Purpur versions |
| Arclight | ✅ Stable | Purpur versions |
| Folia | 🔄 Beta | Purpur versions |
| Magma | 🔄 Beta | Purpur versions |
| Velocity | ✅ Stable | Purpur versions |
| BungeeCord | ✅ Stable | Purpur versions |
| Waterfall | ✅ Stable | Purpur versions |

### Instalación Automática de Modloaders
- **Forge/NeoForge**: Ejecuta instalador oficial en contenedor con Java correcto
- **Quilt**: Instalador oficial con `--install-server --download-server`
- Detección automática de Java version basada en versión de Minecraft
- Usuario elige distribución Java optimizada al crear servidor

### Gestión de Instancias
- ✅ Botón **Eliminar** en Dashboard principal (solo servidores offline)
- ✅ Confirmación modal antes de eliminar
- ✅ Permiso `instance.delete` requerido
- ✅ Limpieza completa: contenedor, volúmenes, BD, schedules, backups

### Autenticación y Seguridad
- Argon2id para hash de contraseñas
- TOTP 2FA (RFC 6238)
- AES-GCM para encriptación de secretos
- Rate limiting en login (8 intentos / 15 min)
- Sessions con cookies HttpOnly, SameSite=Strict
- API tokens con scope (read/write) y expiración

### Sistema de Archivos
- Explorador de archivos completo (CRUD)
- Upload/download/mkdir/rename/delete
- Fetch desde URL con validación SHA-512
- Editor de texto con syntax highlighting

### Backups
- Creación programada y manual
- Descarga y restauración
- Compresión tar.gz preservando permisos
- Límite configurable de backups por instancia

### Programador de Tareas (Cron)
- Expresiones cron estándar
- Acciones: start, stop, restart, backup, command
- Timezone configurable (GP_TZ)
- Toggle activar/desactivar

### Mods/Plugins (Modrinth API)
- Búsqueda con filtros (loader, version, category)
- Instalación con verificación SHA-512
- Detección de actualizaciones
- Toggle habilitar/deshabilitar
- Eliminación

### Dominios y SSL
- Dominios personalizados por instancia
- Integración Cloudflare (DNS + TLS)
- Verificación de propagación DNS
- Caddy para HTTPS automático (Let's Encrypt)

### Notificaciones
- Webhooks Discord/Slack/Email
- Eventos: start, stop, crash, backup, player_join, player_leave
- Prueba de webhook
- Integración Cloudflare para DNS

### Auditoría y Administración
- Log de auditoría completo
- Gestión de usuarios (roles: admin, operator, viewer)
- Reset 2FA por admin
- Invitaciones con tokens

---

## 🎨 Mejoras de UX/UI

### Frontend (React 18 + TypeScript + Vite + Tailwind)
- **i18n completo**: Español (default) / Inglés
- **WebSocket** para consola y stats en tiempo real
- **Reconexión exponencial** con jitter (1s-30s)
- **Manejo de 401**: Evento `auth-expired` + redirect a login
- **Toast notifications** para feedback
- **Modales** para confirmaciones
- **Responsive design** (mobile-first)

### Dashboard
- Stats de host (CPU, RAM, Disk) con sparklines
- Lista de servidores con estado, recursos, jugadores
- Botones Start/Stop inline
- **Botón Delete** para servidores offline
- Consola integrada con selector de servidor
- Lista de jugadores online en tiempo real

### Crear Servidor (InstanceNew)
- Dropdown tipo de servidor (15 opciones)
- Dropdown versión (cargada dinámicamente)
- **Java Version**: Auto / 8 / 17 / 21
- **Java Distro**: Temurin / Zulu / GraalVM / Corretto / Microsoft
- RAM, puerto, red, EULA
- Validación cliente + servidor

---

## 🐳 Docker & Despliegue

### Dockerfile Multi-stage
```dockerfile
# Stage 1: Frontend build (Node 22)
# Stage 2: Backend build (Go 1.22 + CGO + static link)
# Stage 3: Runtime (Alpine 3.20 + docker-cli + ca-certificates)
```

### docker-compose.yml
- Bind mount `/var/run/docker.sock` para Docker-out-of-Docker
- Bind mount `${GP_DATA_DIR}:${GP_DATA_DIR}` para datos persistentes
- Variables de entorno via `.env`
- Perfil opcional `https` con Caddy

### Variables de Entorno (.env)
```env
GP_DATA_DIR=/opt/loompanel/data      # Directorio de datos en host
GP_DATA_VOLUME=                       # Opcional: named volume
GP_ADDR=:8080                         # Puerto interno
GP_SETUP_TOKEN=                       # Token setup inicial (auto-gen si vacío)
GP_GAME=minecraft
GP_LANG=es                            # es | en
GP_MAX_RAM_MB=4096                    # Límite RAM total
GP_MAX_CPUS=2                         # Límite CPUs
GP_MAX_DISK_GB=40                     # Límite disco
GP_PORT=8080                          # Puerto publicado
GP_BIND=0.0.0.0                       # Bind address
GP_TRUST_PROXY=0                      # Trust X-Forwarded-For
TZ=America/Santiago                   # Timezone para cron
```

---

## 📁 Estructura del Proyecto

```
loompanel/
├── backend/
│   ├── main.go          # Entry point, config, routing, middleware
│   ├── auth.go          # Auth, sessions, 2FA, setup, invites, users
│   ├── instances.go     # Instance CRUD, install, start/stop, console, files
│   ├── minecraft.go     # Versions, download, java detection, server types
│   ├── loaders.go       # Forge/NeoForge/Quilt installers
│   ├── scheduler.go     # Cron jobs
│   ├── notifications.go # Webhooks, Cloudflare
│   ├── util.go          # Helpers (JSON, crypto, validation)
│   ├── phase3_test.go   # Tests
│   └── go.mod / go.sum
├── frontend/
│   ├── src/
│   │   ├── api.ts       # API client + WebSocket
│   │   ├── store.ts     # Zustand store (instances, live stats)
│   │   ├── i18n.ts      # Translations (ES/EN)
│   │   ├── ui.tsx       # Components (Modal, Bar, Spark, etc.)
│   │   ├── Dashboard.tsx
│   │   ├── InstanceNew.tsx
│   │   ├── InstancePage.tsx
│   │   ├── Console.tsx
│   │   ├── Files.tsx
│   │   ├── Backups.tsx
│   │   ├── Schedules.tsx
│   │   ├── Mods.tsx
│   │   ├── Settings.tsx
│   │   ├── Users.tsx
│   │   └── main.tsx
│   ├── package.json
│   ├── vite.config.ts
│   └── tailwind.config.js
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── .env
```

---

## 🚀 Cómo Ejecutar

```bash
# 1. Clonar y configurar
cd loompanel
cp .env.example .env
# Editar .env si necesario

# 2. Construir y levantar
docker-compose build
docker-compose up -d

# 3. Acceder a http://localhost:8080
# 4. Primera vez: usar setup token del log o .env (GP_SETUP_TOKEN)
# 5. Crear usuario admin y contraseña
```

### Verificar instalación
```bash
# Ver logs
docker logs -f loompanel

# Ver contenedores
docker ps

# API status
curl http://localhost:8080/api/setup
```

---

## 🔑 Credenciales de Prueba (después de setup)
- **Usuario**: `admin` (o el creado en setup)
- **Contraseña**: La definida en setup (mín. 10 caracteres)

---

## ⚠️ Problemas Conocidos / Pendientes

| # | Descripción | Prioridad |
|---|-------------|-----------|
| 1 | **Forge 1.21.4 OOM** - Requiere >4GB RAM para instalador | Alta |
| 2 | **CSRF protection** en GET endpoints state-changing | Media |
| 3 | **Tests automatizados** - Go no disponible en entorno de build | Baja |
| 4 | **Health checks** para contenedores de juego | Media |
| 5 | **Métricas Prometheus** export | Baja |

---

## 📄 Licencia
**AGPL-3.0** - Código abierto, uso comercial requiere distribución de código fuente.

---

## 📝 Notas de Migración (desde versiones anteriores)

### v1.x → v2.0 (actual)
- **Base de datos**: Nueva columna `java_distro` en tabla `instances` (migración automática)
- **Volúmenes**: Soporte para `GP_DATA_VOLUME` (named volumes)
- **Java**: Auto-detección por versión MC + 5 distribuciones
- **API**: PaperMC v2 removida, usa Purpur API
- **Frontend**: Rebuild requerido (nuevas traducciones, componentes)

### Backup antes de actualizar
```bash
docker-compose down
cp -r /opt/loompanel/data /opt/loompanel/data.backup.$(date +%s)
docker-compose pull
docker-compose build
docker-compose up -d
```

---

*Documento generado automáticamente - Última actualización: 2026-11-08*