using Godot;
using Google.Protobuf;
using Starfarer.Protocol.V1;
using System;
using System.Collections.Generic;

public partial class TransportProbe : Control
{
    private const uint Version = 1;
    private PacketPeerUdp? _peer;
    private Label _status = null!;
    private Label _stats = null!;
    private RichTextLabel _log = null!;
    private Label _position = null!;
    private readonly Queue<string> _lines = new();

    private double _elapsed, _lastReceive, _nextHello, _nextInput;
    private ulong _player, _inputSequence, _lastTick;
    private int _snapshots;

    public event Action<WorldSnapshot>? SnapshotReceived;
    public event Action? SessionClosed;

    public override void _Ready()
    {
        var panel = new PanelContainer();
        panel.Position = new Vector2(16, 16);
        panel.CustomMinimumSize = new Vector2(400, 300);
        AddChild(panel);
        var margin = new MarginContainer();
        foreach (var side in new[] { "left", "top", "right", "bottom" })
            margin.AddThemeConstantOverride("margin_" + side, 28);
        panel.AddChild(margin);
        var column = new VBoxContainer();
        column.AddThemeConstantOverride("separation", 16);
        margin.AddChild(column);
        var title = new Label { Text = "STARFARER / PROTOBUF LAB" };
        title.AddThemeFontSizeOverride("font_size", 28);
        column.AddChild(title);
        column.AddChild(new Label { Text = "Godot C# ↔ Go · UDP local 127.0.0.1:7777 · sin gRPC" });
        var buttons = new HBoxContainer();
        column.AddChild(buttons);
        var connect = new Button { Text = "Conectar / reconectar" };
        connect.Pressed += Connect;
        buttons.AddChild(connect);
        var disconnect = new Button { Text = "Desconectar" };
        disconnect.Pressed += () => { Close(); _status.Text = "DESCONECTADO"; Record("Conexión cerrada."); };
        buttons.AddChild(disconnect);
        _status = new Label();
        _status.AddThemeFontSizeOverride("font_size", 22);
        column.AddChild(_status);
        _stats = new Label();
        column.AddChild(_stats);
        _position = new Label
        {
            Text = "Esperando posición del servidor..."
        };
        column.AddChild(_position);

        column.AddChild(new Label
        {
            Text = "WASD para moverte. La posición la calcula Go."
        });


        column.AddChild(new Label { Text = "Posición de diagnóstico enviada por Go a 10 Hz (sin física de juego)." });
        _log = new RichTextLabel { SizeFlagsVertical = SizeFlags.ExpandFill, ScrollFollowing = true };
        column.AddChild(_log);
        column.AddChild(new Label { Text = "Prueba local: sin login, cifrado, persistencia ni garantía de entrega UDP.\nHello se reintenta; Ping cada 2 s mantiene la sesión activa." });
        Connect();
    }

    private void Connect()
    {
        Close();
        _snapshots = 0;
        _lastTick = 0;
        _inputSequence = 0;
        _nextInput = _elapsed;

        _position.Text = "Esperando posición del servidor...";
        _peer = new PacketPeerUdp();
        var error = _peer.ConnectToHost("127.0.0.1", 7777);
        if (error != Error.Ok) { Close(); _status.Text = $"ERROR: {error}"; return; }
        _lastReceive = _elapsed;
        _nextHello = _elapsed;
        _status.Text = "ESPERANDO WELCOME · iniciá el servidor Go";
        Record("Conectando a 127.0.0.1:7777...");
    }

    private void Close()
    {
        _peer?.Close();
        _peer?.Dispose();
        _peer = null;
        _player = 0;
        SessionClosed?.Invoke();
    }

    private bool Send(ClientEvents message)
    {
        if (_peer == null) return false;
        message.ProtocolVersion = Version;
        var error = _peer.PutPacket(message.ToByteArray());
        if (error == Error.Ok) return true;
        Record($"Error de envío: {error}");
        return false;
    }

    private void SendPlayerInput()
    {
        if (_player == 0)
            return;

        // Al perder el foco, enviamos una dirección cero.
        Vector2 direction = GetWindow().HasFocus()
            ? Input.GetVector(
                "move_left",
                "move_right",
                "move_forward",
                "move_backward"
            )
            : Vector2.Zero;

        Send(new ClientEvents
        {
            PlayerInput = new PlayerInput
            {
                Sequence = ++_inputSequence,
                MoveX = direction.X,
                MoveZ = direction.Y
            }
        });
    }

    public override void _Process(double delta)
    {
        _elapsed += delta;
        if (_peer != null)
        {
            for (var i = 0; i < 128 && _peer.GetAvailablePacketCount() > 0; i++)
            {
                var bytes = _peer.GetPacket();
                if (bytes.Length == 0 || bytes.Length > 1200) continue;
                try
                {
                    var message = ServerEvents.Parser.ParseFrom(bytes);
                    if (message.ProtocolVersion != Version || message.EventCase == ServerEvents.EventOneofCase.None)
                    { Record("Evento rechazado: versión o variante desconocida."); continue; }
                    _lastReceive = _elapsed;
                    Handle(message);
                }
                catch (InvalidProtocolBufferException ex) { Record($"Protobuf inválido: {ex.Message}"); }
            }
            if (_player != 0 && _elapsed - _lastReceive > 5)
            {
                Record("Servidor sin respuesta durante 5 s; reconectando.");
                Connect();
            }
            if (_player == 0 && _elapsed >= _nextHello)
            {
                Send(new ClientEvents { Hello = new Hello { ClientVersion = "protobuf-lab/1" } });
                _nextHello = _elapsed + 1;
            }
            if (_player != 0 && _elapsed >= _nextInput)
            {
                SendPlayerInput();
                _nextInput = _elapsed + 1.0 / 30.0;
            }
        }
        _stats.Text =
            $"Jugador: {_player} | Último input enviado: {_inputSequence}\n" +
            $"Snapshots: {_snapshots} | Último tick: {_lastTick}";
    }

    private void Handle(ServerEvents message)
    {
        switch (message.EventCase)
        {
            case ServerEvents.EventOneofCase.Welcome:
                if (message.Welcome.PlayerId == 0)
                    return;

                _player = message.Welcome.PlayerId;
                _status.Text = $"CONECTADO · jugador {_player}";
                _nextInput = _elapsed;

                Record($"← Welcome player={_player}");
                break;

            case ServerEvents.EventOneofCase.Snapshot:
                var snapshot = message.Snapshot;

                if (_player == 0 || snapshot.Tick <= _lastTick)
                    return;

                _lastTick = snapshot.Tick;
                _snapshots++;

                SnapshotReceived?.Invoke(snapshot);

                foreach (var player in snapshot.Players)
                {
                    if (player.PlayerId != _player)
                        continue;

                    _position.Text =
                        $"Posición en Go: X={player.PositionX:F2} " +
                        $"Z={player.PositionZ:F2}\n" +
                        $"Último input procesado: {player.LastProcessedInput}";

                    break;
                }

                if (_snapshots == 1)
                    Record("← Primer snapshot de jugadores recibido.");

                break;
        }
    }

    private void Record(string text)
    {
        GD.Print(text);
        _lines.Enqueue($"[{_elapsed:F1}s] {text}");
        while (_lines.Count > 24) _lines.Dequeue();
        _log.Text = string.Join("\n", _lines);
    }

    public override void _ExitTree() => Close();
}
