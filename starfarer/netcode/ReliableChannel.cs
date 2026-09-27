namespace Starfarer.Netcode;

public readonly record struct ReliablePacket(ulong Sequence, byte[] Payload);

// Delivery is ordered and deduplicated during the retained session. Application
// acceptance is separate from durable persistence or completion of side effects.
public sealed class ReliableChannel
{
    private readonly Queue<ReliablePacket> _pending = new();
    private double _next;
    public ulong Sent { get; private set; }
    public ulong Received { get; private set; }
    public int Pending => _pending.Count;
    public bool Enqueue(byte[] payload)
    {
        if (payload.Length is < 1 or > 512 || _pending.Count >= 64 || Sent == ulong.MaxValue) return false;
        _pending.Enqueue(new(++Sent, (byte[])payload.Clone())); return true;
    }
    public bool TrySend(double now, double retry, out ReliablePacket packet)
    {
        packet = default;
        if (_pending.Count == 0 || now < _next) return false;
        _next = now + Math.Clamp(retry, 0.1, 1); packet = _pending.Peek(); return true;
    }
    public bool Acknowledge(ulong sequence)
    {
        if (_pending.Count == 0 || _pending.Peek().Sequence != sequence) return false;
        _pending.Dequeue(); _next = 0; return true;
    }
    public ulong Receive(ulong sequence, byte[] payload, Func<byte[], bool> accept)
    {
        if (sequence != 0 && sequence == Received + 1 && payload.Length is > 0 and <= 512 && accept(payload)) Received = sequence;
        return Received;
    }
}
