# LoomPanel

Panel web open source para crear y administrar servidores de juegos desde el navegador. Fase 1: **Minecraft Java**.
Un solo binario en Go + frontend React embebido + SQLite. Cada servidor corre aislado en su propio contenedor Docker.

## Instalar (VPS Linux, como root)

```bash
git clone <URL-DEL-REPO> loompanel && cd loompanel
sudo bash install.sh
```

El instalador detecta el SO, instala Docker si falta, te pregunta cuánta RAM, CPU y disco dedicar a los juegos,
opcionalmente un dominio (HTTPS automático con Caddy) y te muestra la URL y un **código de instalación** de un solo uso
para crear la cuenta de administrador. Instalación manual: copia `.env.example` a `.env`, edítalo y ejecuta `docker compose up -d --build`.

## Instalación rápida (one-liner)

```bash
curl -sSL https://raw.githubusercontent.com/joacoolopezz/LoomPanel/main/install.sh | sudo bash
```

Requiere: Linux (Ubuntu/Debian/CentOS/Fedora/Arch), root/sudo, conexión a internet.

## Qué incluye

**Fase 1**
- Login, primer arranque con código de instalación, **registro solo por invitación** (enlace de un solo uso, 7 días)
- Roles admin / moderador / solo lectura, y acceso por servidor
- Crear servidores con Java 8/17/21 elegido según la versión, RAM, puerto libre y `server.properties` iniciales según el hardware, flags de Aikar
- Iniciar, detener, reiniciar, forzar cierre; reinicio automático ante caídas (3 intentos)
- Consola en tiempo real con comandos e historial; CPU/RAM en vivo; jugadores conectados y gestión (whitelist, ban, kick, op)
- Explorador de archivos: subir (arrastrar y soltar), descargar (carpetas en zip), editar, crear, renombrar, eliminar, descargar desde un enlace
- Redes privadas entre servidores, registro de actividad, interfaz es/en, menú colapsable y movible, hamburguesa en móvil

**Fase 2**
- Tipos de servidor: Paper, Purpur, Vanilla, Fabric y, **en beta**, Quilt, Forge (1.17+) y NeoForge (el instalador oficial corre dentro de un contenedor con el Java correcto)
- Mods y plugins: buscador de **Modrinth** filtrado por versión y loader de tu servidor, instalación en un clic con verificación SHA-512 y dependencias obligatorias, activar/desactivar, quitar y buscar actualizaciones
- **Copias de seguridad** locales (tar.gz): manuales y programadas, retención, descarga, restauración (con copia automática previa) y el mundo se pausa y guarda (`save-off`/`save-all`) mientras se copia
- **Tareas programadas** por servidor: iniciar, detener, reiniciar, enviar comando y copia, con horarios simples o cron personalizado

**Fase 3**
- **Verificación en dos pasos (2FA)** con app autenticadora (TOTP, con código QR), 8 códigos de recuperación de un solo uso y protección contra reutilización y fuerza bruta; un admin puede quitarla a un usuario bloqueado
- **Tokens de API** (`Authorization: Bearer gp_...`) con alcance completo o solo lectura y caducidad opcional; heredan los permisos de tu usuario y no pueden crear más tokens
- **Notificaciones** por Discord, Telegram, webhook firmado (HMAC-SHA256) y email (SMTP) cuando un servidor se inicia, se detiene, se cae, o una copia termina o falla. Las credenciales se guardan cifradas (AES-GCM)
- **Dominios**: asistente que explica y genera los registros DNS (A y SRV), comprueba si ya resolvieron y, con un token de Cloudflare, los crea automáticamente
- Los avisos usan `GP_LANG` (`es` o `en`)

## Aún no incluido

CurseForge y Hangar, modpacks (.mrpack), copias a S3/SFTP, Forge anterior a 1.17, login con Discord/Google/GitHub, roles personalizados,
**multi-VPS con agentes** (requiere separar el panel de la capa que habla con Docker), Dynmap y estadísticas, app de escritorio.

## Seguridad: lo que debes saber

- El panel necesita acceso al socket de Docker, que equivale a **root en el servidor**. Protege las cuentas de admin con contraseñas largas.
- Contraseñas con Argon2id, 2FA opcional, tokens de API guardados solo como hash; sesiones en cookie HttpOnly + SameSite=Strict; cabecera anti-CSRF en toda escritura; límite de intentos de login.
- Explorador de archivos confinado a la carpeta de cada servidor (también contra symlinks). La descarga por enlace bloquea direcciones privadas/locales (SSRF).
- Contenedores de juego: `--cap-drop ALL`, `no-new-privileges`, límites de memoria y procesos.
- Sin dominio el panel va por HTTP sin cifrar: usa un dominio en cuanto puedas.
- Los límites de RAM y CPU se aplican; el de disco hoy es informativo.

## Desarrollo

```bash
cd frontend && npm install && npm run dev        # http://localhost:5173 (proxy a :8080)
cd backend && GP_DATA=./data GP_SETUP_TOKEN=dev go run .   # necesita Docker para iniciar servidores
npm run build                                    # deja el frontend en backend/web (se embebe en el binario)
```

Licencia: AGPL-3.0.
