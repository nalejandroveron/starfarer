# Starfarer — prueba de eventos Protobuf

## Contrato

`starfarer_server/proto/starfarer/protocol/v1/connection.proto` define `ClientEvents` y `ServerEvents`, con `oneof event`. No hay servicios RPC ni gRPC.

- Cliente → Go: `Hello`, `Ping`.
- Go → cliente: `Welcome`, `Pong`, `WorldSnapshot` de diagnóstico a 10 Hz.

Desde `starfarer_server`, ejecutar `buf lint` y `buf generate` después de editar el contrato. Se generan los tipos Go y C#; no editar los archivos generados. Buf requiere acceso a sus plugins remotos.

## Ejecutar

1. En `starfarer_server`, ejecutar `go run .`. Escucha únicamente en `127.0.0.1:7777` por defecto.
2. Abrir `starfarer/project.godot` con Godot .NET y pulsar **F5**. Compilar C# si el editor lo solicita.
3. La escena se conecta automáticamente. Debe mostrar **CONECTADO**, un jugador, respuestas Pong y snapshots crecientes. La barra se mueve con datos enviados por Go.
4. `Enviar Ping` muestra el mensaje de ida y su respuesta. `Desconectar` y `Conectar / reconectar` permiten repetir la prueba.

Para compilar por consola: `dotnet build` desde `starfarer`. Si el puerto está ocupado, detener la instancia anterior del servidor antes de iniciar otra.

## Comprobaciones

- Servidor: `go test ./...`.
- Cliente real sin ventana: ejecutar Godot .NET con `--headless --path <carpeta-del-cliente> -- --smoke-test`, con Go activo. Sale con código 0 y `SMOKE PASS` tras recibir Welcome, Pong y tres snapshots; falla tras 12 segundos si falta alguno.

## Alcance

UDP local: un mensaje Protobuf por datagrama, máximo 1200 bytes. No incluye login, cifrado, persistencia, fragmentación ni entrega fiable. Hello se reintenta cada segundo y conserva la sesión mientras siga activa. Ping automático cada 2 segundos mantiene la sesión; Go expira sesiones tras 15 segundos y el cliente intenta reconectar tras 5 segundos sin eventos válidos.

El servidor asigna una sesión por endpoint UDP; esto es identificación de diagnóstico, no autenticación. Descarta Ping sin Hello. El cliente descarta snapshots viejos. La posición animada no representa física ni movimiento autoritativo. Los handlers usan un switch tipado sobre el oneof; el transporte puede sustituirse conservando los mensajes.
