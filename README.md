# brinco-cli

CLI en Go con chat P2P por salas, codigo de invitacion y colores por usuario en consola.

## Estructura

- cmd/brinco/main.go: punto de entrada
- internal/cli/cli.go: parser de comandos
- internal/chat/chat.go: motor de chat por sala (TCP)
- go.mod: modulo de Go

## Requisitos

- Go 1.22 o superior

## Ejecutar

```bash
go run ./cmd/brinco --help
go run ./cmd/brinco room help
```

## Flujo rapido de chat

1. Terminal 1: crear sala

```bash
go run ./cmd/brinco room create --name Ana --listen 0.0.0.0:9090 --public TU_IP_PUBLICA:9090 --password secreto123
```

2. Copiar el codigo de sala que aparece en consola.

3. Terminal 2: unirse a la sala

```bash
go run ./cmd/brinco room join --name Luis --code CODIGO_DE_SALA --password secreto123
```

4. En el chat interactivo usar:

```text
/code   Muestra codigo de sala
/peers  Lista usuarios conectados
/quit   Salir del chat
/help   Ayuda de comandos de chat
```

## Comandos disponibles

```bash
go run ./cmd/brinco room create --name <nombre> --listen <host:puerto> --public <host:puerto> --password <clave>
go run ./cmd/brinco room join --name <nombre> --code <codigo> --password <clave>
go run ./cmd/brinco room code
```

Nota:
- `--password` es obligatorio para crear y unirse.
- `--public` es obligatorio si `--listen` usa `0.0.0.0`.
- Los nombres de usuario se colorean automaticamente para distinguir remitentes.

## Compilar

```bash
go build -o bin/brinco ./cmd/brinco
```
