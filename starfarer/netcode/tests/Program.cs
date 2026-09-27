using System.Diagnostics;
using System.Net;
using System.Net.Sockets;
using System.Numerics;
using Google.Protobuf;
using Starfarer.Netcode;
using Starfarer.Networking;
using Starfarer.Protocol.V1;

static class Program
{
    static void Check(bool condition, string message) { if (!condition) throw new Exception(message); }
    static int Main(string[] args)
    {
        try
        {
            // Unit tests use explicit fake transports, independent of deployment keys.
            string? key = Environment.GetEnvironmentVariable("STARFARER_NET_KEY");
            Environment.SetEnvironmentVariable("STARFARER_NET_KEY", null);
            try { CoreTests(); BoundaryTests(); }
            finally { Environment.SetEnvironmentVariable("STARFARER_NET_KEY", key); }
            if (args.Length >= 2 && args[0] == "--integration") Integration(int.Parse(args[1]), args.Contains("--impaired"));
            Console.WriteLine("PASS netcode tests"); return 0;
        }
        catch (Exception ex) { Console.Error.WriteLine(ex); return 1; }
    }
    static void CoreTests()
    {
        var pending = new PendingInputs<int>(2);
        var budget = new TrafficBudget(100, 200);
        Check(budget.Spend(200, 0) && !budget.Spend(1, 0) && budget.Spend(100, 1), "traffic budget");
        Check(pending.TryAdd(10, out _) && pending.TryAdd(20, out _) && !pending.TryAdd(30, out _), "history capacity");
        Check(!pending.Acknowledge(3) && pending.Acknowledge(1), "ack validation");
        Check(pending.Replay(5, (state, input) => state + input.Value) == 25, "generic replay");
        var buffer = new SnapshotBuffer<float>(3);
        Check(!buffer.TrySample(0, Lerp, out _), "empty interpolation");
        buffer.Add(1, 10); buffer.Add(2, 20);
        Check(buffer.TrySample(1.5, Lerp, out var middle) && middle == 15, "interpolation");
        Check(!buffer.Add(0, 99) && !buffer.Add(double.NaN, 0), "invalid interpolation time");
        Check(buffer.TrySample(3, Lerp, out var held) && held == 20, "missing future sample");
        var clock = new FixedStepClock(1.0 / 30); int steps = 0;
        for (int i = 0; i < 60; i++) steps += clock.Advance(1.0 / 60);
        Check(steps == 30 && clock.Advance(1) == 5 && clock.DroppedSteps > 0, "fixed-step clock");
        var networkClock = new NetworkClock(); networkClock.Initialize(10, 0, 0.2);
        Check(Math.Abs(networkClock.ServerTime(0.2) - 10.1) < 1e-6, "clock offset");
        networkClock.Observe(10, 0.2); networkClock.Observe(10.1, 0.4);
        Check(networkClock.Jitter > 0 && networkClock.Lead > 0.1, "jitter adaptation");
        var assembler = new BaselineAssembler<int>();
        Check(assembler.Add(10, 1, 2, 1, 0, new[] { 2 }) == null, "partial baseline applied");
        Check(assembler.Add(10, 0, 3, 1, 0, new[] { 9 }) == null, "inconsistent fragmentation accepted");
        var complete = assembler.Add(10, 0, 2, 1, 0, new[] { 1 });
        Check(complete != null && complete.SequenceEqual(new[] { 1, 2 }), "fragment order");
        Check(assembler.Add(9, 0, 1, 1, 0, new[] { 99 }) == null, "old baseline applied");
        for (ulong tick = 11; tick < 100; tick++) assembler.Add(tick, 0, 2, tick, 0, new[] { 1 });
        Check(assembler.Pending <= 8, "unbounded fragment memory");
        var prediction = new MovementPrediction(); prediction.Reset(0, Vector2.Zero);
        prediction.Generate(120, new MoveInput(0.6f, 0.8f));
        Check(Vector2.Distance(prediction.Position, new Vector2(6, 8)) < 0.0001, "Go/C# movement vector mismatch");
        prediction.Reconcile(60, new Vector2(3, 4));
        Check(prediction.Pending == 60 && Vector2.Distance(prediction.Position, new(6, 8)) < 0.0001, "reconciliation replay");
        prediction.Reconcile(200, new(10, 0));
        Check(prediction.Pending == 0 && prediction.LastTick == 200, "resolved fallback frontier blocked history");
        prediction.Generate(1000, new(1, 0));
        Check(prediction.Pending == 1 && prediction.Position.X < 11, "long client pause replayed unbounded time");
        ReliableTests(); ProtectionTests();
    }
    static float Lerp(float a, float b, float t) => a + (b - a) * t;
    static void ReliableTests()
    {
        var sender = new ReliableChannel(); var receiver = new ReliableChannel(); int executions = 0;
        for (int i = 0; i < 64; i++) Check(sender.Enqueue(new byte[] { (byte)i }), "bounded action queue");
        Check(!sender.Enqueue(new byte[] { 99 }), "action backpressure");
        for (int i = 0; i < 64; i++)
        {
            Check(sender.TrySend(i, 0.1, out var packet), "pending action missing");
            ulong ack = receiver.Receive(packet.Sequence, packet.Payload, _ => { executions++; return true; });
            receiver.Receive(packet.Sequence, packet.Payload, _ => { executions++; return true; });
            Check(!sender.Acknowledge(ack + 1) && sender.Acknowledge(ack), "action acknowledgement");
        }
        Check(executions == 64 && sender.Pending == 0, "duplicate action executed");
    }
    static void ProtectionTests()
    {
        var a = new FakeTransport(); var b = new FakeTransport(); byte[] key = Enumerable.Repeat((byte)7, 32).ToArray();
        using var sender = new ProtectedTransport(a, key); using var receiver = new ProtectedTransport(b, key);
        sender.Send(new byte[] { 1 }); sender.Send(new byte[] { 2 });
        b.Incoming.Enqueue(a.Sent[1]); b.Incoming.Enqueue(a.Sent[0]); b.Incoming.Enqueue(a.Sent[0]);
        Check(receiver.Receive()![0] == 2 && receiver.Receive()![0] == 1 && receiver.Receive() == null, "protected reorder/replay");
        sender.Send(new byte[] { 3 }); byte[] corrupt = (byte[])a.Sent[2].Clone(); corrupt[^1] ^= 1;
        b.Incoming.Enqueue(corrupt); Check(receiver.Receive() == null, "tampered ciphertext accepted");
        b.Incoming.Enqueue(a.Sent[2]); Check(receiver.Receive()![0] == 3, "tamper poisoned replay window");
    }
    static void BoundaryTests()
    {
        var transport = new FakeTransport(); using var client = new GameClient(() => transport);
        client.Connect(); client.Update(0, new());
        var hello = ClientEvents.Parser.ParseFrom(transport.Sent[0]).Hello;
        var welcome = new ServerEvents { ProtocolVersion = 2, SessionEpoch = 1, Welcome = new Welcome {
            PlayerId = 1, SimulationHz = 20, SnapshotHz = 4, InputHz = 60, InputBatchLimit = 16, InputHistoryLimit = 256,
            ConnectionId = hello.ConnectionId, ResumeToken = ByteString.CopyFrom(new byte[32]), SessionEpoch = 1,
            Player = new PlayerState { PlayerId = 1 } } };
        void Deliver(ServerEvents message) => transport.Incoming.Enqueue(message.ToByteArray());
        transport.Incoming.Enqueue(new byte[] { 255 }); transport.Incoming.Enqueue(new byte[1101]); Deliver(welcome);
        client.Update(0.05, new(1, 0));
        Check(client.PlayerId == 1 && client.PredictedPosition.X > 0 && client.SnapshotHz == 4, "prediction waits for snapshot");
        ulong input = client.LastInput; Deliver(welcome); client.Update(0, new(1, 0));
        Check(client.LastInput == input, "duplicate welcome reset history");
        var snapshot = new WorldSnapshot { Tick = 3, ServerTime = 3.0 / 60, PartCount = 2, SnapshotHz = 7, SimulationHz = 30,
            Players = { new PlayerState { PlayerId = 1, ResolvedTick = 3, PositionX = 0.25f } } };
        Deliver(new ServerEvents { ProtocolVersion = 2, SessionEpoch = 1, Snapshot = snapshot }); client.Update(0, new());
        Check(client.Snapshots == 0, "partial snapshot changed visibility");
        var second = snapshot.Clone(); second.PartIndex = 1; second.Players.Clear(); second.Players.Add(new PlayerState { PlayerId = 2 });
        Deliver(new ServerEvents { ProtocolVersion = 2, SessionEpoch = 1, Snapshot = second }); client.Update(0, new());
        Check(client.Snapshots == 1 && client.SnapshotHz == 7, "complete baseline/live frequency missing");
        var stale = snapshot.Clone(); stale.Tick = 60; stale.ServerTime = 1; stale.PartCount = 1;
        Deliver(new ServerEvents { ProtocolVersion = 2, SessionEpoch = 99, Snapshot = stale }); client.Update(0, new());
        Check(client.Snapshots == 1, "stale authority epoch accepted");
        client.Connect(); client.Update(0, new());
        var resume = ClientEvents.Parser.ParseFrom(transport.Sent[^1]).Hello;
        Check(resume.ResumeToken.Length == 32 && resume.Attempt == 2 && !resume.ConnectionId.Equals(hello.ConnectionId), "resume identity not preserved");
    }
    static void Integration(int port, bool impaired)
    {
        var watch = Stopwatch.StartNew(); int factoryCount = 0;
        IDatagramTransport Factory() => impaired ? new ImpairedTransport(new SocketTransport(port), watch, 42 + factoryCount++) : new SocketTransport(port);
        using var a = new GameClient(Factory); using var b = new GameClient(() => new SocketTransport(port));
        PlayerState? stateA = null, stateB = null; int seenByA = 0, seenByB = 0, results = 0; ulong firstId = 0;
        a.SnapshotReceived += snapshot => { seenByA = snapshot.Players.Count; stateA = snapshot.Players.FirstOrDefault(p => p.PlayerId == a.PlayerId); };
        b.SnapshotReceived += snapshot => { seenByB = snapshot.Players.Count; stateB = snapshot.Players.FirstOrDefault(p => p.PlayerId == b.PlayerId); };
        a.ActionResult += text => { Check(text == "exactly once", "wrong reliable result"); results++; };
        a.Log += text => Console.WriteLine("CLIENT " + text);
        a.Connect(); b.Connect(); double previous = 0; bool queued = false;
        double duration = impaired ? 15 : 3;
        while (watch.Elapsed.TotalSeconds < duration)
        {
            double now = watch.Elapsed.TotalSeconds, delta = now - previous; previous = now;
            a.Update(delta, new(1, 0)); b.Update(delta, new());
            if (a.PlayerId != 0)
            {
                if (firstId == 0) firstId = a.PlayerId;
                Check(a.PlayerId == firstId, "reconnection lost player identity");
                if (!queued) queued = a.SendEcho("exactly once");
            }
            Thread.Sleep(5);
        }
        Check(a.PlayerId != 0 && b.PlayerId != 0 && a.PlayerId != b.PlayerId, "distinct connected sessions");
        Check(seenByA >= 2 && seenByB >= 2, "missing nearby players");
        Check(stateA != null && stateA.PositionX > 5 && stateA.ResolvedTick > 0, "authoritative movement missing");
        Check(stateB != null && stateB.PositionX == 0 && stateB.PositionZ == 0, "input crossed sessions");
        Check(a.PendingCount < 80 && b.PendingCount < 40, "history backlog after recovery");
        Check(results == 1, "reliable action did not execute exactly once in session");
        Check(Math.Abs(a.PredictedPosition.X - stateA!.PositionX) < 4, "prediction diverged after recovery");
        Console.WriteLine($"PASS UDP impaired={impaired} player={a.PlayerId} x={stateA.PositionX:F2} predicted={a.PredictedPosition.X:F2} pending={a.PendingCount} results={results} snapshots={a.Snapshots} snapshotHz={a.SnapshotHz}");
    }
    sealed class FakeTransport : IDatagramTransport
    {
        public Queue<byte[]> Incoming { get; } = new(); public List<byte[]> Sent { get; } = new();
        public void Send(byte[] data) => Sent.Add(data); public byte[]? Receive() => Incoming.TryDequeue(out var bytes) ? bytes : null; public void Dispose() { }
    }
    sealed class SocketTransport : IDatagramTransport
    {
        readonly Socket _socket = new(AddressFamily.InterNetwork, SocketType.Dgram, ProtocolType.Udp);
        public SocketTransport(int port) { _socket.Connect(IPAddress.Loopback, port); _socket.Blocking = false; }
        public void Send(byte[] data) => _socket.Send(data);
        public byte[]? Receive()
        {
            var data = new byte[65535];
            try { int count = _socket.Receive(data); return data[..count]; }
            catch (SocketException ex) when (ex.SocketErrorCode is SocketError.WouldBlock or SocketError.ConnectionReset) { return null; }
        }
        public void Dispose() => _socket.Dispose();
    }
    sealed class ImpairedTransport : IDatagramTransport
    {
        readonly IDatagramTransport _inner; readonly Stopwatch _watch; readonly Random _random;
        readonly List<(double At, byte[] Data)> _outgoing = new(), _incoming = new();
        public ImpairedTransport(IDatagramTransport inner, Stopwatch watch, int seed) { _inner = inner; _watch = watch; _random = new(seed); }
        bool Outage => _watch.Elapsed.TotalSeconds is >= 3 and < 8;
        void Schedule(List<(double At, byte[] Data)> queue, byte[] data)
        {
            if (Outage || _random.NextDouble() < 0.1 || queue.Count >= 512) return;
            queue.Add((_watch.Elapsed.TotalSeconds + 0.075 + _random.NextDouble() * 0.12 - 0.06, data));
            if (_random.NextDouble() < 0.03) queue.Add((_watch.Elapsed.TotalSeconds + 0.2, data));
        }
        void Pump()
        {
            if (Outage) { _outgoing.Clear(); _incoming.Clear(); }
            foreach (var item in _outgoing.Where(item => item.At <= _watch.Elapsed.TotalSeconds).ToArray()) { if (!Outage) _inner.Send(item.Data); _outgoing.Remove(item); }
            for (int i = 0; i < 128; i++) { var data = _inner.Receive(); if (data == null) break; Schedule(_incoming, data); }
        }
        public void Send(byte[] data) { Pump(); Schedule(_outgoing, data); }
        public byte[]? Receive()
        {
            Pump(); var due = _incoming.Where(item => item.At <= _watch.Elapsed.TotalSeconds).OrderBy(item => item.At).Take(1).ToArray();
            if (due.Length == 0) return null; _incoming.Remove(due[0]); return due[0].Data;
        }
        public void Dispose() => _inner.Dispose();
    }
}
