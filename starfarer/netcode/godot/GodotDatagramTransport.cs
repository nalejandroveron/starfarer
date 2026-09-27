using Godot;
using Starfarer.Netcode;

namespace Starfarer.Netcode.GodotAdapter;

// Engine-specific adapter; excluded from the headless test runner.
public sealed class GodotDatagramTransport : IDatagramTransport
{
    private readonly PacketPeerUdp _peer = new();

    public GodotDatagramTransport(string host, int port)
    {
        var error = _peer.ConnectToHost(host, port);
        if (error != Error.Ok) { _peer.Dispose(); throw new InvalidOperationException($"UDP connect: {error}"); }
    }

    public void Send(byte[] data)
    {
        var error = _peer.PutPacket(data);
        if (error != Error.Ok) throw new InvalidOperationException($"UDP send: {error}");
    }

    public byte[]? Receive() =>
        _peer.GetAvailablePacketCount() > 0 ? _peer.GetPacket() : null;

    public void Dispose() { _peer.Close(); _peer.Dispose(); }
}
