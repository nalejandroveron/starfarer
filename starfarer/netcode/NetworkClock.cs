namespace Starfarer.Netcode;

// Estimates server monotonic time using echoed probes. Never use local wall time.
public sealed class NetworkClock
{
    private double _offset, _lastArrival, _lastServer, _jitter;
    public double Rtt { get; private set; } = 0.1;
    public double Jitter => _jitter;
    public double ServerTime(double local) => Math.Max(0, local + _offset);
    public double Lead => Math.Clamp(Rtt / 2 + 0.05 + 2 * _jitter, 0.05, 0.75);
    public double InterpolationDelay(uint hz) => Math.Clamp(Rtt / 2 + 2.0 / Math.Max(1, hz) + 2 * _jitter, 0.04, 1.5);
    public void Initialize(double server, double sent, double now)
    {
        Rtt = Math.Clamp(now - sent, 0, 2);
        _offset = server + Rtt / 2 - now;
        _lastArrival = _lastServer = _jitter = 0;
    }
    public bool Probe(double server, double sent, double now)
    {
        double rtt = now - sent;
        if (!double.IsFinite(server) || server < 0 || rtt < 0 || rtt > 2) return false;
        Rtt += 0.2 * (rtt - Rtt);
        double desired = server + rtt / 2 - now;
        // Bounded slew prevents reordering local predicted time on noisy probes.
        _offset += Math.Clamp(desired - _offset, -0.01, 0.01);
        return true;
    }
    public void Observe(double server, double arrival)
    {
        if (_lastArrival > 0 && server > _lastServer)
            _jitter += 0.1 * (Math.Abs((arrival - _lastArrival) - (server - _lastServer)) - _jitter);
        _lastArrival = arrival; _lastServer = server;
    }
}
