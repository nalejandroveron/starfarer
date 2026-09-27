using Godot;
using Starfarer.Networking;
using Starfarer.Netcode.GodotAdapter;
using Starfarer.Protocol.V1;

public partial class TransportProbe : Control
{
    private GameClient? _client;
    private Label _status = null!;
    private Label _stats = null!;
    private RichTextLabel _log = null!;
    private Label _position = null!;
    private readonly Queue<string> _lines = new();
    private ulong _lastProcessTime;
    public ulong LocalPlayerId => _client?.PlayerId ?? 0;
    public uint SnapshotHz => _client?.SnapshotHz ?? 0;
    public double RenderTime => _client?.RenderTime ?? 0;
    public Vector3 LocalVisualPosition => _client == null ? Vector3.Zero : new(_client.VisualPosition.X, 0, _client.VisualPosition.Y);
    public event Action<WorldSnapshot>? SnapshotReceived;
    public event Action? SessionClosed;
    public override void _Ready()
    {
        int port = 7777;
        string host = "127.0.0.1";
        foreach (var arg in OS.GetCmdlineUserArgs())
        {
            if (arg.StartsWith("--server-port=") && int.TryParse(arg[14..], out var parsed) && parsed is > 0 and <= 65535)
                port = parsed;
            if (arg.StartsWith("--server-host=") && !string.IsNullOrWhiteSpace(arg[14..]))
                host = arg[14..];
        }
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
        column.AddChild(new Label { Text = $"Godot C# ↔ Go · UDP {host}:{port} · sin gRPC" });
        var buttons = new HBoxContainer();
        column.AddChild(buttons);
        var connect = new Button { Text = "Conectar / reconectar" };
        connect.Pressed += Connect;
        buttons.AddChild(connect);
        var disconnect = new Button { Text = "Desconectar" };
        disconnect.Pressed += () => _client?.Disconnect();
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


        column.AddChild(new Label { Text = "Predicción local · reconciliación con Go · visibilidad por cercanía" });
        var echo = new Button { Text = "Probar acción confirmada" };
        echo.Pressed += () => { if (_client?.SendEcho("Prueba " + Time.GetTicksMsec()) != true) Record("Cola de acciones llena o sin conexión."); };
        column.AddChild(echo);
        _log = new RichTextLabel { SizeFlagsVertical = SizeFlags.ExpandFill, ScrollFollowing = true };
        column.AddChild(_log);
        column.AddChild(new Label { Text = "Sesión recuperable · acciones confirmadas · cifrado con STARFARER_NET_KEY\nFrecuencias indicadas por Go. Sin persistencia de mundo todavía." });
        _client = new GameClient(() => new GodotDatagramTransport(host, port));
        _client.Log += Record;
        _client.ActionResult += OnActionResult;
        _client.SessionClosed += OnSessionClosed;
        _client.SnapshotReceived += OnSnapshot;
        _client.Connect();
        _lastProcessTime = Time.GetTicksUsec();
    }

    private void Connect() => _client?.Connect();

    public override void _Process(double delta)
    {
        if (_client == null) return;
        Vector2 direction = GetWindow().HasFocus()
            ? Input.GetVector("move_left", "move_right", "move_forward", "move_backward")
            : Vector2.Zero;
        ulong now = Time.GetTicksUsec();
        _client.Update((now - _lastProcessTime) / 1000000.0, new MoveInput(direction.X, direction.Y));
        _lastProcessTime = now;
        _status.Text = _client.Status;
        _stats.Text = $"Jugador: {_client.PlayerId} | Input: {_client.LastInput} | Pendientes: {_client.PendingCount}\nSnapshots: {_client.Snapshots} | Tick: {_client.LastTick} | {_client.SnapshotHz} Hz\nRTT: {_client.Rtt * 1000:F0} ms | Jitter: {_client.Jitter * 1000:F0} ms";
    }

    private void OnSnapshot(WorldSnapshot snapshot)
    {
        foreach (var player in snapshot.Players)
        {
            if (player.PlayerId != LocalPlayerId) continue;
            _position.Text = $"Posición Go: X={player.PositionX:F2} Z={player.PositionZ:F2}\nInput confirmado: {player.LastProcessedInput}";
            break;
        }
        SnapshotReceived?.Invoke(snapshot);
    }

    private void OnSessionClosed()
    {
        _position.Text = "Esperando posición del servidor...";
        SessionClosed?.Invoke();
    }
    private void OnActionResult(string result) => Record("Acción confirmada: " + result);

    private void Record(string text)
    {
        GD.Print(text);
        _lines.Enqueue(text);
        while (_lines.Count > 24) _lines.Dequeue();
        _log.Text = string.Join("\n", _lines);
    }

    public override void _ExitTree()
    {
        if (_client == null) return;
        _client.Log -= Record;
        _client.ActionResult -= OnActionResult;
        _client.SessionClosed -= OnSessionClosed;
        _client.SnapshotReceived -= OnSnapshot;
        _client.Dispose();
    }
}
