namespace Starfarer.Netcode;

// One poll delivers one complete datagram; null means no message is ready.
public interface IDatagramTransport : IDisposable
{
    void Send(byte[] data);
    byte[]? Receive();
}

public interface IEventCodec<TOutgoing, TIncoming>
{
    byte[] Encode(TOutgoing message);
    TIncoming Decode(byte[] data);
}

public sealed class EventChannel<TOutgoing, TIncoming> : IDisposable
{
    private readonly IDatagramTransport _transport;
    private readonly IEventCodec<TOutgoing, TIncoming> _codec;
    private readonly int _maxBytes;

    public EventChannel(IDatagramTransport transport,
        IEventCodec<TOutgoing, TIncoming> codec, int maxBytes = 1100)
    {
        if (maxBytes < 1) throw new ArgumentOutOfRangeException(nameof(maxBytes));
        _transport = transport;
        _codec = codec;
        _maxBytes = maxBytes;
    }

    public void Send(TOutgoing message)
    {
        var bytes = _codec.Encode(message);
        Validate(bytes);
        _transport.Send(bytes);
    }

    public void Poll(Action<TIncoming> receive, Action<Exception> rejected, int budget = 128)
    {
        for (int i = 0; i < budget; i++)
        {
            var bytes = _transport.Receive();
            if (bytes == null) return;
            TIncoming message;
            try { Validate(bytes); message = _codec.Decode(bytes); }
            catch (Exception ex) when (ex is FormatException or ArgumentException)
            { rejected(ex); continue; }
            receive(message);
        }
    }

    private void Validate(byte[] bytes)
    {
        if (bytes.Length == 0 || bytes.Length > _maxBytes)
            throw new FormatException($"Invalid datagram size: {bytes.Length}");
    }

    public void Dispose() => _transport.Dispose();
}
