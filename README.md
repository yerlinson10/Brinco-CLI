# brinco-cli

> Chat de consola en tiempo real, peer-to-peer, sin servidores centrales.
> Crea salas privadas, comparte el codigo y habla directamente con otros usuarios desde cualquier red.

Brinco-CLI esta escrito en Go y usa [libp2p](https://libp2p.io/) (la misma red que IPFS) para
establecer conexiones entre pares. Cada sala genera un codigo de invitacion unico que incluye
el protocolo usado, por lo que unirse es tan simple como pegar el codigo.

## Que hace Brinco y que debes hacer primero

Brinco te permite crear una sala de chat P2P desde terminal, compartir un codigo y empezar a
chatear sin servidor central.

Pasos minimos para usarlo:
1. Tener el ejecutable `brinco` (o `brinco.exe`) compilado.
2. Abrir una terminal.
3. Crear sala: `brinco room create --mode guaranteed --name Ana`.
4. Compartir el codigo generado.
5. El otro usuario se une: `brinco room join --name Luis --code CODIGO`.

## Uso con binario compilado (sin go run)

Si ya tienes el archivo compilado, puedes usar Brinco como comando global en consola.

### Windows

1. Compila:
```powershell
go build -o bin/brinco.exe ./cmd/brinco
```
2. Agrega la carpeta `bin` al PATH o copia `brinco.exe` a una carpeta del PATH.
3. Cierra y abre la terminal.
4. Verifica:
```powershell
brinco version
```

### Linux / macOS

1. Compila:
```bash
go build -o bin/brinco ./cmd/brinco
```
2. Da permisos y mueve al PATH:
```bash
chmod +x bin/brinco
sudo mv bin/brinco /usr/local/bin/brinco
```
3. Verifica:
```bash
brinco version
```

### Flujo rapido con binario

```bash
# Usuario A
brinco room create --mode guaranteed --name Ana

# Usuario B
brinco room join --name Luis --code CODIGO
```

## Instalacion por gestores y paquetes

Opciones disponibles para usuario final:
1. Scoop (Windows)
2. deb/rpm (Linux)
3. Instalador Windows .exe

### Instalar por Scoop (Windows)

```powershell
scoop bucket add brinco-bucket https://github.com/yerlinson10/scoop-bucket
scoop install brinco
```

### Instalar por paquete Linux (.deb/.rpm)

1. Descarga el archivo .deb o .rpm desde Releases.
2. Instala con:
```bash
# Debian/Ubuntu
sudo dpkg -i brinco_*.deb

# RHEL/Fedora
sudo rpm -i brinco-*.rpm
```

### Instalar con el setup de Windows (.exe)

1. Descarga Brinco-Setup-<version>.exe desde Releases.
2. Ejecuta el instalador y finaliza el wizard.
3. (Opcional) marca Add Brinco to PATH durante la instalacion.

---

## Tabla de contenidos

- [Que hace Brinco y que debes hacer primero](#que-hace-brinco-y-que-debes-hacer-primero)
- [Uso con binario compilado (sin go run)](#uso-con-binario-compilado-sin-go-run)
- [Instalacion por gestores y paquetes](#instalacion-por-gestores-y-paquetes)
- [Caracteristicas](#caracteristicas)
- [Requisitos](#requisitos)
- [Instalacion](#instalacion)
- [Inicio rapido](#inicio-rapido)
- [Modos de conexion](#modos-de-conexion)
- [Referencia de comandos](#referencia-de-comandos)
- [Comandos dentro del chat](#comandos-dentro-del-chat)
- [Logs](#logs)
- [Arquitectura interna](#arquitectura-interna)
- [Solucion de problemas](#solucion-de-problemas)

---

## Caracteristicas

- **Chat P2P real** — los mensajes van de peer a peer sin intermediarios.
- **Salas privadas** — cada sala tiene un codigo unico de invitacion.
- **4 modos de conexion** — desde directo TCP hasta relay automatico via red IPFS.
- **Join inteligente** — el codigo ya lleva el protocolo; no hace falta especificar el modo.
- **Colores por usuario** — cada persona tiene un color distinto en la terminal.
- **Logs persistentes** — registro en archivo con nivel configurable.
- **Sin configuracion obligatoria** — los modos `p2p` y `guaranteed` funcionan solos.

---

## Requisitos

| Requisito | Version minima |
|-----------|---------------|
| Go | 1.22 |
| Terminal | ANSI recomendado (para colores) |
| Conectividad | Internet para redes distintas |

---

## Instalacion

```bash
git clone https://github.com/yerlinson10/Brinco-CLI.git
cd Brinco-CLI
go mod tidy
```

Compilar binario:

```bash
# Windows
go build -o bin/brinco.exe ./cmd/brinco

# Linux / macOS
go build -o bin/brinco ./cmd/brinco
```

O ejecutar directamente sin compilar:

```bash
go run ./cmd/brinco help
```

---

## Inicio rapido

El caso mas comun: dos personas en redes distintas quieren chatear.

**Usuario A — crea la sala:**

```bash
go run ./cmd/brinco room create --name Ana
```

Copia el codigo que aparece, por ejemplo: `guaranteed-eyJ...`

**Usuario B — se une:**

```bash
go run ./cmd/brinco room join --name Luis --code guaranteed-eyJ...
```

Listo. Si el codigo tiene prefijo `guaranteed-`, el modo se detecta automaticamente.

---

## Modos de conexion

Brinco-CLI tiene cuatro modos. Cada uno genera un codigo con un prefijo distinto que identifica
el protocolo. Al hacer join con `--mode auto` (o sin especificar modo), el protocolo se detecta
del codigo automaticamente.

| Modo | Prefijo | Relay manual | Cuando usarlo |
|-----------|-------------|--------------|----------------------------------------------------------------|
| `p2p` | `p2p-` | No | Redes normales. Conexion directa con hole-punching libp2p. |
| `guaranteed` | `guaranteed-` | No | NAT estricto, VMs, CG-NAT. Relay automatico via red IPFS. |
| `relay` | `relay-` | Si (TCP) | Control total con relay propio en servidor publico. |
| `direct` | `direct-` | Si (IP publica) | Ambos lados tienen IP y puerto accesible directamente. |

### p2p — default, automatico

Usa la red libp2p/IPFS para descubrimiento y hole-punching. No requiere configuracion.

```bash
# Crear sala
go run ./cmd/brinco room create --mode p2p --name Ana

# Unirse
go run ./cmd/brinco room join --name Luis --code p2p-...
```

Opcionalmente puedes aportar tu propio relay libp2p:

```bash
go run ./cmd/brinco room create --mode p2p --name Ana --relay /ip4/IP/tcp/PUERTO/p2p/PEER_ID
```

---

### guaranteed — P2P con relay IPFS automatico

Igual que `p2p` pero registra los nodos bootstrap de IPFS como candidatos de relay estatico.
Libp2p negocia circuitos relay con ellos automaticamente. **No necesitas servidores propios.**
Recomendado cuando `p2p` no logra conectar por NAT estricto o firewalls.

```bash
# Crear sala
go run ./cmd/brinco room create --mode guaranteed --name Ana

# Unirse
go run ./cmd/brinco room join --name Luis --code guaranteed-...
```

---

### relay — relay TCP propio

Requiere levantar un servidor relay con `brinco relay serve`. Da control total sobre la
infraestructura pero necesitas una maquina con IP publica.

```bash
# Paso 1: levantar el servidor relay (maquina con IP publica)
go run ./cmd/brinco relay serve --listen 0.0.0.0:10000 --public TU_IP_PUBLICA:10000

# Paso 2: crear sala apuntando al relay
go run ./cmd/brinco room create --mode relay --name Ana --relay TU_IP_PUBLICA:10000 --password clave123

# Paso 3: unirse
go run ./cmd/brinco room join --name Luis --code relay-... --password clave123
```

---

### direct — TCP directo

Conexion TCP pura entre dos maquinas. El host necesita tener un puerto abierto y accesible.

```bash
# Crear sala (host con IP/puerto publico)
go run ./cmd/brinco room create --mode direct --name Ana \
  --listen 0.0.0.0:9090 --public TU_IP_PUBLICA:9090 --password clave123

# Unirse
go run ./cmd/brinco room join --name Luis --code direct-... --password clave123
```

---

## Referencia de comandos

### `room create`

```
--name      Nombre de usuario (default: host)
--mode      p2p | direct | relay | guaranteed  (default: p2p)
--relay     [p2p] Multiaddr relay libp2p propio (opcional)
            [relay] IP:PUERTO del relay TCP
--listen    [direct] Direccion local a escuchar (default: 0.0.0.0:9090)
--public    [direct] IP:PUERTO publico del host
--password  [direct|relay] Password de la sala
```

### `room join`

```
--name      Nombre de usuario (default: guest)
--code      Codigo de sala (obligatorio)
--mode      auto | p2p | direct | relay | guaranteed  (default: auto)
--relay     [p2p] Relay libp2p adicional (opcional)
--password  [direct|relay] Password de la sala
```

### `room code`

Muestra el ultimo codigo de sala generado en esta maquina.

```bash
go run ./cmd/brinco room code
```

### `relay serve`

Levanta un servidor relay TCP para el modo `relay`.

```
--listen    Direccion local (ej: 0.0.0.0:10000)
--public    IP:PUERTO publico accesible por otros
```

---

## Comandos dentro del chat

Una vez dentro de una sala, puedes usar estos comandos:

| Comando | Descripcion |
|---------|-------------|
| `/code` | Muestra el codigo de sala actual |
| `/peers` | Cantidad de peers enlazados (modos p2p/guaranteed) |
| `/quit` | Salir del chat |
| `/help` | Lista de comandos disponibles |

---

## Logs

Brinco guarda un log persistente en el directorio de cache del sistema.

| Sistema | Ruta |
|---------|------|
| Windows | `%LOCALAPPDATA%\brinco-cli\logs\brinco.log` |
| Linux | `~/.cache/brinco-cli/logs/brinco.log` |
| macOS | `~/Library/Caches/brinco-cli/logs/brinco.log` |

Configurar el nivel de detalle:

```bash
# PowerShell
$env:BRINCO_LOG_LEVEL = "debug"

# Bash / Zsh
export BRINCO_LOG_LEVEL=debug
```

Niveles disponibles: `debug` · `info` · `warn` · `error`

---

## Arquitectura interna

```
cmd/brinco/main.go          Punto de entrada
internal/
  cli/cli.go                Parser de comandos y flags, routing de modos
  p2p/
    node.go                 Nodo libp2p, GossipSub, mDNS, relay circuit v2
    commands.go             RunCreate / RunJoin / RunCreateGuaranteed / RunJoinGuaranteed
    store.go                Persistencia del ultimo codigo de sala
  chat/
    chat.go                 Chat TCP directo, parseo de codigos direct/relay
    relay.go                Servidor relay TCP y cliente relay
  roomproto/roomproto.go    Prefijos de protocolo en codigos de sala
  logx/logx.go              Sistema de logs con niveles y archivo
```

El codigo de sala es un string con el formato `PROTOCOLO-BASE64_JSON`, donde el JSON contiene
el topic de la sala, las direcciones del creador y (en modos TCP) la del relay.

---

## Solucion de problemas

### `go` no se reconoce en la terminal

Instala Go 1.22+ desde [go.dev/dl](https://go.dev/dl/) y reinicia la terminal.
Verifica con `go version`.

### Error al parsear el codigo de sala

- Asegurate de copiar el codigo completo, incluyendo el prefijo (`p2p-`, `guaranteed-`, etc.).
- No fuerces `--mode p2p` con un codigo `relay-` o `direct-`; usa `--mode auto`.

### No hay peers conectados (`/peers` muestra 0)

En modo `p2p`, puede ser NAT estricto o firewall bloqueando el puerto negociado.
Soluciones en orden de simplicidad:
1. Esperar 10-15 segundos, libp2p intenta hole-punching en segundo plano.
2. Cambiar a modo `guaranteed` — usa relay IPFS automatico, no requiere puertos abiertos.
3. Levantar un relay propio con `relay serve` y usar modo `relay`.

### Password incorrecta al unirse

Aplica a modos `direct` y `relay`. La clave debe ser exactamente igual a la del host.

### El relay propio no esta disponible

- Verifica que `relay serve` este corriendo.
- Confirma que el puerto este abierto en el firewall de la maquina relay.
- Asegurate de pasar la IP/PUERTO publico correcto con `--public`.

### No veo colores en la terminal

- En Windows usa Windows Terminal o PowerShell 7+.
- Verifica que la variable `NO_COLOR` no este definida en el entorno.
