using System.Buffers.Binary;
using System.Security.Cryptography;

namespace Starfarer.Netcode;

// Wire-compatible with Go's Protector. Group PSK is provisioned out of band;
// account login and individual authorization remain application concerns.
public sealed class ProtectedTransport : IDatagramTransport
{
    private sealed class Peer : IDisposable
    {
        public ulong High, Bits;
        public readonly AesGcm Cipher;
        public Peer(byte[] key) => Cipher = new(key, 16);
        public void Dispose() => Cipher.Dispose();
    }
    private readonly IDatagramTransport _inner;
    private readonly byte[] _key, _sender = RandomNumberGenerator.GetBytes(16);
    private readonly AesGcm _cipher;
    private readonly Dictionary<string, Peer> _peers = new();
    private ulong _counter;
    public ProtectedTransport(IDatagramTransport inner, byte[] key)
    {
        if (key.Length != 32) throw new ArgumentException("Packet key must contain 32 bytes.");
        _inner = inner; _key = (byte[])key.Clone(); _cipher = new(Derive(_sender), 16);
    }
    private byte[] Derive(ReadOnlySpan<byte> id) => HMACSHA256.HashData(_key, id);
    public void Send(byte[] data)
    {
        if (data.Length is < 1 or > 1100 || _counter == ulong.MaxValue) throw new InvalidOperationException("Invalid protected packet.");
        var packet = new byte[data.Length + 52];
        "SFN2"u8.CopyTo(packet); _sender.CopyTo(packet, 4);
        BinaryPrimitives.WriteUInt64BigEndian(packet.AsSpan(20), ++_counter);
        BinaryPrimitives.WriteInt64BigEndian(packet.AsSpan(28), DateTimeOffset.UtcNow.ToUnixTimeMilliseconds());
        Span<byte> nonce = stackalloc byte[12]; nonce.Clear(); packet.AsSpan(20, 8).CopyTo(nonce[4..]);
        _cipher.Encrypt(nonce, data, packet.AsSpan(36, data.Length), packet.AsSpan(36 + data.Length, 16), packet.AsSpan(0, 36));
        _inner.Send(packet);
    }
    public byte[]? Receive()
    {
        for (int attempt = 0; attempt < 128; attempt++)
        {
            var packet = _inner.Receive(); if (packet == null) return null;
            if (packet.Length is < 53 or > 1152 || !packet.AsSpan(0, 4).SequenceEqual("SFN2"u8)) continue;
            long stamp = BinaryPrimitives.ReadInt64BigEndian(packet.AsSpan(28, 8));
            long now = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
            if (stamp < now - 30000 || stamp > now + 30000) continue;
            ulong sequence = BinaryPrimitives.ReadUInt64BigEndian(packet.AsSpan(20, 8)); if (sequence == 0) continue;
            string id = Convert.ToHexString(packet.AsSpan(4, 16));
            bool known = _peers.TryGetValue(id, out var peer);
            if (!known && _peers.Count >= 8) continue;
            peer ??= new(Derive(packet.AsSpan(4, 16)));
            var plain = new byte[packet.Length - 52]; var nonce = new byte[12]; packet.AsSpan(20, 8).CopyTo(nonce.AsSpan(4));
            try { peer.Cipher.Decrypt(nonce, packet.AsSpan(36, plain.Length), packet.AsSpan(packet.Length - 16), plain, packet.AsSpan(0, 36)); }
            catch (CryptographicException) { if (!known) peer.Dispose(); continue; }
            if (!known) _peers.Add(id, peer);
            if (sequence > peer.High)
            {
                ulong shift = sequence - peer.High; peer.Bits = shift >= 64 ? 0 : peer.Bits << (int)shift;
                peer.High = sequence; peer.Bits |= 1;
            }
            else
            {
                ulong age = peer.High - sequence;
                if (age >= 64 || (peer.Bits & (1UL << (int)age)) != 0) continue;
                peer.Bits |= 1UL << (int)age;
            }
            return plain;
        }
        return null;
    }
    public void Dispose()
    {
        _cipher.Dispose(); foreach (var peer in _peers.Values) peer.Dispose();
        CryptographicOperations.ZeroMemory(_key); _inner.Dispose();
    }
}
