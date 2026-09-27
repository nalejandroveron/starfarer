using System.Numerics;
using System.Security.Cryptography;
using Google.Protobuf;
using Starfarer.Netcode;
using Starfarer.Protocol.V1;

namespace Starfarer.Networking;

public sealed class GameClient : IDisposable
{
    private readonly Func<IDatagramTransport> _transportFactory;
    private EventChannel<ClientEvents, ServerEvents>? _channel;
    private readonly NetworkClock _clock = new();
    private readonly BaselineAssembler<PlayerState> _assembler = new();
    private readonly MovementPrediction _prediction = new();
    private ReliableChannel _reliable = new();
    private readonly TrafficBudget _sendBudget = new(32768, 4096);
    private readonly HashSet<double> _probes = new();
    private byte[] _connection = Array.Empty<byte>(), _token = Array.Empty<byte>();
    private ulong _attempt, _epoch, _previousPlayer;
    private double _elapsed, _lastReceive, _nextHello, _nextSend, _nextProbe;
    private Vector2 _visualError, _fractional;
    private bool _autoReconnect;
    private double _retryAt, _retryDelay = 0.5;
    public ulong PlayerId { get; private set; }
    public ulong LastInput => _prediction.LastTick;
    public ulong LastTick { get; private set; }
    public int Snapshots { get; private set; }
    public int PendingCount => _prediction.Pending;
    public uint SnapshotHz { get; private set; }
    public uint SimulationHz { get; private set; }
    public ulong TimingRevision { get; private set; }
    public double Rtt => _clock.Rtt;
    public double Jitter => _clock.Jitter;
    public double RenderTime => _clock.ServerTime(_elapsed) - _clock.InterpolationDelay(SnapshotHz);
    public Vector2 PredictedPosition => _prediction.Position;
    public Vector2 VisualPosition => _prediction.Position + _fractional + _visualError;
    public string Status { get; private set; } = "DESCONECTADO";
    public event Action<WorldSnapshot>? SnapshotReceived;
    public event Action? SessionClosed;
    public event Action<string>? Log;
    public event Action<string>? ActionResult;
    public GameClient(Func<IDatagramTransport> transportFactory) => _transportFactory = transportFactory;

    public void Connect()
    {
        Disconnect(); _connection = RandomNumberGenerator.GetBytes(16); _attempt++;
        _assembler.Reset(); _probes.Clear(); LastTick = 0; Snapshots = 0;
        try
        {
            IDatagramTransport transport = _transportFactory();
            string? configuredKey = Environment.GetEnvironmentVariable("STARFARER_NET_KEY");
            if (!string.IsNullOrEmpty(configuredKey))
            {
                try { transport = new ProtectedTransport(transport, Convert.FromBase64String(configuredKey)); }
                catch { transport.Dispose(); throw; }
            }
            _channel = new(transport, new GameEventCodec());
            _lastReceive = _elapsed; _nextHello = _elapsed;
            Status = "CONECTANDO"; Log?.Invoke("Conectando...");
        }
        catch (Exception ex) { Status = "ERROR DE CONEXIÓN"; Log?.Invoke(ex.Message); }
    }
    public void Disconnect()
    {
        _channel?.Dispose(); _channel = null; PlayerId = 0; SnapshotHz = 0;
        _autoReconnect = false;
        Status = "DESCONECTADO"; SessionClosed?.Invoke();
    }
    public bool SendEcho(string text)
    {
        if (PlayerId == 0 || System.Text.Encoding.UTF8.GetByteCount(text) is < 1 or > 256) return false;
        return _reliable.Enqueue(new DiagnosticAction { Echo = text }.ToByteArray());
    }
    private void Send(ClientEvents message)
    {
        message.SessionEpoch = _epoch; message.ResumeToken = ByteString.CopyFrom(_token); message.SnapshotAck = LastTick;
        if (_sendBudget.Spend(message.CalculateSize() + 54, _elapsed)) _channel?.Send(message);
    }
    private void Retry()
    {
        Disconnect(); _autoReconnect = true; _retryAt = _elapsed + _retryDelay;
        _retryDelay = Math.Min(5, _retryDelay * 2); Status = "RECONECTANDO";
    }
    public void Update(double delta, MoveInput direction)
    {
        if (!double.IsFinite(delta) || delta < 0) throw new ArgumentOutOfRangeException(nameof(delta));
        _elapsed += delta;
        if (_channel == null) { if (_autoReconnect && _elapsed >= _retryAt) Connect(); return; }
        try
        {
            _channel.Poll(Receive, ex => Log?.Invoke(ex.Message));
            if (PlayerId != 0 && _elapsed - _lastReceive > 5) { Retry(); return; }
            if (PlayerId == 0)
            {
                if (_elapsed >= _nextHello)
                {
                    _nextHello = _elapsed + 1;
                    Send(new ClientEvents { Hello = new Hello { ClientVersion = "starfarer/2", ConnectionId = ByteString.CopyFrom(_connection), ResumeToken = ByteString.CopyFrom(_token), Attempt = _attempt, ClientTime = _elapsed } });
                }
                return;
            }
            _visualError *= (float)Math.Exp(-12 * delta);
            double targetTime = _clock.ServerTime(_elapsed) + _clock.Lead;
            _prediction.Generate((ulong)(targetTime * MovementPrediction.InputHz), direction);
            float fraction = (float)Math.Clamp(targetTime * MovementPrediction.InputHz - _prediction.LastTick, 0, 1);
            _fractional = (MovementPrediction.Step(_prediction.Position, direction) - _prediction.Position) * fraction;
            if (_elapsed >= _nextSend)
            {
                _nextSend = _elapsed + 1.0 / 60;
                var batch = new PlayerInputBatch();
                foreach (var input in _prediction.Recent(16))
                    batch.Inputs.Add(new PlayerInput { Sequence = input.Key, MoveX = input.Value.X, MoveZ = input.Value.Z });
                Send(new ClientEvents { PlayerInputs = batch });
            }
            if (_elapsed >= _nextProbe)
            {
                _nextProbe = _elapsed + 0.5;
                _probes.RemoveWhere(sent => _elapsed - sent > 2); _probes.Add(_elapsed);
                Send(new ClientEvents { ClockProbe = new ClockProbe { ClientTime = _elapsed } });
            }
            if (_reliable.TrySend(_elapsed, 2 * _clock.Rtt, out var action))
                Send(new ClientEvents { Reliable = new ReliableFrame { Sequence = action.Sequence, Payload = ByteString.CopyFrom(action.Payload) } });
        }
        catch (Exception ex) when (ex is InvalidOperationException or FormatException or System.Net.Sockets.SocketException)
        { Log?.Invoke(ex.Message); Retry(); }
    }
    private static bool Valid(PlayerState state) => state.PlayerId != 0 && float.IsFinite(state.PositionX) && float.IsFinite(state.PositionZ);
    private void Receive(ServerEvents message)
    {
        if (message.Welcome != null)
        {
            var welcome = message.Welcome;
            if (PlayerId != 0 || !welcome.ConnectionId.Span.SequenceEqual(_connection) || welcome.PlayerId == 0 ||
                welcome.ResumeToken.Length != 32 || welcome.InputHz != MovementPrediction.InputHz || welcome.SnapshotHz is < 1 or > 240 ||
                welcome.SimulationHz is < 1 or > 240 || welcome.SessionEpoch == 0 || welcome.SessionEpoch != message.SessionEpoch ||
                welcome.Player == null || !Valid(welcome.Player) || welcome.Player.PlayerId != welcome.PlayerId ||
                !double.IsFinite(welcome.ServerTime) || welcome.ServerTime < 0 ||
                !double.IsFinite(welcome.ClientTime) || welcome.ClientTime > _elapsed || _elapsed - welcome.ClientTime > 2) return;
            if (_previousPlayer != 0 && (_previousPlayer != welcome.PlayerId || !_token.AsSpan().SequenceEqual(welcome.ResumeToken.Span)))
            { _reliable = new(); Log?.Invoke("La sesión anterior expiró; las acciones pendientes no se reproducen en la nueva sesión."); }
            PlayerId = _previousPlayer = welcome.PlayerId; _epoch = welcome.SessionEpoch; _token = welcome.ResumeToken.ToByteArray();
            SnapshotHz = welcome.SnapshotHz; SimulationHz = welcome.SimulationHz; TimingRevision = 0; _lastReceive = _elapsed;
            _retryDelay = 0.5;
            _clock.Initialize(welcome.ServerTime, welcome.ClientTime, _elapsed);
            _prediction.Reset(welcome.Player.ResolvedTick, new(welcome.Player.PositionX, welcome.Player.PositionZ)); _visualError = _fractional = Vector2.Zero;
            _reliable.Acknowledge(welcome.ActionAck);
            Status = $"CONECTADO · jugador {PlayerId}";
            Log?.Invoke($"Welcome player={PlayerId} sim={welcome.SimulationHz} Hz snapshots={SnapshotHz} Hz");
            return;
        }
        if (PlayerId == 0 || message.SessionEpoch != _epoch) return;
        switch (message.EventCase)
        {
            case ServerEvents.EventOneofCase.ClockReply:
                var clock = message.ClockReply;
                if (_probes.Remove(clock.ClientTime) && _clock.Probe(clock.ServerTime, clock.ClientTime, _elapsed)) _lastReceive = _elapsed;
                break;
            case ServerEvents.EventOneofCase.ReliableAck:
                _reliable.Acknowledge(message.ReliableAck.Sequence); _lastReceive = _elapsed; break;
            case ServerEvents.EventOneofCase.Reliable:
                var frame = message.Reliable;
                ulong ack = _reliable.Receive(frame.Sequence, frame.Payload.ToByteArray(), payload =>
                {
                    try { var result = DiagnosticResult.Parser.ParseFrom(payload); ActionResult?.Invoke(result.Echo); return true; }
                    catch (InvalidProtocolBufferException) { return false; }
                });
                Send(new ClientEvents { ReliableAck = new ReliableAck { Sequence = ack } }); _lastReceive = _elapsed; break;
            case ServerEvents.EventOneofCase.Snapshot:
                var part = message.Snapshot;
                if (part.SnapshotHz is < 1 or > 240 || part.Tick <= LastTick || !double.IsFinite(part.ServerTime) || part.ServerTime < 0 ||
                    Math.Abs(part.ServerTime * MovementPrediction.InputHz - part.Tick) > 0.01 || part.Players.Any(player => !Valid(player) || player.ResolvedTick > part.Tick)) return;
                var players = _assembler.Add(part.Tick, (int)part.PartIndex, (int)part.PartCount, part.ServerTime, _elapsed, part.Players.ToArray());
                if (players == null) return;
                var local = players.FirstOrDefault(player => player.PlayerId == PlayerId);
                if (local == null || players.Select(player => player.PlayerId).Distinct().Count() != players.Length) return;
                _lastReceive = _elapsed; LastTick = part.Tick; SnapshotHz = part.SnapshotHz; Snapshots++;
                if (part.TimingRevision > TimingRevision)
                {
                    TimingRevision = part.TimingRevision; SimulationHz = part.SimulationHz;
                    Log?.Invoke($"Timing revision={TimingRevision} sim={SimulationHz} Hz snapshots={SnapshotHz} Hz");
                }
                _clock.Observe(part.ServerTime, _elapsed);
                Vector2 before = VisualPosition;
                _prediction.Reconcile(local.ResolvedTick, new(local.PositionX, local.PositionZ));
                _visualError = before - (_prediction.Position + _fractional);
                if (_visualError.LengthSquared() > 4) _visualError = Vector2.Zero;
                var baseline = part.Clone(); baseline.PartIndex = 0; baseline.PartCount = 1; baseline.Players.Clear(); baseline.Players.AddRange(players);
                SnapshotReceived?.Invoke(baseline);
                if (Snapshots == 1) Log?.Invoke("Primer snapshot completo recibido.");
                break;
        }
    }
    public void Dispose() => Disconnect();
}
