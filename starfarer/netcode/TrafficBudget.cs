namespace Starfarer.Netcode;

public sealed class TrafficBudget
{
    private readonly double _rate, _burst;
    private double _tokens, _at;
    public TrafficBudget(double bytesPerSecond, double burst)
    {
        if (!double.IsFinite(bytesPerSecond) || !double.IsFinite(burst) || bytesPerSecond <= 0 || burst <= 0)
            throw new ArgumentOutOfRangeException(nameof(bytesPerSecond));
        _rate = bytesPerSecond; _tokens = _burst = burst;
    }
    public bool Spend(int bytes, double now)
    {
        if (bytes < 0 || !double.IsFinite(now)) return false;
        if (now > _at) { _tokens = Math.Min(_burst, _tokens + (now - _at) * _rate); _at = now; }
        if (bytes > _tokens) return false;
        _tokens -= bytes; return true;
    }
}
