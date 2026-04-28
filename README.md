# brinco-cli

Chat de consola en Go con salas, codigo de invitacion y nombres coloreados por usuario.

La aplicacion soporta tres formas de conexion:

1. Modo P2P con libp2p (modo por defecto, recomendado para comunidad, sin servidor obligatorio).
2. Modo directo TCP (sin libp2p, requiere exponer puertos).
3. Modo relay propio TCP (opcional, para mayor control en internet).

## 1) Requisitos

1. Go 1.22 o superior.
2. Terminal con salida ANSI para ver colores (opcional).
3. Conexion a internet para modo P2P/libp2p.

## 2) Estructura del proyecto

- cmd/brinco/main.go
	Punto de entrada.

- internal/cli/cli.go
	Parser de comandos y flags.

- internal/p2p/node.go
	Nodo libp2p, pubsub y descubrimiento.

- internal/p2p/commands.go
	Flujos create y join en modo P2P.

- internal/chat/chat.go
	Motor de chat TCP directo.

- internal/chat/relay.go
	Relay TCP propio y clientes relay.

## 3) Instalacion y arranque

Desde la raiz del proyecto:

go mod tidy
go run ./cmd/brinco --help

## 4) Comandos principales

Comandos globales:

- go run ./cmd/brinco help
- go run ./cmd/brinco version
- go run ./cmd/brinco room help
- go run ./cmd/brinco relay help

Comandos de sala:

- go run ./cmd/brinco room create --name Ana
- go run ./cmd/brinco room join --name Luis --code CODIGO
- go run ./cmd/brinco room code

Comando de relay propio:

- go run ./cmd/brinco relay serve --listen 0.0.0.0:10000 --public IP_PUBLICA:10000

## 5) Modos de uso detallados

### 5.1 Modo P2P (recomendado)

Este es el modo por defecto. No necesita que el host abra puertos manualmente en la mayoria de escenarios.

Crear sala:

go run ./cmd/brinco room create --name Ana

Unirse desde otro equipo:

go run ./cmd/brinco room join --name Luis --code CODIGO_GENERADO

Opcional con relay libp2p propio:

go run ./cmd/brinco room create --name Ana --relay /ip4/IP/tcp/PUERTO/p2p/PEER_ID
go run ./cmd/brinco room join --name Luis --code CODIGO_GENERADO --relay /ip4/IP/tcp/PUERTO/p2p/PEER_ID

### 5.2 Modo directo TCP

Usa la bandera --direct. Requiere que el host sea alcanzable desde internet o LAN.

Crear sala directa:

go run ./cmd/brinco room create --direct --name Ana --listen 0.0.0.0:9090 --public TU_IP_PUBLICA:9090 --password clave123

Unirse en directo:

go run ./cmd/brinco room join --direct --name Luis --code CODIGO_GENERADO --password clave123

### 5.3 Modo relay propio TCP

Primero levantas tu relay:

go run ./cmd/brinco relay serve --listen 0.0.0.0:10000 --public TU_IP_PUBLICA:10000

Luego creas sala en directo usando relay TCP:

go run ./cmd/brinco room create --direct --name Ana --relay TU_IP_PUBLICA:10000 --password clave123

Y el cliente se une en directo:

go run ./cmd/brinco room join --direct --name Luis --code CODIGO_GENERADO --password clave123

## 6) Comandos dentro del chat

Comandos interactivos:

- /code
	Muestra el codigo de la sala actual.

- /help
	Muestra ayuda de comandos interactivos.

- /quit
	Sale del chat.

Nota:
En modo directo TCP existen ademas respuestas de peers en el protocolo interno; en la UI actual la ayuda visible es /code, /help y /quit.

## 7) Flujo recomendado para pruebas

### Prueba local rapida (mismo PC, dos terminales)

1. Terminal A:
	 go run ./cmd/brinco room create --name Ana

2. Copiar codigo de sala.

3. Terminal B:
	 go run ./cmd/brinco room join --name Luis --code CODIGO

4. Enviar mensajes desde ambos lados y verificar colores por nombre.

### Prueba en dos redes distintas

1. Crear sala en equipo 1:
	 go run ./cmd/brinco room create --name Ana

2. Compartir codigo con equipo 2.

3. Unirse en equipo 2:
	 go run ./cmd/brinco room join --name Luis --code CODIGO

Si no conecta, revisar la seccion de errores comunes.

## 8) Errores comunes y solucion

### Error: go no se reconoce como comando

Sintoma:
- El terminal muestra que go no existe o no se reconoce.

Causa:
- Go no esta instalado o no esta en PATH.

Solucion:
1. Instalar Go 1.22+.
2. Cerrar y abrir la terminal.
3. Verificar con: go version.

### Error: Error parseando codigo

Sintoma:
- Al hacer join, falla al parsear el codigo.

Causa:
- Codigo copiado incompleto o con espacios extra.

Solucion:
1. Copiar el codigo completo de una sola linea.
2. Evitar comillas adicionales.
3. Probar room code en el host para recuperar el ultimo codigo.

### Error: no hay codigo guardado

Sintoma:
- room code devuelve que no hay codigo guardado.

Causa:
- No se ha creado sala en esa maquina o se limpio cache de usuario.

Solucion:
1. Crear una sala nueva.
2. Confirmar que aparezca Codigo de sala en pantalla.

### Error: no se pudo unir al topic / no conecta en P2P

Sintoma:
- La app inicia pero no logra comunicacion estable.

Causa:
- Red muy restringida, proxy corporativo, bloqueo de puertos salientes o NAT estricto.

Solucion:
1. Probar desde otra red (por ejemplo datos moviles).
2. Probar con relay libp2p propio en --relay.
3. Si sigue fallando, usar modo --direct con relay TCP propio.

### Error: dial tcp ... actively refused

Sintoma:
- Conexion rechazada al puerto remoto.

Causa:
- No hay servicio escuchando en ese puerto, firewall bloquea o IP incorrecta.

Solucion:
1. Verificar que el proceso este corriendo en host.
2. Revisar firewall del host.
3. Revisar direccion y puerto exactos.
4. Si es internet, validar NAT/port forwarding en modo directo.

### Error: password incorrecta

Sintoma:
- Al unirse en modo directo/relay TCP, el servidor rechaza credenciales.

Causa:
- Password distinta a la usada al crear sala.

Solucion:
1. Repetir join con la misma password del creador.
2. Evitar espacios al final.

### Error: relay no disponible

Sintoma:
- Advertencia al reservar circuito o conectar relay.

Causa:
- Multiaddr invalida, relay caido o inaccesible.

Solucion:
1. Validar formato multiaddr:
	 /ip4/IP/tcp/PUERTO/p2p/PEER_ID
2. Verificar que relay este encendido.
3. Probar sin --relay para usar red publica.

### Error: Cloud Shell conecta mal o se queda colgado

Sintoma:
- Desde entornos tipo shell web no llega conexion estable en modos TCP.

Causa:
- Restricciones de puertos entrantes/salientes del entorno.

Solucion:
1. Usar modo P2P por defecto.
2. Si usas relay TCP, ejecutarlo en VM con puertos abiertos reales.

### No veo colores en consola

Sintoma:
- Los nombres salen sin color.

Causa:
- Terminal sin soporte ANSI o variable NO_COLOR activa.

Solucion:
1. Probar otra terminal (Windows Terminal, PowerShell moderno).
2. Revisar variable NO_COLOR y desactivarla si aplica.

## 9) Buenas practicas operativas

1. Para comunidad, prioriza modo P2P por defecto.
2. Usa relay propio solo cuando necesites control o debugging.
3. Si publicas un relay, monitorea disponibilidad y limita abuso.
4. Versiona cambios de protocolo si vas a extender mensajes.

## 10) Compilar binario

Windows:

go build -o bin/brinco.exe ./cmd/brinco

Linux/macOS:

go build -o bin/brinco ./cmd/brinco
