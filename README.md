# brinco-cli

Chat de consola en Go con salas, codigo de invitacion y nombres coloreados por usuario.

## 1) Que trae esta version

1. Modos de conexion:
	- p2p
	- direct
	- relay
	- guaranteed (relay-first)
2. Codigos de sala con prefijo de protocolo:
	- p2p-...
	- direct-...
	- relay-...
	- guaranteed-...
3. Join en modo auto segun prefijo de codigo.
4. Sistema de logs persistente con nivel configurable.

## 2) Requisitos

1. Go 1.22 o superior.
2. Terminal con soporte ANSI para colores (opcional).
3. Internet para modo p2p y relay en redes distintas.

## 3) Estructura relevante

1. cmd/brinco/main.go
	- punto de entrada
2. internal/cli/cli.go
	- comandos, flags y seleccion de modo
3. internal/p2p/node.go
	- nodo libp2p y chat p2p
4. internal/p2p/commands.go
	- create/join p2p
5. internal/chat/chat.go
	- chat TCP directo y parseo de codigos direct/relay/guaranteed
6. internal/chat/relay.go
	- servidor relay TCP y cliente relay
7. internal/roomproto/roomproto.go
	- prefijo y parseo de protocolo en codigos de sala
8. internal/logx/logx.go
	- logs a archivo y niveles

## 4) Instalacion rapida

```bash
go mod tidy
go run ./cmd/brinco --help
```

## 5) Comandos principales

```bash
go run ./cmd/brinco help
go run ./cmd/brinco version
go run ./cmd/brinco room help
go run ./cmd/brinco relay help

go run ./cmd/brinco room create --name Ana
go run ./cmd/brinco room join --name Luis --code CODIGO
go run ./cmd/brinco room code

go run ./cmd/brinco relay serve --listen 0.0.0.0:10000 --public IP_PUBLICA:10000
```

## 6) Modos de sala

| Modo | Protocolo codigo | Requiere relay manual | Cuando usar |
|-----------|------------------|----------------------|-------------------------------------------------------|
| p2p | p2p-... | No | Conexion directa o relay automatico libp2p |
| guaranteed| guaranteed-... | No | NAT estricto, VM, firewalls. Relay IPFS automatico |
| relay | relay-... | Si (TCP propio) | Relay TCP propio con control total |
| direct | direct-... | Si (IP publica) | Ambos lados tienen IP/puerto accesible |
### 6.1 p2p (default)

```bash
go run ./cmd/brinco room create --mode p2p --name Ana
go run ./cmd/brinco room join --mode p2p --name Luis --code p2p-...
```

Tambien puedes usar relay libp2p opcional:

```bash
go run ./cmd/brinco room create --mode p2p --name Ana --relay /ip4/IP/tcp/PUERTO/p2p/PEER_ID
go run ./cmd/brinco room join --mode p2p --name Luis --code p2p-... --relay /ip4/IP/tcp/PUERTO/p2p/PEER_ID
```

### 6.2 direct (TCP puro)

```bash
go run ./cmd/brinco room create --mode direct --name Ana --listen 0.0.0.0:9090 --public TU_IP_PUBLICA:9090 --password clave123
go run ./cmd/brinco room join --mode direct --name Luis --code direct-... --password clave123
```

Atajo legacy equivalente:

```bash
go run ./cmd/brinco room create --direct ...
go run ./cmd/brinco room join --direct ...
```

### 6.3 relay (TCP relay dedicado)

```bash
go run ./cmd/brinco relay serve --listen 0.0.0.0:10000 --public TU_IP_PUBLICA:10000
go run ./cmd/brinco room create --mode relay --name Ana --relay TU_IP_PUBLICA:10000 --password clave123
go run ./cmd/brinco room join --mode relay --name Luis --code relay-... --password clave123
```

### 6.4 guaranteed (relay libp2p automatico)

Este modo usa la red IPFS/libp2p para descubrir relays automaticamente.
**No requiere configurar ningun relay manual.** Es la opcion recomendada cuando p2p
no logra conectar por NAT estricto, VMs o firewalls.

```bash
# Crear sala (no se necesita --relay)
go run ./cmd/brinco room create --mode guaranteed --name Ana

# Unirse
go run ./cmd/brinco room join --mode guaranteed --name Luis --code guaranteed-...
```

Diferencias con p2p:
- Registra los nodos bootstrap de IPFS como candidatos de relay estatico.
- Libp2p negocia automaticamente circuitos relay con esos nodos.
- Mas robusto en redes restrictivas sin necesidad de infraestructura propia.

## 7) Join automatico por protocolo

Si no quieres pensar en modo, usa auto:

```bash
go run ./cmd/brinco room join --mode auto --name Luis --code CODIGO
```

Regla:
1. Si el codigo tiene prefijo conocido, usa ese protocolo.
2. Si no tiene prefijo, cae en p2p por compatibilidad.

## 8) Comandos dentro del chat

```text
/code   Muestra el codigo de sala
/peers  Muestra peers enlazados (p2p)
/quit   Salir
/help   Ayuda
```

## 9) Logging

Ruta de log:
1. Se guarda en cache de usuario.
2. Nombre: brinco.log.

Ejemplo de ubicacion:
1. Windows: %LOCALAPPDATA%\brinco-cli\logs\brinco.log
2. Linux/macOS: ~/.cache/brinco-cli/logs/brinco.log

Nivel de log:

```bash
# PowerShell
$env:BRINCO_LOG_LEVEL="debug"

# Bash
export BRINCO_LOG_LEVEL=debug
```

Valores validos: debug, info, warn, error.

## 10) Pruebas recomendadas

### 10.1 Basica local (2 terminales)

1. Terminal A:
```bash
go run ./cmd/brinco room create --mode p2p --name Ana
```
2. Copiar codigo.
3. Terminal B:
```bash
go run ./cmd/brinco room join --mode auto --name Luis --code CODIGO
```
4. Probar envio bidireccional y /peers.

### 10.2 Entre redes distintas (estable)

Si p2p no enruta por NAT/VM, usa guaranteed. No necesitas relay propio:

```bash
# Terminal A
go run ./cmd/brinco room create --mode guaranteed --name Ana

# Copiar el codigo guaranteed-...

# Terminal B (otra red, VM, etc.)
go run ./cmd/brinco room join --mode auto --name Luis --code guaranteed-...
```

Libp2p descubrira relay disponibles en la red IPFS automaticamente.

## 11) Errores comunes y solucion

### 11.1 go no se reconoce

1. Instalar Go 1.22+.
2. Cerrar y abrir terminal.
3. Verificar con go version.

### 11.2 Error parseando codigo

1. Revisa que no haya espacios o cortes al copiar.
2. Usa el codigo completo con su prefijo.
3. Si el codigo es de modo relay/direct/guaranteed, no fuerces mode p2p.

### 11.3 no hay codigo guardado

1. Debes crear una sala antes de room code.
2. Si limpiaste cache, se pierde el ultimo codigo local.

### 11.4 no hay peers conectados en la sala

1. En p2p esto suele ser problema de ruta NAT/firewall.
2. Prueba /peers para confirmar.
3. Usa mode guaranteed para conectividad consistente.

### 11.5 password incorrecta

1. Aplica a direct/relay/guaranteed.
2. Debe ser exactamente la misma clave del host.

### 11.6 relay no disponible

1. Verifica IP/puerto y firewall del relay.
2. Confirma que relay serve siga corriendo.

### 11.7 no veo colores

1. Prueba otra terminal con ANSI.
2. Revisa si NO_COLOR esta definida.

## 12) Compilar

```bash
# Windows
go build -o bin/brinco.exe ./cmd/brinco

# Linux/macOS
go build -o bin/brinco ./cmd/brinco
```
