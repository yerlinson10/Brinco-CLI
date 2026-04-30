# brinco-cli

> Chat de consola en tiempo real: modos **p2p** y **guaranteed** (libp2p / IPFS, sin servidor tuyo) y modos **relay** y **direct** (sala TCP con control: moderacion, rate limit por conexion, etc.).
> Cada sala usa un **codigo de invitacion** con prefijo de protocolo; unirse es pegar el codigo.

Brinco-CLI esta escrito en Go y usa [libp2p](https://libp2p.io/) (la misma red que IPFS) para conexiones entre pares cuando aplica.

---

## Tabla de contenidos

1. [Panorama](#1-panorama) — que es, requisitos, funciones  
2. [Instalacion](#2-instalacion) — fuente, PATH, paquetes  
3. [Primeros pasos](#3-primeros-pasos) — flujo minimo, asistente, atajos y perfiles  
4. [Modos de conexion](#4-modos-de-conexion) — p2p, guaranteed, relay, direct  
5. [Referencia CLI](#5-referencia-cli) — room, relay, update, ayuda  
6. [Chat: comandos en sala](#6-chat-comandos-en-sala) — /code, moderacion, reacciones  
7. [Logs y depuracion](#7-logs-y-depuracion)  
8. [Desarrollo y pruebas](#8-desarrollo-y-pruebas) — E2E, arquitectura  
9. [Solucion de problemas](#9-solucion-de-problemas)

---

## 1. Panorama

### Que es Brinco

Permite crear una sala desde la terminal, compartir un código y chatear. El comportamiento depende del **modo**: en p2p/guaranteed no hay un servidor de sala tuyo; en relay/direct el tráfico de la sala pasa por un proceso TCP que tú controlas (o el host de la sala).

### Requisitos

| Requisito | Version minima |
|-----------|-----------------|
| Go (solo si compilas desde fuente) | 1.25+ (ver `go.mod` del repo) |
| Terminal | ANSI recomendado (colores) |
| Red | Internet si los peers están en redes distintas (según modo) |

### Funciones principales

**Conexión y salas**

- Cuatro modos: `p2p`, `guaranteed`, `relay`, `direct`; el código lleva el protocolo (`p2p-`, `guaranteed-`, …).
- Join con `--mode auto` o sin modo: se infiere del prefijo del código.
- **Asistente**: `brinco host`, `brinco join`, `brinco room create` o `brinco room join` sin argumentos extra (Enter en password = sala sin clave en relay/direct).
- **Portapapeles**: al crear una sala (p2p, guaranteed, direct o relay cliente) el codigo se copia al portapapeles cuando el sistema lo permite (Windows `clip`, macOS `pbcopy`, Linux `wl-copy` / `xclip` / `xsel`).

**Chat**

- Privados (`/msg`, `@usuario`), reacciones (`+1`, `:emoji:`, …), archivos pequeños (`/send`).
- Mismo formato para mensajes propios y ajenos; tu línea se marca con **`(tu)`** cuando es el mismo nick que el tuyo en sala.
- **Colores**: paleta ANSI 256 sin repetir color hasta agotarla; el **host** (relay/direct) en **dorado** para todos.

**Relay / direct (servidor TCP)**

- Moderación por host: `/kick`, `/mute`, `/unmute`, `/ban`. Si el host sale, otro cliente pasa a ser host (aviso en consola).
- **Rate limit** en cliente y servidor; aviso local en **rojo** (stderr) si envías demasiado rápido.

**Terminal en sesión**

- `/clear`, `/history`, `!!` (repite la última línea del historial local).

**Otros**

- Ayuda en CLI: `brinco help`, `brinco room help`, `brinco relay help`, `brinco update help`.
- Diagnóstico: `/diag`, `/peers`; reconexión TCP con backoff donde aplica.
- P2p: cifrado de payload derivado del código de sala, huella por nick (TOFU).
- **Actualizador**: `brinco update check` / `apply` contra GitHub Releases.
- Pruebas E2E: `go test ./internal/e2e -v`.

---

## 2. Instalacion

### Desde el codigo fuente

```bash
git clone https://github.com/yerlinson10/Brinco-CLI.git
cd Brinco-CLI
go mod tidy
```

Compilar:

```bash
# Windows
go build -o bin/brinco.exe ./cmd/brinco

# Linux / macOS
go build -o bin/brinco ./cmd/brinco
```

Ejecutar sin instalar:

```bash
go run ./cmd/brinco help
brinco room help
brinco relay help
brinco update help
```

### Binario en el PATH

**Windows**

1. `go build -o bin/brinco.exe ./cmd/brinco`
2. Añade `bin` al PATH o copia `brinco.exe` a una carpeta del PATH.
3. Reinicia la terminal y comprueba: `brinco version`

**Linux / macOS**

```bash
go build -o bin/brinco ./cmd/brinco
chmod +x bin/brinco
sudo mv bin/brinco /usr/local/bin/brinco   # o otra ruta en tu PATH
brinco version
```

### Paquetes (usuario final)

| Canal | Notas |
|-------|--------|
| **Scoop (Windows)** | `scoop bucket add brinco-bucket https://github.com/yerlinson10/scoop-bucket` → `scoop install brinco` |
| **.deb / .rpm** | Descarga desde Releases; `dpkg -i` / `rpm -i` |
| **Instalador .exe** | Releases → setup; opcional “Add Brinco to PATH” |

---

## 3. Primeros pasos

### Flujo minimo (recomendado: guaranteed)

**Usuario A — crea la sala**

```bash
go run ./cmd/brinco room create --name Ana
```

Copia el codigo (ej. `guaranteed-eyJ...`).

**Usuario B — se une**

```bash
go run ./cmd/brinco room join --name Luis --code guaranteed-eyJ...
```

Con prefijo `guaranteed-` el modo se detecta automaticamente.

### Asistente interactivo

| Comando | Comportamiento |
|---------|----------------|
| `brinco host` | Sin flags: preguntas guiadas (nombre, modo, relay, password opcional...). |
| `brinco join` | Sin args: codigo (Enter = ultimo guardado), nombre, password si aplica. |
| `brinco room create` / `brinco room join` | Solo la suborden, sin mas argumentos: mismo asistente que los atajos. |

### Atajos y perfiles

| Atajo | Equivale a |
|--------|------------|
| `brinco host [flags]` | `brinco room create` |
| `brinco join [CODIGO] [flags]` | `brinco room join` (el codigo puede ir **sin** `--code`) |
| `brinco join @perfil` | Lee `profiles.json` (rutas en `brinco doctor`) |
| `brinco relay --listen ...` | `brinco relay serve` (**serve** es opcional) |
| `brinco doctor` | Versión, rutas de caché, config y perfiles |
| `--pass` | Alias de `--password` |

**Password en relay/direct:** opcional; vacio = sala **sin** clave. La contrasena en terminal va en claro; evita compartir pantalla.

**Perfiles** (`profiles.json` en el directorio de config):

```json
{
  "casa": {
    "mode": "relay",
    "relay": "127.0.0.1:10000",
    "password": "demo",
    "code": "relay-REEMPLAZA_CON_TU_CODIGO",
    "name": "yo"
  }
}
```

Uso: `brinco join @casa` (los flags en linea pueden sobrescribir el perfil).

**Privado rapido:** `@usuario texto` (ademas de `/msg usuario texto`).

---

## 4. Modos de conexion

| Modo | Prefijo | Relay manual | Cuando usarlo |
|------|---------|--------------|---------------|
| `p2p` | `p2p-` | No | Redes normales; hole-punching libp2p. |
| `guaranteed` | `guaranteed-` | No | NAT estricto, VMs, CG-NAT; relay vía red IPFS. |
| `relay` | `relay-` | Sí (TCP propio) | Control total; máquina con IP pública. |
| `direct` | `direct-` | Sí (IP/puerto público) | TCP directo entre máquinas. |

### p2p

```bash
go run ./cmd/brinco room create --mode p2p --name Ana
go run ./cmd/brinco room join --name Luis --code p2p-...
```

Relay libp2p opcional:

```bash
go run ./cmd/brinco room create --mode p2p --name Ana --relay /ip4/IP/tcp/PUERTO/p2p/PEER_ID
```

### guaranteed

```bash
go run ./cmd/brinco room create --mode guaranteed --name Ana
go run ./cmd/brinco room join --name Luis --code guaranteed-...
```

### relay (servidor TCP propio)

El transporte es **TCP en claro** entre clientes y relay: no sustituye VPN/TLS en redes no confiables. En **p2p/guaranteed** el payload de chat va cifrado con secreto derivado del codigo de sala (modelo tipo “quien tenga el codigo”).

```bash
# 1) Maquina con IP publica (Ctrl+C detiene el relay y corta conexiones abiertas)
go run ./cmd/brinco relay serve --listen 0.0.0.0:10000 --public TU_IP_PUBLICA:10000

# Limites opcionales contra abuso (por defecto 64 TCP por IP; 0 = sin limite por IP)
go run ./cmd/brinco relay serve --listen 0.0.0.0:10000 --public TU_IP_PUBLICA:10000 --max-per-ip 32 --max-connections 500

# 2) Crear sala (password opcional; omite --password para sala abierta)
go run ./cmd/brinco room create --mode relay --name Ana --relay TU_IP_PUBLICA:10000 --password clave123

# 3) Unirse
go run ./cmd/brinco room join --name Luis --code relay-... --password clave123
```

### direct

```bash
go run ./cmd/brinco room create --mode direct --name Ana \
  --listen 0.0.0.0:9090 --public TU_IP_PUBLICA:9090 --password clave123
go run ./cmd/brinco room join --name Luis --code direct-... --password clave123
```

---

## 5. Referencia CLI

La ayuda completa esta en el binario:

```bash
brinco help
brinco room help
brinco relay help
brinco update help
```

### `room create` / `host`

| Flag | Uso |
|------|-----|
| `--name` | Nombre visible (default `host`; colisiones → `nombre#2`). |
| `--mode` | `p2p` (default) \| `direct` \| `relay` \| `guaranteed` |
| `--relay` | **p2p**: multiaddr opcional. **relay**: `host:puerto` del relay TCP (obligatorio). |
| `--password` / `--pass` | Opcional en **direct** y **relay** (vacío = sin clave). No aplica a p2p/guaranteed. |
| `--direct` | Atajo a `--mode direct`. |
| `--listen` / `--public` | Solo **direct** (`--public` obligatorio si escuchas en `0.0.0.0`). |

### `room join` / `join`

| Flag | Uso |
|------|-----|
| `--code` | Codigo de sala (obligatorio salvo asistente o prompt). |
| `--mode` | `auto` (default): se infiere del prefijo. |
| `--name` | Nombre visible (default `guest`). |
| `--password` / `--pass` | Opcional en direct/relay si la sala tiene clave. |
| `--relay` | Opcional solo en **p2p**. |

### `room code`

Ultimo codigo guardado en disco (p2p o ultimo TCP/relay):

```bash
go run ./cmd/brinco room code
```

### `relay serve`

Relay TCP para salas **relay**. Flags: `brinco relay help` (`--max-per-ip`, `--max-connections`, cierre con Ctrl+C o SIGTERM en Unix).

Completado bash opcional: [completions/brinco.bash](completions/brinco.bash) (carga con `source` desde tu `~/.bashrc` o similar).

### `update`

Requiere salida a Internet (API y releases de GitHub):

```bash
brinco update help
brinco update check
brinco update apply
```

En **Windows** el binario en uso puede bloquearse; el updater puede dejar un `.exe.new` para sustituir manualmente al cerrar el proceso.

---

## 6. Chat: comandos en sala

Los prefijos `/` son comandos; el resto es mensaje de chat.

**Alcance:** en **relay** y **direct** hay servidor TCP comun. En **p2p / guaranteed** no hay host central de sala: la moderacion **no aplica**.

### Comandos generales

| Entrada | Descripcion |
|---------|-------------|
| `/code` | Código de sala actual |
| `/peers` | TCP: lista de nombres. P2p: cantidad de peers en el topic |
| `/diag` | Estado, relay/NAT; en p2p RTT por peer |
| `/clear` | Limpia la consola (solo visual) |
| `/history` | Ultimas lineas escritas en la sesion (local) |
| `!!` | Repite el ultimo envio del historial local |
| `/msg USER TEXTO` | Privado (usa el nombre exacto de **Tu nombre:** al entrar) |
| `/send RUTA` | Archivo ~1.5 MB; el receptor ve la ruta donde se guardo |
| `/quit` | Salir |
| `/help` | Resumen en consola |

### Moderacion (solo host, solo relay/direct)

El servidor anuncia el nick del **host** al entrar; si el host sale, otro pasa a serlo (mensaje **Nuevo host de la sala:**).

| Comando | Efecto |
|---------|--------|
| `/kick USUARIO` | Expulsa la conexión con ese nick |
| `/mute USUARIO` | No puede enviar chat, privados, reacciones ni archivos |
| `/unmute USUARIO` | Quita el silencio |
| `/ban USUARIO` | No puede volver a entrar con ese nombre |

### Reacciones (sin `/`)

Línea sola: `+1`, `-1`, `ok`, `:emoji:`. Cualquier otra línea (p. ej. `+2`) es chat normal.

---

## 7. Logs y depuracion

| Sistema | Ruta |
|---------|------|
| Windows | `%LOCALAPPDATA%\brinco-cli\logs\brinco.log` |
| Linux | `~/.cache/brinco-cli/logs/brinco.log` |
| macOS | `~/Library/Caches/brinco-cli/logs/brinco.log` |

Nivel de log (variable de entorno):

```powershell
# PowerShell
$env:BRINCO_LOG_LEVEL = "debug"
```

```bash
# Bash / Zsh
export BRINCO_LOG_LEVEL=debug
```

Niveles: `debug` · `info` · `warn` · `error`

---

## 8. Desarrollo y pruebas

### Pruebas E2E

Desde la raíz del repo:

```bash
go test ./internal/e2e -v
```

Incluye flujos **direct** y **relay** con puerto libre. Suite completa del repo: `go test ./...`.

### Arquitectura interna

```
cmd/brinco/main.go          Punto de entrada
internal/
  cli/
    cli.go                  Parser, ayuda, flags
    guided.go               Asistente host/join
  p2p/
    node.go                 libp2p, GossipSub, chat, cifrado app-level, diag
    commands.go             RunCreate / RunJoin / guaranteed
    store.go                Ultimo codigo de sala
  chat/
    chat.go                 TCP directo: servidor de sala, cliente, moderación, colores
    relay.go                Relay TCP: hub, salas, moderación
  e2e/                      Tests entre procesos
  updater/updater.go        Releases GitHub
  roomproto/roomproto.go    Prefijos en códigos de sala
  logx/logx.go              Logs a archivo
```

El codigo de sala sigue el formato `PROTOCOLO-BASE64_JSON` con el payload segun modo (topic, direcciones, relay TCP, etc.).

---

## 9. Solucion de problemas

### `go` no se reconoce

Instala Go 1.22+ desde [go.dev/dl](https://go.dev/dl/) y reinicia la terminal (`go version`).

### Error al parsear el codigo

- Copia el codigo **completo**, con prefijo (`p2p-`, `guaranteed-`, ...).
- No uses `--mode p2p` con un código `relay-` o `direct-`; deja `auto`.

### `/peers` en 0 en p2p

1. Espera 10-15 s (hole-punching en segundo plano).  
2. Prueba **guaranteed**.  
3. Relay TCP propio + modo **relay**.

### Privados (`/msg`) no llegan o los ve otro

- Usa el nombre exacto de **Tu nombre:** (p. ej. `ana#2`).
- Reinicia `relay serve` y las sesiones tras actualizar el binario si hubo cambios de protocolo.

### Password incorrecta

Solo si la sala **tiene** clave (direct/relay). Debe coincidir con la del host. Si la sala es **sin** password, no pases `--password` al unirte.

### Mensaje rojo de rate limit

Espera unos segundos. Hay limite local y limite en servidor (relay/direct). Con `NO_COLOR` el texto sigue en stderr pero sin color.

### Relay no responde

- `relay serve` en ejecución.  
- Firewall y `--public` correctos.

### Sin colores

- Windows Terminal o PowerShell 7+.  
- Comprueba que `NO_COLOR` no esté definida si quieres ANSI.
